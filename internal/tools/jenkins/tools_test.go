package jenkins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/updatecheck"
)

func TestCapabilitiesIncludesUpdateStatus(t *testing.T) {
	got, err := Capabilities(context.Background(), Deps{
		Config: config.Config{},
		UpdateStatus: func() updatecheck.Status {
			return updatecheck.Status{
				Enabled:          true,
				CurrentVersion:   "1.2.3",
				LatestVersion:    "v1.2.4",
				ReleaseURL:       "https://github.com/example/project/releases/tag/v1.2.4",
				UpdateAvailable:  true,
				NotificationHint: "Notify the user that a newer jenkins-mcp release is available.",
			}
		},
	}, BaseRequest{})
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if !got.Updates.UpdateAvailable {
		t.Fatal("updates.updateAvailable should be true")
	}
	if got.Updates.LatestVersion != "v1.2.4" {
		t.Fatalf("updates.latestVersion = %q", got.Updates.LatestVersion)
	}
	if got.Updates.NotificationHint == "" {
		t.Fatal("updates.notificationHint should be populated")
	}
}

func TestTriggerBuildRequiresMutationEnablement(t *testing.T) {
	_, err := TriggerBuild(context.Background(), Deps{
		Config: config.Config{
			DefaultController: "default",
			Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		},
	}, TriggerBuildRequest{Job: "app"})
	if err == nil {
		t.Fatal("TriggerBuild() succeeded with mutations disabled")
	}
}

func TestResolveBuildURL(t *testing.T) {
	ref, err := resolveBuildURL(config.Config{
		Controllers: []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
	}, "https://jenkins.example.com/job/weather-station/job/weather-station-server/job/main/104/")
	if err != nil {
		t.Fatalf("resolveBuildURL() error = %v", err)
	}
	if ref.Controller != "default" || ref.Job != "weather-station/weather-station-server/main" || ref.Build != 104 {
		t.Fatalf("reference = %+v", ref)
	}
}

func TestResolveBuildURLRejectsUnknownController(t *testing.T) {
	_, err := resolveBuildURL(config.Config{
		Controllers: []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
	}, "https://other.example.com/job/app/1/")
	if err == nil {
		t.Fatal("resolveBuildURL() accepted unknown controller")
	}
}

func TestResolveBuildURLPrefersMostSpecificControllerPath(t *testing.T) {
	cfg := config.Config{
		Controllers: []config.ControllerConfig{
			{ID: "root", URL: "https://ci.example.com"},
			{ID: "jenkins", URL: "https://ci.example.com/jenkins"},
			{ID: "jenkins-alt", URL: "https://ci.example.com/jenkins-alt"},
		},
	}

	ref, err := resolveBuildURL(cfg, "https://ci.example.com/jenkins/job/app/42/")
	if err != nil {
		t.Fatalf("resolveBuildURL() error = %v", err)
	}
	if ref.Controller != "jenkins" || ref.Job != "app" || ref.Build != 42 {
		t.Fatalf("reference = %+v", ref)
	}

	ref, err = resolveBuildURL(cfg, "https://ci.example.com/jenkins-alt/job/api/7/")
	if err != nil {
		t.Fatalf("resolveBuildURL() error = %v", err)
	}
	if ref.Controller != "jenkins-alt" || ref.Job != "api" || ref.Build != 7 {
		t.Fatalf("reference = %+v", ref)
	}
}

func TestResolveBuildURLMatchesControllerPathWithTrailingSlash(t *testing.T) {
	cfg := config.Config{
		Controllers: []config.ControllerConfig{
			{ID: "jenkins", URL: "https://ci.example.com/jenkins/"},
		},
	}

	ref, err := resolveBuildURL(cfg, "https://ci.example.com/jenkins/job/app/42/")
	if err != nil {
		t.Fatalf("resolveBuildURL() error = %v", err)
	}
	if ref.Controller != "jenkins" || ref.Job != "app" || ref.Build != 42 {
		t.Fatalf("reference = %+v", ref)
	}
}

func TestValidateTriggerParametersRejectsUnknown(t *testing.T) {
	err := validateTriggerParameters([]model.ParameterDefinition{{Name: "BRANCH"}}, map[string]string{"UNKNOWN": "main"})
	if err == nil {
		t.Fatal("validateTriggerParameters() accepted unknown parameter")
	}
}

