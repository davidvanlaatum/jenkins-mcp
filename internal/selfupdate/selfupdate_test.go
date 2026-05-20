package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateInstallsPOSIXBinary(t *testing.T) {
	r := require.New(t)

	archiveBytes := tarGzArchive(t, "jenkins-mcp-server", "new binary")
	sum := sha256.Sum256(archiveBytes)
	checksums := fmt.Sprintf("%s  jenkins-mcp-server_Darwin_arm64.tar.gz\n", hex.EncodeToString(sum[:]))
	server := releaseServer(t, "v1.2.4", map[string][]byte{
		"/darwin.tar.gz": archiveBytes,
		"/checksums.txt": []byte(checksums),
	})

	target := filepath.Join(t.TempDir(), "jenkins-mcp-server")
	err := os.WriteFile(target, []byte("old binary"), 0o755)
	r.NoError(err, "WriteFile()")
	resolvedTarget, err := filepath.EvalSymlinks(target)
	r.NoError(err, "EvalSymlinks()")

	result, err := Update(t.Context(), Options{
		Repository:     "example/project",
		CurrentVersion: "1.2.3",
		ExecutablePath: target,
		GOOS:           "darwin",
		GOARCH:         "arm64",
		APIURL:         server.URL + "/latest",
	})
	r.NoError(err, "Update()")
	r.Equal(resolvedTarget, result.InstalledPath, "InstalledPath")
	r.True(result.RestartRequired, "RestartRequired")
	got, err := os.ReadFile(target)
	r.NoError(err, "ReadFile()")
	r.Equal("new binary", string(got), "installed binary")
	info, err := os.Stat(target)
	r.NoError(err, "Stat()")
	r.Equal(os.FileMode(0o755), info.Mode().Perm(), "mode")
}

func TestUpdateInstallsThroughSymlinkTarget(t *testing.T) {
	r := require.New(t)

	archiveBytes := tarGzArchive(t, "jenkins-mcp-server", "new binary")
	sum := sha256.Sum256(archiveBytes)
	checksums := fmt.Sprintf("%s  jenkins-mcp-server_Darwin_arm64.tar.gz\n", hex.EncodeToString(sum[:]))
	server := releaseServer(t, "v1.2.4", map[string][]byte{
		"/darwin.tar.gz": archiveBytes,
		"/checksums.txt": []byte(checksums),
	})

	dir := t.TempDir()
	target := filepath.Join(dir, "real", "jenkins-mcp-server")
	err := os.MkdirAll(filepath.Dir(target), 0o700)
	r.NoError(err, "MkdirAll()")
	err = os.WriteFile(target, []byte("old binary"), 0o700)
	r.NoError(err, "WriteFile()")
	link := filepath.Join(dir, "jenkins-mcp-server")
	err = os.Symlink(target, link)
	r.NoError(err, "Symlink()")
	resolvedTarget, err := filepath.EvalSymlinks(link)
	r.NoError(err, "EvalSymlinks()")

	result, err := Update(t.Context(), Options{
		Repository:     "example/project",
		CurrentVersion: "1.2.3",
		ExecutablePath: link,
		GOOS:           "darwin",
		GOARCH:         "arm64",
		APIURL:         server.URL + "/latest",
	})
	r.NoError(err, "Update()")
	r.Equal(resolvedTarget, result.InstalledPath, "InstalledPath should resolve symlink target")
	got, err := os.ReadFile(target)
	r.NoError(err, "ReadFile(target)")
	r.Equal("new binary", string(got), "target binary")
	linkTarget, err := os.Readlink(link)
	r.NoError(err, "Readlink()")
	r.Equal(target, linkTarget, "symlink should remain intact")
	info, err := os.Stat(target)
	r.NoError(err, "Stat()")
	r.Equal(os.FileMode(0o700), info.Mode().Perm(), "mode should not be broadened")
}

