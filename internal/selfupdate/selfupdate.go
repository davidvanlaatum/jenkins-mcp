package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultTimeout          = 30 * time.Second
	binaryName              = "jenkins-mcp-server"
	defaultMaxDownloadBytes = 256 * 1024 * 1024
)

type releaseResponse struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Options struct {
	Repository       string
	CurrentVersion   string
	ExecutablePath   string
	GOOS             string
	GOARCH           string
	Force            bool
	MaxDownloadBytes int64
	APIURL           string
	HTTPClient       *http.Client
}

type Result struct {
	CurrentVersion  string `json:"currentVersion" jsonschema:"Current server version before the update"`
	LatestVersion   string `json:"latestVersion" jsonschema:"Latest GitHub release version selected for installation"`
	ReleaseURL      string `json:"releaseUrl" jsonschema:"Browser URL for the selected GitHub release"`
	AssetName       string `json:"assetName" jsonschema:"Release asset selected for this operating system and architecture"`
	Checksum        string `json:"checksum" jsonschema:"SHA-256 checksum verified before installation or staging"`
	InstalledPath   string `json:"installedPath,omitempty" jsonschema:"Filesystem path replaced with the verified updated binary"`
	StagedPath      string `json:"stagedPath,omitempty" jsonschema:"Filesystem path where the verified update was staged when direct replacement is not supported"`
	ManifestPath    string `json:"manifestPath,omitempty" jsonschema:"Filesystem path of the staged update manifest, when one was written"`
	Platform        string `json:"platform" jsonschema:"Operating system and architecture used for release asset selection"`
	UpdateAvailable bool   `json:"updateAvailable" jsonschema:"Whether the selected release is newer than the current version or force was requested"`
	RestartRequired bool   `json:"restartRequired" jsonschema:"Whether the IDE or MCP client must restart before the installed or staged update is used"`
	Message         string `json:"message" jsonschema:"Human-readable summary of the update result and any required follow-up"`
}

type manifest struct {
	TargetPath string `json:"targetPath"`
	Version    string `json:"version"`
	Checksum   string `json:"checksum"`
	CreatedAt  string `json:"createdAt"`
}

func Update(ctx context.Context, opts Options) (Result, error) {
	if opts.Repository == "" {
		return Result{}, errors.New("repository is required")
	}
	if opts.CurrentVersion == "" {
		return Result{}, errors.New("current version is required")
	}
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := opts.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	executablePath := opts.ExecutablePath
	if executablePath == "" {
		path, err := os.Executable()
		if err != nil {
			return Result{}, err
		}
		executablePath = path
	}
	executablePath, err := filepath.Abs(executablePath)
	if err != nil {
		return Result{}, err
	}
	executablePath, err = filepath.EvalSymlinks(executablePath)
	if err != nil {
		return Result{}, err
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	maxDownloadBytes := opts.MaxDownloadBytes
	if maxDownloadBytes <= 0 {
		maxDownloadBytes = defaultMaxDownloadBytes
	}

	apiURL := opts.APIURL
	if apiURL == "" {
		apiURL = "https://api.github.com/repos/" + opts.Repository + "/releases/latest"
	}
	release, err := fetchRelease(ctx, client, apiURL, opts.CurrentVersion)
	if err != nil {
		return Result{}, err
	}
	base := Result{
		CurrentVersion: opts.CurrentVersion,
		LatestVersion:  release.TagName,
		ReleaseURL:     release.HTMLURL,
		Platform:       goos + "/" + goarch,
	}
	if !opts.Force && !isNewer(release.TagName, opts.CurrentVersion) {
		base.Message = "server is already at the latest release"
		return base, nil
	}

	asset, err := selectAsset(release.Assets, goos, goarch)
	if err != nil {
		return Result{}, err
	}
	checksumAsset, err := selectChecksumAsset(release.Assets)
	if err != nil {
		return Result{}, err
	}
	archiveBytes, err := download(ctx, client, asset.BrowserDownloadURL, opts.CurrentVersion, maxDownloadBytes)
	if err != nil {
		return Result{}, err
	}
	checksumBytes, err := download(ctx, client, checksumAsset.BrowserDownloadURL, opts.CurrentVersion, maxDownloadBytes)
	if err != nil {
		return Result{}, err
	}
	checksum, err := checksumFor(checksumBytes, asset.Name)
	if err != nil {
		return Result{}, err
	}
	if got := sha256Hex(archiveBytes); got != checksum {
		return Result{}, fmt.Errorf("checksum mismatch for %s: got %s, want %s", asset.Name, got, checksum)
	}
	binaryBytes, err := extractBinary(asset.Name, archiveBytes, goos, maxDownloadBytes)
	if err != nil {
		return Result{}, err
	}
	result := base
	result.AssetName = asset.Name
	result.Checksum = checksum
	result.UpdateAvailable = true
	result.RestartRequired = true

	if goos == "windows" {
		stagedPath, manifestPath, err := stageWindowsUpdate(executablePath, release.TagName, sha256Hex(binaryBytes), binaryBytes)
		if err != nil {
			return Result{}, err
		}
		result.StagedPath = stagedPath
		result.ManifestPath = manifestPath
		result.Message = "update staged; stop the IDE or MCP client and replace the current executable with the staged file"
		return result, nil
	}

	if err := installPOSIX(executablePath, binaryBytes); err != nil {
		return Result{}, err
	}
	result.InstalledPath = executablePath
	result.Message = "update installed; restart the IDE or MCP client to use the new binary"
	return result, nil
}

func fetchRelease(ctx context.Context, client *http.Client, apiURL, version string) (releaseResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return releaseResponse{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "jenkins-mcp-server/"+version)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return releaseResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return releaseResponse{}, fmt.Errorf("github release API returned %s", resp.Status)
	}
	var release releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return releaseResponse{}, err
	}
	if release.TagName == "" {
		return releaseResponse{}, errors.New("latest release is missing tag_name")
	}
	return release, nil
}