func TestValidateTriggerParametersRequiresRequired(t *testing.T) {
	err := validateTriggerParameters([]model.ParameterDefinition{{Name: "BRANCH", Required: true}}, nil)
	if err == nil {
		t.Fatal("validateTriggerParameters() accepted missing required parameter")
	}
}

func TestValidateTriggerParametersAcceptsKnown(t *testing.T) {
	err := validateTriggerParameters([]model.ParameterDefinition{{Name: "BRANCH", Required: true}}, map[string]string{"BRANCH": "main"})
	if err != nil {
		t.Fatalf("validateTriggerParameters() error = %v", err)
	}
}

func TestListJobsDerivesStatusAndAppliesFilters(t *testing.T) {
	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/json" {
			http.NotFound(w, r)
			return
		}
		tree := r.URL.Query().Get("tree")
		if !strings.Contains(tree, "lastBuild[number,result,building]") || !strings.Contains(tree, "lastCompletedBuild[number,result,building]") {
			t.Fatalf("tree query = %q, want build status fields", tree)
		}
		if !strings.Contains(tree, "disabled") {
			t.Fatalf("tree query = %q, want disabled field", tree)
		}
		writeJSON(w, `{"jobs":[
			{"name":"deploy-main","url":"https://jenkins.example.com/job/deploy-main/","color":"red_anime","_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","lastBuild":{"number":12,"result":"","building":true},"lastCompletedBuild":{"number":11,"result":"FAILURE","building":false}},
			{"name":"deploy-old","url":"https://jenkins.example.com/job/deploy-old/","color":"blue","_class":"hudson.model.FreeStyleProject","lastBuild":{"number":3,"result":"SUCCESS","building":false}},
			{"name":"tests","url":"https://jenkins.example.com/job/tests/","color":"yellow","_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob"}
		]}`)
	})

	building := true
	got, err := ListJobs(context.Background(), deps, ListJobsRequest{
		NameContains: "DEPLOY",
		Type:         "pipeline",
		Status:       "failure",
		Building:     &building,
	})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(got.Jobs) != 1 {
		t.Fatalf("ListJobs() returned %d jobs, want 1: %+v", len(got.Jobs), got.Jobs)
	}
	job := got.Jobs[0]
	if job.Name != "deploy-main" || job.Status != "failed" || !job.Building {
		t.Fatalf("job = %+v, want deploy-main failed building", job)
	}
}

