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
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_resolve_build_url", Description: "Resolve a Jenkins build URL to controller, job path, and build number."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.ResolveBuildURLRequest) (*mcp.CallToolResult, jenkinstools.ResolveBuildURLResponse, error) {
		out, err := jenkinstools.ResolveBuildURL(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_list_jobs", Description: "List Jenkins jobs at the controller root or within a folder."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.ListJobsRequest) (*mcp.CallToolResult, jenkinstools.ListJobsResponse, error) {
		out, err := jenkinstools.ListJobs(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_get_job", Description: "Get Jenkins job metadata, recent build references, and parameter definitions."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.JobRequest) (*mcp.CallToolResult, jenkinstools.GetJobResponse, error) {
		out, err := jenkinstools.GetJob(ctx, s.deps, in)
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
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_search_log", Description: "Search a bounded console log chunk for text and return matching lines with optional context."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.SearchLogRequest) (*mcp.CallToolResult, jenkinstools.SearchLogResponse, error) {
		out, err := jenkinstools.SearchLog(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_tail_log", Description: "Read the tail of a Jenkins console log using progressive log offsets."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.TailLogRequest) (*mcp.CallToolResult, jenkinstools.TailLogResponse, error) {
		out, err := jenkinstools.TailLog(ctx, s.deps, in)
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
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_get_pipeline_stage", Description: "Fetch Pipeline stage details and child flow nodes for a stage id."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.PipelineStageRequest) (*mcp.CallToolResult, jenkinstools.PipelineStageResponse, error) {
		out, err := jenkinstools.PipelineStage(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_get_pipeline_node_log", Description: "Fetch bounded log output for a Pipeline flow node id."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.PipelineNodeLogRequest) (*mcp.CallToolResult, jenkinstools.PipelineNodeLogResponse, error) {
		out, err := jenkinstools.PipelineNodeLog(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_download_artifact", Description: "Download a Jenkins artifact to the configured safe local artifact directory."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.DownloadArtifactRequest) (*mcp.CallToolResult, jenkinstools.DownloadArtifactResponse, error) {
		out, err := jenkinstools.DownloadArtifact(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_list_artifacts", Description: "List artifacts for a Jenkins build without fetching artifact content."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.BuildRequest) (*mcp.CallToolResult, jenkinstools.ListArtifactsResponse, error) {
		out, err := jenkinstools.ListArtifacts(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_read_artifact", Description: "Read a small text Jenkins artifact inline, bounded by configured inline response limits."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.ReadArtifactRequest) (*mcp.CallToolResult, jenkinstools.ReadArtifactResponse, error) {
		out, err := jenkinstools.ReadArtifact(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_get_coverage", Description: "Fetch coverage summary from common Jenkins coverage plugin endpoints when available."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.BuildRequest) (*mcp.CallToolResult, jenkinstools.CoverageResponse, error) {
		out, err := jenkinstools.Coverage(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_get_issues", Description: "Fetch Warnings NG or analysis issue summary from common Jenkins plugin endpoints when available."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.BuildRequest) (*mcp.CallToolResult, jenkinstools.IssuesResponse, error) {
		out, err := jenkinstools.Issues(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_get_changes", Description: "Fetch SCM change sets for a Jenkins build."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.BuildRequest) (*mcp.CallToolResult, jenkinstools.ChangesResponse, error) {
		out, err := jenkinstools.Changes(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_watch_build", Description: "Inspect build state plus a progressive log chunk and Pipeline stages for polling running builds."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.WatchBuildRequest) (*mcp.CallToolResult, jenkinstools.WatchBuildResponse, error) {
		out, err := jenkinstools.WatchBuild(ctx, s.deps, in)
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
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_list_queue", Description: "List current Jenkins queue items."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.BaseRequest) (*mcp.CallToolResult, jenkinstools.ListQueueResponse, error) {
		out, err := jenkinstools.ListQueue(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_cancel_queue_item", Description: "Cancel a queued Jenkins item. Disabled unless mutations.enabled is true."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.QueueItemRequest) (*mcp.CallToolResult, jenkinstools.CancelQueueItemResponse, error) {
		out, err := jenkinstools.CancelQueueItem(ctx, s.deps, in)
		return nil, out, err
	})
	mcp.AddTool(s.raw, &mcp.Tool{Name: "jenkins_cancel_build", Description: "Cancel a running Jenkins build. Disabled unless mutations.enabled is true."}, func(ctx context.Context, req *mcp.CallToolRequest, in jenkinstools.BuildRequest) (*mcp.CallToolResult, jenkinstools.CancelBuildResponse, error) {
		out, err := jenkinstools.CancelBuild(ctx, s.deps, in)
		return nil, out, err
	})
}
