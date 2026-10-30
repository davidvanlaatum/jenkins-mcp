package jenkins

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/require"
)

func TestPipelineInputMutationValidation(t *testing.T) {
	for _, tc := range []struct {
		name, job, id, controller string
		build                     int
		enabled                   bool
		code                      apperrors.Code
	}{
		{"disabled", "app", "Approve", "", 1, false, apperrors.CodeMutationDisabled},
		{"job", "", "Approve", "", 1, true, apperrors.CodeInvalidRequest},
		{"build", "app", "Approve", "", 0, true, apperrors.CodeInvalidRequest},
		{"identifier", "app", "../bad", "", 1, true, apperrors.CodeInvalidRequest},
		{"controller", "app", "Approve", "unknown", 1, true, apperrors.CodeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, req *http.Request) { t.Error("validation must prevent HTTP requests") })
			deps.Config.Mutations.Enabled = tc.enabled
			in := PipelineInputRequest{Job: tc.job, Build: tc.build, InputID: tc.id, Controller: tc.controller}
			for _, abort := range []bool{false, true} {
				_, err := respondPipelineInput(ctx, deps, in, abort, nil)
				var e *apperrors.Error
				r.ErrorAs(err, &e)
				r.Equal(tc.code, e.Code)
			}
		})
	}
}

func TestPipelineInputAuditsAttempt(t *testing.T) {
	for _, status := range []int{200, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/job/app/1/api/json":
					writeJSON(w, `{"actions":[{"executions":[{"id":"Approve","input":{"message":"Continue?","parameters":[{"_class":"hudson.model.PasswordParameterDefinition","name":"arbitrary"}]}}]}]}`)
				case "/job/app/1/input/Approve/proceed", "/job/app/1/input/Approve/abort":
					w.WriteHeader(status)
				default:
					http.NotFound(w, req)
				}
			})
			deps.Config.Mutations.Enabled = true
			for _, abort := range []bool{false, true} {
				path := filepath.Join(t.TempDir(), "audit.jsonl")
				var err error
				deps.Audit, err = audit.New(config.AuditConfig{Path: path})
				r.NoError(err)
				got, err := respondPipelineInput(ctx, deps, PipelineInputRequest{Job: "app", Build: 1, InputID: "Approve"}, abort, map[string]any{"arbitrary": "private-value"})
				if status == 200 {
					r.NoError(err)
					r.True(got.Accepted)
				} else {
					r.Error(err)
					r.False(got.Accepted)
				}
				data, err := os.ReadFile(path)
				r.NoError(err)
				r.NotContains(string(data), "private-value")
				var event audit.Event
				r.NoError(json.Unmarshal(data, &event))
				action := "proceed_pipeline_input"
				if abort {
					action = "abort_pipeline_input"
				}
				r.Equal(action, event.Action)
				r.Equal("app#1/input/Approve", event.Target)
				outcome := "success"
				if status == 500 {
					outcome = "error"
				}
				r.Equal(outcome, event.Outcome)
			}
		})
	}
}
