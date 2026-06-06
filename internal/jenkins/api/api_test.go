package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
		if req.URL.Path != "/job/app/7/replay/" {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write([]byte(`
<html>
  <body data-model-type="org.jenkinsci.plugins.workflow.cps.replay.ReplayAction">
    <form method="POST" action="run">
      <textarea name="_.mainScript">pipeline { echo &#39;main&#39; }</textarea>
      <textarea name="_.Script1_groovy">echo &#39;loaded&#39;</textarea>
    </form>
  </body>
</html>`))
	})

	mainScript, loadedScripts, enabled, rebuildEnabled, err := api.ReplayScripts(t.Context(), "app", 7)
	r.NoError(err, "ReplayScripts() error")
	r.Equal("pipeline { echo 'main' }", mainScript, "main script")
	r.Equal("echo 'loaded'", loadedScripts["Script1_groovy"], "loaded script")
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
	var tree string
	api := newTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/job/app/7/testReport/api/json" {
			http.NotFound(w, r)
			return
		}
		tree = r.URL.Query().Get("tree")
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
	r.Equal("totalCount,failCount,skipCount,passCount,suites[name,cases[className,name,safeName,status,duration]]", tree, "tree query")
	r.Equal(4, got.TotalCount, "summary total should remain full report count")
	r.False(got.FailureDetailsIncluded, "broad report should omit failure details")
	r.True(got.Truncated, "third matching passed case should be truncated")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 2, "limit should apply after filtering")
	r.Equal("passesOne", got.Suites[0].Cases[0].Name, "first returned case")
	r.Equal("passesTwo", got.Suites[0].Cases[1].Name, "second returned case")
}

func TestTestReportFiltersByRegexAndDuration(t *testing.T) {
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
		Status:            "failed",
		SuiteNameContains: "auth",
		CaseNameRegex:     "^Login",
		ClassNameContains: "service",
		DurationMillisMin: &minMillis,
		DurationMillisMax: &maxMillis,
	}, 50)
	r.NoError(err, "TestReport() error")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 1, "filtered cases")
	r.Equal("LoginTimeout", got.Suites[0].Cases[0].Name, "matching case")
	r.Empty(got.Suites[0].Cases[0].ErrorDetails, "failure details should not be included without exact follow-up filters")
}

func TestTestReportFetchesFailureDetailsForExactFollowUp(t *testing.T) {
	r := require.New(t)
	var paths []string
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path)
		switch req.URL.Path {
		case "/job/app/9/testReport/api/json":
			r.Equal("totalCount,failCount,skipCount,passCount,suites[name,cases[className,name,safeName,status,duration]]", req.URL.Query().Get("tree"), "compact tree query")
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
		case "/job/app/9/testReport/junit/example/AuthTest/Login/api/json":
			writeAPIJSON(w, `{
				"className":"example.AuthTest",
				"name":"Login",
				"status":"FAILED",
				"duration":0.1,
				"errorDetails":"login failed",
				"errorStackTrace":"stack trace"
			}`)
		default:
			http.NotFound(w, req)
		}
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
	r.Equal("login failed", got.Suites[0].Cases[0].ErrorDetails, "failure details")
	r.Equal("stack trace", got.Suites[0].Cases[0].ErrorStackTrace, "failure stack")
	r.True(got.FailureDetailsIncluded, "exact follow-up should include failure details")
	r.Equal([]string{"/job/app/9/testReport/api/json", "/job/app/9/testReport/junit/example/AuthTest/Login/api/json"}, paths, "request paths")
}

