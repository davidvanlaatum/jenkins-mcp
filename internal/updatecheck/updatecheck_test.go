package updatecheck

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/david/jenkins-mcp/internal/config"
)

func TestCheckReportsNewerRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent header")
		}
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.4","html_url":"https://github.com/example/project/releases/tag/v1.2.4"}`))
	}))
	defer server.Close()

	checker := New(config.UpdateCheckConfig{Enabled: true, Repository: "example/project", CheckIntervalHours: 24}, "1.2.3", slog.New(slog.NewTextHandler(io.Discard, nil)))
	checker.releaseURL = server.URL

	result, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !result.UpdateAvailable {
		t.Fatal("UpdateAvailable should be true")
	}
	if result.LatestVersion != "v1.2.4" {
		t.Fatalf("LatestVersion = %q", result.LatestVersion)
	}
}

func TestStatusCachesCheckResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.4","html_url":"https://github.com/example/project/releases/tag/v1.2.4"}`))
	}))
	defer server.Close()

	checker := New(config.UpdateCheckConfig{Enabled: true, Repository: "example/project", CheckIntervalHours: 24}, "1.2.3", slog.New(slog.NewTextHandler(io.Discard, nil)))
	checker.releaseURL = server.URL

	checker.checkAndLog(context.Background())
	status := checker.Status()
	if !status.UpdateAvailable {
		t.Fatal("status.UpdateAvailable should be true")
	}
	if status.CheckedAt == "" {
		t.Fatal("status.CheckedAt should be populated")
	}
	if status.ReleaseURL == "" {
		t.Fatal("status.ReleaseURL should be populated")
	}
	if status.NotificationHint == "" {
		t.Fatal("status.NotificationHint should be populated")
	}
}

func TestCheckSkipsDevVersion(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	checker := New(config.UpdateCheckConfig{Enabled: true, Repository: "example/project", CheckIntervalHours: 24}, "0.1.0-dev", slog.New(slog.NewTextHandler(io.Discard, nil)))
	checker.releaseURL = server.URL

	result, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if called {
		t.Fatal("dev versions should not call the release API")
	}
	if result.UpdateAvailable {
		t.Fatal("dev versions should not report updates")
	}
}

func TestCheckIgnoresOlderRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.2","html_url":"https://github.com/example/project/releases/tag/v1.2.2"}`))
	}))
	defer server.Close()

	checker := New(config.UpdateCheckConfig{Enabled: true, Repository: "example/project", CheckIntervalHours: 24}, "1.2.3", slog.New(slog.NewTextHandler(io.Discard, nil)))
	checker.releaseURL = server.URL

	result, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.UpdateAvailable {
		t.Fatal("UpdateAvailable should be false for older releases")
	}
}
