package artifacts

import (
	"context"
	"path/filepath"
	"testing"
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
	fetcher := &fakeFetcher{}
	result, err := Download(context.Background(), t.TempDir(), fetcher, "folder/job", 1, "linux/report.xml")
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if fetcher.path != "linux/report.xml" {
		t.Fatalf("fetched path = %q", fetcher.path)
	}
	if filepath.Base(filepath.Dir(result.Path)) != "linux" {
		t.Fatalf("download path did not preserve artifact directory: %q", result.Path)
	}
}

func TestDownloadRejectsUnsafeArtifactPathBeforeFetch(t *testing.T) {
	fetcher := &fakeFetcher{}
	if _, err := Download(context.Background(), t.TempDir(), fetcher, "job", 1, "../consoleText"); err == nil {
		t.Fatal("Download() accepted unsafe artifact path")
	}
	if fetcher.called {
		t.Fatal("fetcher was called for unsafe artifact path")
	}
}
