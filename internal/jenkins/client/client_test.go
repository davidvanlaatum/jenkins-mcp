package client

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/david/jenkins-mcp/internal/config"

	"github.com/stretchr/testify/require"
)

func TestReadBoundedReturnsErrorWhenLimitExceeded(t *testing.T) {
	r := require.New(t)

	_, err := readBounded(strings.NewReader("abcdef"), 5)
	r.Error(err, "readBounded() should fail when response exceeds limit")
}

func TestReadBoundedAllowsExactLimit(t *testing.T) {
	r := require.New(t)

	got, err := readBounded(strings.NewReader("abcde"), 5)
	r.NoError(err, "readBounded()")
	r.Equal("abcde", string(got), "readBounded()")
}

func TestDoPreservesControllerBasePath(t *testing.T) {
	r := require.New(t)
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	c, err := New(config.ControllerConfig{ID: "work", URL: server.URL + "/team-jenkins/"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err, "New()")
	status, _, _, err := c.Do(t.Context(), http.MethodGet, "job/Folder/job/App/api/json", url.Values{"tree": {"name"}}, nil, nil)
	r.NoError(err, "Do()")
	r.Equal(http.StatusOK, status, "status")
	r.Equal("/team-jenkins/job/Folder/job/App/api/json", gotPath, "path")
	r.Equal("tree=name", gotQuery, "query")
}

func TestDoPreservesEscapedPathSegments(t *testing.T) {
	r := require.New(t)
	var gotPath, gotRequestURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRequestURI = r.RequestURI
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	c, err := New(config.ControllerConfig{ID: "work", URL: server.URL}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err, "New()")
	status, _, _, err := c.Do(t.Context(), http.MethodGet, "job/Download%20Debs/api/json", url.Values{"tree": {"name"}}, nil, nil)
	r.NoError(err, "Do()")
	r.Equal(http.StatusOK, status, "status")
	r.Equal("/job/Download Debs/api/json", gotPath, "path")
	r.Equal("/job/Download%20Debs/api/json?tree=name", gotRequestURI, "request URI")
}

func TestEndpointURLBuildsFromEscapedPaths(t *testing.T) {
	r := require.New(t)

	c, err := New(config.ControllerConfig{ID: "work", URL: "https://jenkins.example.com/team%20jenkins/"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err, "New()")

	got, err := c.endpointURL("job/Folder%20A/job/Download%20Debs/api/json", url.Values{"tree": {"name"}})
	r.NoError(err, "endpointURL()")

	want := "https://jenkins.example.com/team%20jenkins/job/Folder%20A/job/Download%20Debs/api/json?tree=name"
	r.Equal(want, got.String(), "endpointURL()")
}

func TestEndpointURLRejectsInvalidEscapedPath(t *testing.T) {
	r := require.New(t)

	c, err := New(config.ControllerConfig{ID: "work", URL: "https://jenkins.example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err, "New()")

	_, err = c.endpointURL("job/bad%zz/api/json", nil)
	r.Error(err, "endpointURL() should reject an invalid escaped path")
}
