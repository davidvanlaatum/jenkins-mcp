package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
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

func TestReplayScriptsFetchesNativeReplayAction(t *testing.T) {
	r := require.New(t)
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/job/app/7/replay/api/json" {
			http.NotFound(w, req)
			return
		}
		r.Contains(req.URL.Query().Get("tree"), "originalScript", "tree")
		writeAPIJSON(w, `{
			"originalScript": "pipeline { echo 'main' }",
			"originalLoadedScripts": {
				"Script1.groovy": "echo 'loaded'"
			},
			"enabled": true,
			"rebuildEnabled": true
		}`)
	})

	mainScript, loadedScripts, enabled, rebuildEnabled, err := api.ReplayScripts(t.Context(), "app", 7)
	r.NoError(err, "ReplayScripts() error")
	r.Equal("pipeline { echo 'main' }", mainScript, "main script")
	r.Equal("echo 'loaded'", loadedScripts["Script1.groovy"], "loaded script")
	r.True(enabled, "enabled")
	r.True(rebuildEnabled, "rebuild enabled")
}

func TestReplayBuildSubmitsNativeReplayJSONForm(t *testing.T) {
	r := require.New(t)
	var gotJSON string
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/crumbIssuer/api/json":
			http.NotFound(w, req)
		case "/job/app/7/replay/run":
			err := req.ParseForm()
			r.NoError(err, "ParseForm()")
			gotJSON = req.Form.Get("json")
			w.Header().Set("Location", "https://jenkins.example.com/job/app/")
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, req)
		}
	})

	redirect, err := api.ReplayBuild(t.Context(), "app", 7, "pipeline { echo 'new' }", map[string]string{"Script1.groovy": "echo 'loaded'"}, false)
	r.NoError(err, "ReplayBuild() error")
	r.Equal("https://jenkins.example.com/job/app/", redirect, "redirect")
	var form map[string]string
	err = json.Unmarshal([]byte(gotJSON), &form)
	r.NoError(err, "unmarshal replay json form")
	r.Equal("pipeline { echo 'new' }", form["mainScript"], "mainScript form field")
	r.Equal("echo 'loaded'", form["Script1_groovy"], "loaded script form field should use Jenkins sanitized identifier")
}

func TestReplayBuildUsesNativeRebuildEndpointForUnchangedReplay(t *testing.T) {
	r := require.New(t)
	var gotPath string
	api := newTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crumbIssuer/api/json":
			http.NotFound(w, r)
		case "/job/app/7/replay/rebuild":
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	_, err := api.ReplayBuild(t.Context(), "app", 7, "", nil, true)
	r.NoError(err, "ReplayBuild() error")
	r.Equal("/job/app/7/replay/rebuild", gotPath, "rebuild path")
}

func TestTestReportFiltersCasesBeforeLimit(t *testing.T) {
	r := require.New(t)
	api := newTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/job/app/7/testReport/api/json" {
			http.NotFound(w, r)
			return
		}
		writeAPIJSON(w, `{
			"totalCount": 4,
			"failCount": 1,
			"skipCount": 0,
			"passCount": 3,
			"suites": [{
				"name": "example.MixedTest",
				"cases": [
					{"className":"example.MixedTest","name":"failsFirst","status":"FAILED","duration":0.1,"errorDetails":"boom"},
					{"className":"example.MixedTest","name":"passesOne","status":"PASSED","duration":0.2},
					{"className":"example.MixedTest","name":"passesTwo","status":"PASSED","duration":0.3},
					{"className":"example.MixedTest","name":"passesThree","status":"PASSED","duration":0.4}
				]
			}]
		}`)
	})

	got, err := api.TestReport(t.Context(), "app", 7, model.TestCaseFilter{Status: "PASSED"}, 2)
	r.NoError(err, "TestReport() error")
	r.Equal(4, got.TotalCount, "summary total should remain full report count")
	r.True(got.Truncated, "third matching passed case should be truncated")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 2, "limit should apply after filtering")
	r.Equal("passesOne", got.Suites[0].Cases[0].Name, "first returned case")
	r.Equal("passesTwo", got.Suites[0].Cases[1].Name, "second returned case")
}

