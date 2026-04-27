package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
)

func TestToolErrorsAreStructured(t *testing.T) {
	cfg := config.Config{
		DefaultController: "default",
		Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		Limits:            config.Defaults().Limits,
		Artifacts:         config.Defaults().Artifacts,
	}
	server := New(Dependencies{Config: cfg, Jenkins: nil, Audit: &audit.Logger{}, Version: "test"}).Raw()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer func() { _ = serverSession.Close() }()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = clientSession.Close() }()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "jenkins_trigger_build",
		Arguments: map[string]any{"job": "app"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !result.IsError {
		t.Fatal("CallTool() IsError = false")
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	content, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("structured error marshal: %v", err)
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf("structured error unmarshal: %v", err)
	}
	if payload.Error.Code != "mutation_disabled" {
		t.Fatalf("error code = %q", payload.Error.Code)
	}
}