func TestTestReportUsesJenkinsSafeNameForCaseDetailURL(t *testing.T) {
	r := require.New(t)
	var paths []string
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path)
		switch req.URL.Path {
		case "/job/app/13/testReport/api/json":
			writeAPIJSON(w, `{
				"totalCount": 1,
				"failCount": 1,
				"skipCount": 0,
				"passCount": 0,
				"suites": [{
					"name": "ParamSuite",
					"cases": [
						{"className":"example.ParamTest","name":"case with spaces","safeName":"case_with_spaces","status":"FAILED","duration":0.1}
					]
				}]
			}`)
		case "/job/app/13/testReport/junit/example/ParamTest/case_with_spaces/api/json":
			writeAPIJSON(w, `{
				"className":"example.ParamTest",
				"name":"case with spaces",
				"status":"FAILED",
				"duration":0.1,
				"errorDetails":"parameter failed"
			}`)
		default:
			http.NotFound(w, req)
		}
	})

	got, err := api.TestReport(t.Context(), "app", 13, model.TestCaseFilter{
		CaseName: "case with spaces",
	}, 50)
	r.NoError(err, "TestReport() error")
	r.True(got.FailureDetailsIncluded, "exact follow-up should include failure details")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 1, "case count")
	r.Equal("parameter failed", got.Suites[0].Cases[0].ErrorDetails, "failure details")
	r.Equal([]string{"/job/app/13/testReport/api/json", "/job/app/13/testReport/junit/example/ParamTest/case_with_spaces/api/json"}, paths, "request paths")
}

func TestTestReportUsesJenkinsSafeNameForLegacyCaseDetailURL(t *testing.T) {
	r := require.New(t)
	var paths []string
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path+"?tree="+req.URL.Query().Get("tree"))
		switch req.URL.Path {
		case "/job/app/16/testReport/api/json":
			writeAPIJSON(w, `{
				"totalCount": 1,
				"failCount": 1,
				"skipCount": 0,
				"passCount": 0,
				"suites": [{
					"name": "CalendarRulesTest",
					"cases": [
						{"className":"CalendarRulesTest","name":"testShouldRefreshSeasonalCutoffDate","safeName":"unit___Unit_Tests___testShouldRefreshSeasonalCutoffDate","status":"FAILED","duration":0.05}
					]
				}]
			}`)
		case "/job/app/16/testReport/(root)/CalendarRulesTest/unit___Unit_Tests___testShouldRefreshSeasonalCutoffDate/api/json":
			writeAPIJSON(w, `{
				"className":"CalendarRulesTest",
				"name":"testShouldRefreshSeasonalCutoffDate",
				"status":"FAILED",
				"duration":0.05,
				"errorDetails":"seasonal cutoff date mismatch",
				"errorStackTrace":"calendar stack"
			}`)
		default:
			http.NotFound(w, req)
		}
	})

	got, err := api.TestReport(t.Context(), "app", 16, model.TestCaseFilter{
		ClassName: "CalendarRulesTest",
		CaseName:  "testShouldRefreshSeasonalCutoffDate",
	}, 10)
	r.NoError(err, "TestReport() error")
	r.True(got.FailureDetailsIncluded, "exact follow-up should include failure details")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 1, "case count")
	r.Equal("seasonal cutoff date mismatch", got.Suites[0].Cases[0].ErrorDetails, "failure details")
	r.Equal("calendar stack", got.Suites[0].Cases[0].ErrorStackTrace, "failure stack")
	r.Equal([]string{
		"/job/app/16/testReport/api/json?tree=totalCount,failCount,skipCount,passCount,suites[name,cases[className,name,safeName,status,duration]]",
		"/job/app/16/testReport/junit/(root)/CalendarRulesTest/unit___Unit_Tests___testShouldRefreshSeasonalCutoffDate/api/json?tree=className,name,status,duration,errorDetails,errorStackTrace",
		"/job/app/16/testReport/(root)/CalendarRulesTest/unit___Unit_Tests___testShouldRefreshSeasonalCutoffDate/api/json?tree=className,name,status,duration,errorDetails,errorStackTrace",
	}, paths, "request paths")
}