func TestUpdateStagesWindowsBinary(t *testing.T) {
	r := require.New(t)

	archiveBytes := zipArchive(t, "jenkins-mcp-server.exe", "new windows binary")
	sum := sha256.Sum256(archiveBytes)
	checksums := fmt.Sprintf("%s  jenkins-mcp-server_Windows_x86_64.zip\n", hex.EncodeToString(sum[:]))
	server := releaseServer(t, "v1.2.4", map[string][]byte{
		"/windows.zip":   archiveBytes,
		"/checksums.txt": []byte(checksums),
	})

	target := filepath.Join(t.TempDir(), "jenkins-mcp-server.exe")
	err := os.WriteFile(target, []byte("old binary"), 0o755)
	r.NoError(err, "WriteFile()")
	resolvedTarget, err := filepath.EvalSymlinks(target)
	r.NoError(err, "EvalSymlinks()")

	result, err := Update(t.Context(), Options{
		Repository:     "example/project",
		CurrentVersion: "1.2.3",
		ExecutablePath: target,
		GOOS:           "windows",
		GOARCH:         "x86_64",
		APIURL:         server.URL + "/latest",
	})
	r.NoError(err, "Update()")
	r.Equal(resolvedTarget+".update", result.StagedPath, "StagedPath")
	r.Equal(resolvedTarget+".update.json", result.ManifestPath, "ManifestPath")
	got, err := os.ReadFile(result.StagedPath)
	r.NoError(err, "ReadFile(staged)")
	r.Equal("new windows binary", string(got), "staged binary")
	manifestBytes, err := os.ReadFile(result.ManifestPath)
	r.NoError(err, "ReadFile(manifest)")
	var gotManifest manifest
	err = json.Unmarshal(manifestBytes, &gotManifest)
	r.NoError(err, "Unmarshal(manifest)")
	r.Equal(sha256Hex(got), gotManifest.Checksum, "manifest checksum should describe staged binary")
	original, err := os.ReadFile(target)
	r.NoError(err, "ReadFile(target)")
	r.Equal("old binary", string(original), "target should remain unchanged")
}

func TestStageWindowsUpdateCleansStagedBinaryWhenManifestRenameFails(t *testing.T) {
	r := require.New(t)

	dir := t.TempDir()
	target := filepath.Join(dir, "jenkins-mcp-server.exe")
	err := os.WriteFile(target, []byte("old binary"), 0o755)
	r.NoError(err, "WriteFile()")
	err = os.Mkdir(target+".update.json", 0o700)
	r.NoError(err, "Mkdir(manifest collision)")

	_, _, err = stageWindowsUpdate(target, "v1.2.4", sha256Hex([]byte("new binary")), []byte("new binary"))
	r.Error(err, "stageWindowsUpdate() should fail when manifest rename target is a directory")
	_, err = os.Stat(target + ".update")
	r.ErrorIs(err, os.ErrNotExist, "staged binary should be cleaned up")
}

func TestUpdateRejectsChecksumMismatch(t *testing.T) {
	r := require.New(t)

	archiveBytes := tarGzArchive(t, "jenkins-mcp-server", "new binary")
	server := releaseServer(t, "v1.2.4", map[string][]byte{
		"/linux.tar.gz":  archiveBytes,
		"/checksums.txt": []byte("0000000000000000000000000000000000000000000000000000000000000000  jenkins-mcp-server_Linux_x86_64.tar.gz\n"),
	})

	target := filepath.Join(t.TempDir(), "jenkins-mcp-server")
	err := os.WriteFile(target, []byte("old binary"), 0o755)
	r.NoError(err, "WriteFile()")

	_, err = Update(t.Context(), Options{
		Repository:     "example/project",
		CurrentVersion: "1.2.3",
		ExecutablePath: target,
		GOOS:           "linux",
		GOARCH:         "x86_64",
		APIURL:         server.URL + "/latest",
	})
	r.Error(err, "Update() should reject checksum mismatch")
	got, readErr := os.ReadFile(target)
	r.NoError(readErr, "ReadFile()")
	r.Equal("old binary", string(got), "target should not change")
}

func TestDownloadRejectsContentLengthOverLimit(t *testing.T) {
	r := require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()

	_, err := download(t.Context(), server.Client(), server.URL, "1.2.3", 4)
	r.Error(err, "download() should reject oversized content length")
}

func TestDownloadRejectsBodyOverLimit(t *testing.T) {
	r := require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()

	_, err := download(t.Context(), server.Client(), server.URL, "1.2.3", 4)
	r.Error(err, "download() should reject oversized body")
}

