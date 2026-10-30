package api

import (
	"encoding/json"
	"net/http"
	"testing"

	apperrors "github.com/david/jenkins-mcp/internal/errors"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/stretchr/testify/require"
)

const pendingInputJSON = `{"actions":[{}, {"executions":[{"id":"approve","input":{"message":"Continue?","parameters":[{"_class":"hudson.model.BooleanParameterDefinition","name":"CONFIRM"},{"_class":"hudson.model.PasswordParameterDefinition","name":"SECRET"}]}}]}]}`

func TestRespondPipelineInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		abort  bool
		status int
		code   apperrors.Code
	}{
		{"proceed", false, 200, ""}, {"abort", true, 200, ""}, {"missing", false, 404, apperrors.CodeNotFound}, {"permission", false, 403, apperrors.CodePermissionDenied}, {"server failure", false, 500, apperrors.CodeJenkins}, {"redirect", false, 302, apperrors.CodeJenkins},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			posts := 0
			api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/job/folder/job/app/3/api/json":
					r.NotContains(req.URL.Query().Get("tree"), "defaultParameterValue")
					_, _ = w.Write([]byte(pendingInputJSON))
				case "/crumbIssuer/api/json":
					http.NotFound(w, req)
				case "/job/folder/job/app/3/input/approve/proceed", "/job/folder/job/app/3/input/approve/abort":
					posts++
					r.Equal(http.MethodPost, req.Method)
					if !tc.abort {
						r.NoError(req.ParseForm())
						var form struct {
							Parameter []struct {
								Name  string
								Value any
							}
						}
						r.NoError(json.Unmarshal([]byte(req.Form.Get("json")), &form))
						r.Len(form.Parameter, 2)
						r.Equal(true, form.Parameter[0].Value)
						r.Equal("secret-value", form.Parameter[1].Value)
					}
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte("SECRET_RESPONSE"))
				default:
					t.Errorf("unexpected request %s", req.URL)
					http.NotFound(w, req)
				}
			})
			got, err := api.RespondPipelineInput(ctx, "folder/app", 3, "approve", tc.abort, map[string]any{"CONFIRM": true, "SECRET": "secret-value"})
			if tc.code == "" {
				r.NoError(err)
				r.True(got.Accepted)
			} else {
				var e *apperrors.Error
				r.ErrorAs(err, &e)
				r.Equal(tc.code, e.Code)
				r.NotContains(err.Error(), "SECRET_RESPONSE")
			}
			// The shared HTTP client may refresh a crumb after a 403; operational failures are never retried.
			if tc.status != 403 {
				r.Equal(1, posts)
			}
		})
	}
}

func TestPipelineInputValues(t *testing.T) {
	input := model.PipelineInput{Parameters: []model.PipelineInputParameter{{Name: "flag", Type: "boolean", Supported: true}, {Name: "region", Type: "choice", Choices: []string{"east", "west"}, Supported: true}}}
	for _, tc := range []struct {
		name   string
		values map[string]any
		ok     bool
	}{
		{"valid", map[string]any{"flag": false, "region": "east"}, true},
		{"missing", map[string]any{"flag": true}, false},
		{"extra", map[string]any{"flag": true, "region": "east", "other": "value"}, false},
		{"string boolean", map[string]any{"flag": "true", "region": "east"}, false},
		{"bad choice", map[string]any{"flag": true, "region": "secret-value"}, false},
		{"null", map[string]any{"flag": true, "region": nil}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			_, err := pipelineInputValues(input, tc.values)
			if tc.ok {
				r.NoError(err)
			} else {
				r.Error(err)
				r.NotContains(err.Error(), "secret-value")
			}
		})
	}
	r := require.New(t)
	_, err := pipelineInputValues(model.PipelineInput{Parameters: []model.PipelineInputParameter{{Name: "file", Type: "unsupported"}}}, map[string]any{"file": "data"})
	var e *apperrors.Error
	r.ErrorAs(err, &e)
	r.Equal(apperrors.CodeUnsupported, e.Code)
}

func TestPipelineInputSelectionPreventsMutation(t *testing.T) {
	for _, id := range []string{"", "..", "../approve", "x%2fy", "x/y", "x\\y", "x?y", "x#y", "missing"} {
		t.Run(id, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
				r.Equal(http.MethodGet, req.Method)
				_, _ = w.Write([]byte(pendingInputJSON))
			})
			got, err := api.RespondPipelineInput(ctx, "app", 1, id, false, nil)
			r.Error(err)
			r.False(got.Accepted)
		})
	}
}

func TestPipelineInputApprovalOnly(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	posts := 0
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/job/app/1/api/json":
			_, _ = w.Write([]byte(`{"actions":[{"executions":[{"id":"approve","input":{"message":"OK?","parameters":[]}}]}]}`))
		case "/crumbIssuer/api/json":
			http.NotFound(w, req)
		case "/job/app/1/input/approve/proceedEmpty":
			posts++
			r.Equal(http.MethodPost, req.Method)
		default:
			t.Errorf("unexpected request %s", req.URL)
		}
	})
	got, err := api.RespondPipelineInput(ctx, "app", 1, "approve", false, nil)
	r.NoError(err)
	r.True(got.Accepted)
	r.Equal(1, posts)
}

func TestPipelineInputMalformedMetadataPreventsMutation(t *testing.T) {
	for _, body := range []string{
		`{"actions":[{"executions":[{"id":"Approve"}]}]}`,
		`{"actions":[{"executions":[{"id":"Approve","input":{"message":"Continue?"}}]}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			ctx, _, _ := testhandler.SetupTestHandler(t)
			r := require.New(t)
			api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
				r.Equal(http.MethodGet, req.Method)
				_, _ = w.Write([]byte(body))
			})
			got, err := api.RespondPipelineInput(ctx, "app", 1, "Approve", false, nil)
			r.Error(err)
			r.False(got.Accepted)
		})
	}
}