func TestTestReportDerivesJenkinsSafeNameWhenCompactMetadataOmitsIt(t *testing.T) {
	r := require.New(t)
	var paths []string
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path+"?tree="+req.URL.Query().Get("tree"))
		switch req.URL.Path {
		case "/job/app/17/testReport/api/json":
			writeAPIJSON(w, `{
				"totalCount": 1,
				"failCount": 1,
				"skipCount": 0,
				"passCount": 0,
				"suites": [{
					"name": "CalendarRulesTest",
					"cases": [
						{"className":"CalendarRulesTest","name":"test should refresh seasonal cutoff date","status":"FAILED","duration":0.05}
					]
				}]
			}`)
		case "/job/app/17/testReport/junit/(root)/CalendarRulesTest/test_should_refresh_seasonal_cutoff_date/api/json":
			writeAPIJSON(w, `{
				"className":"CalendarRulesTest",
				"name":"test should refresh seasonal cutoff date",
				"status":"FAILED",
				"duration":0.05,
				"errorDetails":"seasonal cutoff date mismatch",
				"errorStackTrace":"calendar stack"
			}`)
		default:
			http.NotFound(w, req)
		}
	})

	got, err := api.TestReport(t.Context(), "app", 17, model.TestCaseFilter{
		ClassName: "CalendarRulesTest",
		CaseName:  "test should refresh seasonal cutoff date",
	}, 10)
	r.NoError(err, "TestReport() error")
	r.True(got.FailureDetailsIncluded, "exact follow-up should include failure details")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 1, "case count")
	r.Equal("seasonal cutoff date mismatch", got.Suites[0].Cases[0].ErrorDetails, "failure details")
	r.Equal("calendar stack", got.Suites[0].Cases[0].ErrorStackTrace, "failure stack")
	r.Equal([]string{
		"/job/app/17/testReport/api/json?tree=totalCount,failCount,skipCount,passCount,suites[name,cases[className,name,safeName,status,duration]]",
		"/job/app/17/testReport/junit/(root)/CalendarRulesTest/test_should_refresh_seasonal_cutoff_date/api/json?tree=className,name,status,duration,errorDetails,errorStackTrace",
	}, paths, "request paths")
}

