package api

import (
	"net/http"
	"testing"

	"github.com/david/jenkins-mcp/internal/testutil"

	apperrors "github.com/david/jenkins-mcp/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestPollSCM(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		jobStatus, pollStatus int
		code                  apperrors.Code
	}{
		{"accepted", 200, 302, ""},
		{"missing job", 404, 0, apperrors.CodeNotFound},
		{"unsupported job", 200, 404, apperrors.CodeUnsupported},
		{"permission denied", 200, 403, apperrors.CodePermissionDenied},
		{"server error", 200, 500, apperrors.CodeJenkins},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			polls := 0
			api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/job/folder/job/app/api/json":
					r.Equal(http.MethodGet, req.Method)
					w.WriteHeader(tc.jobStatus)
					_, _ = w.Write([]byte(`{"name":"app"}`))
				case "/crumbIssuer/api/json":
					_, _ = w.Write([]byte(`{"crumbRequestField":"Jenkins-Crumb","crumb":"test-crumb"}`))
				case "/job/folder/job/app/":
					r.Equal(http.MethodGet, req.Method)
					_, _ = w.Write([]byte("job page"))
				case "/job/folder/job/app/polling":
					polls++
					r.Equal(http.MethodPost, req.Method)
					r.Equal("test-crumb", req.Header.Get("Jenkins-Crumb"))
					w.Header().Set("Location", ".")
					w.WriteHeader(tc.pollStatus)
					_, _ = w.Write([]byte("SECRET_RESPONSE_BODY"))
				default:
					t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
					http.NotFound(w, req)
				}
			})
			got, err := api.PollSCM(testutil.Context(t), "folder/app")
			if tc.code == "" {
				r.NoError(err)
				r.True(got.Accepted)
				r.Equal(1, polls)
			} else {
				var appErr *apperrors.Error
				r.ErrorAs(err, &appErr)
				r.Equal(tc.code, appErr.Code)
				r.False(got.Accepted)
				if tc.jobStatus == 404 {
					r.Zero(polls, "missing jobs must not be polled")
				} else {
					r.NotContains(err.Error(), "SECRET_RESPONSE_BODY")
				}
			}
		})
	}
}
