//go:build !no_integration

package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	"github.com/david/jenkins-mcp/internal/testutil/jenkinscontainer"
)

func TestIntegrationJenkinsMCP(t *testing.T) {
	jenkins := jenkinscontainer.Start(t)

	t.Run("list jobs", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		result, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{
			Name:      "jenkins_list_jobs",
			Arguments: map[string]any{"controller": jenkinscontainer.ControllerID},
		})
		r.NoError(err, "CallTool()")
		r.False(result.IsError, "CallTool() IsError")
		r.NotNil(result.StructuredContent, "CallTool() structured content")

		var got struct {
			Items []struct {
				Name     string `json:"name"`
				FullName string `json:"fullName"`
			} `json:"items"`
		}
		payload, err := json.Marshal(result.StructuredContent)
		r.NoError(err, "marshal structured content")
		r.NoError(json.Unmarshal(payload, &got), "unmarshal structured content")
		names := map[string]bool{}
		for _, item := range got.Items {
			names[item.Name] = true
		}
		r.Contains(names, "example-freestyle", "freestyle job")
		r.Contains(names, "example-pipeline", "pipeline job")
		r.Contains(names, "example-junit", "JUnit job")
		r.Contains(names, "example-warnings", "Warnings NG job")
		r.Contains(names, "example-artifacts", "artifact job")
	})

	t.Run("artifact tools", func(t *testing.T) {
		r := require.New(t)
		_, readAPI := jenkins.Controller(t, "read-only")
		buildNumber := waitForIntegrationBuild(t, readAPI, "example-artifacts", 1)

		downloadDir := t.TempDir()
		clientSession, cleanup := connectIntegrationMCPWithConfig(t, jenkins, "read-only", func(cfg *config.Config) {
			r.False(cfg.Mutations.Enabled, "artifact downloads should not require Jenkins mutations to be enabled")
			cfg.Artifacts.DownloadDir = downloadDir
		})
		defer cleanup()

		list := callIntegrationTool[struct {
			Artifacts []struct {
				DisplayPath  string `json:"displayPath"`
				FileName     string `json:"fileName"`
				RelativePath string `json:"relativePath"`
			} `json:"artifacts"`
		}](t, clientSession, "jenkins_list_artifacts", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-artifacts",
			"build":      buildNumber,
		})
		artifactPaths := map[string]bool{}
		for _, artifact := range list.Artifacts {
			artifactPaths[artifact.RelativePath] = true
		}
		r.Contains(artifactPaths, "artifacts/report.txt", "text artifact")
		r.Contains(artifactPaths, "artifacts/nested/details.txt", "nested text artifact")
		r.Contains(artifactPaths, "artifacts/blob.bin", "binary artifact")

		text := callIntegrationTool[struct {
			Artifact struct {
				RelativePath string `json:"relativePath"`
				Text         string `json:"text"`
				Bytes        int    `json:"bytes"`
				Inline       bool   `json:"inline"`
				Truncated    bool   `json:"truncated"`
			} `json:"artifact"`
		}](t, clientSession, "jenkins_read_artifact", map[string]any{
			"controller":   jenkinscontainer.ControllerID,
			"job":          "example-artifacts",
			"build":        buildNumber,
			"relativePath": "artifacts/report.txt",
			"maxBytes":     10,
		})
		r.Equal("artifacts/report.txt", text.Artifact.RelativePath, "relative path")
		r.True(text.Artifact.Inline, "text artifact should be returned inline")
		r.True(text.Artifact.Truncated, "text artifact should respect maxBytes")
		r.Equal("hello from", text.Artifact.Text, "truncated text")
		r.Equal(10, text.Artifact.Bytes, "returned bytes")

		binary := callIntegrationTool[struct {
			Artifact struct {
				RelativePath string `json:"relativePath"`
				Bytes        int    `json:"bytes"`
				Inline       bool   `json:"inline"`
				Truncated    bool   `json:"truncated"`
			} `json:"artifact"`
		}](t, clientSession, "jenkins_read_artifact", map[string]any{
			"controller":   jenkinscontainer.ControllerID,
			"job":          "example-artifacts",
			"build":        buildNumber,
			"relativePath": "artifacts/blob.bin",
		})
		r.Equal("artifacts/blob.bin", binary.Artifact.RelativePath, "relative path")
		r.False(binary.Artifact.Inline, "binary artifact should not be returned inline")
		r.False(binary.Artifact.Truncated, "binary artifact should fit within default limit")
		r.Greater(binary.Artifact.Bytes, 0, "binary bytes")

		download := callIntegrationTool[struct {
			Download struct {
				Path  string `json:"path"`
				Bytes int    `json:"bytes"`
			} `json:"download"`
		}](t, clientSession, "jenkins_download_artifact", map[string]any{
			"controller":   jenkinscontainer.ControllerID,
			"job":          "example-artifacts",
			"build":        buildNumber,
			"relativePath": "artifacts/nested/details.txt",
		})
		wantPath := filepath.Join(downloadDir, "example-artifacts", "artifacts", "nested", "details.txt")
		r.Equal(wantPath, download.Download.Path, "download path")
		got, err := os.ReadFile(download.Download.Path)
		r.NoError(err, "read downloaded artifact")
		r.Equal("nested artifact fixture\n", string(got), "downloaded content")
		r.Equal(len(got), download.Download.Bytes, "downloaded byte count")

		missing, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{
			Name: "jenkins_read_artifact",
			Arguments: map[string]any{
				"controller":   jenkinscontainer.ControllerID,
				"job":          "example-artifacts",
				"build":        buildNumber,
				"relativePath": "artifacts/missing.txt",
			},
		})
		r.NoError(err, "CallTool() missing artifact")
		r.True(missing.IsError, "missing artifact should be returned as a structured tool error")
	})
}

