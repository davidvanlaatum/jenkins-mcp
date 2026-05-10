//go:build !no_integration

package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/david/jenkins-mcp/internal/audit"
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
	})
}

func connectIntegrationMCP(t *testing.T, jenkins jenkinscontainer.Fixture, user string) (*mcp.ClientSession, func()) {
	t.Helper()

	r := require.New(t)
	cfg, api := jenkins.Controller(t, user)
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
