package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
)

func TestToolErrorsAreStructured(t *testing.T) {
	r := require.New(t)
	cfg := config.Config{
		DefaultController: "default",
		Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		Limits:            config.Defaults().Limits,
		Artifacts:         config.Defaults().Artifacts,
	}
	server := New(Dependencies{Config: cfg, Jenkins: nil, Audit: &audit.Logger{}, Version: "test"}).Raw()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err, "server connect")
	defer func() { _ = serverSession.Close() }()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err, "client connect")
	defer func() { _ = clientSession.Close() }()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "jenkins_trigger_build",
		Arguments: map[string]any{"job": "app"},
	})
	r.NoError(err, "CallTool()")
	r.True(result.IsError, "CallTool() IsError")
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	r.Len(result.Content, 1, "CallTool() content")
	r.Nil(result.StructuredContent, "CallTool() structured content should be nil for error result")
	textContent, ok := result.Content[0].(*mcp.TextContent)
	r.True(ok, "CallTool() content type should be *mcp.TextContent")
	err = json.Unmarshal([]byte(textContent.Text), &payload)
	r.NoError(err, "structured error unmarshal")
	r.Equal("mutation_disabled", payload.Error.Code, "error code")
}

func TestToolErrorsDoNotExposeJenkinsHTMLResponseBodies(t *testing.T) {
	r := require.New(t)
	const htmlBody = `<!doctype html><html><body><h1>Jenkins failed</h1><p>secret-html-token</p></body></html>`
	jenkins := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer jenkins.Close()

	cfg := config.Config{
		DefaultController: "default",
		Controllers:       []config.ControllerConfig{{ID: "default", URL: jenkins.URL}},
		Limits:            config.Defaults().Limits,
		Artifacts:         config.Defaults().Artifacts,
	}
	controller, err := jenkinsclient.New(cfg.Controllers[0], slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err, "jenkins client")
	server := New(Dependencies{
		Config:  cfg,
		Jenkins: map[string]*jenkinsapi.API{"default": jenkinsapi.New("default", controller)},
		Audit:   &audit.Logger{},
		Version: "test",
	}).Raw()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err, "server connect")
	defer func() { _ = serverSession.Close() }()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err, "client connect")
	defer func() { _ = clientSession.Close() }()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "jenkins_get_job",
		Arguments: map[string]any{"job": "app"},
	})
	r.NoError(err, "CallTool()")
	r.True(result.IsError, "CallTool() IsError")
	r.Len(result.Content, 1, "CallTool() content")
	textContent, ok := result.Content[0].(*mcp.TextContent)
	r.True(ok, "CallTool() content type should be *mcp.TextContent")
	r.NotContains(textContent.Text, "<html", "tool error should not include raw HTML")
	r.NotContains(textContent.Text, "secret-html-token", "tool error should not leak response body")

	var payload struct {
		Error struct {
			Code   string         `json:"code"`
			Detail map[string]int `json:"detail"`
		} `json:"error"`
	}
	err = json.Unmarshal([]byte(textContent.Text), &payload)
	r.NoError(err, "structured error unmarshal")
	r.Equal("jenkins_error", payload.Error.Code, "error code")
	r.Equal(http.StatusInternalServerError, payload.Error.Detail["status"], "error detail status")
}

func TestNormalizeErrorMapsContextDeadline(t *testing.T) {
	r := require.New(t)

	appErr := normalizeError(context.DeadlineExceeded)

	r.Equal(apperrors.CodeUnavailable, appErr.Code, "error code")
	r.Contains(appErr.Message, "context deadline", "error message")
}

func TestToolCallsAreLoggedWithPayloadsWhenEnabled(t *testing.T) {
	r := require.New(t)
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

	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err, "server connect")
	defer func() { _ = serverSession.Close() }()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err, "client connect")
	defer func() { _ = clientSession.Close() }()

	_, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "jenkins_trigger_build",
		Arguments: map[string]any{"job": "app"},
	})
	r.NoError(err, "CallTool()")
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
		r.Contains(got, want, "logs")
	}
}