func TestUpdateRejectsZipThatExpandsPastLimit(t *testing.T) {
	r := require.New(t)

	archiveBytes := zipArchive(t, "jenkins-mcp-server.exe", strings.Repeat("a", 2048))
	r.Less(len(archiveBytes), 1024, "test archive should be compressed below the configured limit")
	sum := sha256.Sum256(archiveBytes)
	checksums := fmt.Sprintf("%s  jenkins-mcp-server_Windows_x86_64.zip\n", hex.EncodeToString(sum[:]))
	server := releaseServer(t, "v1.2.4", map[string][]byte{
		"/windows.zip":   archiveBytes,
		"/checksums.txt": []byte(checksums),
	})

	target := filepath.Join(t.TempDir(), "jenkins-mcp-server.exe")
	err := os.WriteFile(target, []byte("old binary"), 0o755)
	r.NoError(err, "WriteFile()")

	_, err = Update(t.Context(), Options{
		Repository:       "example/project",
		CurrentVersion:   "1.2.3",
		ExecutablePath:   target,
		GOOS:             "windows",
		GOARCH:           "x86_64",
		APIURL:           server.URL + "/latest",
		MaxDownloadBytes: 1024,
	})
	r.Error(err, "Update() should reject zip payload that expands past the limit")
}

func TestUpdateRejectsTarGzThatExpandsPastLimit(t *testing.T) {
	r := require.New(t)

	archiveBytes := tarGzArchive(t, "jenkins-mcp-server", strings.Repeat("a", 2048))
	r.Less(len(archiveBytes), 1024, "test archive should be compressed below the configured limit")
	sum := sha256.Sum256(archiveBytes)
	checksums := fmt.Sprintf("%s  jenkins-mcp-server_Linux_x86_64.tar.gz\n", hex.EncodeToString(sum[:]))
	server := releaseServer(t, "v1.2.4", map[string][]byte{
		"/linux.tar.gz":  archiveBytes,
		"/checksums.txt": []byte(checksums),
	})

	target := filepath.Join(t.TempDir(), "jenkins-mcp-server")
	err := os.WriteFile(target, []byte("old binary"), 0o755)
	r.NoError(err, "WriteFile()")

	_, err = Update(t.Context(), Options{
		Repository:       "example/project",
		CurrentVersion:   "1.2.3",
		ExecutablePath:   target,
		GOOS:             "linux",
		GOARCH:           "x86_64",
		APIURL:           server.URL + "/latest",
		MaxDownloadBytes: 1024,
	})
	r.Error(err, "Update() should reject tar.gz payload that expands past the limit")
}

func TestExtractBinaryAllowsGoReleaserMetadata(t *testing.T) {
	r := require.New(t)

	files := []archiveFile{
		{Name: "README", Content: "readme"},
		{Name: "README.md", Content: "readme"},
		{Name: "LICENSE", Content: "license"},
		{Name: "LICENSE.txt", Content: "license"},
		{Name: "CHANGELOG", Content: "changes"},
		{Name: "CHANGELOG.md", Content: "changes"},
		{Name: "jenkins-mcp-server", Content: "binary"},
	}
	archiveBytes := tarGzArchiveFiles(t, files)
	got, err := extractBinary("jenkins-mcp-server_Linux_x86_64.tar.gz", archiveBytes, "linux", 1024)
	r.NoError(err, "extractBinary() should ignore GoReleaser metadata")
	r.Equal("binary", string(got), "binary content")

	zipBytes := zipArchiveFiles(t, []archiveFile{
		{Name: "README.md", Content: "readme"},
		{Name: "LICENSE", Content: "license"},
		{Name: "jenkins-mcp-server.exe", Content: "windows binary"},
	})
	got, err = extractBinary("jenkins-mcp-server_Windows_x86_64.zip", zipBytes, "windows", 1024)
	r.NoError(err, "extractBinary() should ignore GoReleaser metadata in zip archives")
	r.Equal("windows binary", string(got), "windows binary content")
}

func TestExtractBinaryRejectsMetadataLookalikes(t *testing.T) {
	r := require.New(t)

	archiveBytes := tarGzArchiveFiles(t, []archiveFile{
		{Name: "README.exe", Content: "not metadata"},
		{Name: "jenkins-mcp-server", Content: "binary"},
	})
	_, err := extractBinary("jenkins-mcp-server_Linux_x86_64.tar.gz", archiveBytes, "linux", 1024)
	r.Error(err, "extractBinary() should reject README lookalikes")

	zipBytes := zipArchiveFiles(t, []archiveFile{
		{Name: "LICENSE-backup", Content: "not metadata"},
		{Name: "jenkins-mcp-server.exe", Content: "binary"},
	})
	_, err = extractBinary("jenkins-mcp-server_Windows_x86_64.zip", zipBytes, "windows", 1024)
	r.Error(err, "extractBinary() should reject LICENSE lookalikes")
}

