package jenkins

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/updatecheck"
	"github.com/stretchr/testify/require"
)

func TestCapabilitiesIncludesUpdateStatus(t *testing.T) {
	r := require.New(t)

	got, err := Capabilities(t.Context(), Deps{
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
	r.NoError(err, "Capabilities() error")
	r.True(got.Updates.UpdateAvailable, "updates.updateAvailable")
	r.Equal("v1.2.4", got.Updates.LatestVersion, "updates.latestVersion")
	r.NotEmpty(got.Updates.NotificationHint, "updates.notificationHint")
}

func TestCapabilitiesLabelsPluginDiscoveryFailureAsOptionalWarning(t *testing.T) {
	r := require.New(t)

	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/json":
			writeJSON(w, `{"nodeName":"built-in","useSecurity":true}`)
		case "/pluginManager/api/json":
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	})

	got, err := Capabilities(t.Context(), deps, BaseRequest{})
	r.NoError(err, "Capabilities() error")
	r.Len(got.Capabilities, 1, "capabilities")
	caps := got.Capabilities[0]
	r.True(caps.Controller.Available, "controller should remain available when plugin discovery fails")
	r.NotEmpty(caps.Error, "legacy error field should remain populated for compatibility")
	r.Len(caps.Warnings, 1, "warnings")
	warning := caps.Warnings[0]
	r.Equal("optional_plugin_discovery_failed", warning.Code, "warning code")
	r.Equal("plugins", warning.Source, "warning source")
	r.True(warning.Optional, "warning optional")
	r.NotEmpty(warning.Error, "warning.error should include the underlying failure")
}

func TestCapabilitiesCanSkipPluginDiscovery(t *testing.T) {
	r := require.New(t)

	var pluginRequests int32
	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/json":
			writeJSON(w, `{"nodeName":"built-in","useSecurity":true}`)
		case "/pluginManager/api/json":
			atomic.AddInt32(&pluginRequests, 1)
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	})
	deps.Config.Capabilities.PluginDiscoveryEnabled = false

	got, err := Capabilities(t.Context(), deps, BaseRequest{})
	r.NoError(err, "Capabilities() error")
	r.Equal(int32(0), atomic.LoadInt32(&pluginRequests), "plugin discovery endpoint should not be queried when disabled")
	r.False(got.CapabilityConfig.PluginDiscoveryEnabled, "response should report plugin discovery disabled")
	r.Len(got.Capabilities, 1, "capabilities")
	r.Len(got.Capabilities[0].Warnings, 1, "warnings")
	warning := got.Capabilities[0].Warnings[0]
	r.Equal("optional_plugin_discovery_disabled", warning.Code, "warning code")
	r.True(warning.Optional, "warning optional")
	r.Empty(warning.Error, "warning error")
}

func TestTriggerBuildRequiresMutationEnablement(t *testing.T) {
	r := require.New(t)

	_, err := TriggerBuild(t.Context(), Deps{
		Config: config.Config{
			DefaultController: "default",
			Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		},
	}, TriggerBuildRequest{Job: "app"})
	r.Error(err, "TriggerBuild() succeeded with mutations disabled")
}

func TestResolveBuildURL(t *testing.T) {
	r := require.New(t)

	ref, err := resolveBuildURL(config.Config{
		Controllers: []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
	}, "https://jenkins.example.com/job/weather-station/job/weather-station-server/job/main/104/")
	r.NoError(err, "resolveBuildURL() error")
	r.Equal("default", ref.Controller, "reference controller")
	r.Equal("weather-station/weather-station-server/main", ref.Job, "reference job")
	r.Equal(104, ref.Build, "reference build")
}

func TestResolveBuildURLRejectsUnknownController(t *testing.T) {
	r := require.New(t)

	_, err := resolveBuildURL(config.Config{
		Controllers: []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
	}, "https://other.example.com/job/app/1/")
	r.Error(err, "resolveBuildURL() accepted unknown controller")
}

func TestResolveBuildURLPrefersMostSpecificControllerPath(t *testing.T) {
	r := require.New(t)

	cfg := config.Config{
		Controllers: []config.ControllerConfig{
			{ID: "root", URL: "https://ci.example.com"},
			{ID: "jenkins", URL: "https://ci.example.com/jenkins"},
			{ID: "jenkins-alt", URL: "https://ci.example.com/jenkins-alt"},
		},
	}

	ref, err := resolveBuildURL(cfg, "https://ci.example.com/jenkins/job/app/42/")
	r.NoError(err, "resolveBuildURL() error")
	r.Equal("jenkins", ref.Controller, "reference controller")
	r.Equal("app", ref.Job, "reference job")
	r.Equal(42, ref.Build, "reference build")

	ref, err = resolveBuildURL(cfg, "https://ci.example.com/jenkins-alt/job/api/7/")
	r.NoError(err, "resolveBuildURL() error")
	r.Equal("jenkins-alt", ref.Controller, "reference controller")
	r.Equal("api", ref.Job, "reference job")
	r.Equal(7, ref.Build, "reference build")
}

func TestResolveBuildURLMatchesControllerPathWithTrailingSlash(t *testing.T) {
	r := require.New(t)

	cfg := config.Config{
		Controllers: []config.ControllerConfig{
			{ID: "jenkins", URL: "https://ci.example.com/jenkins/"},
		},
	}

	ref, err := resolveBuildURL(cfg, "https://ci.example.com/jenkins/job/app/42/")
	r.NoError(err, "resolveBuildURL() error")
	r.Equal("jenkins", ref.Controller, "reference controller")
	r.Equal("app", ref.Job, "reference job")
	r.Equal(42, ref.Build, "reference build")
}

func TestValidateTriggerParametersRejectsUnknown(t *testing.T) {
	r := require.New(t)

	err := validateTriggerParameters([]model.ParameterDefinition{{Name: "BRANCH"}}, map[string]string{"UNKNOWN": "main"})
	r.Error(err, "validateTriggerParameters() accepted unknown parameter")
}

func TestValidateTriggerParametersRequiresRequired(t *testing.T) {
	r := require.New(t)

	err := validateTriggerParameters([]model.ParameterDefinition{{Name: "BRANCH", Required: true}}, nil)
	r.Error(err, "validateTriggerParameters() accepted missing required parameter")
}