func TestRegisteredToolNames(t *testing.T) {
	r := require.New(t)
	cfg := config.Config{
		DefaultController: "default",
		Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		Limits:            config.Defaults().Limits,
		Artifacts:         config.Defaults().Artifacts,
	}
	server := New(Dependencies{Config: cfg, Jenkins: nil, Audit: &audit.Logger{}, Version: "test"}).Raw()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err, "server connect")
	defer func() { _ = serverSession.Close() }()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err, "client connect")
	defer func() { _ = clientSession.Close() }()

	var names []string
	for tool, err := range clientSession.Tools(ctx, nil) {
		r.NoError(err, "Tools()")
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
		"jenkins_get_job",
		"jenkins_get_job_config",
		"jenkins_get_log",
		"jenkins_get_pipeline_node_log",
		"jenkins_get_pipeline_run",
		"jenkins_get_pipeline_stage",
		"jenkins_get_queue_item",
		"jenkins_get_test_report",
		"jenkins_list_artifacts",
		"jenkins_list_builds",
		"jenkins_list_issues",
		"jenkins_list_jobs",
		"jenkins_list_queue",
		"jenkins_read_artifact",
		"jenkins_resolve_build_url",
		"jenkins_search_log",
		"jenkins_tail_log",
		"jenkins_trigger_build",
		"jenkins_update_server",
		"jenkins_watch_build",
		"jenkins_watch_queue_item",
	}
	r.Equal(want, names, "tool names")
}

func TestRegisteredToolAnnotations(t *testing.T) {
	r := require.New(t)
	cfg := config.Config{
		DefaultController: "default",
		Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		Limits:            config.Defaults().Limits,
		Artifacts:         config.Defaults().Artifacts,
	}
	server := New(Dependencies{Config: cfg, Jenkins: nil, Audit: &audit.Logger{}, Version: "test"}).Raw()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err, "server connect")
	defer func() { _ = serverSession.Close() }()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err, "client connect")
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
		"jenkins_get_job_config":        {readOnly: true},
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
		"jenkins_list_issues":           {readOnly: true},
		"jenkins_get_changes":           {readOnly: true},
		"jenkins_watch_build":           {readOnly: true},
		"jenkins_get_queue_item":        {readOnly: true},
		"jenkins_watch_queue_item":      {readOnly: true},
		"jenkins_list_queue":            {readOnly: true},
		"jenkins_download_artifact":     {destructive: ptrBool(false)},
		"jenkins_trigger_build":         {destructive: ptrBool(false)},
		"jenkins_update_server":         {destructive: ptrBool(true)},
		"jenkins_cancel_queue_item":     {destructive: ptrBool(true)},
		"jenkins_cancel_build":          {destructive: ptrBool(true)},
	}

	for tool, err := range clientSession.Tools(ctx, nil) {
		r.NoError(err, "Tools()")
		got, ok := want[tool.Name]
		r.True(ok, "unexpected tool %q", tool.Name)
		r.NotNil(tool.Annotations, "tool %q annotations", tool.Name)
		r.Equal(got.readOnly, tool.Annotations.ReadOnlyHint, "tool %q readOnlyHint", tool.Name)
		r.Equal(got.idempotent, tool.Annotations.IdempotentHint, "tool %q idempotentHint", tool.Name)
		switch {
		case tool.Annotations.DestructiveHint == nil && got.destructive == nil:
		case tool.Annotations.DestructiveHint == nil || got.destructive == nil:
			r.Equal(got.destructive, tool.Annotations.DestructiveHint, "tool %q destructiveHint", tool.Name)
		case *tool.Annotations.DestructiveHint != *got.destructive:
			r.Equal(*got.destructive, *tool.Annotations.DestructiveHint, "tool %q destructiveHint", tool.Name)
		}
		delete(want, tool.Name)
	}

	r.Empty(want, "missing tools from annotations check")
}

func ptrBool(v bool) *bool {
	return &v
}

