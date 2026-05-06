package mcpserver

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"log/slog"
	"reflect"
	"strconv"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
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
	raw    *mcp.Server
	deps   jenkinstools.Deps
	logger *slog.Logger
}

func New(deps Dependencies) *Server {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{raw: mcp.NewServer(&mcp.Implementation{Name: "jenkins-mcp-server", Version: deps.Version}, &mcp.ServerOptions{Logger: logger}), deps: jenkinstools.Deps{Config: deps.Config, Jenkins: deps.Jenkins, Audit: deps.Audit, UpdateStatus: deps.UpdateStatus}, logger: logger}
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

func addConfiguredTool[In, Out any](s *Server, tool *mcp.Tool, handler func(context.Context, In) (Out, error)) {
	addTool(s.raw, tool, s.deps.Config.Logging, s.logger, handler)
}

func addTool[In, Out any](server *mcp.Server, tool *mcp.Tool, logging config.LoggingConfig, logger *slog.Logger, handler func(context.Context, In) (Out, error)) {
	toolWithOutputSchema := *tool
	if toolWithOutputSchema.OutputSchema == nil {
		// The SDK only infers output schemas when the handler returns typed Out.
		// This wrapper returns any so tool errors can keep their IsError result
		// instead of being overwritten by zero Out.
		toolWithOutputSchema.OutputSchema = inferOutputSchema[Out](tool.Name)
	}
	mcp.AddTool(server, &toolWithOutputSchema, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		started := time.Now()
		logPayloads := logging.ToolPayloads
		logCalls := logging.ToolCalls || logPayloads || logger.Enabled(ctx, slog.LevelDebug)
		if logCalls {
			attrs := []any{"tool", tool.Name}
			if logPayloads {
				attrs = append(attrs, "arguments", requestPayload(req, in))
			}
			logToolCall(ctx, logger, logging, "tool_call_started", attrs...)
		}
		out, err := handler(ctx, in)
		if err != nil {
			content, appErr := errorPayload(err)
			if logCalls {
				attrs := []any{
					"tool", tool.Name,
					"duration_ms", time.Since(started).Milliseconds(),
					"is_error", true,
					"error_code", appErr.Code,
					"error_message", appErr.Message,
					"response_bytes", len(content),
				}
				if logPayloads {
					attrs = append(attrs, "error_payload", string(content))
				}
				logToolCall(ctx, logger, logging, "tool_call_finished", attrs...)
			}
			return errorResult(err), nil, nil
		}
		if logCalls {
			responsePayload := payloadBytes(out)
			attrs := []any{
				"tool", tool.Name,
				"duration_ms", time.Since(started).Milliseconds(),
				"is_error", false,
				"response_bytes", len(responsePayload),
			}
			if logPayloads {
				attrs = append(attrs, "response_payload", string(responsePayload))
			}
			logToolCall(ctx, logger, logging, "tool_call_finished", attrs...)
		}
		return nil, out, nil
	})
}

func logToolCall(ctx context.Context, logger *slog.Logger, logging config.LoggingConfig, message string, attrs ...any) {
	if logging.ToolCalls || logging.ToolPayloads {
		logger.InfoContext(ctx, message, attrs...)
		return
	}
	logger.DebugContext(ctx, message, attrs...)
}

func inferOutputSchema[Out any](toolName string) any {
	outputType := reflect.TypeFor[Out]()
	if outputType == reflect.TypeFor[any]() {
		return nil
	}
	if outputType.Kind() == reflect.Pointer {
		outputType = outputType.Elem()
	}
	outputSchema, err := jsonschema.ForType(outputType, nil)
	if err != nil {
		panic("infer output schema for " + toolName + ": " + err.Error())
	}
	return outputSchema
}

func errorResult(err error) *mcp.CallToolResult {
	content, _ := errorPayload(err)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(content)}},
		IsError: true,
	}
}

func errorPayload(err error) ([]byte, apperrors.Error) {
	appErr := normalizeError(err)
	content, marshalErr := json.Marshal(toolErrorResponse{Error: appErr})
	if marshalErr != nil {
		content = []byte(`{"error":{"code":"jenkins_error","message":"failed to render error"}}`)
		appErr = apperrors.Error{Code: apperrors.CodeJenkins, Message: "failed to render error"}
	}
	return content, appErr
}