func TestValidateTriggerParametersAcceptsKnown(t *testing.T) {
	r := require.New(t)

	err := validateTriggerParameters([]model.ParameterDefinition{{Name: "BRANCH", Required: true}}, map[string]string{"BRANCH": "main"})
	r.NoError(err, "validateTriggerParameters() error")
}

func TestListJobsDerivesStatusAndAppliesFilters(t *testing.T) {
	r := require.New(t)

	treeCh := make(chan string, 1)
	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/json" {
			http.NotFound(w, r)
			return
		}
		tree := r.URL.Query().Get("tree")
		treeCh <- tree
		writeJSON(w, `{"jobs":[
			{"name":"deploy-main","url":"https://jenkins.example.com/job/deploy-main/","color":"red_anime","_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","lastBuild":{"number":12,"result":"","building":true},"lastCompletedBuild":{"number":11,"result":"FAILURE","building":false}},
			{"name":"deploy-old","url":"https://jenkins.example.com/job/deploy-old/","color":"blue","_class":"hudson.model.FreeStyleProject","lastBuild":{"number":3,"result":"SUCCESS","building":false}},
			{"name":"tests","url":"https://jenkins.example.com/job/tests/","color":"yellow","_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob"}
		]}`)
	})

	building := true
	got, err := ListJobs(t.Context(), deps, ListJobsRequest{
		NameContains: "DEPLOY",
		Type:         "pipeline",
		Status:       "failure",
		Building:     &building,
	})
	r.NoError(err, "ListJobs() error")
	tree := <-treeCh
	r.Contains(tree, "lastBuild[number,result,building]", "tree query should include lastBuild status fields")
	r.Contains(tree, "lastCompletedBuild[number,result,building]", "tree query should include lastCompletedBuild status fields")
	r.Contains(tree, "disabled", "tree query should include disabled field")
	r.Len(got.Items, 1, "ListJobs() items")
	job := got.Items[0]
	r.Equal("deploy-main", job.Name, "job name")
	r.Equal("failed", job.Status, "job status")
	r.True(job.Building, "job building")
	r.Equal(100, got.Limit, "pagination limit")
	r.False(got.HasMore, "pagination hasMore")
	r.False(got.Truncated, "pagination truncated")
	r.Empty(got.NextCursor, "pagination next cursor")
}

func TestListJobsRegexMatchesFullName(t *testing.T) {
	r := require.New(t)

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

	got, err := ListJobs(t.Context(), deps, ListJobsRequest{Folder: "team", NameRegex: "^team/api"})
	r.NoError(err, "ListJobs() error")
	r.Len(got.Items, 1, "ListJobs() items")
	r.Equal("team/api-main", got.Items[0].FullName, "job full name")
}

func TestListJobsDisabledStatusWinsOverCompletedBuildResult(t *testing.T) {
	r := require.New(t)

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

	got, err := ListJobs(t.Context(), deps, ListJobsRequest{Status: "disabled"})
	r.NoError(err, "ListJobs() error")
	r.Len(got.Items, 1, "ListJobs() items")
	r.Equal("disabled-job", got.Items[0].Name, "job name")
	r.Equal("disabled", got.Items[0].Status, "job status")
	r.NotNil(got.Items[0].Disabled, "job disabled")
	r.True(*got.Items[0].Disabled, "job disabled")
}

