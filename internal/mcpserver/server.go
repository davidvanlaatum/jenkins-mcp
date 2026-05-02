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
	"github.com/david/jenkins-mcp/internal/updatecheck"
)

type Dependencies struct {
	Config       config.Config
	Jenkins      map[string]*jenkinsapi.API
	Audit        *audit.Logger
	Logger       *slog.Logger
	Version      string
	UpdateStatus func() updatecheck.Status
}
type Server struct {
	raw  *mcp.Server
	deps jenkinstools.Deps
}

func New(deps Dependencies) *Server {
	s := &Server{raw: mcp.NewServer(&mcp.Implementation{Name: "jenkins-mcp-server", Version: deps.Version}, &mcp.ServerOptions{Logger: deps.Logger}), deps: jenkinstools.Deps{Config: deps.Config, Jenkins: deps.Jenkins, Audit: deps.Audit, UpdateStatus: deps.UpdateStatus}}
	s.register()
	return s
}
func (s *Server) Raw() *mcp.Server { return s.raw }

type toolErrorResponse struct {
	Error apperrors.Error `json:"error"`
}

func tool(name, title, description string, annotations *mcp.ToolAnnotations) *mcp.Tool {
	return &mcp.Tool{
		Name:        name,
		Title:       title,
		Description: description,
		Annotations: annotations,
	}
}

func readOnlyTool(name, title, description string) *mcp.Tool {
	return tool(name, title, description, &mcp.ToolAnnotations{ReadOnlyHint: true})
}

func additiveMutationTool(name, title, description string) *mcp.Tool {
	destructive := false
	return tool(name, title, description, &mcp.ToolAnnotations{DestructiveHint: &destructive})
}