func TestListJobsRegexMatchesFullName(t *testing.T) {
	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/job/team/api/json" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, `{"jobs":[
			{"name":"api-main","url":"https://jenkins.example.com/job/team/job/api-main/","color":"blue","_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","lastCompletedBuild":{"number":5,"result":"SUCCESS","building":false}},
			{"name":"web-main","url":"https://jenkins.example.com/job/team/job/web-main/","color":"red","_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","lastCompletedBuild":{"number":4,"result":"FAILURE","building":false}}
		]}`)
	})

	got, err := ListJobs(context.Background(), deps, ListJobsRequest{Folder: "team", NameRegex: "^team/api"})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(got.Jobs) != 1 || got.Jobs[0].FullName != "team/api-main" {
		t.Fatalf("ListJobs() jobs = %+v, want team/api-main", got.Jobs)
	}
}

func TestListJobsDisabledStatusWinsOverCompletedBuildResult(t *testing.T) {
	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/json" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, `{"jobs":[
			{"name":"disabled-job","url":"https://jenkins.example.com/job/disabled-job/","color":"blue","disabled":true,"_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","lastCompletedBuild":{"number":2,"result":"SUCCESS","building":false}},
			{"name":"active-job","url":"https://jenkins.example.com/job/active-job/","color":"blue","_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","lastCompletedBuild":{"number":3,"result":"SUCCESS","building":false}}
		]}`)
	})

	got, err := ListJobs(context.Background(), deps, ListJobsRequest{Status: "disabled"})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(got.Jobs) != 1 {
		t.Fatalf("ListJobs() returned %d jobs, want 1: %+v", len(got.Jobs), got.Jobs)
	}
	if got.Jobs[0].Name != "disabled-job" || got.Jobs[0].Status != "disabled" {
		t.Fatalf("job = %+v, want disabled-job with disabled status", got.Jobs[0])
	}
	if got.Jobs[0].Disabled == nil || !*got.Jobs[0].Disabled {
		t.Fatalf("job disabled = %v, want true", got.Jobs[0].Disabled)
	}
}

func TestListJobsRejectsInvalidNameRegex(t *testing.T) {
	_, err := ListJobs(context.Background(), Deps{}, ListJobsRequest{NameRegex: "["})
	if err == nil {
		t.Fatal("ListJobs() accepted invalid regex")
	}
	assertAppErrorCode(t, err, apperrors.CodeInvalidRequest)
}

func TestGetJobReturnsDerivedStatusAndDisabledState(t *testing.T) {
	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/job/app/api/json" {
			http.NotFound(w, r)
			return
		}
		tree := r.URL.Query().Get("tree")
		if !strings.Contains(tree, "disabled") || !strings.Contains(tree, "lastCompletedBuild[number,url,result,building,timestamp,duration]") {
			t.Fatalf("tree query = %q, want disabled and lastCompletedBuild fields", tree)
		}
		writeJSON(w, `{
			"name":"app",
			"fullName":"folder/app",
			"url":"https://jenkins.example.com/job/folder/job/app/",
			"color":"blue",
			"_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob",
			"disabled":true,
			"description":"disabled app",
			"buildable":false,
			"inQueue":false,
			"nextBuildNumber":4,
			"lastBuild":{"number":3,"url":"https://jenkins.example.com/job/folder/job/app/3/","result":"","building":true,"timestamp":1,"duration":20},
			"lastCompletedBuild":{"number":2,"url":"https://jenkins.example.com/job/folder/job/app/2/","result":"SUCCESS","building":false,"timestamp":1,"duration":10},
			"property":[]
		}`)
	})

	got, err := GetJob(context.Background(), deps, JobRequest{Job: "app"})
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if got.Job.Status != "disabled" || !got.Job.Building {
		t.Fatalf("job = %+v, want disabled status and building=true", got.Job.Job)
	}
	if got.Job.Disabled == nil || !*got.Job.Disabled {
		t.Fatalf("job disabled = %v, want true", got.Job.Disabled)
	}
}

func TestWatchBuildTimesOutWithoutSemanticChange(t *testing.T) {
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"","building":true,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
		case "/job/app/42/wfapi/describe":
			writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":"IN_PROGRESS"}]}`)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 25, MaxWaitTimeoutMs: 25, MaxConsecutiveFailures: 3})

	first, err := WatchBuild(context.Background(), deps, WatchBuildRequest{Job: "app", Build: 42})
	if err != nil {
		t.Fatalf("first WatchBuild() error = %v", err)
	}
	if first.Watch.State == "" {
		t.Fatal("first WatchBuild() returned empty state")
	}

	second, err := WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 25,
	})
	if err != nil {
		t.Fatalf("second WatchBuild() error = %v", err)
	}
	if !second.Watch.TimedOut {
		t.Fatal("second WatchBuild() did not time out")
	}
	if second.Watch.State != first.Watch.State {
		t.Fatal("second WatchBuild() changed state without a semantic update")
	}
}

func TestWatchBuildKeepsStateStableWhenOnlyDurationChanges(t *testing.T) {
	var buildRequests int
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			buildRequests++
			if buildRequests == 1 {
				writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"","building":true,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
				return
			}
			writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"","building":true,"timestamp":1,"duration":20,"artifacts":[],"actions":[],"changeSets":[]}`)
		case "/job/app/42/wfapi/describe":
			writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":"IN_PROGRESS"}]}`)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 25, MaxWaitTimeoutMs: 25, MaxConsecutiveFailures: 3})

	first, err := WatchBuild(context.Background(), deps, WatchBuildRequest{Job: "app", Build: 42})
	if err != nil {
		t.Fatalf("first WatchBuild() error = %v", err)
	}
	second, err := WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 25,
	})
	if err != nil {
		t.Fatalf("second WatchBuild() error = %v", err)
	}
	if !second.Watch.TimedOut {
		t.Fatal("second WatchBuild() did not time out when only duration changed")
	}
	if second.Watch.State != first.Watch.State {
		t.Fatal("second WatchBuild() changed state when only duration changed")
	}
}