func TestListJobsPagesNonRecursiveResults(t *testing.T) {
	r := require.New(t)

	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/json" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, `{"jobs":[
			{"name":"one","url":"https://jenkins.example.com/job/one/","color":"blue","_class":"hudson.model.FreeStyleProject"},
			{"name":"two","url":"https://jenkins.example.com/job/two/","color":"blue","_class":"hudson.model.FreeStyleProject"},
			{"name":"three","url":"https://jenkins.example.com/job/three/","color":"blue","_class":"hudson.model.FreeStyleProject"}
		]}`)
	})

	first, err := ListJobs(t.Context(), deps, ListJobsRequest{Limit: 2})
	r.NoError(err, "ListJobs() first page error")
	r.Len(first.Items, 2, "first page items")
	r.Equal("one", first.Items[0].Name, "first item name")
	r.Equal("two", first.Items[1].Name, "second item name")
	r.True(first.HasMore, "first page hasMore")
	r.True(first.Truncated, "first page truncated")
	r.NotEmpty(first.NextCursor, "first page next cursor")
	r.Equal(2, first.Limit, "first page limit")

	second, err := ListJobs(t.Context(), deps, ListJobsRequest{Limit: 2, Cursor: first.NextCursor})
	r.NoError(err, "ListJobs() second page error")
	r.Len(second.Items, 1, "second page items")
	r.Equal("three", second.Items[0].Name, "second page item name")
	r.False(second.HasMore, "second page hasMore")
	r.False(second.Truncated, "second page truncated")
	r.Empty(second.NextCursor, "second page next cursor")
	r.Equal(2, second.Limit, "second page limit")
}

func TestListJobsRejectsCursorForDifferentRequest(t *testing.T) {
	r := require.New(t)

	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/json" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, `{"jobs":[
			{"name":"one","url":"https://jenkins.example.com/job/one/","color":"blue","_class":"hudson.model.FreeStyleProject"},
			{"name":"two","url":"https://jenkins.example.com/job/two/","color":"blue","_class":"hudson.model.FreeStyleProject"}
		]}`)
	})

	first, err := ListJobs(t.Context(), deps, ListJobsRequest{Limit: 1})
	r.NoError(err, "ListJobs() first page error")
	_, err = ListJobs(t.Context(), deps, ListJobsRequest{Limit: 1, NameContains: "two", Cursor: first.NextCursor})
	r.Error(err, "ListJobs() accepted cursor for a changed request")
	assertAppErrorCode(t, err, apperrors.CodeInvalidRequest)
}

func TestListJobsRecursiveDetectsTruncationAcrossFolderBoundary(t *testing.T) {
	r := require.New(t)

	var requested []string
	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/api/json":
			writeJSON(w, `{"jobs":[
				{"name":"folder-a","url":"https://jenkins.example.com/job/folder-a/","color":"blue","_class":"com.cloudbees.hudson.plugins.folder.Folder"},
				{"name":"folder-b","url":"https://jenkins.example.com/job/folder-b/","color":"blue","_class":"com.cloudbees.hudson.plugins.folder.Folder"}
			]}`)
		case "/job/folder-a/api/json":
			writeJSON(w, `{"jobs":[
				{"name":"job-1","url":"https://jenkins.example.com/job/folder-a/job/job-1/","color":"blue","_class":"hudson.model.FreeStyleProject"}
			]}`)
		case "/job/folder-b/api/json":
			writeJSON(w, `{"jobs":[
				{"name":"job-2","url":"https://jenkins.example.com/job/folder-b/job/job-2/","color":"blue","_class":"hudson.model.FreeStyleProject"}
			]}`)
		default:
			http.NotFound(w, r)
		}
	})

	got, err := ListJobs(t.Context(), deps, ListJobsRequest{Recursive: true, Limit: 1, Type: "freestyle"})
	r.NoError(err, "ListJobs() error")
	r.Len(got.Items, 1, "items")
	r.Equal("folder-a/job-1", got.Items[0].FullName, "item full name")
	r.True(got.HasMore, "pagination hasMore")
	r.True(got.Truncated, "pagination truncated")
	r.NotEmpty(got.NextCursor, "pagination next cursor")
	r.Contains(requested, "/job/folder-b/api/json", "requested paths")
}

func TestListJobsRecursiveUsesCursorForNextPage(t *testing.T) {
	r := require.New(t)

	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/json":
			writeJSON(w, `{"jobs":[
				{"name":"folder-a","url":"https://jenkins.example.com/job/folder-a/","color":"blue","_class":"com.cloudbees.hudson.plugins.folder.Folder"},
				{"name":"folder-b","url":"https://jenkins.example.com/job/folder-b/","color":"blue","_class":"com.cloudbees.hudson.plugins.folder.Folder"}
			]}`)
		case "/job/folder-a/api/json":
			writeJSON(w, `{"jobs":[
				{"name":"job-1","url":"https://jenkins.example.com/job/folder-a/job/job-1/","color":"blue","_class":"hudson.model.FreeStyleProject"}
			]}`)
		case "/job/folder-b/api/json":
			writeJSON(w, `{"jobs":[
				{"name":"job-2","url":"https://jenkins.example.com/job/folder-b/job/job-2/","color":"blue","_class":"hudson.model.FreeStyleProject"}
			]}`)
		default:
			http.NotFound(w, r)
		}
	})

	first, err := ListJobs(t.Context(), deps, ListJobsRequest{Recursive: true, Limit: 1, Type: "freestyle"})
	r.NoError(err, "ListJobs() first page error")
	second, err := ListJobs(t.Context(), deps, ListJobsRequest{Recursive: true, Limit: 1, Type: "freestyle", Cursor: first.NextCursor})
	r.NoError(err, "ListJobs() second page error")
	r.Len(second.Items, 1, "second page items")
	r.Equal("folder-b/job-2", second.Items[0].FullName, "second page item full name")
	r.False(second.HasMore, "second page hasMore")
	r.False(second.Truncated, "second page truncated")
	r.Empty(second.NextCursor, "second page next cursor")
}

func TestListBuildsPagesRecentBuildsWithSummaryFields(t *testing.T) {
	r := require.New(t)

	var trees []string
	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/job/app/api/json" {
			http.NotFound(w, req)
			return
		}
		tree := req.URL.Query().Get("tree")
		trees = append(trees, tree)
		switch {
		case strings.Contains(tree, "{0,2}"):
			writeJSON(w, `{"builds":[
				{"id":"43","number":43,"url":"https://jenkins.example.com/job/app/43/","result":"SUCCESS","building":false,"timestamp":1000,"duration":2000,"description":"deployed prod","displayName":"v1.2.3","queueId":99,"estimatedDuration":3000,"keepLog":true},
				{"id":"42","number":42,"url":"https://jenkins.example.com/job/app/42/","result":"FAILURE","building":false,"timestamp":900,"duration":1500,"description":"failed tests","displayName":"#42","queueId":98,"estimatedDuration":2500,"keepLog":false}
			]}`)
		case strings.Contains(tree, "{1,3}"):
			writeJSON(w, `{"builds":[
					{"id":"42","number":42,"url":"https://jenkins.example.com/job/app/42/","result":"FAILURE","building":false,"timestamp":900,"duration":1500,"description":"failed tests","displayName":"#42","queueId":98,"estimatedDuration":2500,"keepLog":false}
				]}`)
		default:
			r.Failf("unexpected tree query", "tree query %q", tree)
		}
	})

	first, err := ListBuilds(t.Context(), deps, ListBuildsRequest{Job: "app", Limit: 1})
	r.NoError(err, "ListBuilds() first page error")
	r.Len(first.Items, 1, "first page items")
	build := first.Items[0]
	r.Equal("43", build.ID, "first build ID")
	r.Equal("deployed prod", build.Description, "first build description")
	r.Equal("v1.2.3", build.DisplayName, "first build display name")
	r.Equal(int64(99), build.QueueID, "first build queue ID")
	r.Equal(int64(3000), build.EstimatedDuration, "first build estimated duration")
	r.NotNil(build.KeepLog, "first build keepLog")
	r.True(*build.KeepLog, "first build keepLog")
	r.Equal("SUCCESS", build.Result, "first build result")
	r.True(first.HasMore, "first page hasMore")
	r.True(first.Truncated, "first page truncated")
	r.NotEmpty(first.NextCursor, "first page next cursor")
	r.Equal(1, first.Limit, "first page limit")

	second, err := ListBuilds(t.Context(), deps, ListBuildsRequest{Job: "app", Limit: 1, Cursor: first.NextCursor})
	r.NoError(err, "ListBuilds() second page error")
	r.Len(second.Items, 1, "second page items")
	r.Equal(42, second.Items[0].Number, "second page build number")
	r.False(second.HasMore, "second page hasMore")
	r.False(second.Truncated, "second page truncated")
	r.Empty(second.NextCursor, "second page next cursor")
	r.Equal(1, second.Limit, "second page limit")
	r.Len(trees, 2, "tree queries")
}

func TestListBuildsRejectsCursorForDifferentRequest(t *testing.T) {
	r := require.New(t)

	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/job/app/api/json" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, `{"builds":[
			{"id":"43","number":43,"url":"https://jenkins.example.com/job/app/43/","result":"SUCCESS","building":false},
			{"id":"42","number":42,"url":"https://jenkins.example.com/job/app/42/","result":"FAILURE","building":false}
		]}`)
	})

	first, err := ListBuilds(t.Context(), deps, ListBuildsRequest{Job: "app", Limit: 1})
	r.NoError(err, "ListBuilds() first page error")
	_, err = ListBuilds(t.Context(), deps, ListBuildsRequest{Job: "other", Limit: 1, Cursor: first.NextCursor})
	r.Error(err, "ListBuilds() accepted cursor for a changed request")
	assertAppErrorCode(t, err, apperrors.CodeInvalidRequest)
}

func TestGetBuildIncludesExtendedSummaryFields(t *testing.T) {
	r := require.New(t)

	treeCh := make(chan string, 1)
	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/job/app/43/api/json" {
			http.NotFound(w, req)
			return
		}
		tree := req.URL.Query().Get("tree")
		treeCh <- tree
		writeJSON(w, `{
			"id":"43",
			"number":43,
			"url":"https://jenkins.example.com/job/app/43/",
			"result":"SUCCESS",
			"building":false,
			"timestamp":1000,
			"duration":2000,
			"description":"deployed prod",
			"displayName":"v1.2.3",
			"fullDisplayName":"app v1.2.3",
			"queueId":99,
			"estimatedDuration":3000,
			"keepLog":true,
			"artifacts":[],
			"actions":[],
			"changeSets":[]
		}`)
	})

	got, err := GetBuild(t.Context(), deps, BuildRequest{Job: "app", Build: 43})
	r.NoError(err, "GetBuild() error")
	tree := <-treeCh
	for _, want := range []string{"id", "queueId", "estimatedDuration", "keepLog"} {
		r.Contains(tree, want, "tree query")
	}
	build := got.Build
	r.Equal("43", build.ID, "build ID")
	r.Equal("deployed prod", build.Description, "build description")
	r.Equal("v1.2.3", build.DisplayName, "build display name")
	r.Equal("app v1.2.3", build.FullDisplayName, "build full display name")
	r.Equal(int64(99), build.QueueID, "build queue ID")
	r.Equal(int64(3000), build.EstimatedDuration, "build estimated duration")
	r.NotNil(build.KeepLog, "build keepLog")
	r.True(*build.KeepLog, "build keepLog")
}

func TestListJobsRejectsInvalidNameRegex(t *testing.T) {
	r := require.New(t)

	_, err := ListJobs(t.Context(), Deps{}, ListJobsRequest{NameRegex: "["})
	r.Error(err, "ListJobs() accepted invalid regex")
	assertAppErrorCode(t, err, apperrors.CodeInvalidRequest)
}

func TestGetJobReturnsDerivedStatusAndDisabledState(t *testing.T) {
	r := require.New(t)

	treeCh := make(chan string, 1)
	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/job/app/api/json" {
			http.NotFound(w, r)
			return
		}
		tree := r.URL.Query().Get("tree")
		treeCh <- tree
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

	got, err := GetJob(t.Context(), deps, JobRequest{Job: "app"})
	r.NoError(err, "GetJob() error")
	tree := <-treeCh
	r.Contains(tree, "disabled", "tree query should include disabled field")
	r.Contains(tree, "lastCompletedBuild[number,url,result,building,timestamp,duration]", "tree query should include lastCompletedBuild fields")
	r.Equal("disabled", got.Job.Status, "job status")
	r.True(got.Job.Building, "job building")
	r.NotNil(got.Job.Disabled, "job disabled")
	r.True(*got.Job.Disabled, "job disabled")
}

func TestGetJobConfigReturnsRedactedXMLAndSummary(t *testing.T) {
	r := require.New(t)

	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/github-org/api/json":
			writeJSON(w, `{
				"name":"github-org",
				"fullName":"github-org",
				"url":"https://jenkins.example.com/job/github-org/",
				"_class":"jenkins.branch.OrganizationFolder",
				"buildable":false,
				"inQueue":false,
				"property":[{"parameterDefinitions":[
					{"_class":"hudson.model.StringParameterDefinition","name":"BRANCH","defaultValue":"main"},
					{"_class":"hudson.model.PasswordParameterDefinition","name":"API_TOKEN","defaultValue":"fallback-secret-token"}
				]}]
			}`)
		case "/job/github-org/config.xml":
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `<?xml version='1.1' encoding='UTF-8'?>
				<jenkins.branch.OrganizationFolder plugin="branch-api">
					<description>GitHub org</description>
					<definition class="org.jenkinsci.plugins.workflow.cps.CpsFlowDefinition">
						<script>
							<!-- secret-comment-in-script -->
							<?jenkins secret-processing-instruction?>
							<step arg="secret-descendant-attribute"/>
							echo "secret-token-in-script"
						</script>
					</definition>
				<navigators>
					<org.jenkinsci.plugins.github_branch_source.GitHubSCMNavigator plugin="github-branch-source">
						<repoOwner>example-org</repoOwner>
						<remote>https://user:secret-url-token@github.com/example-org/repo.git?access_token=secret-query-token&amp;branch=main</remote>
						<credentialsId>github-token</credentialsId>
						<repository>https://user:secret-repository-token@github.com/example-org/repository.git?token=secret-repository-query-token&amp;branch=main</repository>
						<passphrase>ssh-private-key-passphrase</passphrase>
						<accessKey>AKIASECRETACCESSKEY</accessKey>
						<cloudCredential accessKeyId="AKIASECRETATTRIBUTE"/>
						<value defaultValue="secret-from-default-value-attribute" value="secret-from-value-attribute">secret-from-generic-value</value>
						<serverName>https://user:secret-server-token@api.github.com/org?token=secret-server-query-token&amp;region=au</serverName>
						<traits>
							<org.jenkinsci.plugins.github_branch_source.BranchDiscoveryTrait/>
						</traits>
					</org.jenkinsci.plugins.github_branch_source.GitHubSCMNavigator>
				</navigators>
				<projectFactories>
					<org.jenkinsci.plugins.workflow.multibranch.WorkflowBranchProjectFactory>
						<scriptPath>ci/Jenkinsfile</scriptPath>
					</org.jenkinsci.plugins.workflow.multibranch.WorkflowBranchProjectFactory>
				</projectFactories>
				<triggers>
					<com.cloudbees.hudson.plugins.folder.computed.PeriodicFolderTrigger plugin="cloudbees-folder"/>
				</triggers>
			</jenkins.branch.OrganizationFolder>`)
		default:
			http.NotFound(w, r)
		}
	})

	got, err := GetJobConfig(t.Context(), deps, JobConfigRequest{Job: "github-org", Mode: "both", MaxBytes: 4096})
	r.NoError(err, "GetJobConfig() error")
	config := got.Config
	r.True(config.ConfigAccessible, "config accessible")
	r.Equal("config.xml", config.Source, "source")
	r.Equal("both", config.Mode, "mode")
	r.Equal("organizationFolder", config.Summary.Kind, "kind")
	r.Equal("ci/Jenkinsfile", config.Summary.ScriptPath, "script path")
	r.NotEmpty(config.XML, "xml")
	r.NotContains(config.XML, "github-token", "credentials id should be redacted")
	r.NotContains(config.XML, "secret-token-in-script", "pipeline script body should be redacted")
	r.NotContains(config.XML, "secret-comment-in-script", "comments in sensitive subtrees should be redacted")
	r.NotContains(config.XML, "secret-processing-instruction", "processing instructions in sensitive subtrees should be redacted")
	r.NotContains(config.XML, "secret-descendant-attribute", "descendant attributes in sensitive subtrees should be redacted")
	r.NotContains(config.XML, "secret-from-generic-value", "generic value fields should be redacted")
	r.NotContains(config.XML, "secret-from-default-value-attribute", "default value attributes should be redacted")
	r.NotContains(config.XML, "secret-from-value-attribute", "value attributes should be redacted")
	r.NotContains(config.XML, "secret-url-token", "URL userinfo should be redacted")
	r.NotContains(config.XML, "secret-query-token", "URL query secrets should be redacted")
	r.NotContains(config.XML, "secret-server-token", "serverName URL userinfo should be redacted")
	r.NotContains(config.XML, "secret-server-query-token", "serverName URL query secrets should be redacted")
	r.NotContains(config.XML, "secret-repository-token", "repository URL userinfo should be redacted")
	r.NotContains(config.XML, "secret-repository-query-token", "repository URL query secrets should be redacted")
	r.NotContains(config.XML, "ssh-private-key-passphrase", "passphrases should be redacted")
	r.NotContains(config.XML, "AKIASECRETACCESSKEY", "access key elements should be redacted")
	r.NotContains(config.XML, "AKIASECRETATTRIBUTE", "access key attributes should be redacted")
	r.Contains(config.XML, "[REDACTED]", "xml should show redaction placeholder")
	r.Contains(config.Warnings[0].Message, "Sensitive and high-risk", "redaction warning")
	r.NotNil(config.Summary.Buildable, "buildable should be preserved from api/json fallback")
	r.False(*config.Summary.Buildable, "buildable")
	r.Len(config.Summary.Parameters, 2, "parameters should be preserved from api/json fallback")
	r.Equal("BRANCH", config.Summary.Parameters[0].Name, "parameter name")
	r.Equal("main", config.Summary.Parameters[0].Default, "non-sensitive parameter default")
	r.Equal("API_TOKEN", config.Summary.Parameters[1].Name, "sensitive parameter name")
	r.Equal("[REDACTED]", config.Summary.Parameters[1].Default, "sensitive parameter default")
	r.Len(config.Summary.Sources, 1, "sources")
	r.Equal("navigator", config.Summary.Sources[0].Kind, "source kind")
	r.Equal("example-org", config.Summary.Sources[0].RepoOwner, "repo owner")
	r.Contains(config.Summary.Sources[0].Remote, "github.com/example-org/repo.git", "summary remote should preserve repository location")
	r.Contains(config.Summary.Sources[0].Remote, "branch=main", "summary remote should preserve non-sensitive query parameters")
	r.NotContains(config.Summary.Sources[0].Remote, "secret-url-token", "summary remote should redact URL userinfo")
	r.NotContains(config.Summary.Sources[0].Remote, "secret-query-token", "summary remote should redact URL query secrets")
	r.Contains(config.Summary.Sources[0].ServerURL, "api.github.com/org", "summary server URL should preserve server location")
	r.Contains(config.Summary.Sources[0].ServerURL, "region=au", "summary server URL should preserve non-sensitive query parameters")
	r.NotContains(config.Summary.Sources[0].ServerURL, "secret-server-token", "summary server URL should redact URL userinfo")
	r.NotContains(config.Summary.Sources[0].ServerURL, "secret-server-query-token", "summary server URL should redact URL query secrets")
	r.Contains(config.Summary.Sources[0].Repository, "github.com/example-org/repository.git", "summary repository should preserve repository location")
	r.Contains(config.Summary.Sources[0].Repository, "branch=main", "summary repository should preserve non-sensitive query parameters")
	r.NotContains(config.Summary.Sources[0].Repository, "secret-repository-token", "summary repository should redact URL userinfo")
	r.NotContains(config.Summary.Sources[0].Repository, "secret-repository-query-token", "summary repository should redact URL query secrets")
	r.Equal("[REDACTED]", config.Summary.Sources[0].CredentialsID, "summary credentials id")
	r.Len(config.Summary.ProjectFactories, 1, "project factories")
	r.Len(config.Summary.Triggers, 1, "triggers")
}

func TestGetJobConfigFallsBackWhenConfigXMLForbidden(t *testing.T) {
	r := require.New(t)

	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/job/main/api/json":
			writeJSON(w, `{
				"name":"main",
				"fullName":"app/main",
				"url":"https://jenkins.example.com/job/app/job/main/",
				"_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob",
				"buildable":true,
				"inQueue":false,
				"property":[{"parameterDefinitions":[
					{"_class":"hudson.model.StringParameterDefinition","name":"BRANCH","defaultValue":"main"},
					{"_class":"hudson.model.PasswordParameterDefinition","name":"PASSWORD","defaultValue":"fallback-secret-password","choices":["fallback-secret-choice"]},
					{"_class":"hudson.model.ChoiceParameterDefinition","name":"API_TOKEN","defaultValue":"fallback-secret-token","choices":["fallback-secret-token-choice"]},
					{"_class":"hudson.model.ChoiceParameterDefinition","name":"REPO_URL","defaultValue":"https://user:secret-url-token@github.com/example/app.git?token=secret-query-token&branch=main","choices":["https://user:secret-choice-token@github.com/example/app.git?token=secret-choice-query-token&branch=main","main"]}
				]}]
			}`)
		case "/job/app/job/main/config.xml":
			http.Error(w, `missing Extended Read permission <password>body-secret</password> https://user:secret-body-token@github.com/example/app.git?token=secret-body-query-token `+strings.Repeat("deployment-detail ", 100), http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	})

	got, err := GetJobConfig(t.Context(), deps, JobConfigRequest{Job: "app/main"})
	r.NoError(err, "GetJobConfig() error")
	config := got.Config
	r.False(config.ConfigAccessible, "config accessible")
	r.Equal("api/json", config.Source, "source")
	r.Equal("branchJob", config.Summary.Kind, "kind")
	r.NotEmpty(config.AccessError, "access error")
	r.Len(config.Warnings, 1, "warnings")
	r.Equal("config_permission_denied", config.Warnings[0].Code, "warning code")
	r.Contains(config.Warnings[0].Detail, "body omitted", "warning detail should not include raw config.xml response body")
	r.NotContains(config.Warnings[0].Detail, "body-secret", "warning detail should not leak reflected XML secrets")
	r.NotContains(config.Warnings[0].Detail, "secret-body-token", "warning detail should not leak URL userinfo")
	r.NotContains(config.Warnings[0].Detail, "secret-body-query-token", "warning detail should not leak URL query tokens")
	r.LessOrEqual(len(config.Warnings[0].Detail), 160, "warning detail should be bounded")
	r.Len(config.Summary.Parameters, 4, "parameters")
	r.Equal("BRANCH", config.Summary.Parameters[0].Name, "parameter name")
	r.Equal("main", config.Summary.Parameters[0].Default, "non-sensitive parameter default")
	r.Equal("PASSWORD", config.Summary.Parameters[1].Name, "sensitive parameter name")
	r.Equal("[REDACTED]", config.Summary.Parameters[1].Default, "sensitive parameter default")
	r.Empty(config.Summary.Parameters[1].Choices, "sensitive password parameter choices should be cleared")
	r.Equal("API_TOKEN", config.Summary.Parameters[2].Name, "sensitive choice parameter name")
	r.Equal("[REDACTED]", config.Summary.Parameters[2].Default, "sensitive choice parameter default")
	r.Empty(config.Summary.Parameters[2].Choices, "sensitive token parameter choices should be cleared")
	r.Equal("REPO_URL", config.Summary.Parameters[3].Name, "url parameter name")
	r.NotContains(fmt.Sprint(config.Summary.Parameters[3].Default), "secret-url-token", "URL default userinfo should be redacted")
	r.NotContains(fmt.Sprint(config.Summary.Parameters[3].Default), "secret-query-token", "URL default query secrets should be redacted")
	r.Contains(fmt.Sprint(config.Summary.Parameters[3].Default), "branch=main", "URL default should preserve safe query parameters")
	r.Len(config.Summary.Parameters[3].Choices, 2, "non-sensitive choices should be preserved")
	r.NotContains(config.Summary.Parameters[3].Choices[0], "secret-choice-token", "URL choice userinfo should be redacted")
	r.NotContains(config.Summary.Parameters[3].Choices[0], "secret-choice-query-token", "URL choice query secrets should be redacted")
	r.Contains(config.Summary.Parameters[3].Choices[0], "branch=main", "URL choice should preserve safe query parameters")
	r.Equal("main", config.Summary.Parameters[3].Choices[1], "plain choice should be preserved")
}

