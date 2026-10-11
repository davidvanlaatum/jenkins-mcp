package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	apperrors "github.com/david/jenkins-mcp/internal/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Keep the same full-source ceiling as the native Jenkins HTTP client. Tool-level
// truncation happens later, so digests always cover the complete source.
const maxMCPResponseBytes = 8 * 1024 * 1024

// CallTool returns available=false only when the endpoint or named tool is
// absent. Sessions are scoped to a single operation and never shared across
// controllers, credentials, or calls.
func CallTool(ctx context.Context, policy *http.Client, endpoint, username, token, name string, arguments any) (json.RawMessage, bool, error) {
	timeout := policy.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	base := policy.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	transport := &mcpHTTPTransport{base: base, username: username, token: token}
	httpClient := *policy
	httpClient.Transport = transport
	// Do not follow login redirects or forward controller credentials to a
	// redirected endpoint. A redirect is an operational error, not absence.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// The Jenkins plugin uses the legacy initialize handshake. Pin its supported
	// version so newer SDK discovery probes cannot obscure endpoint absence.
	session, err := mcp.NewClient(&mcp.Implementation{Name: "jenkins-mcp", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: endpoint, HTTPClient: &httpClient, DisableStandaloneSSE: true,
		MaxRetries: -1, MaxEventSize: maxMCPResponseBytes,
	}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-06-18"})
	if err != nil {
		if transport.endpointAbsent() {
			return nil, false, nil
		}
		return nil, true, transport.normalize(err)
	}
	defer func() { _ = session.Close() }()
	params := &mcp.ListToolsParams{}
	// Bound discovery even if an upstream repeatedly advertises a cursor.
	for page := 0; page < 100; page++ {
		list, err := session.ListTools(ctx, params)
		if err != nil {
			return nil, true, transport.normalize(err)
		}
		for _, tool := range list.Tools {
			if tool.Name == name {
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
				if err != nil {
					return nil, true, transport.normalize(err)
				}
				if result.IsError {
					// Never copy upstream tool text (which may contain scripts or secrets)
					// into the public normalized error.
					return nil, true, apperrors.Wrap(apperrors.CodeJenkins, "Jenkins MCP tool failed", map[string]any{"tool": name})
				}
				if result.StructuredContent == nil {
					return nil, true, apperrors.Wrap(apperrors.CodeJenkins, "Jenkins MCP tool returned no structured content", map[string]any{"tool": name})
				}
				data, err := json.Marshal(result.StructuredContent)
				if err != nil {
					return nil, true, apperrors.Wrap(apperrors.CodeJenkins, "invalid Jenkins MCP structured content", map[string]any{"tool": name})
				}
				return data, true, nil
			}
		}
		if list.NextCursor == "" {
			return nil, false, nil
		}
		params.Cursor = list.NextCursor
	}
	return nil, true, apperrors.Wrap(apperrors.CodeJenkins, "Jenkins MCP discovery exceeded page limit", map[string]any{"maxPages": 100})
}

type mcpHTTPTransport struct {
	base            http.RoundTripper
	username, token string
	mu              sync.Mutex
	status          int
	failure         error
	requests        int
	absent          bool
}

func (t *mcpHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests++
	firstRequest := t.requests == 1
	t.mu.Unlock()
	req = req.Clone(req.Context())
	if t.username != "" || t.token != "" {
		req.SetBasicAuth(t.username, t.token)
	}
	res, err := t.base.RoundTrip(req)
	if err != nil {
		t.recordFailure(apperrors.Wrap(apperrors.CodeUnavailable, "Jenkins MCP connection failed", nil))
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		t.mu.Lock()
		t.status = res.StatusCode
		if firstRequest && res.StatusCode == http.StatusNotFound {
			t.absent = true
		}
		t.mu.Unlock()
		_ = res.Body.Close()
		return nil, apperrors.Wrap(apperrors.CodeJenkins, "Jenkins MCP HTTP request failed", map[string]any{"status": res.StatusCode})
	}
	res.Body = &mcpBoundedBody{ReadCloser: res.Body, remaining: maxMCPResponseBytes, onFailure: t.recordFailure}
	return res, nil
}

func (t *mcpHTTPTransport) endpointAbsent() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.absent
}

func (t *mcpHTTPTransport) recordFailure(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failure == nil {
		t.failure = err
	}
}

func (t *mcpHTTPTransport) statusCode() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

func (t *mcpHTTPTransport) normalize(err error) error {
	if e, ok := apperrors.FromContext(err); ok {
		return e
	}
	if status := t.statusCode(); status != 0 {
		code := apperrors.CodeJenkins
		switch status {
		case http.StatusUnauthorized, http.StatusForbidden:
			code = apperrors.CodePermissionDenied
		case http.StatusNotFound:
			code = apperrors.CodeNotFound
		}
		return apperrors.Wrap(code, "Jenkins MCP HTTP request failed", map[string]any{"status": status})
	}
	t.mu.Lock()
	failure := t.failure
	t.mu.Unlock()
	if failure != nil {
		return failure
	}
	var appErr *apperrors.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return apperrors.Wrap(apperrors.CodeJenkins, "invalid Jenkins MCP response", nil)
}

type mcpBoundedBody struct {
	io.ReadCloser
	remaining int64
	onFailure func(error)
}

func (b *mcpBoundedBody) Read(p []byte) (int, error) {
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		err = apperrors.Wrap(apperrors.CodeJenkins, "Jenkins MCP response exceeded maximum body size", map[string]any{"maxBytes": maxMCPResponseBytes})
		b.onFailure(err)
		return 0, err
	}
	return n, err
}