func TestTestReportFiltersByTextRegexAndDuration(t *testing.T) {
	r := require.New(t)
	api := newTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/job/app/8/testReport/api/json" {
			http.NotFound(w, r)
			return
		}
		writeAPIJSON(w, `{
			"totalCount": 3,
			"failCount": 2,
			"skipCount": 1,
			"passCount": 0,
			"suites": [{
				"name": "AuthServiceSuite",
				"cases": [
					{"className":"example.AuthServiceTest","name":"LoginTimeout","status":"FAILED","duration":1.5,"errorDetails":"Database timeout","errorStackTrace":"TimeoutException"},
					{"className":"example.AuthServiceTest","name":"LogoutTimeout","status":"FAILED","duration":3.5,"errorDetails":"Database timeout","errorStackTrace":"TimeoutException"}
				]
			}, {
				"name": "BillingSuite",
				"cases": [
					{"className":"example.BillingTest","name":"skipsBilling","status":"SKIPPED","duration":0.01}
				]
			}]
		}`)
	})
	minMillis := int64(1000)
	maxMillis := int64(2000)

	got, err := api.TestReport(t.Context(), "app", 8, model.TestCaseFilter{
		Status:                  "failed",
		SuiteNameContains:       "auth",
		CaseNameRegex:           "^Login",
		ClassNameContains:       "service",
		DurationMillisMin:       &minMillis,
		DurationMillisMax:       &maxMillis,
		ErrorDetailsContains:    "DATABASE",
		ErrorStackTraceContains: "timeoutexception",
	}, 50)
	r.NoError(err, "TestReport() error")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 1, "filtered cases")
	r.Equal("LoginTimeout", got.Suites[0].Cases[0].Name, "matching case")
}

func TestTestReportFiltersByExactSuiteClassAndCase(t *testing.T) {
	r := require.New(t)
	api := newTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/job/app/9/testReport/api/json" {
			http.NotFound(w, r)
			return
		}
		writeAPIJSON(w, `{
			"totalCount": 3,
			"failCount": 2,
			"skipCount": 0,
			"passCount": 1,
			"suites": [{
				"name": "AuthSuite",
				"cases": [
					{"className":"example.AuthTest","name":"Login","status":"FAILED","duration":0.1},
					{"className":"example.AuthTest","name":"Logout","status":"FAILED","duration":0.1}
				]
			}, {
				"name": "OtherSuite",
				"cases": [
					{"className":"example.AuthTest","name":"Login","status":"PASSED","duration":0.1}
				]
			}]
		}`)
	})

	got, err := api.TestReport(t.Context(), "app", 9, model.TestCaseFilter{
		SuiteName: "AuthSuite",
		ClassName: "example.AuthTest",
		CaseName:  "Login",
	}, 50)
	r.NoError(err, "TestReport() error")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 1, "case count")
	r.Equal("AuthSuite", got.Suites[0].Name, "suite name")
	r.Equal("Login", got.Suites[0].Cases[0].Name, "case name")
}

func TestCompactTestReportUsesTreeAndOmitsFailureText(t *testing.T) {
	r := require.New(t)
	var tree string
	api := newTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/job/app/10/testReport/api/json" {
			http.NotFound(w, r)
			return
		}
		tree = r.URL.Query().Get("tree")
		writeAPIJSON(w, `{
			"totalCount": 1,
			"failCount": 1,
			"skipCount": 0,
			"passCount": 0,
			"suites": [{
				"name": "AuthSuite",
				"cases": [
					{"className":"example.AuthTest","name":"Login","status":"FAILED","duration":0.1,"errorDetails":"secret","errorStackTrace":"stack"}
				]
			}]
		}`)
	})

	got, err := api.CompactTestReport(t.Context(), "app", 10)
	r.NoError(err, "CompactTestReport() error")
	r.Equal("totalCount,failCount,skipCount,passCount,suites[name,cases[className,name,status,duration]]", tree, "tree query")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 1, "case count")
	r.Empty(got.Suites[0].Cases[0].ErrorDetails, "error details")
	r.Empty(got.Suites[0].Cases[0].ErrorStackTrace, "error stack")
}

func TestTestReportRejectsInvalidRegex(t *testing.T) {
	r := require.New(t)
	api := newTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	_, err := api.TestReport(t.Context(), "app", 1, model.TestCaseFilter{SuiteNameRegex: "["}, 50)
	r.Error(err, "TestReport() accepted invalid regex")
	appErr, ok := err.(*apperrors.Error)
	r.True(ok, "error type")
	r.Equal(apperrors.CodeInvalidRequest, appErr.Code, "error code")
}

func newTestAPI(t *testing.T, handler http.HandlerFunc) *API {
	t.Helper()
	r := require.New(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := jenkinsclient.New(config.ControllerConfig{ID: "test", URL: server.URL}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err, "client New()")
	return New("test", client)
}

func writeAPIJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, body)
}
