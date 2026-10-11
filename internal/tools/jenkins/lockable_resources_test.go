package jenkins

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/stretchr/testify/require"
)

func lockDeps(t *testing.T) Deps {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		require.New(t).Equal("/lockable-resources/api/json", req.URL.Path)
		_, _ = fmt.Fprint(w, `{"resources":[{"name":"a-phone","labels":"lab","free":true,"locked":false,"reserved":false},{"name":"b-phone","labels":"lab mobile","free":false,"locked":true,"reserved":false},{"name":"c-device","labels":"lab","free":false,"locked":false,"reserved":true,"ephemeral":true},{"name":"d-queued","labels":"lab","free":false,"locked":false,"reserved":false},{"name":"e-unknown","labels":"lab"}]}`)
	}))
	t.Cleanup(server.Close)
	cfg := config.Defaults()
	cfg.DefaultController = "test"
	cfg.Controllers = []config.ControllerConfig{{ID: "test", URL: server.URL}}
	client, err := jenkinsclient.New(loggerCtx, cfg.Controllers[0])
	require.New(t).NoError(err)
	return Deps{Config: cfg, Jenkins: map[string]*jenkinsapi.API{"test": jenkinsapi.New("test", client)}}
}

func TestLockableFiltersAndLabels(t *testing.T) {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	deps := lockDeps(t)
	for _, tc := range []struct {
		name    string
		request ListLockableResourcesRequest
		count   int
	}{
		{"all", ListLockableResourcesRequest{}, 5},
		{"exact", ListLockableResourcesRequest{Name: "b-phone"}, 1},
		{"exact case", ListLockableResourcesRequest{Name: "B-phone"}, 0},
		{"substring", ListLockableResourcesRequest{NameContains: "PHONE"}, 2},
		{"label", ListLockableResourcesRequest{Label: "mobile"}, 1},
		{"label case", ListLockableResourcesRequest{Label: "LAB"}, 0},
		{"free", ListLockableResourcesRequest{State: "free"}, 1},
		{"locked", ListLockableResourcesRequest{State: "locked"}, 1},
		{"reserved", ListLockableResourcesRequest{State: "reserved"}, 1},
		{"unavailable", ListLockableResourcesRequest{State: "unavailable"}, 3},
		{"ephemeral", ListLockableResourcesRequest{State: "ephemeral"}, 1},
		{"combined", ListLockableResourcesRequest{State: "free", Label: "mobile"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loggerCtx, _, _ := testhandler.SetupTestHandler(t)

			r := require.New(t)
			got, err := ListLockableResources(loggerCtx, deps, tc.request)
			r.NoError(err)
			r.Len(got.Resources, tc.count)
		})
	}
	r := require.New(t)
	got, err := ListLockableResources(loggerCtx, deps, ListLockableResourcesRequest{Limit: 1})
	r.NoError(err)
	r.Len(got.Resources, 1)
	r.Len(got.Labels, 2)
	r.Equal(5, got.Labels[0].ResourceCount)
	r.Equal(1, got.Labels[0].FreeCount)
	r.Equal(1, got.Labels[0].LockedCount)
	r.Equal(1, got.Labels[0].ReservedCount)
	r.Equal(3, got.Labels[0].UnavailableCount)
	disabled := false
	got, err = ListLockableResources(loggerCtx, deps, ListLockableResourcesRequest{IncludeLabelSummary: &disabled})
	r.NoError(err)
	r.Empty(got.Labels)
}

func TestLockablePaginationAndCursorBinding(t *testing.T) {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	r := require.New(t)
	deps := lockDeps(t)
	first, err := ListLockableResources(loggerCtx, deps, ListLockableResourcesRequest{Limit: 1})
	r.NoError(err)
	r.True(first.HasMore)
	r.True(first.Truncated)
	r.NotEmpty(first.NextCursor)
	second, err := ListLockableResources(loggerCtx, deps, ListLockableResourcesRequest{Limit: 500, Cursor: first.NextCursor})
	r.NoError(err)
	r.Len(second.Resources, 4)
	r.Equal("b-phone", second.Resources[0].Name)
	r.False(second.HasMore)
	for _, request := range []ListLockableResourcesRequest{{Name: "a-phone"}, {NameContains: "phone"}, {Label: "lab"}, {State: "free"}, {Cursor: "bad"}} {
		if request.Cursor == "" {
			request.Cursor = first.NextCursor
		}
		_, err := ListLockableResources(loggerCtx, deps, request)
		var domain *apperrors.Error
		r.ErrorAs(err, &domain)
		r.Equal(apperrors.CodeInvalidRequest, domain.Code)
	}
	_, err = ListLockableResources(loggerCtx, deps, ListLockableResourcesRequest{State: "invalid"})
	r.Error(err)
}