func TestWatchBuildReturnsWhenStageStatusChanges(t *testing.T) {
	var polls int
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"","building":true,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
		case "/job/app/42/wfapi/describe":
			polls++
			if polls < 3 {
				writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":"IN_PROGRESS"},{"id":"2","name":"Test","status":"NOT_EXECUTED"}]}`)
				return
			}
			writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":"SUCCESS"},{"id":"2","name":"Test","status":"IN_PROGRESS"}]}`)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 100, MaxWaitTimeoutMs: 100, MaxConsecutiveFailures: 3})

	first, err := WatchBuild(context.Background(), deps, WatchBuildRequest{Job: "app", Build: 42})
	if err != nil {
		t.Fatalf("first WatchBuild() error = %v", err)
	}
	second, err := WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 100,
	})
	if err != nil {
		t.Fatalf("second WatchBuild() error = %v", err)
	}
	if second.Watch.TimedOut {
		t.Fatal("second WatchBuild() timed out despite a stage transition")
	}
	if second.Watch.State == first.Watch.State {
		t.Fatal("second WatchBuild() did not advance state")
	}
	if second.Watch.Pipeline == nil || len(second.Watch.Pipeline.Stages) < 2 || second.Watch.Pipeline.Stages[1].Status != "IN_PROGRESS" {
		t.Fatalf("second WatchBuild() pipeline = %+v", second.Watch.Pipeline)
	}
}

func TestWatchBuildReturnsWhenPipelineRunStatusChanges(t *testing.T) {
	var polls int
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"","building":true,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
		case "/job/app/42/wfapi/describe":
			polls++
			if polls < 3 {
				writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Deploy","status":"IN_PROGRESS"}]}`)
				return
			}
			writeJSON(w, `{"id":"42","name":"#42","status":"PAUSED_PENDING_INPUT","stages":[{"id":"1","name":"Deploy","status":"IN_PROGRESS"}]}`)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 100, MaxWaitTimeoutMs: 100, MaxConsecutiveFailures: 3})

	first, err := WatchBuild(context.Background(), deps, WatchBuildRequest{Job: "app", Build: 42})
	if err != nil {
		t.Fatalf("first WatchBuild() error = %v", err)
	}
	second, err := WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 100,
	})
	if err != nil {
		t.Fatalf("second WatchBuild() error = %v", err)
	}
	if second.Watch.TimedOut {
		t.Fatal("second WatchBuild() timed out despite a pipeline run-status transition")
	}
	if second.Watch.State == first.Watch.State {
		t.Fatal("second WatchBuild() did not advance state on pipeline run-status change")
	}
	if second.Watch.Pipeline == nil || second.Watch.Pipeline.Status != "PAUSED_PENDING_INPUT" {
		t.Fatalf("second WatchBuild() pipeline = %+v", second.Watch.Pipeline)
	}
}

func TestWatchBuildLargeStageListStateRoundTrips(t *testing.T) {
	stageItems := make([]string, 0, 700)
	for i := 0; i < 700; i++ {
		stageItems = append(stageItems, fmt.Sprintf(`{"id":"stage-%d","name":"Long Stage Name %d","status":"IN_PROGRESS"}`, i, i))
	}
	stagePayload := "[" + strings.Join(stageItems, ",") + "]"

	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"","building":true,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
		case "/job/app/42/wfapi/describe":
			writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":`+stagePayload+`}`)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 15, MaxWaitTimeoutMs: 15, MaxConsecutiveFailures: 3})

	first, err := WatchBuild(context.Background(), deps, WatchBuildRequest{Job: "app", Build: 42})
	if err != nil {
		t.Fatalf("first WatchBuild() error = %v", err)
	}
	if first.Watch.State == "" {
		t.Fatal("first WatchBuild() returned empty state")
	}

	second, err := WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 15,
	})
	if err != nil {
		t.Fatalf("second WatchBuild() error = %v", err)
	}
	if !second.Watch.TimedOut {
		t.Fatal("second WatchBuild() did not time out")
	}
}