func TestWatchBuildTimesOutWithoutSemanticChange(t *testing.T) {
	r := require.New(t)

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

	first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.NoError(err, "first WatchBuild() error")
	r.NotEmpty(first.Watch.State, "first WatchBuild() state")

	second, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 25,
	})
	r.NoError(err, "second WatchBuild() error")
	r.True(second.Watch.TimedOut, "second WatchBuild() timed out")
	r.Equal(first.Watch.State, second.Watch.State, "second WatchBuild() state")
}

func TestWatchBuildKeepsStateStableWhenOnlyDurationChanges(t *testing.T) {
	r := require.New(t)

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

	first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.NoError(err, "first WatchBuild() error")
	second, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 25,
	})
	r.NoError(err, "second WatchBuild() error")
	r.True(second.Watch.TimedOut, "second WatchBuild() did not time out when only duration changed")
	r.Equal(first.Watch.State, second.Watch.State, "second WatchBuild() changed state when only duration changed")
}

func TestWatchBuildReturnsWhenStageStatusChanges(t *testing.T) {
	r := require.New(t)

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

	first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.NoError(err, "first WatchBuild() error")
	second, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 100,
	})
	r.NoError(err, "second WatchBuild() error")
	r.False(second.Watch.TimedOut, "second WatchBuild() timed out despite a stage transition")
	r.NotEqual(first.Watch.State, second.Watch.State, "second WatchBuild() did not advance state")
	r.NotNil(second.Watch.Pipeline, "second WatchBuild() pipeline")
	r.GreaterOrEqual(len(second.Watch.Pipeline.Stages), 2, "second WatchBuild() pipeline stages")
	r.Equal("IN_PROGRESS", second.Watch.Pipeline.Stages[1].Status, "second WatchBuild() pipeline stage status")
}