func destructiveMutationTool(name, title, description string) *mcp.Tool {
	destructive := true
	return tool(name, title, description, &mcp.ToolAnnotations{DestructiveHint: &destructive})
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
	addTool(s.raw, readOnlyTool("jenkins_get_capabilities", "Get Capabilities", "Discover configured Jenkins controllers, response limits, update-check status, and whether mutating tools are enabled. If updates.updateAvailable is true, agents should notify the user using updates.notificationHint."), func(ctx context.Context, in jenkinstools.BaseRequest) (jenkinstools.CapabilitiesResponse, error) {
		return jenkinstools.Capabilities(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_resolve_build_url", "Resolve Build URL", "Resolve a Jenkins build URL to controller, job path, and build number."), func(ctx context.Context, in jenkinstools.ResolveBuildURLRequest) (jenkinstools.ResolveBuildURLResponse, error) {
		return jenkinstools.ResolveBuildURL(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_list_jobs", "List Jobs", "List Jenkins jobs at the controller root or within a folder."), func(ctx context.Context, in jenkinstools.ListJobsRequest) (jenkinstools.ListJobsResponse, error) {
		return jenkinstools.ListJobs(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_get_job", "Get Job", "Get Jenkins job metadata, recent build references, and parameter definitions."), func(ctx context.Context, in jenkinstools.JobRequest) (jenkinstools.GetJobResponse, error) {
		return jenkinstools.GetJob(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_list_builds", "List Builds", "List recent builds for a Jenkins job."), func(ctx context.Context, in jenkinstools.ListBuildsRequest) (jenkinstools.ListBuildsResponse, error) {
		return jenkinstools.ListBuilds(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_get_build", "Get Build", "Get build details including result, causes, parameters, artifacts, and changes."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.GetBuildResponse, error) {
		return jenkinstools.GetBuild(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_get_log", "Get Log", "Read a bounded progressive console log chunk. For Pipeline builds, prefer jenkins_get_pipeline_node_log to fetch logs for specific stages."), func(ctx context.Context, in jenkinstools.GetLogRequest) (jenkinstools.GetLogResponse, error) {
		return jenkinstools.GetLog(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_search_log", "Search Log", "Search a bounded console log chunk for text and return matching lines with optional context."), func(ctx context.Context, in jenkinstools.SearchLogRequest) (jenkinstools.SearchLogResponse, error) {
		return jenkinstools.SearchLog(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_tail_log", "Tail Log", "Read the tail of a Jenkins console log using progressive log offsets."), func(ctx context.Context, in jenkinstools.TailLogRequest) (jenkinstools.TailLogResponse, error) {
		return jenkinstools.TailLog(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_get_test_report", "Get Test Report", "Fetch JUnit test summary and bounded test case details when available."), func(ctx context.Context, in jenkinstools.TestReportRequest) (jenkinstools.TestReportResponse, error) {
		return jenkinstools.TestReport(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_get_pipeline_run", "Get Pipeline Run", "Fetch Pipeline stage evidence using the Jenkins Pipeline REST wfapi endpoint when available."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.PipelineRunResponse, error) {
		return jenkinstools.PipelineRun(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_get_pipeline_stage", "Get Pipeline Stage", "Fetch Pipeline stage details and child flow nodes for a stage id."), func(ctx context.Context, in jenkinstools.PipelineStageRequest) (jenkinstools.PipelineStageResponse, error) {
		return jenkinstools.PipelineStage(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_get_pipeline_node_log", "Get Pipeline Node Log", "Fetch bounded log output for a Pipeline flow node id. Prefer this over jenkins_get_log for Pipeline builds to reduce noise and context size."), func(ctx context.Context, in jenkinstools.PipelineNodeLogRequest) (jenkinstools.PipelineNodeLogResponse, error) {
		return jenkinstools.PipelineNodeLog(ctx, s.deps, in)
	})
	addTool(s.raw, additiveMutationTool("jenkins_download_artifact", "Download Artifact", "Download a Jenkins artifact to the configured safe local artifact directory."), func(ctx context.Context, in jenkinstools.DownloadArtifactRequest) (jenkinstools.DownloadArtifactResponse, error) {
		return jenkinstools.DownloadArtifact(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_list_artifacts", "List Artifacts", "List artifacts for a Jenkins build without fetching artifact content."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.ListArtifactsResponse, error) {
		return jenkinstools.ListArtifacts(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_read_artifact", "Read Artifact", "Read a small text Jenkins artifact inline, bounded by configured inline response limits."), func(ctx context.Context, in jenkinstools.ReadArtifactRequest) (jenkinstools.ReadArtifactResponse, error) {
		return jenkinstools.ReadArtifact(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_get_coverage", "Get Coverage", "Fetch coverage summary from common Jenkins coverage plugin endpoints when available."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.CoverageResponse, error) {
		return jenkinstools.Coverage(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_get_issues", "Get Issues", "Fetch Warnings NG or analysis issue summary from common Jenkins plugin endpoints when available."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.IssuesResponse, error) {
		return jenkinstools.Issues(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_get_changes", "Get Changes", "Fetch SCM change sets for a Jenkins build."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.ChangesResponse, error) {
		return jenkinstools.Changes(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_watch_build", "Watch Build", "Bootstrap or long-poll a Jenkins build watcher. The first call without lastState returns immediately with watch.state; pass that state back as lastState on later calls to wait until completion or Pipeline stage-status changes. Invalid or expired lastState values return an error and must be re-bootstrapped. Prefer this over jenkins_get_build when waiting for progress."), func(ctx context.Context, in jenkinstools.WatchBuildRequest) (jenkinstools.WatchBuildResponse, error) {
		return jenkinstools.WatchBuild(ctx, s.deps, in)
	})
	addTool(s.raw, additiveMutationTool("jenkins_trigger_build", "Trigger Build", "Trigger a Jenkins build. Disabled unless mutations.enabled is true."), func(ctx context.Context, in jenkinstools.TriggerBuildRequest) (jenkinstools.TriggerBuildResponse, error) {
		return jenkinstools.TriggerBuild(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_get_queue_item", "Get Queue Item", "Inspect a Jenkins queue item by id."), func(ctx context.Context, in jenkinstools.QueueItemRequest) (jenkinstools.QueueItemResponse, error) {
		return jenkinstools.QueueItem(ctx, s.deps, in)
	})
	addTool(s.raw, readOnlyTool("jenkins_list_queue", "List Queue", "List current Jenkins queue items."), func(ctx context.Context, in jenkinstools.BaseRequest) (jenkinstools.ListQueueResponse, error) {
		return jenkinstools.ListQueue(ctx, s.deps, in)
	})
	addTool(s.raw, destructiveMutationTool("jenkins_cancel_queue_item", "Cancel Queue Item", "Cancel a queued Jenkins item. Disabled unless mutations.enabled is true."), func(ctx context.Context, in jenkinstools.QueueItemRequest) (jenkinstools.CancelQueueItemResponse, error) {
		return jenkinstools.CancelQueueItem(ctx, s.deps, in)
	})
	addTool(s.raw, destructiveMutationTool("jenkins_cancel_build", "Cancel Build", "Cancel a running Jenkins build. Disabled unless mutations.enabled is true."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.CancelBuildResponse, error) {
		return jenkinstools.CancelBuild(ctx, s.deps, in)
	})
}