func TestWatchBuildToleratesTransientPollingFailures(t *testing.T) {
	var buildRequests int
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			buildRequests++
			if buildRequests <= 2 {
				http.Error(w, "temporary failure", http.StatusBadGateway)
				return
			}
			writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"","building":true,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
		case "/job/app/42/wfapi/describe":
			writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":"IN_PROGRESS"}]}`)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 50, MaxWaitTimeoutMs: 50, MaxConsecutiveFailures: 3})

	got, err := WatchBuild(context.Background(), deps, WatchBuildRequest{Job: "app", Build: 42})
	if err != nil {
		t.Fatalf("WatchBuild() error = %v", err)
	}
	if got.Watch.State == "" {
		t.Fatal("WatchBuild() returned empty state after transient failures")
	}
}

func TestWatchBuildReturnsImmediatelyWhenBuildAlreadyComplete(t *testing.T) {
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"SUCCESS","building":false,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
		case "/job/app/42/wfapi/describe":
			writeJSON(w, `{"id":"42","name":"#42","status":"SUCCESS","stages":[{"id":"1","name":"Build","status":"SUCCESS"}]}`)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 100, MaxWaitTimeoutMs: 100, MaxConsecutiveFailures: 3})

	first, err := WatchBuild(context.Background(), deps, WatchBuildRequest{Job: "app", Build: 42})
	if err != nil {
		t.Fatalf("first WatchBuild() error = %v", err)
	}

	second, err := WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 100,
	})
	if err != nil {
		t.Fatalf("second WatchBuild() error = %v", err)
	}
	if second.Watch.TimedOut {
		t.Fatal("second WatchBuild() timed out for a completed build")
	}
	if !second.Watch.Complete {
		t.Fatal("second WatchBuild() did not report completed build")
	}
}

func TestWatchBuildDegradesWhenPipelineEndpointIsFlaky(t *testing.T) {
	var pipelineRequests int
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"","building":true,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
		case "/job/app/42/wfapi/describe":
			pipelineRequests++
			if pipelineRequests == 1 {
				writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":"IN_PROGRESS"}]}`)
				return
			}
			http.Error(w, "temporary wfapi failure", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 25, MaxWaitTimeoutMs: 25, MaxConsecutiveFailures: 3})

	first, err := WatchBuild(context.Background(), deps, WatchBuildRequest{Job: "app", Build: 42})
	if err != nil {
		t.Fatalf("first WatchBuild() error = %v", err)
	}
	second, err := WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 25,
	})
	if err != nil {
		t.Fatalf("second WatchBuild() error = %v", err)
	}
	if !second.Watch.TimedOut {
		t.Fatal("second WatchBuild() should time out when only wfapi is flaky")
	}
	if second.Watch.Complete {
		t.Fatal("second WatchBuild() unexpectedly marked build complete")
	}
	if second.Watch.Pipeline == nil {
		t.Fatal("second WatchBuild() lost pipeline context during transient wfapi degradation")
	}
	if second.Watch.Pipeline.Status != first.Watch.Pipeline.Status {
		t.Fatalf("second WatchBuild() pipeline status = %q, want %q", second.Watch.Pipeline.Status, first.Watch.Pipeline.Status)
	}
}

func TestWatchBuildPreservesPipelineSnapshotWhenBuildCompletesDuringWfapiOutage(t *testing.T) {
	var buildRequests int
	var pipelineRequests int
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			buildRequests++
			if buildRequests == 1 {
				writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"","building":true,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
				return
			}
			writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"SUCCESS","building":false,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
		case "/job/app/42/wfapi/describe":
			pipelineRequests++
			if pipelineRequests == 1 {
				writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Deploy","status":"IN_PROGRESS"}]}`)
				return
			}
			http.Error(w, "temporary wfapi failure", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 50, MaxWaitTimeoutMs: 50, MaxConsecutiveFailures: 3})

	first, err := WatchBuild(context.Background(), deps, WatchBuildRequest{Job: "app", Build: 42})
	if err != nil {
		t.Fatalf("first WatchBuild() error = %v", err)
	}
	second, err := WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 50,
	})
	if err != nil {
		t.Fatalf("second WatchBuild() error = %v", err)
	}
	if !second.Watch.Complete {
		t.Fatal("second WatchBuild() did not report completed build")
	}
	if second.Watch.Pipeline == nil {
		t.Fatal("second WatchBuild() lost pipeline snapshot during wfapi outage")
	}
	if second.Watch.Pipeline.Status != first.Watch.Pipeline.Status {
		t.Fatalf("second WatchBuild() pipeline status = %q, want %q", second.Watch.Pipeline.Status, first.Watch.Pipeline.Status)
	}
}

