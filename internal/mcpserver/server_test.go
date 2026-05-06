package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
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
	if len(result.Content) != 1 {
		t.Fatalf("CallTool() content length = %d, want 1", len(result.Content))
	}
	if result.StructuredContent != nil {
		t.Fatalf("CallTool() structured content = %v, want nil for error result", result.StructuredContent)
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool() content type = %T, want *mcp.TextContent", result.Content[0])
	}
	if err := json.Unmarshal([]byte(textContent.Text), &payload); err != nil {
		t.Fatalf("structured error unmarshal: %v", err)
	}
	if payload.Error.Code != "mutation_disabled" {
		t.Fatalf("error code = %q", payload.Error.Code)
	}
}

func TestToolCallsAreLoggedWithPayloadsWhenEnabled(t *testing.T) {
	var logs bytes.Buffer
	cfg := config.Config{
		DefaultController: "default",
		Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		Limits:            config.Defaults().Limits,
		Artifacts:         config.Defaults().Artifacts,
		Logging:           config.LoggingConfig{ToolCalls: true, ToolPayloads: true},
	}
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	server := New(Dependencies{Config: cfg, Jenkins: nil, Audit: &audit.Logger{}, Logger: logger, Version: "test"}).Raw()
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

	_, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "jenkins_trigger_build",
		Arguments: map[string]any{"job": "app"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	got := logs.String()
	for _, want := range []string{
		"tool_call_started",
		"tool_call_finished",
		`tool=jenkins_trigger_build`,
		`arguments="{\"job\":\"app\"}"`,
		`is_error=true`,
		`error_code=mutation_disabled`,
		`error_payload="{\"error\"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs missing %q in:\n%s", want, got)
		}
	}
}

func TestRegisteredToolNames(t *testing.T) {
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

	var names []string
	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("Tools() error = %v", err)
		}
		names = append(names, tool.Name)
	}
	slices.Sort(names)

	want := []string{
		"jenkins_cancel_build",
		"jenkins_cancel_queue_item",
		"jenkins_download_artifact",
		"jenkins_get_build",
		"jenkins_get_capabilities",
		"jenkins_get_changes",
		"jenkins_get_coverage",
		"jenkins_get_issues",
		"jenkins_get_job",
		"jenkins_get_log",
		"jenkins_get_pipeline_node_log",
		"jenkins_get_pipeline_run",
		"jenkins_get_pipeline_stage",
		"jenkins_get_queue_item",
		"jenkins_get_test_report",
		"jenkins_list_artifacts",
		"jenkins_list_builds",
		"jenkins_list_jobs",
		"jenkins_list_queue",
		"jenkins_read_artifact",
		"jenkins_resolve_build_url",
		"jenkins_search_log",
		"jenkins_tail_log",
		"jenkins_trigger_build",
		"jenkins_watch_build",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("tool names = %#v, want %#v", names, want)
	}
}

func TestRegisteredToolAnnotations(t *testing.T) {
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

	type wantToolAnnotations struct {
		readOnly    bool
		destructive *bool
		idempotent  bool
	}
	want := map[string]wantToolAnnotations{
		"jenkins_get_capabilities":      {readOnly: true},
		"jenkins_resolve_build_url":     {readOnly: true},
		"jenkins_list_jobs":             {readOnly: true},
		"jenkins_get_job":               {readOnly: true},
		"jenkins_list_builds":           {readOnly: true},
		"jenkins_get_build":             {readOnly: true},
		"jenkins_get_log":               {readOnly: true},
		"jenkins_search_log":            {readOnly: true},
		"jenkins_tail_log":              {readOnly: true},
		"jenkins_get_test_report":       {readOnly: true},
		"jenkins_get_pipeline_run":      {readOnly: true},
		"jenkins_get_pipeline_stage":    {readOnly: true},
		"jenkins_get_pipeline_node_log": {readOnly: true},
		"jenkins_list_artifacts":        {readOnly: true},
		"jenkins_read_artifact":         {readOnly: true},
		"jenkins_get_coverage":          {readOnly: true},
		"jenkins_get_issues":            {readOnly: true},
		"jenkins_get_changes":           {readOnly: true},
		"jenkins_watch_build":           {readOnly: true},
		"jenkins_get_queue_item":        {readOnly: true},
		"jenkins_list_queue":            {readOnly: true},
		"jenkins_download_artifact":     {destructive: ptrBool(false)},
		"jenkins_trigger_build":         {destructive: ptrBool(false)},
		"jenkins_cancel_queue_item":     {destructive: ptrBool(true)},
		"jenkins_cancel_build":          {destructive: ptrBool(true)},
	}

	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("Tools() error = %v", err)
		}
		got, ok := want[tool.Name]
		if !ok {
			t.Fatalf("unexpected tool %q", tool.Name)
		}
		if tool.Annotations == nil {
			t.Fatalf("tool %q annotations are nil", tool.Name)
		}
		if tool.Annotations.ReadOnlyHint != got.readOnly {
			t.Fatalf("tool %q readOnlyHint = %v, want %v", tool.Name, tool.Annotations.ReadOnlyHint, got.readOnly)
		}
		if tool.Annotations.IdempotentHint != got.idempotent {
			t.Fatalf("tool %q idempotentHint = %v, want %v", tool.Name, tool.Annotations.IdempotentHint, got.idempotent)
		}
		switch {
		case tool.Annotations.DestructiveHint == nil && got.destructive == nil:
		case tool.Annotations.DestructiveHint == nil || got.destructive == nil:
			t.Fatalf("tool %q destructiveHint = %v, want %v", tool.Name, tool.Annotations.DestructiveHint, got.destructive)
		case *tool.Annotations.DestructiveHint != *got.destructive:
			t.Fatalf("tool %q destructiveHint = %v, want %v", tool.Name, *tool.Annotations.DestructiveHint, *got.destructive)
		}
		delete(want, tool.Name)
	}

	if len(want) != 0 {
		t.Fatalf("missing tools from annotations check: %#v", want)
	}
}

func ptrBool(v bool) *bool {
	return &v
}

func TestRegisteredToolTitles(t *testing.T) {
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

	want := map[string]string{
		"jenkins_get_capabilities":      "Get Capabilities",
		"jenkins_resolve_build_url":     "Resolve Build URL",
		"jenkins_list_jobs":             "List Jobs",
		"jenkins_get_job":               "Get Job",
		"jenkins_list_builds":           "List Builds",
		"jenkins_get_build":             "Get Build",
		"jenkins_get_log":               "Get Log",
		"jenkins_search_log":            "Search Log",
		"jenkins_tail_log":              "Tail Log",
		"jenkins_get_test_report":       "Get Test Report",
		"jenkins_get_pipeline_run":      "Get Pipeline Run",
		"jenkins_get_pipeline_stage":    "Get Pipeline Stage",
		"jenkins_get_pipeline_node_log": "Get Pipeline Node Log",
		"jenkins_download_artifact":     "Download Artifact",
		"jenkins_list_artifacts":        "List Artifacts",
		"jenkins_read_artifact":         "Read Artifact",
		"jenkins_get_coverage":          "Get Coverage",
		"jenkins_get_issues":            "Get Issues",
		"jenkins_get_changes":           "Get Changes",
		"jenkins_watch_build":           "Watch Build",
		"jenkins_trigger_build":         "Trigger Build",
		"jenkins_get_queue_item":        "Get Queue Item",
		"jenkins_list_queue":            "List Queue",
		"jenkins_cancel_queue_item":     "Cancel Queue Item",
		"jenkins_cancel_build":          "Cancel Build",
	}

	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("Tools() error = %v", err)
		}
		title, ok := want[tool.Name]
		if !ok {
			t.Fatalf("unexpected tool %q", tool.Name)
		}
		if tool.Title != title {
			t.Fatalf("tool %q title = %q, want %q", tool.Name, tool.Title, title)
		}
		delete(want, tool.Name)
	}

	if len(want) != 0 {
		t.Fatalf("missing tools from title check: %#v", want)
	}
}

func TestListJobsToolDescriptionAndInputSchemaMentionFilters(t *testing.T) {
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

	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("Tools() error = %v", err)
		}
		if tool.Name != "jenkins_list_jobs" {
			continue
		}
		for _, want := range []string{"cursor", "nameContains", "nameRegex", "type", "status", "building"} {
			if !strings.Contains(tool.Description, want) {
				t.Fatalf("description = %q, want mention of %q", tool.Description, want)
			}
		}

		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal input schema: %v", err)
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal input schema: %v", err)
		}
		for _, want := range []string{"cursor", "nameContains", "nameRegex", "type", "status", "building"} {
			property, ok := schema.Properties[want]
			if !ok {
				t.Fatalf("input schema missing property %q", want)
			}
			if property.Description == "" {
				t.Fatalf("input schema property %q has empty description", want)
			}
		}
		return
	}
	t.Fatal("jenkins_list_jobs tool not found")
}

func TestRegisteredToolInputSchemaPropertiesHaveDescriptions(t *testing.T) {
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

	var checked int
	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("Tools() error = %v", err)
		}
		if tool.InputSchema == nil {
			t.Fatalf("tool %q has nil input schema", tool.Name)
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal input schema for %q: %v", tool.Name, err)
		}
		var schema any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal input schema for %q: %v", tool.Name, err)
		}
		object, ok := schema.(map[string]any)
		if !ok {
			t.Fatalf("tool %q input schema is not an object", tool.Name)
		}
		properties, ok := object["properties"].(map[string]any)
		if !ok || len(properties) == 0 {
			t.Fatalf("tool %q input schema has no properties", tool.Name)
		}
		assertSchemaPropertyDescriptions(t, tool.Name, "input", schema)
		checked++
	}
	if checked == 0 {
		t.Fatal("no tools checked")
	}
}

func TestRegisteredToolOutputSchemaPropertiesHaveDescriptions(t *testing.T) {
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

	var checked int
	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("Tools() error = %v", err)
		}
		if tool.OutputSchema == nil {
			t.Fatalf("tool %q has nil output schema", tool.Name)
		}
		raw, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			t.Fatalf("marshal output schema for %q: %v", tool.Name, err)
		}
		var schema any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal output schema for %q: %v", tool.Name, err)
		}
		object, ok := schema.(map[string]any)
		if !ok {
			t.Fatalf("tool %q output schema is not an object", tool.Name)
		}
		properties, ok := object["properties"].(map[string]any)
		if !ok || len(properties) == 0 {
			t.Fatalf("tool %q output schema has no properties", tool.Name)
		}
		assertSchemaPropertyDescriptions(t, tool.Name, "output", schema)
		checked++
	}
	if checked == 0 {
		t.Fatal("no tools checked")
	}
}

func assertSchemaPropertyDescriptions(t *testing.T, toolName, schemaName string, schema any) {
	t.Helper()
	object, ok := schema.(map[string]any)
	if !ok {
		return
	}
	if properties, ok := object["properties"].(map[string]any); ok {
		for propertyName, propertySchema := range properties {
			propertyObject, ok := propertySchema.(map[string]any)
			if !ok {
				t.Fatalf("tool %q %s schema property %q is not an object", toolName, schemaName, propertyName)
			}
			description, _ := propertyObject["description"].(string)
			if description == "" {
				t.Fatalf("tool %q %s schema property %q has empty description", toolName, schemaName, propertyName)
			}
		}
	}
	for _, value := range object {
		switch value := value.(type) {
		case map[string]any:
			assertSchemaPropertyDescriptions(t, toolName, schemaName, value)
		case []any:
			for _, item := range value {
				assertSchemaPropertyDescriptions(t, toolName, schemaName, item)
			}
		}
	}
}