func TestWatchBuildReturnsWhenPipelineRunStatusChanges(t *testing.T) {
	r := require.New(t)

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

	first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.NoError(err, "first WatchBuild() error")
	second, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 100,
	})
	r.NoError(err, "second WatchBuild() error")
	r.False(second.Watch.TimedOut, "second WatchBuild() timed out despite a pipeline run-status transition")
	r.NotEqual(first.Watch.State, second.Watch.State, "second WatchBuild() did not advance state on pipeline run-status change")
	r.NotNil(second.Watch.Pipeline, "second WatchBuild() pipeline")
	r.Equal("PAUSED_PENDING_INPUT", second.Watch.Pipeline.Status, "second WatchBuild() pipeline status")
}

func TestPipelineRunReportsPendingInputActions(t *testing.T) {
	r := require.New(t)

	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/wfapi/describe":
			writeJSON(w, `{"id":"42","name":"#42","status":"PAUSED_PENDING_INPUT","stages":[{"id":"1","name":"Deploy","status":"PAUSED_PENDING_INPUT"}]}`)
		case "/job/app/42/wfapi/pendingInputActions":
			writeJSON(w, `[{"id":"approve-prod","message":"Deploy to production?","proceedUrl":"input/approve-prod/proceedEmpty","abortUrl":"input/approve-prod/abort"}]`)
		default:
			http.NotFound(w, r)
		}
	})

	got, err := PipelineRun(t.Context(), deps, BuildRequest{Job: "app", Build: 42})
	r.NoError(err, "PipelineRun() error")
	r.True(got.Run.WaitingForInput, "PipelineRun() did not report waitingForInput")
	r.Len(got.Run.PendingInputActions, 1, "pendingInputActions")
	action := got.Run.PendingInputActions[0]
	r.Equal("approve-prod", action.ID, "pending input action ID")
	r.Equal("Deploy to production?", action.Message, "pending input action message")
	r.NotEmpty(action.ProceedURL, "pending input action proceed URL")
	r.NotEmpty(action.AbortURL, "pending input action abort URL")
}

