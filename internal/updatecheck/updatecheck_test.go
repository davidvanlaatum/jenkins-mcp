package updatecheck

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/david/jenkins-mcp/internal/config"

	"github.com/stretchr/testify/require"
)

func TestCheckReportsNewerRelease(t *testing.T) {
	r := require.New(t)
	userAgentCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		userAgentCh <- req.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.4","html_url":"https://github.com/example/project/releases/tag/v1.2.4"}`))
	}))
	defer server.Close()

	checker := New(config.UpdateCheckConfig{Enabled: true, SelfUpdateEnabled: true, Repository: "example/project", CheckIntervalHours: 24}, "1.2.3", slog.New(slog.NewTextHandler(io.Discard, nil)))
	checker.releaseURL = server.URL

	result, err := checker.Check(t.Context())
	r.NoError(err, "Check()")
	select {
	case userAgent := <-userAgentCh:
		r.NotEmpty(userAgent, "User-Agent header")
	case <-time.After(time.Second):
		r.Fail("Check() did not request the release endpoint")
	}
	r.True(result.UpdateAvailable, "UpdateAvailable")
	r.Equal("v1.2.4", result.LatestVersion, "LatestVersion")
}

func TestStatusCachesCheckResult(t *testing.T) {
	r := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.4","html_url":"https://github.com/example/project/releases/tag/v1.2.4"}`))
	}))
	defer server.Close()

	checker := New(config.UpdateCheckConfig{Enabled: true, SelfUpdateEnabled: true, Repository: "example/project", CheckIntervalHours: 24}, "1.2.3", slog.New(slog.NewTextHandler(io.Discard, nil)))
	checker.releaseURL = server.URL

	checker.checkAndLog(t.Context())
	status := checker.Status()
	r.True(status.UpdateAvailable, "status.UpdateAvailable")
	r.True(status.SelfUpdateEnabled, "status.SelfUpdateEnabled")
	r.NotEmpty(status.CheckedAt, "status.CheckedAt")
	r.NotEmpty(status.ReleaseURL, "status.ReleaseURL")
	r.NotEmpty(status.NotificationHint, "status.NotificationHint")
}

func TestCheckSkipsDevVersion(t *testing.T) {
	r := require.New(t)
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	checker := New(config.UpdateCheckConfig{Enabled: true, Repository: "example/project", CheckIntervalHours: 24}, "0.1.0-dev", slog.New(slog.NewTextHandler(io.Discard, nil)))
	checker.releaseURL = server.URL

	result, err := checker.Check(t.Context())
	r.NoError(err, "Check()")
	r.False(called, "dev versions should not call the release API")
	r.False(result.UpdateAvailable, "dev versions should not report updates")
}

func TestCheckIgnoresOlderRelease(t *testing.T) {
	r := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.2","html_url":"https://github.com/example/project/releases/tag/v1.2.2"}`))
	}))
	defer server.Close()

	checker := New(config.UpdateCheckConfig{Enabled: true, Repository: "example/project", CheckIntervalHours: 24}, "1.2.3", slog.New(slog.NewTextHandler(io.Discard, nil)))
	checker.releaseURL = server.URL

	result, err := checker.Check(t.Context())
	r.NoError(err, "Check()")
	r.False(result.UpdateAvailable, "UpdateAvailable for older releases")
}
