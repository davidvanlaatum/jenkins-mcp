package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"

	jenkinstools "github.com/david/jenkins-mcp/internal/tools/jenkins"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestReplayBuildPluginQueueAndScriptNames(t *testing.T) {
	for _, variant := range []string{"unchanged", "main", "loaded", "both", "unknown"} {
		t.Run(variant, func(t *testing.T) {
			loggerCtx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			upstream := mcp.NewServer(&mcp.Implementation{Name: "jenkins", Version: "test"}, nil)
			upstream.AddTool(&mcp.Tool{Name: "getReplayScripts", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{StructuredContent: map[string]any{"mainScript": "original main", "loadedScripts": map[string]string{"pkg.Helper": "original helper", "under_score": "original underscore"}}}, nil
			})
			calls := 0
			upstream.AddTool(&mcp.Tool{Name: "replayBuild", InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				calls++
				var args struct {
					Job    string            `json:"jobFullName"`
					Build  int               `json:"buildNumber"`
					Main   string            `json:"mainScript"`
					Loaded map[string]string `json:"loadedScripts"`
				}
				r.NoError(json.Unmarshal(req.Params.Arguments, &args))
				r.Equal("app", args.Job)
				r.Equal(7, args.Build)
				main, helper := "original main", "original helper"
				if variant == "main" || variant == "both" {
					main = "edited main"
				}
				if variant == "loaded" || variant == "both" {
					helper = "edited helper"
				}
				r.Equal(main, args.Main)
				r.Equal(map[string]string{"pkg.Helper": helper, "under_score": "original underscore"}, args.Loaded)
				// The real plugin returns a text ToolResponse envelope, not structuredContent.
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"status":"COMPLETED","result":{"id":42,"url":"queue/item/42/"}}`}}}, nil
			})
			endpoint := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, nil)
			deps := newReplayTestDeps(t, func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/mcp-server/mcp" {
					endpoint.ServeHTTP(w, req)
					return
				}
				if req.URL.Path == "/queue/item/42/api/json" {
					writeReplayJSON(w, `{"id":42,"task":{"name":"app","url":"http://jenkins/job/app/"},"executable":{"number":9,"url":"http://jenkins/job/app/9/"}}`)
					return
				}
				if req.URL.Path == "/job/app/api/json" {
					writeReplayJSON(w, `{"nextBuildNumber":8}`)
					return
				}
				t.Errorf("plugin replay must not call native endpoint: %s", req.URL.Path)
				http.NotFound(w, req)
			})
			deps.Config.Mutations.Enabled = true
			in := jenkinstools.ReplayBuildRequest{Job: "app", Build: 7}
			if variant == "main" || variant == "both" {
				s := "edited main"
				in.MainScriptOverride = &s
			}
			if variant == "loaded" || variant == "both" {
				in.LoadedScriptOverrides = map[string]string{"pkg_Helper": "edited helper"}
			}
			if variant == "unknown" {
				in.LoadedScriptOverrides = map[string]string{"missing": "edited"}
			}
			got, err := jenkinstools.ReplayBuild(loggerCtx, deps, in)
			if variant == "unknown" {
				r.Error(err)
				r.Zero(calls)
				return
			}
			r.NoError(err)
			r.Equal(1, calls)
			b, err := json.Marshal(got.Replay)
			r.NoError(err)
			var result map[string]any
			r.NoError(json.Unmarshal(b, &result))
			r.Equal(float64(42), result["queueId"])
			r.Equal("mcp_plugin", result["backend"])
			r.Equal(true, result["queueAuthoritative"])
			r.NotContains(result, "scheduledBuild", "concurrent scheduling makes nextBuildNumber unreliable")
			resolved, err := jenkinstools.QueueItem(loggerCtx, deps, jenkinstools.QueueItemRequest{ID: got.Replay.QueueID})
			r.NoError(err)
			r.NotNil(resolved.Item.Executable)
			r.Equal(9, resolved.Item.Executable.Number, "actual replay build differs from nextBuildNumber=8 under concurrent scheduling")
		})
	}
}

func TestReplayBuildPluginFailuresNeverReschedule(t *testing.T) {
	for _, tc := range []struct {
		name       string
		text       string
		toolError  bool
		status     int
		disconnect bool
		code       apperrors.Code
	}{
		{name: "denied replay", toolError: true, code: apperrors.CodeJenkins},
		{name: "empty result", text: `{"status":"COMPLETED"}`, code: apperrors.CodeJenkins},
		{name: "null result", text: `{"status":"COMPLETED","result":null}`, code: apperrors.CodeJenkins},
		{name: "invalid id", text: `{"status":"COMPLETED","result":{"id":0}}`, code: apperrors.CodeJenkins},
		{name: "malformed response", text: `SECRET`, code: apperrors.CodeJenkins},
		{name: "failed envelope", text: `{"status":"FAILED","result":{"id":42}}`, code: apperrors.CodeJenkins},
		{name: "accepted then disconnected", disconnect: true, code: apperrors.CodeUnavailable},
		{name: "accepted then unavailable", status: 503, code: apperrors.CodeJenkins},
		{name: "accepted then session missing", status: 404, code: apperrors.CodeNotFound},
		{name: "forbidden", status: 403, code: apperrors.CodePermissionDenied},
		{name: "response too large", text: strings.Repeat("x", 8*1024*1024), code: apperrors.CodeJenkins},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loggerCtx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			upstream := mcp.NewServer(&mcp.Implementation{Name: "jenkins", Version: "test"}, nil)
			upstream.AddTool(&mcp.Tool{Name: "getReplayScripts", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{StructuredContent: map[string]any{"mainScript": "original", "loadedScripts": map[string]string{}}}, nil
			})
			upstream.AddTool(&mcp.Tool{Name: "replayBuild", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{IsError: tc.toolError, Content: []mcp.Content{&mcp.TextContent{Text: tc.text}}}, nil
			})
			endpoint := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, nil)
			calls, nativeCalls := 0, 0
			deps := newReplayTestDeps(t, func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path != "/mcp-server/mcp" {
					nativeCalls++
					http.NotFound(w, req)
					return
				}
				if req.Method == http.MethodPost {
					body, err := io.ReadAll(req.Body)
					r.NoError(err)
					req.Body = io.NopCloser(bytes.NewReader(body))
					var rpc struct {
						Method string `json:"method"`
						Params struct {
							Name string `json:"name"`
						} `json:"params"`
					}
					r.NoError(json.Unmarshal(body, &rpc))
					if rpc.Method == "tools/call" && rpc.Params.Name == "replayBuild" {
						calls++
						if tc.disconnect {
							hijacker, ok := w.(http.Hijacker)
							r.True(ok)
							conn, _, err := hijacker.Hijack()
							r.NoError(err)
							r.NoError(conn.Close())
							return
						}
						if tc.status != 0 {
							w.WriteHeader(tc.status)
							return
						}
					}
				}
				endpoint.ServeHTTP(w, req)
			})
			deps.Config.Mutations.Enabled = true
			path := t.TempDir() + "/audit.jsonl"
			auditer, err := audit.New(config.AuditConfig{Path: path})
			r.NoError(err)
			deps.Audit = auditer
			_, err = jenkinstools.ReplayBuild(loggerCtx, deps, jenkinstools.ReplayBuildRequest{Job: "app", Build: 7})
			var appErr *apperrors.Error
			r.ErrorAs(err, &appErr)
			r.Equal(tc.code, appErr.Code)
			r.NotContains(err.Error(), "SECRET")
			r.Contains(err.Error(), "uncertain")
			r.Equal(1, calls, "uncertain mutation must not be retried")
			r.Zero(nativeCalls, "plugin errors must never fall back to native scheduling")
			data, err := os.ReadFile(path)
			r.NoError(err)
			r.Contains(string(data), `"action":"replay_build"`)
			r.NotContains(string(data), "original", "audit must not contain source bodies")
		})
	}
}