func TestPipelineRunTreatsMissingPendingInputEndpointAsOptional(t *testing.T) {
	r := require.New(t)

	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/wfapi/describe":
			writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":"IN_PROGRESS"}]}`)
		case "/job/app/42/wfapi/pendingInputActions":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	got, err := PipelineRun(t.Context(), deps, BuildRequest{Job: "app", Build: 42})
	r.NoError(err, "PipelineRun() error")
	r.False(got.Run.WaitingForInput, "PipelineRun() unexpectedly reported waitingForInput")
}

func TestPipelineRunDerivesWaitingForInputFromStageStatus(t *testing.T) {
	r := require.New(t)

	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/wfapi/describe":
			writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Deploy","status":"PAUSED_PENDING_INPUT"}]}`)
		case "/job/app/42/wfapi/pendingInputActions":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	got, err := PipelineRun(t.Context(), deps, BuildRequest{Job: "app", Build: 42})
	r.NoError(err, "PipelineRun() error")
	r.True(got.Run.WaitingForInput, "PipelineRun() did not derive waitingForInput from stage status")
}

func TestPipelineRunReturnsStagesWithPendingInputEnrichmentError(t *testing.T) {
	r := require.New(t)

	deps := newJenkinsTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/wfapi/describe":
			writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":"IN_PROGRESS"}]}`)
		case "/job/app/42/wfapi/pendingInputActions":
			http.Error(w, "temporary failure", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	})

	got, err := PipelineRun(t.Context(), deps, BuildRequest{Job: "app", Build: 42})
	r.NoError(err, "PipelineRun() error")
	r.Len(got.Run.Stages, 1, "stages")
	r.NotEmpty(got.Run.PendingInputError, "PipelineRun() did not report pending input enrichment error")
}