func TestExtractBinaryRejectsMetadataThatExceedsExpandedLimit(t *testing.T) {
	r := require.New(t)

	archiveBytes := tarGzArchiveFiles(t, []archiveFile{
		{Name: "README.md", Content: strings.Repeat("a", 2048)},
		{Name: "jenkins-mcp-server", Content: "binary"},
	})
	r.Less(len(archiveBytes), 1024, "test archive should be compressed below the configured limit")
	_, err := extractBinary("jenkins-mcp-server_Linux_x86_64.tar.gz", archiveBytes, "linux", 1024)
	r.Error(err, "extractBinary() should reject tar.gz metadata that expands past the limit")

	zipBytes := zipArchiveFiles(t, []archiveFile{
		{Name: "README.md", Content: strings.Repeat("a", 2048)},
		{Name: "jenkins-mcp-server.exe", Content: "binary"},
	})
	r.Less(len(zipBytes), 1024, "test archive should be compressed below the configured limit")
	_, err = extractBinary("jenkins-mcp-server_Windows_x86_64.zip", zipBytes, "windows", 1024)
	r.Error(err, "extractBinary() should reject zip metadata that expands past the limit")
}

func TestExtractBinaryRejectsUnexpectedArchiveFile(t *testing.T) {
	r := require.New(t)

	archiveBytes := tarGzArchive(t, "notes.txt", "unexpected")
	_, err := extractBinary("jenkins-mcp-server_Linux_x86_64.tar.gz", archiveBytes, "linux", 1024)
	r.Error(err, "extractBinary() should reject unexpected archive files")
}

func TestExtractBinaryRejectsPlatformAbsoluteArchivePaths(t *testing.T) {
	r := require.New(t)

	for _, name := range []string{
		"/jenkins-mcp-server",
		`\jenkins-mcp-server`,
		`C:\jenkins-mcp-server`,
		`\\server\share\jenkins-mcp-server`,
	} {
		archiveBytes := tarGzArchive(t, name, "binary")
		_, err := extractBinary("jenkins-mcp-server_Linux_x86_64.tar.gz", archiveBytes, "linux", 1024)
		r.Error(err, "extractBinary() should reject %q", name)
	}
}

func releaseServer(t *testing.T, tag string, assets map[string][]byte) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/latest":
			_, _ = fmt.Fprintf(w, `{
				"tag_name": %q,
				"html_url": "https://github.com/example/project/releases/tag/%s",
				"assets": [
					{"name": "jenkins-mcp-server_Darwin_arm64.tar.gz", "browser_download_url": "%s/darwin.tar.gz"},
					{"name": "jenkins-mcp-server_Linux_x86_64.tar.gz", "browser_download_url": "%s/linux.tar.gz"},
					{"name": "jenkins-mcp-server_Windows_x86_64.zip", "browser_download_url": "%s/windows.zip"},
					{"name": "checksums.txt", "browser_download_url": "%s/checksums.txt"}
				]
			}`, tag, tag, server.URL, server.URL, server.URL, server.URL)
		default:
			data, ok := assets[req.URL.Path]
			if !ok {
				http.NotFound(w, req)
				return
			}
			_, _ = w.Write(data)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func tarGzArchive(t *testing.T, name, content string) []byte {
	t.Helper()
	return tarGzArchiveFiles(t, []archiveFile{{Name: name, Content: content}})
}

type archiveFile struct {
	Name    string
	Content string
}

func tarGzArchiveFiles(t *testing.T, files []archiveFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, file := range files {
		err := tw.WriteHeader(&tar.Header{Name: file.Name, Mode: 0o755, Size: int64(len(file.Content))})
		require.NoError(t, err, "WriteHeader()")
		_, err = tw.Write([]byte(file.Content))
		require.NoError(t, err, "Write()")
	}
	require.NoError(t, tw.Close(), "tar Close()")
	require.NoError(t, gz.Close(), "gzip Close()")
	return buf.Bytes()
}

func zipArchive(t *testing.T, name, content string) []byte {
	t.Helper()
	return zipArchiveFiles(t, []archiveFile{{Name: name, Content: content}})
}

func zipArchiveFiles(t *testing.T, files []archiveFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, file := range files {
		w, err := zw.Create(file.Name)
		require.NoError(t, err, "Create()")
		_, err = w.Write([]byte(file.Content))
		require.NoError(t, err, "Write()")
	}
	require.NoError(t, zw.Close(), "Close()")
	return buf.Bytes()
}
