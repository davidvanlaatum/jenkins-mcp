package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"

	"github.com/david/jenkins-mcp/internal/config"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	jenkinstools "github.com/david/jenkins-mcp/internal/tools/jenkins"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestPluginReplayMetadataAndOmittedOverrides(t *testing.T) {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	r := require.New(t)
	main := "你好-main"
	loaded := map[string]string{"pkg.Helper": "écho 'loaded'", "Script2": "second original"}
	upstream := mcp.NewServer(&mcp.Implementation{Name: "jenkins", Version: "test"}, nil)
	upstream.AddTool(&mcp.Tool{Name: "getReplayScripts", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{StructuredContent: map[string]any{"mainScript": main, "loadedScripts": loaded}}, nil
	})
	endpoint := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, nil)
	var form map[string]string
	deps := newReplayTestDeps(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/mcp-server/mcp":
			endpoint.ServeHTTP(w, req)
		case "/job/app/api/json":
			writeReplayJSON(w, `{"name":"app","fullName":"app","nextBuildNumber":8}`)
		case "/crumbIssuer/api/json":
			http.NotFound(w, req)
		case "/job/app/7/replay/run":
			r.NoError(req.ParseForm())
			r.NoError(json.Unmarshal([]byte(req.Form.Get("json")), &form))
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected request: %s", req.URL.Path)
			http.NotFound(w, req)
		}
	})
	scripts, err := jenkinstools.ReplayScripts(loggerCtx, deps, jenkinstools.ReplayScriptsRequest{Job: "app", Build: 7, MaxBytes: 4})
	r.NoError(err)
	r.Len(scripts.Scripts.Scripts, 3)
	r.True(scripts.Scripts.Truncated)
	r.Equal(int64(len(main)+len(loaded["pkg.Helper"])+len(loaded["Script2"])), scripts.Scripts.TotalBytes)
	primary := scripts.Scripts.Scripts[0]
	r.Equal("你", primary.Content, "truncate at a UTF-8 boundary")
	r.Equal(int64(len(main)), primary.SizeBytes)
	r.Equal(fmt.Sprintf("%x", sha256.Sum256([]byte(main))), primary.SHA256)
	helper := scripts.Scripts.Scripts[2]
	r.Equal("pkg_Helper", helper.ID)
	r.Equal(helper.ID, helper.OverrideKey)
	r.Equal("éch", helper.Content)
	r.Equal(int64(len(loaded["pkg.Helper"])), helper.SizeBytes)
	r.Equal(fmt.Sprintf("%x", sha256.Sum256([]byte(loaded["pkg.Helper"]))), helper.SHA256)

	deps.Config.Mutations.Enabled = true
	got, err := jenkinstools.ReplayBuild(loggerCtx, deps, jenkinstools.ReplayBuildRequest{Job: "app", Build: 7, LoadedScriptOverrides: map[string]string{"pkg_Helper": "replacement"}})
	r.NoError(err)
	r.True(got.Replay.Replayed)
	r.False(got.Replay.MainScriptOverridden)
	r.Equal(main, form["mainScript"], "original full primary source survives a truncated read")
	r.Equal("second original", form["Script2"])
	r.Equal("replacement", form["pkg_Helper"])
}

func newReplayTestDeps(t *testing.T, handler http.HandlerFunc) jenkinstools.Deps {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := jenkinsclient.New(loggerCtx, config.ControllerConfig{ID: "default", URL: server.URL})
	require.New(t).NoError(err)
	cfg := config.Defaults()
	cfg.Controllers = []config.ControllerConfig{{ID: "default", URL: server.URL}}
	return jenkinstools.Deps{Config: cfg, Jenkins: map[string]*jenkinsapi.API{"default": jenkinsapi.New("default", c)}}
}
func writeReplayJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}