func TestWatchBuildReturnsWhenPendingInputAppears(t *testing.T) {
	r := require.New(t)

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
			writeJSON(w, `{"id":"42","name":"#42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Deploy","status":"IN_PROGRESS"}]}`)
		case "/job/app/42/wfapi/pendingInputActions":
			if polls < 3 {
				writeJSON(w, `[]`)
				return
			}
			writeJSON(w, `[{"id":"approve-prod","message":"Deploy to production?","proceedUrl":"input/approve-prod/proceedEmpty","abortUrl":"input/approve-prod/abort"}]`)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 100, MaxWaitTimeoutMs: 100, MaxConsecutiveFailures: 3})

	first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.NoError(err, "first WatchBuild() error")
	r.NotNil(first.Watch.Pipeline, "first WatchBuild() pipeline")
	r.False(first.Watch.Pipeline.WaitingForInput, "first WatchBuild() pipeline waitingForInput")

	second, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 100,
	})
	r.NoError(err, "second WatchBuild() error")
	r.False(second.Watch.TimedOut, "second WatchBuild() timed out despite pending input")
	r.NotNil(second.Watch.Pipeline, "second WatchBuild() pipeline")
	r.True(second.Watch.Pipeline.WaitingForInput, "second WatchBuild() pipeline waitingForInput")
	r.Len(second.Watch.Pipeline.PendingInputActions, 1, "second WatchBuild() pending inputs")
	r.Equal("approve-prod", second.Watch.Pipeline.PendingInputActions[0].ID, "second WatchBuild() pending input ID")
}

func TestPipelineRunFromStateDerivesWaitingForInputFromPausedStatus(t *testing.T) {
	r := require.New(t)

	got := pipelineRunFromState(&watchState{
		Run: watchRunState{Status: "PAUSED_PENDING_INPUT"},
	})
	r.NotNil(got, "pipelineRunFromState() returned nil")
	r.True(got.WaitingForInput, "waitingForInput for status %q", got.Status)
}

func TestPipelineRunFromStateDerivesWaitingForInputFromPausedStage(t *testing.T) {
	r := require.New(t)

	got := pipelineRunFromState(&watchState{
		Stages: []watchStageState{{ID: "1", Name: "Deploy", Status: "PAUSED_PENDING_INPUT"}},
	})
	r.NotNil(got, "pipelineRunFromState() returned nil")
	r.True(got.WaitingForInput, "waitingForInput for stages %+v", got.Stages)
}

func TestWatchBuildLargeStageListStateRoundTrips(t *testing.T) {
	r := require.New(t)

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

	first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.NoError(err, "first WatchBuild() error")
	r.NotEmpty(first.Watch.State, "first WatchBuild() returned empty state")

	second, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 15,
	})
	r.NoError(err, "second WatchBuild() error")
	r.True(second.Watch.TimedOut, "second WatchBuild() did not time out")
}

func TestWatchBuildToleratesTransientPollingFailures(t *testing.T) {
	r := require.New(t)

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

	got, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.NoError(err, "WatchBuild() error")
	r.NotEmpty(got.Watch.State, "WatchBuild() returned empty state after transient failures")
}