func TestTestReportExactFollowUpFetchesDetailsFromClassChildURL(t *testing.T) {
	r := require.New(t)
	var paths []string
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path+"?tree="+req.URL.Query().Get("tree"))
		switch req.URL.Path {
		case "/job/app/18/testReport/api/json":
			writeAPIJSON(w, `{
				"totalCount": 1,
				"failCount": 1,
				"skipCount": 0,
				"passCount": 0,
				"suites": [{
					"name": "PublicHolidayTest",
					"cases": [
						{"className":"PublicHolidayTest","name":"testThisShouldBeUpdatedFor2026GrandFinal","status":"FAILED","duration":0.054784}
					]
				}]
			}`)
		case "/job/app/18/testReport/(root)/PublicHolidayTest/api/json":
			writeAPIJSON(w, `{
				"child": [{
					"name": "testThisShouldBeUpdatedFor2026GrandFinal",
					"url": "http://jenkins.example/job/app/18/testReport/(root)/PublicHolidayTest/unit___Unit_Tests___testThisShouldBeUpdatedFor2026GrandFinal/"
				}]
			}`)
		case "/job/app/18/testReport/(root)/PublicHolidayTest/unit___Unit_Tests___testThisShouldBeUpdatedFor2026GrandFinal/api/json":
			writeAPIJSON(w, `{
				"className":"PublicHolidayTest",
				"name":"testThisShouldBeUpdatedFor2026GrandFinal",
				"status":"FAILED",
				"duration":0.054784,
				"errorDetails":"grand final holiday mismatch",
				"errorStackTrace":"holiday stack"
			}`)
		default:
			http.NotFound(w, req)
		}
	})

	got, err := api.TestReport(t.Context(), "app", 18, model.TestCaseFilter{
		ClassName: "PublicHolidayTest",
		CaseName:  "testThisShouldBeUpdatedFor2026GrandFinal",
	}, 10)
	r.NoError(err, "TestReport() should follow Jenkins class child URLs after direct detail probes 404")
	r.True(got.FailureDetailsIncluded, "failure details should report available")
	r.Len(got.Suites, 1, "suite count")
	r.Equal("PublicHolidayTest", got.Suites[0].Name, "suite name")
	r.Len(got.Suites[0].Cases, 1, "case count")
	testCase := got.Suites[0].Cases[0]
	r.Equal("PublicHolidayTest", testCase.ClassName, "class name")
	r.Equal("testThisShouldBeUpdatedFor2026GrandFinal", testCase.Name, "case name")
	r.Equal("grand final holiday mismatch", testCase.ErrorDetails, "failure details")
	r.Equal("holiday stack", testCase.ErrorStackTrace, "failure stack")
	r.Equal([]string{
		"/job/app/18/testReport/api/json?tree=totalCount,failCount,skipCount,passCount,suites[name,cases[className,name,safeName,status,duration]]",
		"/job/app/18/testReport/junit/(root)/PublicHolidayTest/testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className,name,status,duration,errorDetails,errorStackTrace",
		"/job/app/18/testReport/(root)/PublicHolidayTest/testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className,name,status,duration,errorDetails,errorStackTrace",
		"/job/app/18/testReport/junit/PublicHolidayTest/PublicHolidayTest/testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className,name,status,duration,errorDetails,errorStackTrace",
		"/job/app/18/testReport/PublicHolidayTest/PublicHolidayTest/testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className,name,status,duration,errorDetails,errorStackTrace",
		"/job/app/18/testReport/junit/(root)/PublicHolidayTest/api/json?tree=child[name,url]",
		"/job/app/18/testReport/(root)/PublicHolidayTest/api/json?tree=child[name,url]",
		"/job/app/18/testReport/(root)/PublicHolidayTest/unit___Unit_Tests___testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className,name,status,duration,errorDetails,errorStackTrace",
	}, paths, "request paths")
}

func TestTestReportExactFollowUpReturnsCompactMatchWhenDetailLookup404s(t *testing.T) {
	r := require.New(t)
	var paths []string
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path+"?tree="+req.URL.Query().Get("tree"))
		switch req.URL.Path {
		case "/job/app/19/testReport/api/json":
			writeAPIJSON(w, `{
				"totalCount": 1,
				"failCount": 1,
				"skipCount": 0,
				"passCount": 0,
				"suites": [{
					"name": "PublicHolidayTest",
					"cases": [
						{"className":"PublicHolidayTest","name":"testThisShouldBeUpdatedFor2026GrandFinal","status":"FAILED","duration":0.054784}
					]
				}]
			}`)
		default:
			http.NotFound(w, req)
		}
	})

	got, err := api.TestReport(t.Context(), "app", 19, model.TestCaseFilter{
		ClassName: "PublicHolidayTest",
		CaseName:  "testThisShouldBeUpdatedFor2026GrandFinal",
	}, 10)
	r.NoError(err, "TestReport() should return compact matches when detail URL inference returns 404")
	r.False(got.FailureDetailsIncluded, "failure details should report unavailable")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 1, "case count")
	r.Equal("testThisShouldBeUpdatedFor2026GrandFinal", got.Suites[0].Cases[0].Name, "case name")
	r.Equal([]string{
		"/job/app/19/testReport/api/json?tree=totalCount,failCount,skipCount,passCount,suites[name,cases[className,name,safeName,status,duration]]",
		"/job/app/19/testReport/junit/(root)/PublicHolidayTest/testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className,name,status,duration,errorDetails,errorStackTrace",
		"/job/app/19/testReport/(root)/PublicHolidayTest/testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className,name,status,duration,errorDetails,errorStackTrace",
		"/job/app/19/testReport/junit/PublicHolidayTest/PublicHolidayTest/testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className,name,status,duration,errorDetails,errorStackTrace",
		"/job/app/19/testReport/PublicHolidayTest/PublicHolidayTest/testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className,name,status,duration,errorDetails,errorStackTrace",
		"/job/app/19/testReport/junit/(root)/PublicHolidayTest/api/json?tree=child[name,url]",
		"/job/app/19/testReport/(root)/PublicHolidayTest/api/json?tree=child[name,url]",
	}, paths, "request paths")
}

