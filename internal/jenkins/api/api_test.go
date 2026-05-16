package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/stretchr/testify/require"
)

func TestShouldAbortCoverageProbeHonorsCallerContextOnly(t *testing.T) {
	r := require.New(t)

	r.False(shouldAbortCoverageProbe(t.Context(), context.DeadlineExceeded), "per-endpoint timeout should remain optional")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r.True(shouldAbortCoverageProbe(ctx, context.Canceled), "caller cancellation should abort coverage probing")
}

func TestTriggerBuildOmitsJenkinsErrorBody(t *testing.T) {
	r := require.New(t)
	body := "plugin error reflected SECRET_TOKEN_VALUE"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			http.NotFound(w, r)
		case "/job/app/build":
			http.Error(w, body, http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := jenkinsclient.New(config.ControllerConfig{ID: "test", URL: server.URL}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err, "client New()")
	api := New("test", client)

	_, err = api.TriggerBuild(t.Context(), "app", nil)
	r.Error(err, "TriggerBuild() should return an HTTP error")
	appErr, ok := err.(*apperrors.Error)
	r.True(ok, "TriggerBuild() error should be structured")
	r.Equal(apperrors.CodePermissionDenied, appErr.Code, "error code")
	detail, ok := appErr.Detail.(map[string]any)
	r.True(ok, "error detail")
	r.Equal(http.StatusForbidden, detail["status"], "status detail")
	r.NotContains(appErr.Message, "SECRET_TOKEN_VALUE", "message should not include Jenkins response body")
	_, hasExcerpt := detail["bodyExcerpt"]
	r.False(hasExcerpt, "trigger build errors should not expose Jenkins response bodies")
}