func selectAsset(assets []releaseAsset, goos, goarch string) (releaseAsset, error) {
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, "checksum") {
			continue
		}
		if !strings.Contains(name, strings.ToLower(goos)) || !containsArch(name, goarch) {
			continue
		}
		if strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz") || strings.HasSuffix(name, ".zip") {
			return asset, nil
		}
	}
	return releaseAsset{}, fmt.Errorf("no release archive found for %s/%s", goos, goarch)
}

func containsArch(name, goarch string) bool {
	for _, candidate := range archNames(goarch) {
		if strings.Contains(name, candidate) {
			return true
		}
	}
	return false
}

func archNames(goarch string) []string {
	switch strings.ToLower(goarch) {
	case "amd64", "x86_64":
		return []string{"amd64", "x86_64"}
	case "arm64", "aarch64":
		return []string{"arm64", "aarch64"}
	default:
		return []string{strings.ToLower(goarch)}
	}
}

func selectChecksumAsset(assets []releaseAsset) (releaseAsset, error) {
	for _, asset := range assets {
		if strings.Contains(strings.ToLower(asset.Name), "checksum") {
			return asset, nil
		}
	}
	return releaseAsset{}, errors.New("release is missing checksum asset")
}

func download(ctx context.Context, client *http.Client, rawURL, version string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "jenkins-mcp-server/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %s", resp.Status)
	}
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("download exceeds maximum size: content length %d bytes is greater than %d bytes", resp.ContentLength, maxBytes)
	}
	return readLimited(resp.Body, maxBytes, "download")
}

func checksumFor(b []byte, assetName string) (string, error) {
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if filepath.Base(name) == assetName {
			sum := strings.ToLower(fields[0])
			if len(sum) != sha256.Size*2 {
				return "", fmt.Errorf("checksum for %s is not a SHA-256 hex digest", assetName)
			}
			if _, err := hex.DecodeString(sum); err != nil {
				return "", err
			}
			return sum, nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found", assetName)
}

func extractBinary(assetName string, archiveBytes []byte, goos string, maxBytes int64) ([]byte, error) {
	name := strings.ToLower(assetName)
	switch {
	case strings.HasSuffix(name, ".zip"):
		return extractZipBinary(archiveBytes, expectedBinaryName(goos), maxBytes)
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return extractTarGzBinary(archiveBytes, expectedBinaryName(goos), maxBytes)
	default:
		return nil, fmt.Errorf("unsupported release archive %s", assetName)
	}
}

func expectedBinaryName(goos string) string {
	if goos == "windows" {
		return binaryName + ".exe"
	}
	return binaryName
}

func extractZipBinary(archiveBytes []byte, expected string, maxBytes int64) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		return nil, err
	}
	var out []byte
	var expandedBytes int64
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if !safeArchivePath(file.Name) {
			return nil, fmt.Errorf("release archive contains unexpected file %q", file.Name)
		}
		if err := reserveExpandedBytes(&expandedBytes, file.UncompressedSize64, maxBytes); err != nil {
			return nil, err
		}
		if archiveBase(file.Name) != expected {
			if isGoReleaserArchiveMetadata(file.Name) {
				continue
			}
			return nil, fmt.Errorf("release archive contains unexpected file %q", file.Name)
		}
		if out != nil {
			return nil, errors.New("release archive contains multiple binary files")
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		out, err = readLimited(rc, int64(file.UncompressedSize64), "extracted binary")
		closeErr := rc.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	if out == nil {
		return nil, fmt.Errorf("release archive does not contain %s", expected)
	}
	return out, nil
}