func TestTestReportExactFollowUpFetchesDetailsFromClassDepthChild(t *testing.T) {
	r := require.New(t)
	var paths []string
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path+"?"+req.URL.RawQuery)
		switch {
		case req.URL.Path == "/job/app/20/testReport/api/json":
			writeAPIJSON(w, `{
				"totalCount": 1,
				"failCount": 1,
				"skipCount": 0,
				"passCount": 0,
				"suites": [{
					"name": "PublicHolidayTest",
					"cases": [
						{"className":"PublicHolidayTest","name":"testThisShouldBeUpdatedFor2026GrandFinal","status":"FAILED","duration":0.054784}
					]
				}]
			}`)
		case req.URL.Path == "/job/app/20/testReport/(root)/PublicHolidayTest/api/json" && req.URL.Query().Get("tree") == "child[name,url]":
			writeAPIJSON(w, `{
				"child": [{
					"name": "testThisShouldBeUpdatedFor2026GrandFinal"
				}]
			}`)
		case req.URL.Path == "/job/app/20/testReport/(root)/PublicHolidayTest/api/json" && req.URL.Query().Get("depth") == "1":
			writeAPIJSON(w, `{
				"child": [{
					"className":"PublicHolidayTest",
					"name":"testThisShouldBeUpdatedFor2026GrandFinal",
					"status":"FAILED",
					"duration":0.054784,
					"errorDetails":null,
					"errorStackTrace":"Friday before the AFL Grand Final for VIC must be updated for 2026!"
				}]
			}`)
		default:
			http.NotFound(w, req)
		}
	})

	got, err := api.TestReport(t.Context(), "app", 20, model.TestCaseFilter{
		ClassName: "PublicHolidayTest",
		CaseName:  "testThisShouldBeUpdatedFor2026GrandFinal",
	}, 10)
	r.NoError(err, "TestReport() should fetch failure details from class depth children")
	r.True(got.FailureDetailsIncluded, "failure details should report available")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 1, "case count")
	testCase := got.Suites[0].Cases[0]
	r.Equal("PublicHolidayTest", testCase.ClassName, "class name")
	r.Equal("testThisShouldBeUpdatedFor2026GrandFinal", testCase.Name, "case name")
	r.Empty(testCase.ErrorDetails, "failure details")
	r.Contains(testCase.ErrorStackTrace, "Grand Final", "failure stack")
	r.Equal([]string{
		"/job/app/20/testReport/api/json?tree=totalCount%2CfailCount%2CskipCount%2CpassCount%2Csuites%5Bname%2Ccases%5BclassName%2Cname%2CsafeName%2Cstatus%2Cduration%5D%5D",
		"/job/app/20/testReport/junit/(root)/PublicHolidayTest/testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className%2Cname%2Cstatus%2Cduration%2CerrorDetails%2CerrorStackTrace",
		"/job/app/20/testReport/(root)/PublicHolidayTest/testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className%2Cname%2Cstatus%2Cduration%2CerrorDetails%2CerrorStackTrace",
		"/job/app/20/testReport/junit/PublicHolidayTest/PublicHolidayTest/testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className%2Cname%2Cstatus%2Cduration%2CerrorDetails%2CerrorStackTrace",
		"/job/app/20/testReport/PublicHolidayTest/PublicHolidayTest/testThisShouldBeUpdatedFor2026GrandFinal/api/json?tree=className%2Cname%2Cstatus%2Cduration%2CerrorDetails%2CerrorStackTrace",
		"/job/app/20/testReport/junit/(root)/PublicHolidayTest/api/json?tree=child%5Bname%2Curl%5D",
		"/job/app/20/testReport/(root)/PublicHolidayTest/api/json?tree=child%5Bname%2Curl%5D",
		"/job/app/20/testReport/(root)/PublicHolidayTest/api/json?depth=1",
	}, paths, "request paths")
}

