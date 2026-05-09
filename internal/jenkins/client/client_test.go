package client

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/david/jenkins-mcp/internal/config"
)

func TestReadBoundedReturnsErrorWhenLimitExceeded(t *testing.T) {
	_, err := readBounded(strings.NewReader("abcdef"), 5)
	if err == nil {
		t.Fatal("readBounded() succeeded when response exceeded limit")
	}
}

func TestReadBoundedAllowsExactLimit(t *testing.T) {
	got, err := readBounded(strings.NewReader("abcde"), 5)
	if err != nil {
		t.Fatalf("readBounded() error = %v", err)
	}
	if string(got) != "abcde" {
		t.Fatalf("readBounded() = %q", got)
	}
}

func TestDoPreservesControllerBasePath(t *testing.T) {
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	c, err := New(config.ControllerConfig{ID: "work", URL: server.URL + "/team-jenkins/"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	status, _, _, err := c.Do(context.Background(), http.MethodGet, "job/Folder/job/App/api/json", url.Values{"tree": {"name"}}, nil, nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if gotPath != "/team-jenkins/job/Folder/job/App/api/json" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotQuery != "tree=name" {
		t.Fatalf("query = %q", gotQuery)
	}
}

func TestDoPreservesEscapedPathSegments(t *testing.T) {
	var gotPath, gotRequestURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRequestURI = r.RequestURI
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	c, err := New(config.ControllerConfig{ID: "work", URL: server.URL}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	status, _, _, err := c.Do(context.Background(), http.MethodGet, "job/Download%20Debs/api/json", url.Values{"tree": {"name"}}, nil, nil)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if gotPath != "/job/Download Debs/api/json" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotRequestURI != "/job/Download%20Debs/api/json?tree=name" {
		t.Fatalf("request URI = %q", gotRequestURI)
	}
}

func TestEndpointURLBuildsFromEscapedPaths(t *testing.T) {
	c, err := New(config.ControllerConfig{ID: "work", URL: "https://jenkins.example.com/team%20jenkins/"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	got, err := c.endpointURL("job/Folder%20A/job/Download%20Debs/api/json", url.Values{"tree": {"name"}})
	if err != nil {
		t.Fatalf("endpointURL() error = %v", err)
	}

	want := "https://jenkins.example.com/team%20jenkins/job/Folder%20A/job/Download%20Debs/api/json?tree=name"
	if got.String() != want {
		t.Fatalf("endpointURL() = %q, want %q", got.String(), want)
	}
}

func TestEndpointURLRejectsInvalidEscapedPath(t *testing.T) {
	c, err := New(config.ControllerConfig{ID: "work", URL: "https://jenkins.example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := c.endpointURL("job/bad%zz/api/json", nil); err == nil {
		t.Fatal("endpointURL() accepted an invalid escaped path")
	}
}
