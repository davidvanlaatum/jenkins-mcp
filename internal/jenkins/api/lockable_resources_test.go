package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/stretchr/testify/require"
)

func lockAPI(t *testing.T, handler http.HandlerFunc) *API {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := jenkinsclient.New(loggerCtx, config.ControllerConfig{ID: "test", URL: server.URL})
	require.New(t).NoError(err)
	return New("test", client)
}

func TestListLockableResources(t *testing.T) {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	r := require.New(t)
	api := lockAPI(t, func(w http.ResponseWriter, req *http.Request) {
		r.Equal("GET", req.Method)
		r.Equal("/lockable-resources/api/json", req.URL.Path)
		r.Equal(lockableResourceTree, req.URL.Query().Get("tree"))
		_, _ = fmt.Fprint(w, `{"resources":[{"name":"z-phone","labels":"lab mobile","labelsAsList":["mobile","lab","lab"],"free":false,"locked":true,"reserved":false,"buildName":"holder #1","lockCause":"held by holder","reservedTimestamp":1780000000000,"properties":[{"name":"os","value":"android"}]},{"name":"a-phone","labels":"lab mobile","free":true,"locked":false,"reserved":false},{"name":"unknown"}]}`)
	})
	got, warnings, err := api.ListLockableResources(loggerCtx)
	r.NoError(err)
	r.Len(got, 3)
	r.Equal("a-phone", got[0].Name)
	r.Equal([]string{"lab", "mobile"}, got[0].LabelList)
	r.Nil(got[1].Free)
	r.Equal([]string{"lab", "mobile"}, got[2].LabelList)
	r.Equal("holder #1", got[2].BuildName)
	r.Equal("held by holder", got[2].LockCause)
	r.Equal(int64(1780000000000), *got[2].ReservedTimestamp)
	r.Equal([]model.LockableResourceProperty{{Name: "os", Value: "android"}}, got[2].Properties)
	r.Len(warnings, 1)
}

func TestLockableEndpointErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   apperrors.Code
	}{{404, apperrors.CodeUnsupported}, {403, apperrors.CodePermissionDenied}, {500, apperrors.CodeJenkins}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			loggerCtx, _, _ := testhandler.SetupTestHandler(t)

			r := require.New(t)
			api := lockAPI(t, func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(tc.status) })
			_, _, err := api.ListLockableResources(loggerCtx)
			var domain *apperrors.Error
			r.ErrorAs(err, &domain)
			r.Equal(tc.code, domain.Code)
		})
	}
	for _, body := range []string{`{"resources":[]}`, `{"_class":"plugin"}`} {
		t.Run(body, func(t *testing.T) {
			loggerCtx, _, _ := testhandler.SetupTestHandler(t)

			r := require.New(t)
			api := lockAPI(t, func(w http.ResponseWriter, req *http.Request) { _, _ = fmt.Fprint(w, body) })
			got, _, err := api.ListLockableResources(loggerCtx)
			if body == `{"resources":[]}` {
				r.NoError(err)
				r.NotNil(got)
				r.Empty(got)
			} else {
				var domain *apperrors.Error
				r.ErrorAs(err, &domain)
				r.Equal(apperrors.CodeUnsupported, domain.Code)
			}
		})
	}
}

func TestParseLockResource(t *testing.T) {
	for _, tc := range []struct {
		description string
		names       []string
		ambiguous   bool
	}{
		{"lab-phone-1", []string{"lab-phone-1"}, false},
		{"Resource: lab-phone-1", []string{"lab-phone-1"}, false},
		{"Label: mobile, Quantity: 1", nil, true},
		{"{Resource: a},{Resource: b}", nil, true},
		{"Resource: a, Reason: maintenance", nil, true},
		{"", nil, true}, {"nothing", nil, true}, {"lab && mobile", nil, true},
	} {
		t.Run(tc.description, func(t *testing.T) {
			r := require.New(t)
			names, ambiguous := parseLockResource(tc.description)
			r.Equal(tc.names, names)
			r.Equal(tc.ambiguous, ambiguous)
		})
	}
}

func TestBuildLockableResources(t *testing.T) {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	r := require.New(t)
	api := lockAPI(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/job/app/1/api/json":
			r.Contains(req.URL.Query().Get("tree"), "actions[")
			_, _ = fmt.Fprint(w, `{"actions":[{"_class":"org.jenkinsci.plugins.workflow.job.views.FlowGraphAction","nodes":[{"id":"7","displayName":"Lock shared resource : Start"},{"id":"8","displayName":"Lock shared resource : End"},{"id":"9","displayName":"Lock shared resource : Start"},{"id":"7","displayName":"Lock shared resource : Start"}]}]}`)
		case "/job/app/1/execution/node/7/wfapi/describe":
			_, _ = fmt.Fprint(w, `{"id":"7","name":"Lock shared resource : Start","status":"SUCCESS","parameterDescription":"phone-1"}`)
		case "/job/app/1/execution/node/9/wfapi/describe":
			_, _ = fmt.Fprint(w, `{"id":"9","name":"Lock shared resource : Start","parameterDescription":"Label: mobile"}`)
		default:
			r.Fail("unexpected endpoint", req.URL.String())
			http.NotFound(w, req)
		}
	})
	steps, warnings, err := api.BuildLockableResources(loggerCtx, "app", 1)
	r.NoError(err)
	r.Len(steps, 2)
	r.Equal([]string{"phone-1"}, steps[0].ResourceNames)
	r.False(steps[0].Ambiguous)
	r.True(steps[1].Ambiguous)
	r.Len(warnings, 2)
}