func TestWatchBuildDoesNotMaskBrokenPipelineMetadata(t *testing.T) {
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"","building":true,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
		case "/job/app/42/wfapi/describe":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"id":`)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 25, MaxWaitTimeoutMs: 25, MaxConsecutiveFailures: 3})

	_, err := WatchBuild(context.Background(), deps, WatchBuildRequest{Job: "app", Build: 42})
	if err == nil {
		t.Fatal("WatchBuild() masked broken pipeline metadata")
	}
	assertAppErrorCode(t, err, apperrors.CodeJenkins)
}

func TestWatchBuildRejectsStateFromDifferentBuild(t *testing.T) {
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"","building":true,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
		case "/job/app/42/wfapi/describe":
			writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":"IN_PROGRESS"}]}`)
		case "/job/other/42/api/json":
			writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/other/42/","result":"","building":true,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
		case "/job/other/42/wfapi/describe":
			writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":"IN_PROGRESS"}]}`)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 25, MaxWaitTimeoutMs: 25, MaxConsecutiveFailures: 3})

	first, err := WatchBuild(context.Background(), deps, WatchBuildRequest{Job: "app", Build: 42})
	if err != nil {
		t.Fatalf("first WatchBuild() error = %v", err)
	}

	_, err = WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:           "other",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 25,
	})
	if err == nil {
		t.Fatal("WatchBuild() accepted state token from a different build")
	}
}

func TestWatchBuildTimesOutAfterDeadlineWhenPollingKeepsFailing(t *testing.T) {
	var requests int
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			requests++
			if requests == 1 {
				writeJSON(w, `{"number":42,"url":"https://jenkins.example.com/job/app/42/","result":"","building":true,"timestamp":1,"duration":10,"artifacts":[],"actions":[],"changeSets":[]}`)
				return
			}
			http.Error(w, "temporary failure", http.StatusBadGateway)
		case "/job/app/42/wfapi/describe":
			writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":"IN_PROGRESS"}]}`)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 15, MaxWaitTimeoutMs: 15, MaxConsecutiveFailures: 100})

	first, err := WatchBuild(context.Background(), deps, WatchBuildRequest{Job: "app", Build: 42})
	if err != nil {
		t.Fatalf("first WatchBuild() error = %v", err)
	}
	second, err := WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 15,
	})
	if err != nil {
		t.Fatalf("second WatchBuild() error = %v", err)
	}
	if !second.Watch.TimedOut {
		t.Fatal("second WatchBuild() did not time out after repeated polling failures")
	}
	if second.Watch.State != first.Watch.State {
		t.Fatal("second WatchBuild() changed state after repeated polling failures")
	}
	if second.Watch.Build.URL != first.Watch.Build.URL {
		t.Fatalf("second WatchBuild() build URL = %q, want %q", second.Watch.Build.URL, first.Watch.Build.URL)
	}
	if second.Watch.Pipeline == nil {
		t.Fatal("second WatchBuild() lost pipeline context after repeated polling failures")
	}
	if second.Watch.Pipeline.Status != first.Watch.Pipeline.Status {
		t.Fatalf("second WatchBuild() pipeline status = %q, want %q", second.Watch.Pipeline.Status, first.Watch.Pipeline.Status)
	}
	if len(second.Watch.Pipeline.Stages) != len(first.Watch.Pipeline.Stages) {
		t.Fatalf("second WatchBuild() pipeline stages = %d, want %d", len(second.Watch.Pipeline.Stages), len(first.Watch.Pipeline.Stages))
	}
}