func TestRegisteredToolTitles(t *testing.T) {
	r := require.New(t)
	cfg := config.Config{
		DefaultController: "default",
		Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		Limits:            config.Defaults().Limits,
		Artifacts:         config.Defaults().Artifacts,
	}
	server := New(Dependencies{Config: cfg, Jenkins: nil, Audit: &audit.Logger{}, Version: "test"}).Raw()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err, "server connect")
	defer func() { _ = serverSession.Close() }()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err, "client connect")
	defer func() { _ = clientSession.Close() }()

	want := map[string]string{
		"jenkins_get_capabilities":      "Get Capabilities",
		"jenkins_resolve_build_url":     "Resolve Build URL",
		"jenkins_list_jobs":             "List Jobs",
		"jenkins_get_job":               "Get Job",
		"jenkins_get_job_config":        "Get Job Config",
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
		"jenkins_update_server":         "Update Server",
		"jenkins_list_artifacts":        "List Artifacts",
		"jenkins_read_artifact":         "Read Artifact",
		"jenkins_list_issues":           "List Issues",
		"jenkins_get_changes":           "Get Changes",
		"jenkins_watch_build":           "Watch Build",
		"jenkins_trigger_build":         "Trigger Build",
		"jenkins_get_queue_item":        "Get Queue Item",
		"jenkins_watch_queue_item":      "Watch Queue Item",
		"jenkins_list_queue":            "List Queue",
		"jenkins_cancel_queue_item":     "Cancel Queue Item",
		"jenkins_cancel_build":          "Cancel Build",
	}

	for tool, err := range clientSession.Tools(ctx, nil) {
		r.NoError(err, "Tools()")
		title, ok := want[tool.Name]
		r.True(ok, "unexpected tool %q", tool.Name)
		r.Equal(title, tool.Title, "tool %q title", tool.Name)
		delete(want, tool.Name)
	}

	r.Empty(want, "missing tools from title check")
}

func TestListJobsToolDescriptionAndInputSchemaMentionFilters(t *testing.T) {
	r := require.New(t)
	cfg := config.Config{
		DefaultController: "default",
		Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		Limits:            config.Defaults().Limits,
		Artifacts:         config.Defaults().Artifacts,
	}
	server := New(Dependencies{Config: cfg, Jenkins: nil, Audit: &audit.Logger{}, Version: "test"}).Raw()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err, "server connect")
	defer func() { _ = serverSession.Close() }()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err, "client connect")
	defer func() { _ = clientSession.Close() }()

	for tool, err := range clientSession.Tools(ctx, nil) {
		r.NoError(err, "Tools()")
		if tool.Name != "jenkins_list_jobs" {
			continue
		}
		for _, want := range []string{"cursor", "nameContains", "nameRegex", "type", "status", "building", "hasTests", "hasFailedTests", "hasSkippedTests"} {
			r.Contains(tool.Description, want, "description")
		}

		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		raw, err := json.Marshal(tool.InputSchema)
		r.NoError(err, "marshal input schema")
		err = json.Unmarshal(raw, &schema)
		r.NoError(err, "unmarshal input schema")
		for _, want := range []string{"cursor", "nameContains", "nameRegex", "type", "status", "building", "hasTests", "hasFailedTests", "hasSkippedTests"} {
			property, ok := schema.Properties[want]
			r.True(ok, "input schema missing property %q", want)
			r.NotEmpty(property.Description, "input schema property %q description", want)
		}
		return
	}
	r.Fail("jenkins_list_jobs tool not found")
}

func TestRegisteredToolInputSchemaPropertiesHaveDescriptions(t *testing.T) {
	r := require.New(t)
	cfg := config.Config{
		DefaultController: "default",
		Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		Limits:            config.Defaults().Limits,
		Artifacts:         config.Defaults().Artifacts,
	}
	server := New(Dependencies{Config: cfg, Jenkins: nil, Audit: &audit.Logger{}, Version: "test"}).Raw()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err, "server connect")
	defer func() { _ = serverSession.Close() }()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err, "client connect")
	defer func() { _ = clientSession.Close() }()

	var checked int
	for tool, err := range clientSession.Tools(ctx, nil) {
		r.NoError(err, "Tools()")
		r.NotNil(tool.InputSchema, "tool %q input schema", tool.Name)
		raw, err := json.Marshal(tool.InputSchema)
		r.NoError(err, "marshal input schema for %q", tool.Name)
		var schema any
		err = json.Unmarshal(raw, &schema)
		r.NoError(err, "unmarshal input schema for %q", tool.Name)
		object, ok := schema.(map[string]any)
		r.True(ok, "tool %q input schema should be an object", tool.Name)
		properties, ok := object["properties"].(map[string]any)
		r.True(ok, "tool %q input schema properties should be an object", tool.Name)
		r.NotEmpty(properties, "tool %q input schema properties", tool.Name)
		assertSchemaPropertyDescriptions(t, tool.Name, "input", schema)
		checked++
	}
	r.NotZero(checked, "tools checked")
}

