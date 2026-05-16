package client

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestDoAllowsConcurrentRequests(t *testing.T) {
	r := require.New(t)
	entered := make(chan string, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		entered <- req.URL.Path
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	c, err := New(config.ControllerConfig{ID: "work", URL: server.URL}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err, "New()")

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, path := range []string{"api/json", "job/app/api/json"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, err := c.GetText(ctx, path, nil)
			errs <- err
		}()
	}

	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			r.FailNow("timed out waiting for concurrent requests to reach server")
		}
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		r.NoError(err, "GetText()")
	}
}

func TestPostPreservesCrumbSessionCookie(t *testing.T) {
	r := require.New(t)
	var gotCookie string
	var gotCrumb string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/crumbIssuer/api/json":
			http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "crumb-session", Path: "/"})
			_, _ = w.Write([]byte(`{"crumbRequestField":"Jenkins-Crumb","crumb":"crumb-value"}`))
		case "/job/app/build":
			if cookie, err := req.Cookie("JSESSIONID"); err == nil {
				gotCookie = cookie.Value
			}
			gotCrumb = req.Header.Get("Jenkins-Crumb")
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()

	c, err := New(config.ControllerConfig{ID: "work", URL: server.URL}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err, "New()")
	status, _, _, err := c.Post(t.Context(), "job/app/build", nil, nil)
	r.NoError(err, "Post()")
	r.Equal(http.StatusCreated, status, "status")
	r.Equal("crumb-session", gotCookie, "crumb session cookie")
	r.Equal("crumb-value", gotCrumb, "crumb header")
}

func TestPostRefreshesCrumbSessionAfterForbidden(t *testing.T) {
	r := require.New(t)
	var crumbRequests int
	var buildRequests int
	var gotRetryCookie string
	var gotRetryCrumb string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/jenkins/crumbIssuer/api/json":
			crumbRequests++
			switch crumbRequests {
			case 1:
				http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "stale-session", Path: "/jenkins"})
				_, _ = w.Write([]byte(`{"crumbRequestField":"Jenkins-Crumb","crumb":"stale-crumb"}`))
			default:
				http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: "fresh-session", Path: "/jenkins"})
				_, _ = w.Write([]byte(`{"crumbRequestField":"Jenkins-Crumb","crumb":"fresh-crumb"}`))
			}
		case "/jenkins/job/app/build":
			buildRequests++
			if buildRequests == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			if cookie, err := req.Cookie("JSESSIONID"); err == nil {
				gotRetryCookie = cookie.Value
			}
			gotRetryCrumb = req.Header.Get("Jenkins-Crumb")
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()

	c, err := New(config.ControllerConfig{ID: "work", URL: server.URL + "/jenkins"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err, "New()")
	status, _, _, err := c.Post(t.Context(), "job/app/build", nil, nil)
	r.NoError(err, "Post()")
	r.Equal(http.StatusCreated, status, "status")
	r.Equal(2, crumbRequests, "crumb requests")
	r.Equal(2, buildRequests, "build requests")
	r.Equal("fresh-session", gotRetryCookie, "retry crumb session cookie")
	r.Equal("fresh-crumb", gotRetryCrumb, "retry crumb header")
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