func extractTarGzBinary(archiveBytes []byte, expected string, maxBytes int64) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var out []byte
	var expandedBytes int64
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.FileInfo().IsDir() {
			continue
		}
		if header.Typeflag != tar.TypeReg || !safeArchivePath(header.Name) {
			return nil, fmt.Errorf("release archive contains unexpected file %q", header.Name)
		}
		if header.Size < 0 {
			return nil, fmt.Errorf("release archive contains invalid file size for %q", header.Name)
		}
		if err := reserveExpandedBytes(&expandedBytes, uint64(header.Size), maxBytes); err != nil {
			return nil, err
		}
		if archiveBase(header.Name) != expected {
			if isGoReleaserArchiveMetadata(header.Name) {
				continue
			}
			return nil, fmt.Errorf("release archive contains unexpected file %q", header.Name)
		}
		if out != nil {
			return nil, errors.New("release archive contains multiple binary files")
		}
		out, err = readLimited(tr, header.Size, "extracted binary")
		if err != nil {
			return nil, err
		}
	}
	if out == nil {
		return nil, fmt.Errorf("release archive does not contain %s", expected)
	}
	return out, nil
}

func reserveExpandedBytes(total *int64, size uint64, maxBytes int64) error {
	if size > uint64(maxBytes-*total) {
		return fmt.Errorf("extracted archive exceeds maximum size: more than %d bytes", maxBytes)
	}
	*total += int64(size)
	return nil
}

func readLimited(r io.Reader, maxBytes int64, label string) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("%s exceeds maximum size: more than %d bytes", label, maxBytes)
	}
	return b, nil
}

func safeArchivePath(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) || hasWindowsDrivePrefix(name) {
		return false
	}
	for _, part := range strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func hasWindowsDrivePrefix(name string) bool {
	return len(name) >= 3 &&
		((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z')) &&
		name[1] == ':' &&
		(name[2] == '/' || name[2] == '\\')
}

func archiveBase(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func isGoReleaserArchiveMetadata(name string) bool {
	switch strings.ToLower(archiveBase(name)) {
	case "readme", "readme.md", "license", "license.txt", "changelog", "changelog.md":
		return true
	default:
		return false
	}
}

func installPOSIX(target string, binaryBytes []byte) error {
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(binaryBytes); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func stageWindowsUpdate(target, version, checksum string, binaryBytes []byte) (string, string, error) {
	stagedPath := target + ".update"
	manifestPath := target + ".update.json"
	dir := filepath.Dir(target)
	stagedTmp, err := os.CreateTemp(dir, filepath.Base(stagedPath)+".tmp-*")
	if err != nil {
		return "", "", err
	}
	stagedTmpPath := stagedTmp.Name()
	cleanupStagedTmp := true
	defer func() {
		if cleanupStagedTmp {
			_ = os.Remove(stagedTmpPath)
		}
	}()
	if _, err := stagedTmp.Write(binaryBytes); err != nil {
		_ = stagedTmp.Close()
		return "", "", err
	}
	if err := stagedTmp.Close(); err != nil {
		return "", "", err
	}

	m := manifest{
		TargetPath: target,
		Version:    version,
		Checksum:   checksum,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", "", err
	}
	manifestTmp, err := os.CreateTemp(dir, filepath.Base(manifestPath)+".tmp-*")
	if err != nil {
		return "", "", err
	}
	manifestTmpPath := manifestTmp.Name()
	cleanupManifestTmp := true
	defer func() {
		if cleanupManifestTmp {
			_ = os.Remove(manifestTmpPath)
		}
	}()
	if _, err := manifestTmp.Write(b); err != nil {
		_ = manifestTmp.Close()
		return "", "", err
	}
	if err := manifestTmp.Close(); err != nil {
		return "", "", err
	}

	if err := os.Rename(stagedTmpPath, stagedPath); err != nil {
		return "", "", err
	}
	cleanupStagedTmp = false
	cleanupStagedPath := true
	defer func() {
		if cleanupStagedPath {
			_ = os.Remove(stagedPath)
		}
	}()
	if err := os.Rename(manifestTmpPath, manifestPath); err != nil {
		return "", "", err
	}
	cleanupManifestTmp = false
	cleanupStagedPath = false
	return stagedPath, manifestPath, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func isNewer(latest, current string) bool {
	latestVersion, ok := normalizeVersion(latest)
	if !ok {
		return latest != "" && latest != current
	}
	currentVersion, ok := normalizeVersion(current)
	if !ok {
		return true
	}
	for i := range latestVersion {
		if latestVersion[i] > currentVersion[i] {
			return true
		}
		if latestVersion[i] < currentVersion[i] {
			return false
		}
	}
	return false
}

func normalizeVersion(v string) ([]int, bool) {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if v == "" || strings.Contains(v, "-") {
		return nil, false
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil, false
	}
	out := make([]int, 3)
	for i, part := range parts {
		if part == "" {
			return nil, false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return nil, false
			}
			out[i] = out[i]*10 + int(r-'0')
		}
	}
	return out, true
}
