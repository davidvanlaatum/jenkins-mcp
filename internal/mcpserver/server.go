package mcpserver

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	jenkinstools "github.com/david/jenkins-mcp/internal/tools/jenkins"
)

type Dependencies struct {
	Config  config.Config
	Jenkins map[string]*jenkinsapi.API
	Audit   *audit.Logger
	Logger  *slog.Logger
	Version string
}
type Server struct {
	raw  *mcp.Server
	deps jenkinstools.Deps
}

func New(deps Dependencies) *Server {
	s := &Server{raw: mcp.NewServer(&mcp.Implementation{Name: "jenkins-mcp-server", Version: deps.Version}, &mcp.ServerOptions{Logger: deps.Logger}), deps: jenkinstools.Deps{Config: deps.Config, Jenkins: deps.Jenkins, Audit: deps.Audit}}
	s.register()
	return s
}
func (s *Server) Raw() *mcp.Server { return s.raw }

func (s *Server) register() {
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_get_capabilities", Description: "Discover configured Jenkins controllers, response limits, and whether mutating tools are enabled."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.BaseRequest) (*mcp.CallToolResult, jenkinstools.CapabilitiesResponse, error) {
		out, err := jenkinstools.Capabilities(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_list_jobs", Description: "List Jenkins jobs at the controller root or within a folder."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.ListJobsRequest) (*mcp.CallToolResult, jenkinstools.ListJobsResponse, error) {
		out, err := jenkinstools.ListJobs(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_list_builds", Description: "List recent builds for a Jenkins job."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.ListBuildsRequest) (*mcp.CallToolResult, jenkinstools.ListBuildsResponse, error) {
		out, err := jenkinstools.ListBuilds(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_get_build", Description: "Get build details including result, causes, parameters, artifacts, and changes."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.BuildRequest) (*mcp.CallToolResult, jenkinstools.GetBuildResponse, error) {
		out, err := jenkinstools.GetBuild(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_get_log", Description: "Read a bounded progressive console log chunk using Jenkins log cursor offsets."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.GetLogRequest) (*mcp.CallToolResult, jenkinstools.GetLogResponse, error) {
		out, err := jenkinstools.GetLog(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_get_test_report", Description: "Fetch JUnit test summary and bounded test case details when available."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.TestReportRequest) (*mcp.CallToolResult, jenkinstools.TestReportResponse, error) {
		out, err := jenkinstools.TestReport(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_get_pipeline_run", Description: "Fetch Pipeline stage evidence using the Jenkins Pipeline REST wfapi endpoint when available."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.BuildRequest) (*mcp.CallToolResult, jenkinstools.PipelineRunResponse, error) {
		out, err := jenkinstools.PipelineRun(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_download_artifact", Description: "Download a Jenkins artifact to the configured safe local artifact directory."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.DownloadArtifactRequest) (*mcp.CallToolResult, jenkinstools.DownloadArtifactResponse, error) {
		out, err := jenkinstools.DownloadArtifact(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_trigger_build", Description: "Trigger a Jenkins build. Disabled unless mutations.enabled is true."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.TriggerBuildRequest) (*mcp.CallToolResult, jenkinstools.TriggerBuildResponse, error) {
		out, err := jenkinstools.TriggerBuild(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_get_queue_item", Description: "Inspect a Jenkins queue item by id."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.QueueItemRequest) (*mcp.CallToolResult, jenkinstools.QueueItemResponse, error) {
		out, err := jenkinstools.QueueItem(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_cancel_build", Description: "Cancel a running Jenkins build. Disabled unless mutations.enabled is true."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.BuildRequest) (*mcp.CallToolResult, jenkinstools.CancelBuildResponse, error) {
		out, err := jenkinstools.CancelBuild(ctx, s.deps, in)
		return nil, out, err
	})
}
