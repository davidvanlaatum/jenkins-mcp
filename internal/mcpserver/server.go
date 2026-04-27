package mcpserver

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
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

type toolErrorResponse struct {
	Error apperrors.Error `json:"error"`
}

func addTool[In, Out any](server *mcp.Server, tool *mcp.Tool, handler func(context.Context, In) (Out, error)) {
	mcp.AddTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		out, err := handler(ctx, in)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return nil, out, nil
	})
}

func errorResult(err error) *mcp.CallToolResult {
	appErr := normalizeError(err)
	content, marshalErr := json.Marshal(toolErrorResponse{Error: appErr})
	if marshalErr != nil {
		content = []byte(`{"error":{"code":"jenkins_error","message":"failed to render error"}}`)
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(content)}},
		StructuredContent: json.RawMessage(content),
		IsError:           true,
	}
}

func normalizeError(err error) apperrors.Error {
	var appErr *apperrors.Error
	if stderrors.As(err, &appErr) {
		return *appErr
	}
	return apperrors.Error{Code: apperrors.CodeJenkins, Message: err.Error()}
}

func (s *Server) register() {
	addTool(s.raw, &mcp.Tool{Name: "jenkins_get_capabilities", Description: "Discover configured Jenkins controllers, response limits, and whether mutating tools are enabled."}, func(ctx context.Context, in jenkinstools.BaseRequest) (jenkinstools.CapabilitiesResponse, error) {
		return jenkinstools.Capabilities(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_resolve_build_url", Description: "Resolve a Jenkins build URL to controller, job path, and build number."}, func(ctx context.Context, in jenkinstools.ResolveBuildURLRequest) (jenkinstools.ResolveBuildURLResponse, error) {
		return jenkinstools.ResolveBuildURL(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_list_jobs", Description: "List Jenkins jobs at the controller root or within a folder."}, func(ctx context.Context, in jenkinstools.ListJobsRequest) (jenkinstools.ListJobsResponse, error) {
		return jenkinstools.ListJobs(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_get_job", Description: "Get Jenkins job metadata, recent build references, and parameter definitions."}, func(ctx context.Context, in jenkinstools.JobRequest) (jenkinstools.GetJobResponse, error) {
		return jenkinstools.GetJob(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_list_builds", Description: "List recent builds for a Jenkins job."}, func(ctx context.Context, in jenkinstools.ListBuildsRequest) (jenkinstools.ListBuildsResponse, error) {
		return jenkinstools.ListBuilds(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_get_build", Description: "Get build details including result, causes, parameters, artifacts, and changes."}, func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.GetBuildResponse, error) {
		return jenkinstools.GetBuild(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_get_log", Description: "Read a bounded progressive console log chunk using Jenkins log cursor offsets."}, func(ctx context.Context, in jenkinstools.GetLogRequest) (jenkinstools.GetLogResponse, error) {
		return jenkinstools.GetLog(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_search_log", Description: "Search a bounded console log chunk for text and return matching lines with optional context."}, func(ctx context.Context, in jenkinstools.SearchLogRequest) (jenkinstools.SearchLogResponse, error) {
		return jenkinstools.SearchLog(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_tail_log", Description: "Read the tail of a Jenkins console log using progressive log offsets."}, func(ctx context.Context, in jenkinstools.TailLogRequest) (jenkinstools.TailLogResponse, error) {
		return jenkinstools.TailLog(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_get_test_report", Description: "Fetch JUnit test summary and bounded test case details when available."}, func(ctx context.Context, in jenkinstools.TestReportRequest) (jenkinstools.TestReportResponse, error) {
		return jenkinstools.TestReport(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_get_pipeline_run", Description: "Fetch Pipeline stage evidence using the Jenkins Pipeline REST wfapi endpoint when available."}, func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.PipelineRunResponse, error) {
		return jenkinstools.PipelineRun(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_get_pipeline_stage", Description: "Fetch Pipeline stage details and child flow nodes for a stage id."}, func(ctx context.Context, in jenkinstools.PipelineStageRequest) (jenkinstools.PipelineStageResponse, error) {
		return jenkinstools.PipelineStage(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_get_pipeline_node_log", Description: "Fetch bounded log output for a Pipeline flow node id."}, func(ctx context.Context, in jenkinstools.PipelineNodeLogRequest) (jenkinstools.PipelineNodeLogResponse, error) {
		return jenkinstools.PipelineNodeLog(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_download_artifact", Description: "Download a Jenkins artifact to the configured safe local artifact directory."}, func(ctx context.Context, in jenkinstools.DownloadArtifactRequest) (jenkinstools.DownloadArtifactResponse, error) {
		return jenkinstools.DownloadArtifact(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_list_artifacts", Description: "List artifacts for a Jenkins build without fetching artifact content."}, func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.ListArtifactsResponse, error) {
		return jenkinstools.ListArtifacts(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_read_artifact", Description: "Read a small text Jenkins artifact inline, bounded by configured inline response limits."}, func(ctx context.Context, in jenkinstools.ReadArtifactRequest) (jenkinstools.ReadArtifactResponse, error) {
		return jenkinstools.ReadArtifact(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_get_coverage", Description: "Fetch coverage summary from common Jenkins coverage plugin endpoints when available."}, func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.CoverageResponse, error) {
		return jenkinstools.Coverage(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_get_issues", Description: "Fetch Warnings NG or analysis issue summary from common Jenkins plugin endpoints when available."}, func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.IssuesResponse, error) {
		return jenkinstools.Issues(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_get_changes", Description: "Fetch SCM change sets for a Jenkins build."}, func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.ChangesResponse, error) {
		return jenkinstools.Changes(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_watch_build", Description: "Inspect build state plus a progressive log chunk and Pipeline stages for polling running builds."}, func(ctx context.Context, in jenkinstools.WatchBuildRequest) (jenkinstools.WatchBuildResponse, error) {
		return jenkinstools.WatchBuild(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_trigger_build", Description: "Trigger a Jenkins build. Disabled unless mutations.enabled is true."}, func(ctx context.Context, in jenkinstools.TriggerBuildRequest) (jenkinstools.TriggerBuildResponse, error) {
		return jenkinstools.TriggerBuild(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_get_queue_item", Description: "Inspect a Jenkins queue item by id."}, func(ctx context.Context, in jenkinstools.QueueItemRequest) (jenkinstools.QueueItemResponse, error) {
		return jenkinstools.QueueItem(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_list_queue", Description: "List current Jenkins queue items."}, func(ctx context.Context, in jenkinstools.BaseRequest) (jenkinstools.ListQueueResponse, error) {
		return jenkinstools.ListQueue(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_cancel_queue_item", Description: "Cancel a queued Jenkins item. Disabled unless mutations.enabled is true."}, func(ctx context.Context, in jenkinstools.QueueItemRequest) (jenkinstools.CancelQueueItemResponse, error) {
		return jenkinstools.CancelQueueItem(ctx, s.deps, in)
	})
	addTool(s.raw, &mcp.Tool{Name: "jenkins_cancel_build", Description: "Cancel a running Jenkins build. Disabled unless mutations.enabled is true."}, func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.CancelBuildResponse, error) {
		return jenkinstools.CancelBuild(ctx, s.deps, in)
	})
}