func TestRegisteredToolOutputSchemaPropertiesHaveDescriptions(t *testing.T) {
	r := require.New(t)
	cfg := config.Config{
		DefaultController: "default",
		Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		Limits:            config.Defaults().Limits,
		Artifacts:         config.Defaults().Artifacts,
	}
	server := New(Dependencies{Config: cfg, Jenkins: nil, Audit: &audit.Logger{}, Version: "test"}).Raw()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err, "server connect")
	defer func() { _ = serverSession.Close() }()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err, "client connect")
	defer func() { _ = clientSession.Close() }()

	var checked int
	for tool, err := range clientSession.Tools(ctx, nil) {
		r.NoError(err, "Tools()")
		r.NotNil(tool.OutputSchema, "tool %q output schema", tool.Name)
		raw, err := json.Marshal(tool.OutputSchema)
		r.NoError(err, "marshal output schema for %q", tool.Name)
		var schema any
		err = json.Unmarshal(raw, &schema)
		r.NoError(err, "unmarshal output schema for %q", tool.Name)
		object, ok := schema.(map[string]any)
		r.True(ok, "tool %q output schema should be an object", tool.Name)
		properties, ok := object["properties"].(map[string]any)
		r.True(ok, "tool %q output schema properties should be an object", tool.Name)
		r.NotEmpty(properties, "tool %q output schema properties", tool.Name)
		assertSchemaPropertyDescriptions(t, tool.Name, "output", schema)
		checked++
	}
	r.NotZero(checked, "tools checked")
}

func TestGetBuildOutputSchemaDescribesCoverageFields(t *testing.T) {
	r := require.New(t)
	cfg := config.Config{
		DefaultController: "default",
		Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		Limits:            config.Defaults().Limits,
		Artifacts:         config.Defaults().Artifacts,
	}
	server := New(Dependencies{Config: cfg, Jenkins: nil, Audit: &audit.Logger{}, Version: "test"}).Raw()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err, "server connect")
	defer func() { _ = serverSession.Close() }()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err, "client connect")
	defer func() { _ = clientSession.Close() }()

	for tool, err := range clientSession.Tools(ctx, nil) {
		r.NoError(err, "Tools()")
		if tool.Name != "jenkins_get_build" {
			continue
		}
		raw, err := json.Marshal(tool.OutputSchema)
		r.NoError(err, "marshal output schema")
		var schema any
		err = json.Unmarshal(raw, &schema)
		r.NoError(err, "unmarshal output schema")
		for _, property := range []string{"coverage", "summaries", "metrics", "errors", "checkedEndpoints"} {
			r.True(schemaContainsDescribedProperty(schema, property), "output schema should describe %q", property)
		}
		return
	}
	r.Fail("jenkins_get_build tool not found")
}

func schemaContainsDescribedProperty(schema any, propertyName string) bool {
	object, ok := schema.(map[string]any)
	if !ok {
		return false
	}
	if properties, ok := object["properties"].(map[string]any); ok {
		if property, ok := properties[propertyName].(map[string]any); ok {
			description, _ := property["description"].(string)
			if description != "" {
				return true
			}
		}
	}
	for _, value := range object {
		switch value := value.(type) {
		case map[string]any:
			if schemaContainsDescribedProperty(value, propertyName) {
				return true
			}
		case []any:
			for _, item := range value {
				if schemaContainsDescribedProperty(item, propertyName) {
					return true
				}
			}
		}
	}
	return false
}

func assertSchemaPropertyDescriptions(t *testing.T, toolName, schemaName string, schema any) {
	t.Helper()
	r := require.New(t)
	object, ok := schema.(map[string]any)
	if !ok {
		return
	}
	if properties, ok := object["properties"].(map[string]any); ok {
		for propertyName, propertySchema := range properties {
			propertyObject, ok := propertySchema.(map[string]any)
			r.True(ok, "tool %q %s schema property %q should be an object", toolName, schemaName, propertyName)
			description, _ := propertyObject["description"].(string)
			r.NotEmpty(description, "tool %q %s schema property %q description", toolName, schemaName, propertyName)
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
