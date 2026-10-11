package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestReplayScriptsPrefersPlugin(t *testing.T) {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	r := require.New(t)
	upstream := mcp.NewServer(&mcp.Implementation{Name: "jenkins", Version: "test"}, nil)
	upstream.AddTool(&mcp.Tool{Name: "getReplayScripts", InputSchema: map[string]any{"type": "object"}}, func(_ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		r.NoError(json.Unmarshal(req.Params.Arguments, &args))
		r.Equal("folder/app", args["jobFullName"])
		r.Equal(float64(7), args["buildNumber"])
		return &mcp.CallToolResult{StructuredContent: map[string]any{"mainScript": "echo '你好'", "loadedScripts": map[string]string{"pkg.Helper": "echo 'héllo'", "Script1": "echo 'one'"}}}, nil
	})
	endpoint := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		user, token, ok := req.BasicAuth()
		r.True(ok)
		r.Equal("user", user)
		r.Equal("token", token)
		if req.URL.Path != "/jenkins/mcp-server/mcp" {
			t.Errorf("unexpected request: %s", req.URL.Path)
			http.NotFound(w, req)
			return
		}
		endpoint.ServeHTTP(w, req)
	}))
	t.Cleanup(server.Close)
	c, err := jenkinsclient.New(loggerCtx, config.ControllerConfig{URL: server.URL + "/jenkins", Username: "user", Token: "token"})
	r.NoError(err)
	main, loaded, _, _, err := jenkinsapi.New("test", c).ReplayScripts(loggerCtx, "folder/app", 7)
	r.NoError(err)
	r.Equal("echo '你好'", main)
	r.Equal(map[string]string{"pkg_Helper": "echo 'héllo'", "Script1": "echo 'one'"}, loaded)
	for range upstream.Sessions() {
		t.Error("upstream session was not closed")
	}
}

func TestReplayPluginFallbackAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		method      string
		missingTool bool
		result      *mcp.CallToolResult
		code        apperrors.Code
		fallback    bool
	}{
		{name: "missing endpoint", status: 404, method: "initialize", fallback: true},
		{name: "missing tool", missingTool: true, fallback: true},
		{name: "unauthenticated", status: 401, method: "initialize", code: apperrors.CodePermissionDenied},
		{name: "forbidden", status: 403, method: "tools/list", code: apperrors.CodePermissionDenied},
		{name: "operational failure", status: 503, method: "tools/call", code: apperrors.CodeJenkins},
		{name: "initialized endpoint vanished", status: 404, method: "notifications/initialized", code: apperrors.CodeNotFound},
		{name: "expired session", status: 404, method: "tools/call", code: apperrors.CodeNotFound},
		{name: "login redirect", status: 302, method: "initialize", code: apperrors.CodeJenkins},
		{name: "tool error", result: &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "SECRET"}}}, code: apperrors.CodeJenkins},
		{name: "permission limited", result: &mcp.CallToolResult{StructuredContent: map[string]any{"mainScript": "", "loadedScripts": map[string]string{}}}, code: apperrors.CodePermissionDenied},
		{name: "missing structure", result: &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "SECRET"}}}, code: apperrors.CodeJenkins},
		{name: "missing fields", result: &mcp.CallToolResult{StructuredContent: map[string]any{"mainScript": "main"}}, code: apperrors.CodeJenkins},
		{name: "wrong field type", result: &mcp.CallToolResult{StructuredContent: map[string]any{"mainScript": 5, "loadedScripts": map[string]string{}}}, code: apperrors.CodeJenkins},
		{name: "null loaded script", result: &mcp.CallToolResult{StructuredContent: map[string]any{"mainScript": "main", "loadedScripts": map[string]any{"Script1": nil}}}, code: apperrors.CodeJenkins},
		{name: "identifier collision", result: &mcp.CallToolResult{StructuredContent: map[string]any{"mainScript": "main", "loadedScripts": map[string]string{"pkg.Helper": "one", "pkg_Helper": "two"}}}, code: apperrors.CodeJenkins},
		{name: "reserved identifier", result: &mcp.CallToolResult{StructuredContent: map[string]any{"mainScript": "main", "loadedScripts": map[string]string{"mainScript": "one"}}}, code: apperrors.CodeJenkins},
		{name: "response limit", result: &mcp.CallToolResult{StructuredContent: map[string]any{"mainScript": strings.Repeat("x", 8*1024*1024), "loadedScripts": map[string]string{}}}, code: apperrors.CodeJenkins},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loggerCtx, _, _ := testhandler.SetupTestHandler(t)

			r := require.New(t)
			upstream := mcp.NewServer(&mcp.Implementation{Name: "jenkins", Version: "test"}, nil)
			if !tc.missingTool {
				upstream.AddTool(&mcp.Tool{Name: "getReplayScripts", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) { return tc.result, nil })
			}
			endpoint := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, nil)
			var htmlReads atomic.Int32
			api := newReplayTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/job/app/7/replay/" {
					htmlReads.Add(1)
					_, _ = w.Write([]byte(`<textarea name="_.mainScript">original</textarea>`))
					return
				}
				if req.Method == http.MethodPost {
					body, err := io.ReadAll(req.Body)
					r.NoError(err)
					req.Body = io.NopCloser(bytes.NewReader(body))
					var rpc struct {
						Method string `json:"method"`
					}
					r.NoError(json.Unmarshal(body, &rpc))
					if rpc.Method == tc.method {
						w.WriteHeader(tc.status)
						return
					}
				}
				endpoint.ServeHTTP(w, req)
			})
			main, _, _, _, err := api.ReplayScripts(loggerCtx, "app", 7)
			if tc.fallback {
				r.NoError(err)
				r.Equal("original", main)
				r.Equal(int32(1), htmlReads.Load())
			} else {
				var appErr *apperrors.Error
				r.ErrorAs(err, &appErr)
				r.Equal(tc.code, appErr.Code)
				r.NotContains(err.Error(), "SECRET")
				r.Zero(htmlReads.Load(), "failures must never read HTML")
			}
		})
	}
}

func newReplayTestAPI(t *testing.T, handler http.HandlerFunc) *jenkinsapi.API {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := jenkinsclient.New(loggerCtx, config.ControllerConfig{URL: server.URL})
	require.New(t).NoError(err)
	return jenkinsapi.New("test", c)
}
