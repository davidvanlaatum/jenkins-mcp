package jenkins

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestPollSCMValidation(t *testing.T) {
	for _, tc := range []struct {
		name, job, controller string
		enabled               bool
		code                  apperrors.Code
	}{
		{"disabled mutations", "app", "", false, apperrors.CodeMutationDisabled},
		{"empty job", "", "", true, apperrors.CodeInvalidRequest},
		{"blank job", "  ", "", true, apperrors.CodeInvalidRequest},
		{"unknown controller", "app", "missing", true, apperrors.CodeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loggerCtx, _, _ := testhandler.SetupTestHandler(t)

			r := require.New(t)
			deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, req *http.Request) {
				t.Errorf("validation must prevent HTTP requests")
			})
			deps.Config.Mutations.Enabled = tc.enabled
			got, err := PollSCM(loggerCtx, deps, PollSCMRequest{Job: tc.job, Controller: tc.controller})
			var appErr *apperrors.Error
			r.ErrorAs(err, &appErr)
			r.Equal(tc.code, appErr.Code)
			r.False(got.Accepted)
		})
	}
}

func TestPollSCMAuditsAttempt(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			loggerCtx, _, _ := testhandler.SetupTestHandler(t)

			r := require.New(t)
			deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/job/app/api/json":
					writeJSON(w, `{"name":"app"}`)
				case "/job/app/polling":
					w.WriteHeader(status)
				default:
					http.NotFound(w, req)
				}
			})
			deps.Config.Mutations.Enabled = true
			path := filepath.Join(t.TempDir(), "audit.jsonl")
			var err error
			deps.Audit, err = audit.New(config.AuditConfig{Path: path})
			r.NoError(err)
			got, err := PollSCM(loggerCtx, deps, PollSCMRequest{Job: "app"})
			outcome := "success"
			if status == http.StatusFound {
				r.NoError(err)
				r.True(got.Accepted)
			} else {
				r.Error(err)
				r.False(got.Accepted)
				outcome = "error"
			}
			data, err := os.ReadFile(path)
			r.NoError(err)
			var event audit.Event
			r.NoError(json.Unmarshal(data, &event))
			r.Equal("poll_scm", event.Action)
			r.Equal("default", event.Controller)
			r.Equal("app", event.Target)
			r.Equal(outcome, event.Outcome)
		})
	}
}