func TestTestReportExactFollowUpFallsBackWhenClassDepthExceedsLimit(t *testing.T) {
	r := require.New(t)
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/job/app/21/testReport/api/json":
			writeAPIJSON(w, `{
				"totalCount": 1,
				"failCount": 1,
				"skipCount": 0,
				"passCount": 0,
				"suites": [{
					"name": "PublicHolidayTest",
					"cases": [
						{"className":"PublicHolidayTest","name":"testThisShouldBeUpdatedFor2026GrandFinal","status":"FAILED","duration":0.054784}
					]
				}]
			}`)
		case req.URL.Path == "/job/app/21/testReport/(root)/PublicHolidayTest/api/json" && req.URL.Query().Get("tree") == "child[name,url]":
			writeAPIJSON(w, `{
				"child": [{
					"name": "testThisShouldBeUpdatedFor2026GrandFinal"
				}]
			}`)
		case req.URL.Path == "/job/app/21/testReport/(root)/PublicHolidayTest/api/json" && req.URL.Query().Get("depth") == "1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, strings.Repeat("x", 8*1024*1024+1))
		default:
			http.NotFound(w, req)
		}
	})

	got, err := api.TestReport(t.Context(), "app", 21, model.TestCaseFilter{
		ClassName: "PublicHolidayTest",
		CaseName:  "testThisShouldBeUpdatedFor2026GrandFinal",
	}, 10)
	r.NoError(err, "oversized class depth response should not fail compact exact matches")
	r.False(got.FailureDetailsIncluded, "failure details should report unavailable")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 1, "case count")
	r.Equal("testThisShouldBeUpdatedFor2026GrandFinal", got.Suites[0].Cases[0].Name, "case name")
}

func TestTestCaseDetailPathsUseJenkinsSafeNameForLegacyURLs(t *testing.T) {
	r := require.New(t)

	paths := testCaseDetailPaths(
		"apps/billing/feature%2Fcalendar-refresh",
		191,
		"",
		"CalendarRulesTest",
		"testShouldRefreshSeasonalCutoffDate",
		"unit___Unit_Tests___testShouldRefreshSeasonalCutoffDate",
	)

	r.Contains(paths, "job/apps/job/billing/job/feature%252Fcalendar-refresh/191/testReport/%28root%29/CalendarRulesTest/unit___Unit_Tests___testShouldRefreshSeasonalCutoffDate/api/json")
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
	r.Equal("totalCount,failCount,skipCount,passCount,suites[name,cases[className,name,safeName,status,duration]]", tree, "tree query")
	r.Len(got.Suites, 1, "suite count")
	r.Len(got.Suites[0].Cases, 1, "case count")
	r.Empty(got.Suites[0].Cases[0].ErrorDetails, "error details")
	r.Empty(got.Suites[0].Cases[0].ErrorStackTrace, "error stack")
}