func payloadString(v any) string {
	return string(payloadBytes(v))
}

func requestPayload(req *mcp.CallToolRequest, fallback any) string {
	if req != nil && req.Params != nil && req.Params.Arguments != nil {
		return string(req.Params.Arguments)
	}
	return payloadString(fallback)
}

func payloadBytes(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"marshalError":` + strconv.Quote(err.Error()) + `}`)
	}
	return b
}

func normalizeError(err error) apperrors.Error {
	var appErr *apperrors.Error
	if stderrors.As(err, &appErr) {
		return *appErr
	}
	return apperrors.Error{Code: apperrors.CodeJenkins, Message: err.Error()}
}

func (s *Server) register() {
	addConfiguredTool(s, readOnlyTool("jenkins_get_capabilities", "Get Capabilities", "Discover configured Jenkins controllers, response limits, update-check status, optional capability warnings, capability discovery configuration, and whether mutating tools are enabled. If updates.updateAvailable is true, agents should notify the user using updates.notificationHint."), func(ctx context.Context, in jenkinstools.BaseRequest) (jenkinstools.CapabilitiesResponse, error) {
		return jenkinstools.Capabilities(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_resolve_build_url", "Resolve Build URL", "Resolve a Jenkins build URL to controller, job path, and build number."), func(ctx context.Context, in jenkinstools.ResolveBuildURLRequest) (jenkinstools.ResolveBuildURLResponse, error) {
		return jenkinstools.ResolveBuildURL(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_list_jobs", "List Jobs", "List Jenkins jobs at the controller root or within a folder. Supports cursor pagination, recursive traversal, and filters for case-insensitive nameContains, nameRegex, type, derived status, and building state."), func(ctx context.Context, in jenkinstools.ListJobsRequest) (jenkinstools.ListJobsResponse, error) {
		return jenkinstools.ListJobs(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_get_job", "Get Job", "Get Jenkins job metadata, recent build references, and parameter definitions."), func(ctx context.Context, in jenkinstools.JobRequest) (jenkinstools.GetJobResponse, error) {
		return jenkinstools.GetJob(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_list_builds", "List Builds", "List recent builds for a Jenkins job with cursor pagination. Build summaries include result, description, displayName, id, queueId, estimatedDuration, and keepLog."), func(ctx context.Context, in jenkinstools.ListBuildsRequest) (jenkinstools.ListBuildsResponse, error) {
		return jenkinstools.ListBuilds(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_get_build", "Get Build", "Get build details including result, causes, parameters, artifacts, and changes."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.GetBuildResponse, error) {
		return jenkinstools.GetBuild(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_get_log", "Get Log", "Read a bounded progressive console log chunk. For Pipeline builds, prefer jenkins_get_pipeline_node_log to fetch logs for specific stages."), func(ctx context.Context, in jenkinstools.GetLogRequest) (jenkinstools.GetLogResponse, error) {
		return jenkinstools.GetLog(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_search_log", "Search Log", "Search a bounded console log chunk for text and return matching lines with optional context."), func(ctx context.Context, in jenkinstools.SearchLogRequest) (jenkinstools.SearchLogResponse, error) {
		return jenkinstools.SearchLog(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_tail_log", "Tail Log", "Read the tail of a Jenkins console log using progressive log offsets."), func(ctx context.Context, in jenkinstools.TailLogRequest) (jenkinstools.TailLogResponse, error) {
		return jenkinstools.TailLog(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_get_test_report", "Get Test Report", "Fetch JUnit test summary and bounded test case details when available."), func(ctx context.Context, in jenkinstools.TestReportRequest) (jenkinstools.TestReportResponse, error) {
		return jenkinstools.TestReport(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_get_pipeline_run", "Get Pipeline Run", "Fetch Pipeline stage evidence and pending input-step actions using the Jenkins Pipeline REST wfapi endpoint when available."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.PipelineRunResponse, error) {
		return jenkinstools.PipelineRun(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_get_pipeline_stage", "Get Pipeline Stage", "Fetch Pipeline stage details and child flow nodes for a stage id."), func(ctx context.Context, in jenkinstools.PipelineStageRequest) (jenkinstools.PipelineStageResponse, error) {
		return jenkinstools.PipelineStage(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_get_pipeline_node_log", "Get Pipeline Node Log", "Fetch bounded log output for a Pipeline flow node id. Prefer this over jenkins_get_log for Pipeline builds to reduce noise and context size."), func(ctx context.Context, in jenkinstools.PipelineNodeLogRequest) (jenkinstools.PipelineNodeLogResponse, error) {
		return jenkinstools.PipelineNodeLog(ctx, s.deps, in)
	})
	addConfiguredTool(s, additiveMutationTool("jenkins_download_artifact", "Download Artifact", "Download a Jenkins artifact to the configured safe local artifact directory."), func(ctx context.Context, in jenkinstools.DownloadArtifactRequest) (jenkinstools.DownloadArtifactResponse, error) {
		return jenkinstools.DownloadArtifact(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_list_artifacts", "List Artifacts", "List artifacts for a Jenkins build without fetching artifact content."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.ListArtifactsResponse, error) {
		return jenkinstools.ListArtifacts(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_read_artifact", "Read Artifact", "Read a small text Jenkins artifact inline, bounded by configured inline response limits."), func(ctx context.Context, in jenkinstools.ReadArtifactRequest) (jenkinstools.ReadArtifactResponse, error) {
		return jenkinstools.ReadArtifact(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_get_coverage", "Get Coverage", "Fetch coverage summary from common Jenkins coverage plugin endpoints when available."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.CoverageResponse, error) {
		return jenkinstools.Coverage(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_get_issues", "Get Issues", "Fetch Warnings NG or analysis issue summary from common Jenkins plugin endpoints when available."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.IssuesResponse, error) {
		return jenkinstools.Issues(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_get_changes", "Get Changes", "Fetch SCM change sets for a Jenkins build."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.ChangesResponse, error) {
		return jenkinstools.Changes(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_watch_build", "Watch Build", "Bootstrap or long-poll a Jenkins build watcher. The first call without lastState returns immediately with watch.state; pass that state back as lastState on later calls to wait until completion, Pipeline stage-status changes, or pending input-step changes. Invalid or expired lastState values return an error and must be re-bootstrapped. Prefer this over jenkins_get_build when waiting for progress."), func(ctx context.Context, in jenkinstools.WatchBuildRequest) (jenkinstools.WatchBuildResponse, error) {
		return jenkinstools.WatchBuild(ctx, s.deps, in)
	})
	addConfiguredTool(s, additiveMutationTool("jenkins_trigger_build", "Trigger Build", "Trigger a Jenkins build. Disabled unless mutations.enabled is true."), func(ctx context.Context, in jenkinstools.TriggerBuildRequest) (jenkinstools.TriggerBuildResponse, error) {
		return jenkinstools.TriggerBuild(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_get_queue_item", "Get Queue Item", "Inspect a Jenkins queue item by id."), func(ctx context.Context, in jenkinstools.QueueItemRequest) (jenkinstools.QueueItemResponse, error) {
		return jenkinstools.QueueItem(ctx, s.deps, in)
	})
	addConfiguredTool(s, readOnlyTool("jenkins_list_queue", "List Queue", "List current Jenkins queue items."), func(ctx context.Context, in jenkinstools.BaseRequest) (jenkinstools.ListQueueResponse, error) {
		return jenkinstools.ListQueue(ctx, s.deps, in)
	})
	addConfiguredTool(s, destructiveMutationTool("jenkins_cancel_queue_item", "Cancel Queue Item", "Cancel a queued Jenkins item. Disabled unless mutations.enabled is true."), func(ctx context.Context, in jenkinstools.QueueItemRequest) (jenkinstools.CancelQueueItemResponse, error) {
		return jenkinstools.CancelQueueItem(ctx, s.deps, in)
	})
	addConfiguredTool(s, destructiveMutationTool("jenkins_cancel_build", "Cancel Build", "Cancel a running Jenkins build. Disabled unless mutations.enabled is true."), func(ctx context.Context, in jenkinstools.BuildRequest) (jenkinstools.CancelBuildResponse, error) {
		return jenkinstools.CancelBuild(ctx, s.deps, in)
	})
}
