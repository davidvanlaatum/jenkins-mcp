package artifacts

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/david/jenkins-mcp/internal/security"
)

type Fetcher interface {
	DownloadArtifact(ctx context.Context, job string, number int, relativePath string) ([]byte, error)
}
type DownloadResult struct {
	Path       string `json:"path" jsonschema:"Server-local filesystem path where the artifact was written"`
	ClientPath string `json:"clientPath,omitempty" jsonschema:"MCP client-local filesystem path corresponding to path when artifacts.clientDownloadDir is configured"`
	Bytes      int    `json:"bytes" jsonschema:"Number of artifact bytes written"`
}

func Download(ctx context.Context, root, clientRoot string, fetcher Fetcher, job string, number int, relativePath string) (DownloadResult, error) {
	cleanArtifactPath, err := security.CleanRelativePath(relativePath)
	if err != nil {
		return DownloadResult{}, err
	}
	data, err := fetcher.DownloadArtifact(ctx, job, number, cleanArtifactPath)
	if err != nil {
		return DownloadResult{}, err
	}
	relativeDest := path.Join(strings.ReplaceAll(job, `\`, "/"), filepath.ToSlash(cleanArtifactPath))
	dest, err := security.SafeJoin(root, filepath.FromSlash(relativeDest))
	if err != nil {
		return DownloadResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return DownloadResult{}, err
	}
	if err := os.WriteFile(dest, data, 0600); err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{Path: dest, ClientPath: joinClientPath(clientRoot, relativeDest), Bytes: len(data)}, nil
}

func joinClientPath(root, relative string) string {
	if root == "" {
		return ""
	}
	if isWindowsPath(root) {
		windowsRoot := strings.ReplaceAll(root, "/", `\`)
		windowsRelative := strings.ReplaceAll(relative, "/", `\`)
		return strings.TrimRight(windowsRoot, `\`) + `\` + strings.TrimLeft(windowsRelative, `\`)
	}
	return path.Join(root, relative)
}

func isWindowsPath(value string) bool {
	hasDrive := len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
	return hasDrive || strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, "//")
}
