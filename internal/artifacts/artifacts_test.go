package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeFetcher struct {
	path   string
	called bool
}

func (f *fakeFetcher) DownloadArtifact(_ context.Context, _ string, _ int, relativePath string) ([]byte, error) {
	f.called = true
	f.path = relativePath
	return []byte("artifact"), nil
}

func TestDownloadPreservesArtifactDirectoryStructure(t *testing.T) {
	r := require.New(t)
	fetcher := &fakeFetcher{}

	result, err := Download(t.Context(), t.TempDir(), "", fetcher, "folder/job", 1, "linux/report.xml")

	r.NoError(err, "Download()")
	r.Equal("linux/report.xml", fetcher.path, "fetched path")
	r.Equal("linux", filepath.Base(filepath.Dir(result.Path)), "download path should preserve artifact directory")
}

func TestDownloadReportsWindowsClientPath(t *testing.T) {
	r := require.New(t)
	fetcher := &fakeFetcher{}

	result, err := Download(t.Context(), t.TempDir(), `C:\Users\developer\jenkins-artifacts`, fetcher, "folder/job", 1, "linux/report.xml")

	r.NoError(err, "Download()")
	r.Equal(`C:\Users\developer\jenkins-artifacts\folder\job\linux\report.xml`, result.ClientPath, "client path")
	_, err = os.Stat(result.Path)
	r.NoError(err, "downloaded artifact should exist at the server path")
}

func TestDownloadReportsUnixClientPath(t *testing.T) {
	r := require.New(t)
	fetcher := &fakeFetcher{}

	result, err := Download(t.Context(), t.TempDir(), "/Users/developer/jenkins-artifacts", fetcher, "folder/job", 1, "linux/report.xml")

	r.NoError(err, "Download()")
	r.Equal("/Users/developer/jenkins-artifacts/folder/job/linux/report.xml", result.ClientPath, "client path")
}

func TestDownloadReportsWindowsUNCClientPath(t *testing.T) {
	r := require.New(t)
	fetcher := &fakeFetcher{}

	result, err := Download(t.Context(), t.TempDir(), `\\fileserver\jenkins-artifacts`, fetcher, "folder/job", 1, "linux/report.xml")

	r.NoError(err, "Download()")
	r.Equal(`\\fileserver\jenkins-artifacts\folder\job\linux\report.xml`, result.ClientPath, "client path")
}

func TestDownloadRejectsUnsafeArtifactPathBeforeFetch(t *testing.T) {
	r := require.New(t)
	fetcher := &fakeFetcher{}

	_, err := Download(t.Context(), t.TempDir(), "", fetcher, "job", 1, "../consoleText")

	r.Error(err, "Download() should reject unsafe artifact path")
	r.False(fetcher.called, "fetcher should not be called for unsafe artifact path")
}
