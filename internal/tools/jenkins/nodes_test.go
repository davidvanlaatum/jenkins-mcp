package jenkins

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/stretchr/testify/require"
)

func TestListNodesBuildReferences(t *testing.T) {
	r := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.Equal("/computer/api/json", req.URL.Path)
		_, _ = fmt.Fprintf(w, `{"computer":[{"displayName":"worker","executors":[{"currentExecutable":{"url":"http://%s/job/folder/job/app/7/","number":7}},{"idle":true},{"currentExecutable":{"url":"https://unknown/job/app/8/","build":{"controller":"spoofed"}}}],"oneOffExecutors":[{"currentExecutable":{"url":"https://other.example/jenkins/job/pipeline/9/"}}]}]}`, req.Host)
	}))
	defer server.Close()
	cfg := config.Defaults()
	cfg.DefaultController = "test"
	cfg.Controllers = []config.ControllerConfig{{ID: "test", URL: server.URL}, {ID: "other", URL: "https://other.example/jenkins"}}
	client, err := jenkinsclient.New(cfg.Controllers[0], nil)
	r.NoError(err)
	deps := Deps{Config: cfg, Jenkins: map[string]*jenkinsapi.API{"test": jenkinsapi.New("test", client)}}
	got, err := ListNodes(t.Context(), deps, BaseRequest{})
	r.NoError(err)
	r.Len(got.Nodes, 1)
	ref := got.Nodes[0].Executors[0].CurrentExecutable.Build
	r.NotNil(ref)
	r.Equal("test", ref.Controller)
	r.Equal("folder/app", ref.Job)
	r.Equal(7, ref.Build)
	r.Equal(server.URL+"/job/folder/job/app/7/", ref.URL)
	r.Nil(got.Nodes[0].Executors[1].CurrentExecutable)
	r.Nil(got.Nodes[0].Executors[2].CurrentExecutable.Build)
	other := got.Nodes[0].OneOffExecutors[0].CurrentExecutable.Build
	r.NotNil(other)
	r.Equal("other", other.Controller)
	r.Equal("pipeline", other.Job)
	r.Equal(9, other.Build)
	_, err = ListNodes(t.Context(), deps, BaseRequest{Controller: "missing"})
	r.Error(err)
	deps.Config.Limits.MaxResponseBytes = 10
	_, err = ListNodes(t.Context(), deps, BaseRequest{})
	var domain *apperrors.Error
	r.ErrorAs(err, &domain)
	r.Equal(apperrors.CodeJenkins, domain.Code)
	r.Contains(domain.Message, "response limit")
}