func TestBuildLockableResourcesMissingMetadata(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		buildStatus, wfStatus int
		code                  apperrors.Code
	}{
		{"missing build", 404, 404, apperrors.CodeNotFound},
		{"non pipeline", 200, 404, apperrors.CodeUnsupported},
		{"denied", 200, 403, apperrors.CodePermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loggerCtx, _, _ := testhandler.SetupTestHandler(t)

			r := require.New(t)
			api := lockAPI(t, func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/job/app/1/api/json" {
					w.WriteHeader(tc.buildStatus)
					_, _ = fmt.Fprint(w, `{"actions":[{"_class":"org.jenkinsci.plugins.workflow.job.views.FlowGraphAction","nodes":[{"id":"7","displayName":"Lock shared resource : Start"}]}]}`)
				} else {
					w.WriteHeader(tc.wfStatus)
				}
			})
			_, _, err := api.BuildLockableResources(loggerCtx, "app", 1)
			var domain *apperrors.Error
			r.ErrorAs(err, &domain)
			r.Equal(tc.code, domain.Code)
		})
	}
}

func TestLockableCapability(t *testing.T) {
	r := require.New(t)
	r.False(defaultFeatureMap(nil)["lockableResources"])
	r.True(defaultFeatureMap([]model.PluginInfo{{ShortName: "lockable-resources", Active: true, Enabled: true}})["lockableResources"])
	r.False(defaultFeatureMap([]model.PluginInfo{{ShortName: "lockable-resources", Active: false, Enabled: true}})["lockableResources"])
}

func TestBuildLockableResourcesEmptyGraph(t *testing.T) {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	r := require.New(t)
	api := lockAPI(t, func(w http.ResponseWriter, req *http.Request) {
		r.Equal("/job/app/1/api/json", req.URL.Path)
		_, _ = fmt.Fprint(w, `{"actions":[{"_class":"org.jenkinsci.plugins.workflow.job.views.FlowGraphAction","nodes":[]}]}`)
	})
	steps, warnings, err := api.BuildLockableResources(loggerCtx, "app", 1)
	r.NoError(err)
	r.Empty(steps)
	r.Len(warnings, 2)
	r.Contains(warnings[1], "no nodes")
}

func TestBuildLockableResourcesMissingGraph(t *testing.T) {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	r := require.New(t)
	api := lockAPI(t, func(w http.ResponseWriter, req *http.Request) { _, _ = fmt.Fprint(w, `{"actions":[]}`) })
	_, _, err := api.BuildLockableResources(loggerCtx, "app", 1)
	var domain *apperrors.Error
	r.ErrorAs(err, &domain)
	r.Equal(apperrors.CodeUnsupported, domain.Code)
}

func TestBuildLockableResourcesBoundsGraphAndDetails(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		count, lockIndex, expected int
	}{
		{"graph cap", maxLockGraphNodes + 1, maxLockGraphNodes, 0},
		{"details cap", maxBuildLockSteps + 1, 0, maxBuildLockSteps},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loggerCtx, _, _ := testhandler.SetupTestHandler(t)

			r := require.New(t)
			nodes := make([]map[string]string, tc.count)
			for i := range nodes {
				name := "Print Message"
				if i >= tc.lockIndex {
					name = "Lock shared resource : Start"
				}
				nodes[i] = map[string]string{"id": fmt.Sprint(i + 1), "displayName": name}
			}
			requests := 0
			api := lockAPI(t, func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/job/app/1/api/json" {
					r.Equal(buildLockGraphTree, req.URL.Query().Get("tree"))
					_ = json.NewEncoder(w).Encode(map[string]any{"actions": []any{map[string]any{"_class": flowGraphActionClass, "nodes": nodes}}})
					return
				}
				requests++
				_, _ = fmt.Fprint(w, `{"parameterDescription":"phone-1"}`)
			})
			steps, warnings, err := api.BuildLockableResources(loggerCtx, "app", 1)
			r.NoError(err)
			r.Len(steps, tc.expected)
			r.Equal(tc.expected, requests)
			r.Len(warnings, 2)
			r.Contains(warnings[1], "capped")
		})
	}
}