func TestTestReportExactFollowUpUsesClassEndpointWhenCompactMetadataIsTooLarge(t *testing.T) {
	r := require.New(t)
	var paths []string
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path+"?tree="+req.URL.Query().Get("tree"))
		switch {
		case req.URL.Path == "/job/app/11/testReport/api/json" && strings.Contains(req.URL.Query().Get("tree"), "suites["):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, strings.Repeat("x", 8*1024*1024+1))
		case req.URL.Path == "/job/app/11/testReport/api/json":
			writeAPIJSON(w, `{
				"totalCount": 1,
				"failCount": 1,
				"skipCount": 0,
				"passCount": 0
			}`)
		case req.URL.Path == "/job/app/11/testReport/junit/example/AuthTest/api/json":
			writeAPIJSON(w, `{
				"cases": [{
					"className":"example.AuthTest",
					"name":"case with spaces",
					"safeName":"case_with_spaces",
					"status":"FAILED",
					"duration":0.1,
					"errorDetails":"login failed",
					"errorStackTrace":"stack trace"
				}]
			}`)
		default:
			http.NotFound(w, req)
		}
	})

	got, err := api.TestReport(t.Context(), "app", 11, model.TestCaseFilter{
		ClassName: "example.AuthTest",
		CaseName:  "case with spaces",
	}, 50)
	r.NoError(err, "TestReport() error")
	r.True(got.FailureDetailsIncluded, "direct exact lookup should include failure details")
	r.Len(got.Suites, 1, "suite count")
	r.Equal("example", got.Suites[0].Name, "derived suite name")
	r.Len(got.Suites[0].Cases, 1, "case count")
	r.Equal("login failed", got.Suites[0].Cases[0].ErrorDetails, "failure details")
	r.Equal([]string{
		"/job/app/11/testReport/api/json?tree=totalCount,failCount,skipCount,passCount,suites[name,cases[className,name,safeName,status,duration]]",
		"/job/app/11/testReport/api/json?tree=totalCount,failCount,skipCount,passCount",
		"/job/app/11/testReport/junit/example/AuthTest/api/json?tree=cases[className,name,safeName,status,duration,errorDetails,errorStackTrace]",
	}, paths, "request paths")
}

func TestTestReportExactFollowUpDoesNotBypassCompactMetadataWithSuiteFilter(t *testing.T) {
	r := require.New(t)
	var paths []string
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path+"?tree="+req.URL.Query().Get("tree"))
		if req.URL.Path == "/job/app/12/testReport/api/json" && strings.Contains(req.URL.Query().Get("tree"), "suites[") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, strings.Repeat("x", 8*1024*1024+1))
			return
		}
		http.NotFound(w, req)
	})

	_, err := api.TestReport(t.Context(), "app", 12, model.TestCaseFilter{
		SuiteName: "OtherSuite",
		ClassName: "example.AuthTest",
		CaseName:  "Login",
	}, 50)
	r.Error(err, "TestReport() should not bypass compact metadata with an exact suite filter")
	appErr, ok := err.(*apperrors.Error)
	r.True(ok, "error type")
	r.Equal(apperrors.CodeJenkins, appErr.Code, "error code")
	r.Contains(appErr.Message, "compact test metadata exceeded", "error message")
	r.Equal([]string{
		"/job/app/12/testReport/api/json?tree=totalCount,failCount,skipCount,passCount,suites[name,cases[className,name,safeName,status,duration]]",
	}, paths, "request paths")
}

func TestTestReportExactFollowUpDoesNotBypassCompactMetadataWithSuiteSubstringFilter(t *testing.T) {
	r := require.New(t)
	var paths []string
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.URL.Path+"?tree="+req.URL.Query().Get("tree"))
		if req.URL.Path == "/job/app/15/testReport/api/json" && strings.Contains(req.URL.Query().Get("tree"), "suites[") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, strings.Repeat("x", 8*1024*1024+1))
			return
		}
		http.NotFound(w, req)
	})

	_, err := api.TestReport(t.Context(), "app", 15, model.TestCaseFilter{
		SuiteNameContains: "auth",
		ClassName:         "example.AuthTest",
		CaseName:          "Login",
	}, 50)
	r.Error(err, "TestReport() should not bypass compact metadata with a suite substring filter")
	appErr, ok := err.(*apperrors.Error)
	r.True(ok, "error type")
	r.Equal(apperrors.CodeJenkins, appErr.Code, "error code")
	r.Contains(appErr.Message, "compact test metadata exceeded", "error message")
	r.Equal([]string{
		"/job/app/15/testReport/api/json?tree=totalCount,failCount,skipCount,passCount,suites[name,cases[className,name,safeName,status,duration]]",
	}, paths, "request paths")
}

