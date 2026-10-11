package mcpclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apperrors "github.com/david/jenkins-mcp/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestCallToolDiscoveryPaginationAndPolicy(t *testing.T) {
	for _, endless := range []bool{false, true} {
		t.Run(map[bool]string{false: "second page", true: "bounded pages"}[endless], func(t *testing.T) {
			r := require.New(t)
			var pages, closes atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				user, token, ok := req.BasicAuth()
				r.True(ok)
				r.Equal("controller-user", user)
				r.Equal("controller-token", token)
				if req.Method == http.MethodDelete {
					closes.Add(1)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				var message struct {
					ID     any    `json:"id"`
					Method string `json:"method"`
					Params struct {
						Cursor string `json:"cursor"`
					} `json:"params"`
				}
				r.NoError(json.NewDecoder(req.Body).Decode(&message))
				w.Header().Set("Content-Type", "application/json")
				var result any
				switch message.Method {
				case "server/discover":
					r.NoError(json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "error": map[string]any{"code": -32601, "message": "Method not found"}}))
					return
				case "initialize":
					w.Header().Set("Mcp-Session-Id", "session")
					result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "jenkins", "version": "test"}}
				case "notifications/initialized":
					w.WriteHeader(http.StatusAccepted)
					return
				case "tools/list":
					page := pages.Add(1)
					if page == 1 || endless {
						result = map[string]any{"tools": []any{}, "nextCursor": "next"}
					} else {
						r.Equal("next", message.Params.Cursor)
						result = map[string]any{"tools": []any{map[string]any{"name": "getReplayScripts", "inputSchema": map[string]any{"type": "object"}}}}
					}
				case "tools/call":
					result = map[string]any{"content": []any{}, "structuredContent": map[string]any{"mainScript": "original", "loadedScripts": map[string]string{}}}
				default:
					t.Errorf("unexpected MCP method %s", message.Method)
				}
				r.NoError(json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": result}))
			}))
			t.Cleanup(server.Close)
			// The test's TLS trust policy must be reused, rather than constructing
			// an unrelated HTTP client that rejects the self-signed controller.
			data, available, err := CallTool(t.Context(), server.Client(), server.URL, "controller-user", "controller-token", "getReplayScripts", map[string]any{})
			r.True(available)
			if endless {
				var appErr *apperrors.Error
				r.ErrorAs(err, &appErr)
				r.Equal(apperrors.CodeJenkins, appErr.Code)
				r.Equal(int32(100), pages.Load())
			} else {
				r.NoError(err)
				r.Contains(string(data), "original")
				r.Equal(int32(2), pages.Load())
			}
			r.Equal(int32(1), closes.Load(), "terminate the upstream session on success and failure")
		})
	}
}

func TestCallToolHonorsCancellationAndTimeout(t *testing.T) {
	r := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-req.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	t.Cleanup(server.Close)
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		if canceled {
			cancel()
		}
		_, available, err := CallTool(ctx, &http.Client{Timeout: 20 * time.Millisecond}, server.URL, "", "", "getReplayScripts", nil)
		cancel()
		r.True(available, "timeouts are failures, not plugin absence")
		var appErr *apperrors.Error
		r.ErrorAs(err, &appErr)
		r.Equal(apperrors.CodeUnavailable, appErr.Code)
	}
}

func TestBoundedBodyExactLimit(t *testing.T) {
	r := require.New(t)
	for _, length := range []int{3, 4} {
		var failure error
		body := &mcpBoundedBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("x", length))), remaining: 3, onFailure: func(err error) { failure = err }}
		data, err := io.ReadAll(body)
		if length == 3 {
			r.NoError(err)
			r.Equal("xxx", string(data))
			r.Nil(failure)
		} else {
			r.Error(err)
			r.Equal(err, failure)
		}
	}
}

func TestCallToolKeepsCredentialsAndResultsIsolated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		user, token, _ := req.BasicAuth()
		if token != user+"-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var message struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(req.Body).Decode(&message); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var result any
		switch message.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "jenkins", "version": "test"}}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "getReplayScripts", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"content": []any{}, "structuredContent": map[string]any{"mainScript": user, "loadedScripts": map[string]string{}}}
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": result}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	for _, user := range []string{"controller-one", "controller-two"} {
		t.Run(user, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			for range 2 {
				data, available, err := CallTool(t.Context(), server.Client(), server.URL, user, user+"-token", "getReplayScripts", nil)
				r.NoError(err)
				r.True(available)
				var scripts struct {
					MainScript string `json:"mainScript"`
				}
				r.NoError(json.Unmarshal(data, &scripts))
				r.Equal(user, scripts.MainScript)
			}
		})
	}
}
