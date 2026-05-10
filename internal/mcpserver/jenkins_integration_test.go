//go:build !no_integration

package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	"github.com/david/jenkins-mcp/internal/testutil/jenkinscontainer"
)

func TestIntegrationJenkinsMCP(t *testing.T) {
	jenkins := jenkinscontainer.Start(t)
	_, api := jenkins.Controller(t, "admin")
	freestyleBuild := jenkinscontainer.WaitForSuccessfulBuild(t, api, "example-freestyle")
	pipelineBuild := jenkinscontainer.WaitForSuccessfulBuild(t, api, "example-pipeline")

	t.Run("list jobs", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		got := callIntegrationTool[struct {
			Items []struct {
				Name     string `json:"name"`
				FullName string `json:"fullName"`
			} `json:"items"`
		}](t, clientSession, "jenkins_list_jobs", map[string]any{"controller": jenkinscontainer.ControllerID})
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
		buildNumber := jenkinscontainer.WaitForSuccessfulBuild(t, api, "example-artifacts")

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

	t.Run("list jobs filters and empty result", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		page := callIntegrationTool[struct {
			Items      []struct{ Name string } `json:"items"`
			NextCursor string                  `json:"nextCursor"`
			HasMore    bool                    `json:"hasMore"`
			Limit      int                     `json:"limit"`
		}](t, clientSession, "jenkins_list_jobs", map[string]any{"controller": jenkinscontainer.ControllerID, "limit": 2})
		r.Len(page.Items, 2, "first page job count")
		r.True(page.HasMore, "first page should report more jobs")
		r.NotEmpty(page.NextCursor, "first page cursor")
		r.Equal(2, page.Limit, "first page limit")

		filtered := callIntegrationTool[struct {
			Items []struct {
				Name     string `json:"name"`
				FullName string `json:"fullName"`
				Class    string `json:"class"`
			} `json:"items"`
			HasMore bool `json:"hasMore"`
		}](t, clientSession, "jenkins_list_jobs", map[string]any{
			"controller":   jenkinscontainer.ControllerID,
			"nameContains": "pipeline",
			"type":         "pipeline",
		})
		r.Len(filtered.Items, 1, "filtered pipeline jobs")
		r.Equal("example-pipeline", filtered.Items[0].Name, "filtered pipeline job name")
		r.False(filtered.HasMore, "single filtered result should not have more pages")

		empty := callIntegrationTool[struct {
			Items   []struct{} `json:"items"`
			HasMore bool       `json:"hasMore"`
		}](t, clientSession, "jenkins_list_jobs", map[string]any{
			"controller":   jenkinscontainer.ControllerID,
			"nameContains": "no-such-integration-job",
		})
		r.Len(empty.Items, 0, "empty filtered job result")
		r.False(empty.HasMore, "empty result should not have more pages")
	})

	t.Run("job and build metadata", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		job := callIntegrationTool[struct {
			Job struct {
				Name            string `json:"name"`
				Description     string `json:"description"`
				Buildable       bool   `json:"buildable"`
				NextBuildNumber int    `json:"nextBuildNumber"`
				LastBuild       *struct {
					Number int    `json:"number"`
					Result string `json:"result"`
				} `json:"lastBuild"`
			} `json:"job"`
		}](t, clientSession, "jenkins_get_job", map[string]any{"controller": jenkinscontainer.ControllerID, "job": "example-freestyle"})
		r.Equal("example-freestyle", job.Job.Name, "job name")
		r.Contains(job.Job.Description, "Buildable freestyle job", "job description")
		r.True(job.Job.Buildable, "job should be buildable")
		r.NotNil(job.Job.LastBuild, "last build")
		r.Equal(freestyleBuild, job.Job.LastBuild.Number, "last build number")
		r.Equal("SUCCESS", job.Job.LastBuild.Result, "last build result")
		r.Equal(freestyleBuild+1, job.Job.NextBuildNumber, "next build number")

		builds := callIntegrationTool[struct {
			Items []struct {
				Number   int    `json:"number"`
				Result   string `json:"result"`
				Building bool   `json:"building"`
				URL      string `json:"url"`
			} `json:"items"`
			HasMore bool `json:"hasMore"`
			Limit   int  `json:"limit"`
		}](t, clientSession, "jenkins_list_builds", map[string]any{"controller": jenkinscontainer.ControllerID, "job": "example-freestyle", "limit": 1})
		r.Len(builds.Items, 1, "build summary count")
		r.Equal(1, builds.Limit, "build list limit")
		r.False(builds.HasMore, "single build should not have another page")
		r.Equal(freestyleBuild, builds.Items[0].Number, "build summary number")
		r.Equal("SUCCESS", builds.Items[0].Result, "build summary result")
		r.False(builds.Items[0].Building, "build summary should be complete")
		r.NotEmpty(builds.Items[0].URL, "build summary URL")

		build := callIntegrationTool[struct {
			Build struct {
				Number          int    `json:"number"`
				Result          string `json:"result"`
				Building        bool   `json:"building"`
				FullDisplayName string `json:"fullDisplayName"`
				Causes          []struct {
					ShortDescription string `json:"shortDescription"`
				} `json:"causes"`
			} `json:"build"`
		}](t, clientSession, "jenkins_get_build", map[string]any{"controller": jenkinscontainer.ControllerID, "job": "example-freestyle", "build": freestyleBuild})
		r.Equal(freestyleBuild, build.Build.Number, "build number")
		r.Equal("SUCCESS", build.Build.Result, "build result")
		r.False(build.Build.Building, "build should be complete")
		r.Contains(build.Build.FullDisplayName, "example-freestyle", "build full display name")
		r.NotEmpty(build.Build.Causes, "build causes")
	})

	t.Run("console log tools", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		chunk := callIntegrationTool[struct {
			Log struct {
				Text      string `json:"text"`
				Start     int64  `json:"start"`
				NextStart int64  `json:"nextStart"`
				More      bool   `json:"more"`
				Truncated bool   `json:"truncated"`
			} `json:"log"`
		}](t, clientSession, "jenkins_get_log", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-freestyle",
			"build":      freestyleBuild,
			"maxBytes":   12,
		})
		r.Equal(int64(0), chunk.Log.Start, "log chunk start")
		r.NotEmpty(chunk.Log.Text, "log chunk text")
		r.True(chunk.Log.Truncated, "small log chunk should be truncated")
		r.Greater(chunk.Log.NextStart, int64(0), "log next start")

		search := callIntegrationTool[struct {
			Result struct {
				Query   string `json:"query"`
				Matches []struct {
					Line int    `json:"line"`
					Text string `json:"text"`
				} `json:"matches"`
				ScannedBytes int64 `json:"scannedBytes"`
			} `json:"result"`
		}](t, clientSession, "jenkins_search_log", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-freestyle",
			"build":      freestyleBuild,
			"query":      "hello from freestyle",
			"maxMatches": 1,
		})
		r.Equal("hello from freestyle", search.Result.Query, "search query")
		r.Len(search.Result.Matches, 1, "search matches")
		r.Contains(search.Result.Matches[0].Text, "hello from freestyle", "search match text")
		r.Greater(search.Result.ScannedBytes, int64(0), "search scanned bytes")

		empty := callIntegrationTool[struct {
			Result struct {
				Matches []struct{} `json:"matches"`
			} `json:"result"`
		}](t, clientSession, "jenkins_search_log", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-freestyle",
			"build":      freestyleBuild,
			"query":      "no such log line",
		})
		r.Len(empty.Result.Matches, 0, "empty search matches")

		tail := callIntegrationTool[struct {
			Log struct {
				Text      string `json:"text"`
				Start     int64  `json:"start"`
				NextStart int64  `json:"nextStart"`
				Truncated bool   `json:"truncated"`
			} `json:"log"`
		}](t, clientSession, "jenkins_tail_log", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-freestyle",
			"build":      freestyleBuild,
			"bytes":      64,
		})
		r.NotEmpty(tail.Log.Text, "tail log text")
		r.GreaterOrEqual(tail.Log.NextStart, tail.Log.Start, "tail log offsets")
		r.LessOrEqual(len(tail.Log.Text), 64, "tail log should honor requested byte bound")
	})

	t.Run("pipeline stage and node log tools", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		run := callIntegrationTool[struct {
			Run struct {
				Status string `json:"status"`
				Stages []struct {
					ID     string `json:"id"`
					Name   string `json:"name"`
					Status string `json:"status"`
				} `json:"stages"`
			} `json:"run"`
		}](t, clientSession, "jenkins_get_pipeline_run", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-pipeline",
			"build":      pipelineBuild,
		})
		r.Equal("SUCCESS", run.Run.Status, "pipeline run status")
		r.NotEmpty(run.Run.Stages, "pipeline stages")

		stageID := ""
		for _, stage := range run.Run.Stages {
			if stage.Name == "build" {
				stageID = stage.ID
				r.Equal("SUCCESS", stage.Status, "pipeline build stage status")
				break
			}
		}
		r.NotEmpty(stageID, "pipeline build stage id")

		stage := callIntegrationTool[struct {
			Stage struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Status string `json:"status"`
				Nodes  []struct {
					ID     string `json:"id"`
					Name   string `json:"name"`
					HasLog bool   `json:"hasLog"`
				} `json:"nodes"`
			} `json:"stage"`
		}](t, clientSession, "jenkins_get_pipeline_stage", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-pipeline",
			"build":      pipelineBuild,
			"stageId":    stageID,
		})
		r.Equal(stageID, stage.Stage.ID, "pipeline stage id")
		r.Equal("build", stage.Stage.Name, "pipeline stage name")
		r.Equal("SUCCESS", stage.Stage.Status, "pipeline stage status")

		nodeID := ""
		for _, node := range stage.Stage.Nodes {
			if node.HasLog && strings.Contains(strings.ToLower(node.Name), "echo") {
				nodeID = node.ID
				break
			}
			if node.HasLog && nodeID == "" {
				nodeID = node.ID
			}
		}
		r.NotEmpty(nodeID, "pipeline node with log")

		nodeLog := callIntegrationTool[struct {
			Log struct {
				NodeID    string `json:"nodeId"`
				Text      string `json:"text"`
				Truncated bool   `json:"truncated"`
			} `json:"log"`
		}](t, clientSession, "jenkins_get_pipeline_node_log", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-pipeline",
			"build":      pipelineBuild,
			"nodeId":     nodeID,
			"maxBytes":   128,
		})
		r.Equal(nodeID, nodeLog.Log.NodeID, "pipeline node log id")
		r.Contains(nodeLog.Log.Text, "hello from pipeline", "pipeline node log text")
		r.False(nodeLog.Log.Truncated, "pipeline node log should fit in requested bytes")
	})
}

func callIntegrationTool[T any](t *testing.T, clientSession *mcp.ClientSession, name string, args map[string]any) T {
	t.Helper()

	r := require.New(t)
	result, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	r.NoError(err, "CallTool(%s)", name)
	r.False(result.IsError, "CallTool(%s) IsError", name)
	r.NotNil(result.StructuredContent, "CallTool(%s) structured content", name)

	var got T
	payload, err := json.Marshal(result.StructuredContent)
	r.NoError(err, "marshal %s structured content", name)
	r.NoError(json.Unmarshal(payload, &got), "unmarshal %s structured content", name)
	return got
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
