package artifacts

import (
	"context"
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

	result, err := Download(t.Context(), t.TempDir(), fetcher, "folder/job", 1, "linux/report.xml")

	r.NoError(err, "Download()")
	r.Equal("linux/report.xml", fetcher.path, "fetched path")
	r.Equal("linux", filepath.Base(filepath.Dir(result.Path)), "download path should preserve artifact directory")
}

func TestDownloadRejectsUnsafeArtifactPathBeforeFetch(t *testing.T) {
	r := require.New(t)
	fetcher := &fakeFetcher{}

	_, err := Download(t.Context(), t.TempDir(), fetcher, "job", 1, "../consoleText")

	r.Error(err, "Download() should reject unsafe artifact path")
	r.False(fetcher.called, "fetcher should not be called for unsafe artifact path")
}