func TestWatchBuildReturnsImmediatelyWhenBuildAlreadyComplete(t *testing.T) {
	r := require.New(t)

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

	first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.NoError(err, "first WatchBuild() error")

	second, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 100,
	})
	r.NoError(err, "second WatchBuild() error")
	r.False(second.Watch.TimedOut, "second WatchBuild() timed out for a completed build")
	r.True(second.Watch.Complete, "second WatchBuild() did not report completed build")
}

func TestWatchBuildDegradesWhenPipelineEndpointIsFlaky(t *testing.T) {
	r := require.New(t)

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

	first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.NoError(err, "first WatchBuild() error")
	second, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 25,
	})
	r.NoError(err, "second WatchBuild() error")
	r.True(second.Watch.TimedOut, "second WatchBuild() should time out when only wfapi is flaky")
	r.False(second.Watch.Complete, "second WatchBuild() unexpectedly marked build complete")
	r.NotNil(second.Watch.Pipeline, "second WatchBuild() lost pipeline context during transient wfapi degradation")
	r.Equal(first.Watch.Pipeline.Status, second.Watch.Pipeline.Status, "second WatchBuild() pipeline status")
}

func TestWatchBuildPreservesPipelineSnapshotWhenBuildCompletesDuringWfapiOutage(t *testing.T) {
	r := require.New(t)

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

	first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.NoError(err, "first WatchBuild() error")
	second, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 50,
	})
	r.NoError(err, "second WatchBuild() error")
	r.True(second.Watch.Complete, "second WatchBuild() did not report completed build")
	r.NotNil(second.Watch.Pipeline, "second WatchBuild() lost pipeline snapshot during wfapi outage")
	r.Equal(first.Watch.Pipeline.Status, second.Watch.Pipeline.Status, "second WatchBuild() pipeline status")
}

func TestWatchBuildDoesNotMaskBrokenPipelineMetadata(t *testing.T) {
	r := require.New(t)

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

	_, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.Error(err, "WatchBuild() masked broken pipeline metadata")
	assertAppErrorCode(t, err, apperrors.CodeJenkins)
}

func TestWatchBuildRejectsStateFromDifferentBuild(t *testing.T) {
	r := require.New(t)

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

	first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.NoError(err, "first WatchBuild() error")

	_, err = WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "other",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 25,
	})
	r.Error(err, "WatchBuild() accepted state token from a different build")
}

func TestWatchBuildTimesOutAfterDeadlineWhenPollingKeepsFailing(t *testing.T) {
	r := require.New(t)

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

	first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
	r.NoError(err, "first WatchBuild() error")
	second, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     first.Watch.State,
		WaitTimeoutMs: 15,
	})
	r.NoError(err, "second WatchBuild() error")
	r.True(second.Watch.TimedOut, "second WatchBuild() did not time out after repeated polling failures")
	r.Equal(first.Watch.State, second.Watch.State, "second WatchBuild() changed state after repeated polling failures")
	r.Equal(first.Watch.Build.URL, second.Watch.Build.URL, "second WatchBuild() build URL")
	r.NotNil(second.Watch.Pipeline, "second WatchBuild() lost pipeline context after repeated polling failures")
	r.Equal(first.Watch.Pipeline.Status, second.Watch.Pipeline.Status, "second WatchBuild() pipeline status")
	r.Len(second.Watch.Pipeline.Stages, len(first.Watch.Pipeline.Stages), "second WatchBuild() pipeline stages")
}

func TestWatchBuildBootstrapHonorsTimeoutOnRepeatedFailures(t *testing.T) {
	r := require.New(t)

	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/app/42/api/json":
			http.Error(w, "temporary failure", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 15, MaxWaitTimeoutMs: 15, MaxConsecutiveFailures: 100})

	_, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		WaitTimeoutMs: 15,
	})
	r.Error(err, "WatchBuild() succeeded despite repeated bootstrap failures")
}

func TestWatchBuildRejectsForgedUnsignedStateToken(t *testing.T) {
	r := require.New(t)

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
	r.NoError(err, "rawUnsignedWatchStateToken() error")
	forged := forgedPayload + ".invalid-signature"

	_, err = WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:           "app",
		Build:         42,
		LastState:     forged,
		WaitTimeoutMs: 15,
	})
	r.Error(err, "WatchBuild() accepted forged unsigned watch state")
	assertAppErrorCode(t, err, apperrors.CodeInvalidRequest)
	r.Contains(err.Error(), "expired", "WatchBuild() error should include expired/re-bootstrap guidance")
}

func TestWatchBuildRejectsMalformedStateToken(t *testing.T) {
	r := require.New(t)

	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 15, MaxWaitTimeoutMs: 15, MaxConsecutiveFailures: 100})

	_, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:       "app",
		Build:     42,
		LastState: "not-a-valid-watch-token",
	})
	r.Error(err, "WatchBuild() accepted malformed watch state")
	assertAppErrorCode(t, err, apperrors.CodeInvalidRequest)
}

func TestWatchBuildRejectsOversizedStateToken(t *testing.T) {
	r := require.New(t)

	deps := newWatchTestDeps(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 15, MaxWaitTimeoutMs: 15, MaxConsecutiveFailures: 100})

	oversized := strings.Repeat("a", maxWatchStateTokenBytes+1)
	_, err := WatchBuild(t.Context(), deps, WatchBuildRequest{
		Job:       "app",
		Build:     42,
		LastState: oversized,
	})
	r.Error(err, "WatchBuild() accepted oversized watch state")
	assertAppErrorCode(t, err, apperrors.CodeInvalidRequest)
}

func newWatchTestDeps(t *testing.T, handler http.HandlerFunc, watchCfg config.WatchConfig) Deps {
	t.Helper()
	r := require.New(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := jenkinsclient.New(config.ControllerConfig{ID: "default", URL: server.URL}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err, "client.New() error")

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
	r := require.New(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := jenkinsclient.New(config.ControllerConfig{ID: "default", URL: server.URL}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.NoError(err, "client.New() error")

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
	r := require.New(t)
	var appErr *apperrors.Error
	r.ErrorAs(err, &appErr, "error type")
	r.Equal(want, appErr.Code, "error code")
}
