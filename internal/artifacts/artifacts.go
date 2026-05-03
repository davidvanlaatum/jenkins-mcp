package artifacts

import (
	"context"
	"os"
	"path/filepath"

	"github.com/david/jenkins-mcp/internal/security"
)

type Fetcher interface {
	DownloadArtifact(ctx context.Context, job string, number int, relativePath string) ([]byte, error)
}
type DownloadResult struct {
	Path  string `json:"path" jsonschema:"Local filesystem path where the artifact was written"`
	Bytes int    `json:"bytes" jsonschema:"Number of artifact bytes written"`
}

func Download(ctx context.Context, root string, fetcher Fetcher, job string, number int, relativePath string) (DownloadResult, error) {
	cleanArtifactPath, err := security.CleanRelativePath(relativePath)
	if err != nil {
		return DownloadResult{}, err
	}
	data, err := fetcher.DownloadArtifact(ctx, job, number, cleanArtifactPath)
	if err != nil {
		return DownloadResult{}, err
	}
	dest, err := security.SafeJoin(root, filepath.Join(job, cleanArtifactPath))
	if err != nil {
		return DownloadResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return DownloadResult{}, err
	}
	if err := os.WriteFile(dest, data, 0600); err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{Path: dest, Bytes: len(data)}, nil
}