func TestTestReportExactFollowUpAppliesFiltersAfterClassEndpointLookup(t *testing.T) {
	r := require.New(t)
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/job/app/14/testReport/api/json" && strings.Contains(req.URL.Query().Get("tree"), "suites["):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, strings.Repeat("x", 8*1024*1024+1))
		case req.URL.Path == "/job/app/14/testReport/api/json":
			writeAPIJSON(w, `{
				"totalCount": 1,
				"failCount": 1,
				"skipCount": 0,
				"passCount": 0
			}`)
		case req.URL.Path == "/job/app/14/testReport/junit/example/AuthTest/api/json":
			writeAPIJSON(w, `{
				"cases": [{
					"className":"example.AuthTest",
					"name":"Login",
					"safeName":"Login",
					"status":"FAILED",
					"duration":0.1,
					"errorDetails":"login failed"
				}]
			}`)
		default:
			http.NotFound(w, req)
		}
	})

	got, err := api.TestReport(t.Context(), "app", 14, model.TestCaseFilter{
		ClassName: "example.AuthTest",
		CaseName:  "Login",
		Status:    "PASSED",
	}, 50)
	r.NoError(err, "TestReport() error")
	r.Equal(1, got.TotalCount, "summary total should remain full report count")
	r.Empty(got.Suites, "status filter should exclude the found exact case")
	r.False(got.FailureDetailsIncluded, "filtered-out detail should not be reported as included")
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

func TestSearchLogPagesProgressiveLogUntilMatch(t *testing.T) {
	r := require.New(t)
	log := "start\n" + strings.Repeat("noise\n", 5) + "compiler error: boom\nend\n"
	var starts []string
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		r.Equal("/job/app/7/logText/progressiveText", req.URL.Path, "path")
		rawStart := req.URL.Query().Get("start")
		starts = append(starts, rawStart)
		start, err := strconv.Atoi(rawStart)
		r.NoError(err, "parse start")
		if start > len(log) {
			start = len(log)
		}
		w.Header().Set("X-Text-Size", strconv.Itoa(len(log)))
		w.Header().Set("X-More-Data", "false")
		_, _ = io.WriteString(w, log[start:])
	})

	got, err := api.SearchLog(t.Context(), "app", 7, 0, "error", 12, 200, 20, 0)
	r.NoError(err, "SearchLog() error")
	r.Greater(len(starts), 1, "search should page through multiple progressive chunks")
	r.Len(got.Matches, 1, "matches")
	r.Equal("compiler error: boom", got.Matches[0].Text, "match text")
	r.Equal(int64(len(log)), got.NextStart, "next start")
	r.False(got.More, "more")
	r.False(got.ScanLimitReached, "scan limit")
	r.False(got.Truncated, "truncated")
}

func TestSearchLogStopsAtScanBudget(t *testing.T) {
	r := require.New(t)
	log := strings.Repeat("noise\n", 20)
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		r.Equal("/job/app/8/logText/progressiveText", req.URL.Path, "path")
		start, err := strconv.Atoi(req.URL.Query().Get("start"))
		r.NoError(err, "parse start")
		if start > len(log) {
			start = len(log)
		}
		w.Header().Set("X-Text-Size", strconv.Itoa(len(log)))
		w.Header().Set("X-More-Data", "false")
		_, _ = io.WriteString(w, log[start:])
	})

	got, err := api.SearchLog(t.Context(), "app", 8, 0, "error", 10, 15, 20, 0)
	r.NoError(err, "SearchLog() error")
	r.Empty(got.Matches, "matches")
	r.Equal(int64(15), got.ScannedBytes, "scanned bytes")
	r.Equal(int64(15), got.NextStart, "next start")
	r.True(got.More, "more")
	r.True(got.ScanLimitReached, "scan limit")
	r.True(got.Truncated, "truncated")
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