func TestWatchBuildBootstrapHonorsTimeoutOnRepeatedFailures(t *testing.T) {
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			http.Error(w, "temporary failure", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 15, MaxWaitTimeoutMs: 15, MaxConsecutiveFailures: 100})

	_, err := WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		WaitTimeoutMs: 15,
	})
	if err == nil {
		t.Fatal("WatchBuild() succeeded despite repeated bootstrap failures")
	}
}

func TestWatchBuildRejectsForgedUnsignedStateToken(t *testing.T) {
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 15, MaxWaitTimeoutMs: 15, MaxConsecutiveFailures: 100})

	forgedPayload, err := rawUnsignedWatchStateToken(watchState{
		Version: 1,
		Target: watchTargetState{
			Controller: "default",
			Job:        "app",
			Build:      42,
		},
		Summary: model.BuildSummary{
			Number:   42,
			URL:      "https://evil.example.com/job/app/42/",
			Building: true,
		},
		Build: watchBuildState{
			Building: true,
		},
	})
	if err != nil {
		t.Fatalf("rawUnsignedWatchStateToken() error = %v", err)
	}
	forged := forgedPayload + ".invalid-signature"

	_, err = WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     forged,
		WaitTimeoutMs: 15,
	})
	if err == nil {
		t.Fatal("WatchBuild() accepted forged unsigned watch state")
	}
	assertAppErrorCode(t, err, apperrors.CodeInvalidRequest)
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("WatchBuild() error = %v, want expired/re-bootstrap guidance", err)
	}
}

func TestWatchBuildRejectsMalformedStateToken(t *testing.T) {
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 15, MaxWaitTimeoutMs: 15, MaxConsecutiveFailures: 100})

	_, err := WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:       "app",
		Build:     42,
		LastState: "not-a-valid-watch-token",
	})
	if err == nil {
		t.Fatal("WatchBuild() accepted malformed watch state")
	}
	assertAppErrorCode(t, err, apperrors.CodeInvalidRequest)
}

func TestWatchBuildRejectsOversizedStateToken(t *testing.T) {
	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 15, MaxWaitTimeoutMs: 15, MaxConsecutiveFailures: 100})

	oversized := strings.Repeat("a", maxWatchStateTokenBytes+1)
	_, err := WatchBuild(context.Background(), deps, WatchBuildRequest{
		Job:       "app",
		Build:     42,
		LastState: oversized,
	})
	if err == nil {
		t.Fatal("WatchBuild() accepted oversized watch state")
	}
	assertAppErrorCode(t, err, apperrors.CodeInvalidRequest)
}

func newWatchTestDeps(t *testing.T, handler http.HandlerFunc, watchCfg config.WatchConfig) Deps {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := jenkinsclient.New(config.ControllerConfig{ID: "default", URL: server.URL}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("client.New() error = %v", err)
	}

	cfg := config.Defaults()
	cfg.Controllers = []config.ControllerConfig{{ID: "default", URL: server.URL}}
	cfg.DefaultController = "default"
	cfg.Watch = watchCfg

	return Deps{
		Config:  cfg,
		Jenkins: map[string]*jenkinsapi.API{"default": jenkinsapi.New("default", client)},
	}
}

func newJenkinsTestDeps(t *testing.T, handler http.HandlerFunc) Deps {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := jenkinsclient.New(config.ControllerConfig{ID: "default", URL: server.URL}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("client.New() error = %v", err)
	}

	cfg := config.Defaults()
	cfg.Controllers = []config.ControllerConfig{{ID: "default", URL: server.URL}}
	cfg.DefaultController = "default"

	return Deps{
		Config:  cfg,
		Jenkins: map[string]*jenkinsapi.API{"default": jenkinsapi.New("default", client)},
	}
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, body)
}

func rawUnsignedWatchStateToken(state watchState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func assertAppErrorCode(t *testing.T, err error, want apperrors.Code) {
	t.Helper()
	appErr, ok := err.(*apperrors.Error)
	if !ok {
		t.Fatalf("error type = %T, want *errors.Error", err)
	}
	if appErr.Code != want {
		t.Fatalf("error code = %q, want %q", appErr.Code, want)
	}
}