func connectIntegrationMCP(t *testing.T, jenkins jenkinscontainer.Fixture, user string) (*mcp.ClientSession, func()) {
	t.Helper()

	return connectIntegrationMCPWithConfig(t, jenkins, user, nil)
}

func connectIntegrationMCPWithConfig(t *testing.T, jenkins jenkinscontainer.Fixture, user string, mutate func(*config.Config)) (*mcp.ClientSession, func()) {
	t.Helper()

	r := require.New(t)
	cfg, api := jenkins.Controller(t, user)
	if mutate != nil {
		mutate(&cfg)
	}
	server := New(Dependencies{
		Config:  cfg,
		Jenkins: map[string]*jenkinsapi.API{jenkinscontainer.ControllerID: api},
		Audit:   &audit.Logger{},
		Version: "test",
	}).Raw()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err, "server connect")
	clientSession, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
	}
	r.NoError(err, "client connect")

	cleanup := func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	}
	return clientSession, cleanup
}

func callIntegrationTool[T any](t *testing.T, clientSession *mcp.ClientSession, name string, args map[string]any) T {
	t.Helper()

	r := require.New(t)
	result, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	r.NoError(err, "CallTool()")
	r.False(result.IsError, "CallTool() IsError")
	r.NotNil(result.StructuredContent, "CallTool() structured content")

	var got T
	payload, err := json.Marshal(result.StructuredContent)
	r.NoError(err, "marshal structured content")
	r.NoError(json.Unmarshal(payload, &got), "unmarshal structured content")
	return got
}

func waitForIntegrationBuild(t *testing.T, api *jenkinsapi.API, jobName string, buildNumber int) int {
	t.Helper()

	r := require.New(t)
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		build, err := api.GetBuild(t.Context(), jobName, buildNumber)
		if err == nil && !build.Building && build.Result != "" {
			r.Equal("SUCCESS", build.Result, "fixture build result")
			return buildNumber
		}
		if err != nil {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	r.NoError(lastErr, "poll fixture build")
	r.Failf("wait for fixture build", "timed out waiting for %s #%d", jobName, buildNumber)
	return 0
}
