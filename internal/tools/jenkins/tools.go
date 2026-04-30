package jenkins

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/david/jenkins-mcp/internal/artifacts"
	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/pagination"
	"github.com/david/jenkins-mcp/internal/validation"
)

type Deps struct {
	Config  config.Config
	Jenkins map[string]*jenkinsapi.API
	Audit   *audit.Logger
}

const (
	maxWatchStateTokenBytes        = 8 * 1024
	maxWatchStateUncompressedBytes = 512 * 1024
)

var (
	watchStateSigningKey     []byte
	watchStateSigningKeyErr  error
	watchStateSigningKeyOnce sync.Once
)

type BaseRequest struct {
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
}
type JobRequest struct {
	Controller string `json:"controller,omitempty"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
}
type BuildRequest struct {
	Controller string `json:"controller,omitempty"`
	Job        string `json:"job"`
	Build      int    `json:"build"`
}

type ResolveBuildURLRequest struct {
	URL string `json:"url"`
}

type ResolveBuildURLResponse struct {
	Reference model.BuildReference `json:"reference"`
}

func ResolveBuildURL(_ context.Context, deps Deps, in ResolveBuildURLRequest) (ResolveBuildURLResponse, error) {
	ref, err := resolveBuildURL(deps.Config, in.URL)
	return ResolveBuildURLResponse{Reference: ref}, err
}

func resolveBuildURL(cfg config.Config, rawURL string) (model.BuildReference, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return model.BuildReference{}, apperrors.New(apperrors.CodeInvalidRequest, "invalid Jenkins build URL")
	}
	bestMatchLen := -1
	var bestRef model.BuildReference
	for _, controller := range cfg.Controllers {
		base, err := url.Parse(controller.URL)
		if err != nil || !sameURLHost(base, parsed) {
			continue
		}
		relativePath, matchLen, ok := trimControllerPathPrefix(parsed.EscapedPath(), base.EscapedPath())
		if !ok {
			continue
		}
		job, build, ok := parseBuildPath(relativePath)
		if !ok {
			continue
		}
		if matchLen > bestMatchLen {
			bestMatchLen = matchLen
			bestRef = model.BuildReference{Controller: controller.ID, Job: job, Build: build, URL: rawURL}
		}
	}
	if bestMatchLen >= 0 {
		return bestRef, nil
	}
	return model.BuildReference{}, apperrors.New(apperrors.CodeInvalidRequest, "URL does not match a configured Jenkins build")
}

func sameURLHost(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func trimControllerPathPrefix(buildPath, controllerPath string) (string, int, bool) {
	normalizedControllerPath := normalizeURLPath(controllerPath)
	if normalizedControllerPath == "/" {
		return buildPath, 1, strings.HasPrefix(buildPath, "/")
	}
	if buildPath == normalizedControllerPath {
		return "/", len(normalizedControllerPath), true
	}
	prefix := normalizedControllerPath + "/"
	if strings.HasPrefix(buildPath, prefix) {
		return strings.TrimPrefix(buildPath, normalizedControllerPath), len(normalizedControllerPath), true
	}
	return "", 0, false
}

func normalizeURLPath(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	return strings.TrimRight(path, "/")
}

func parseBuildPath(path string) (string, int, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var jobParts []string
	for i := 0; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		if parts[i] == "job" && i+1 < len(parts) {
			name, err := url.PathUnescape(parts[i+1])
			if err != nil {
				return "", 0, false
			}
			jobParts = append(jobParts, name)
			i++
			continue
		}
		build, err := strconv.Atoi(parts[i])
		if err == nil && build > 0 && len(jobParts) > 0 {
			return strings.Join(jobParts, "/"), build, true
		}
	}
	return "", 0, false
}

type CapabilitiesResponse struct {
	Controllers      []model.ControllerInfo         `json:"controllers"`
	Capabilities     []model.ControllerCapabilities `json:"capabilities"`
	MutationsEnabled bool                           `json:"mutationsEnabled"`
	Limits           config.LimitsConfig            `json:"limits"`
}

func Capabilities(ctx context.Context, deps Deps, in BaseRequest) (CapabilitiesResponse, error) {
	infos := []model.ControllerInfo{}
	capabilities := []model.ControllerCapabilities{}
	for _, c := range deps.Config.Controllers {
		api := deps.Jenkins[c.ID]
		caps := api.Capabilities(ctx)
		infos = append(infos, caps.Controller)
		capabilities = append(capabilities, caps)
	}
	return CapabilitiesResponse{Controllers: infos, Capabilities: capabilities, MutationsEnabled: deps.Config.Mutations.Enabled, Limits: deps.Config.Limits}, nil
}

type ListJobsRequest struct {
	Controller string `json:"controller,omitempty"`
	Folder     string `json:"folder,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Recursive  bool   `json:"recursive,omitempty"`
}
type ListJobsResponse struct {
	Jobs      []model.Job `json:"jobs"`
	Truncated bool        `json:"truncated"`
}

func ListJobs(ctx context.Context, deps Deps, in ListJobsRequest) (ListJobsResponse, error) {
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return ListJobsResponse{}, err
	}
	limit := pagination.BoundLimit(in.Limit, 100, 500)
	var jobs []model.Job
	if in.Recursive {
		jobs, err = listJobsRecursive(ctx, api, in.Folder, limit)
	} else {
		jobs, err = api.ListJobs(ctx, in.Folder)
	}
	if err != nil {
		return ListJobsResponse{}, err
	}
	truncated := len(jobs) > limit
	if truncated {
		jobs = jobs[:limit]
	}
	return ListJobsResponse{Jobs: jobs, Truncated: truncated}, nil
}

func listJobsRecursive(ctx context.Context, api *jenkinsapi.API, folder string, limit int) ([]model.Job, error) {
	seen := map[string]bool{}
	var out []model.Job
	var walk func(string) error
	walk = func(current string) error {
		if len(out) > limit {
			return nil
		}
		jobs, err := api.ListJobs(ctx, current)
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if seen[job.FullName] {
				continue
			}
			seen[job.FullName] = true
			out = append(out, job)
			if len(out) > limit {
				return nil
			}
			if isFolderLike(job.Class) {
				if err := walk(job.FullName); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return out, walk(folder)
}

func isFolderLike(class string) bool {
	class = strings.ToLower(class)
	return strings.Contains(class, "folder") || strings.Contains(class, "multibranch")
}

type ListBuildsRequest struct {
	Controller string `json:"controller,omitempty"`
	Job        string `json:"job"`
	Limit      int    `json:"limit,omitempty"`
}
type ListBuildsResponse struct {
	Builds []model.BuildSummary `json:"builds"`
}

func ListBuilds(ctx context.Context, deps Deps, in ListBuildsRequest) (ListBuildsResponse, error) {
	if err := validation.JobPath(in.Job); err != nil {
		return ListBuildsResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return ListBuildsResponse{}, err
	}
	builds, err := api.ListBuilds(ctx, in.Job, pagination.BoundLimit(in.Limit, 20, 100))
	return ListBuildsResponse{Builds: builds}, err
}

type GetBuildResponse struct {
	Build model.Build `json:"build"`
}

type GetJobResponse struct {
	Job model.JobDetail `json:"job"`
}

func GetJob(ctx context.Context, deps Deps, in JobRequest) (GetJobResponse, error) {
	if err := validation.JobPath(in.Job); err != nil {
		return GetJobResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return GetJobResponse{}, err
	}
	job, err := api.GetJob(ctx, in.Job)
	return GetJobResponse{Job: job}, err
}

func GetBuild(ctx context.Context, deps Deps, in BuildRequest) (GetBuildResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return GetBuildResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return GetBuildResponse{}, err
	}
	build, err := api.GetBuild(ctx, in.Job, in.Build)
	return GetBuildResponse{Build: build}, err
}

type GetLogRequest struct {
	Controller string `json:"controller,omitempty"`
	Job        string `json:"job"`
	Build      int    `json:"build"`
	Start      int64  `json:"start,omitempty"`
	MaxBytes   int64  `json:"maxBytes,omitempty"`
}
type GetLogResponse struct {
	Log model.LogChunk `json:"log"`
}

func GetLog(ctx context.Context, deps Deps, in GetLogRequest) (GetLogResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return GetLogResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return GetLogResponse{}, err
	}
	max := in.MaxBytes
	if max <= 0 || max > deps.Config.Limits.LogChunkBytes {
		max = deps.Config.Limits.LogChunkBytes
	}
	log, err := api.GetLog(ctx, in.Job, in.Build, in.Start, max)
	return GetLogResponse{Log: log}, err
}

type SearchLogRequest struct {
	Controller   string `json:"controller,omitempty"`
	Job          string `json:"job"`
	Build        int    `json:"build"`
	Query        string `json:"query"`
	Start        int64  `json:"start,omitempty"`
	MaxBytes     int64  `json:"maxBytes,omitempty"`
	MaxMatches   int    `json:"maxMatches,omitempty"`
	ContextLines int    `json:"contextLines,omitempty"`
}
type SearchLogResponse struct {
	Result model.LogSearchResult `json:"result"`
}

func SearchLog(ctx context.Context, deps Deps, in SearchLogRequest) (SearchLogResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return SearchLogResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return SearchLogResponse{}, err
	}
	maxBytes := in.MaxBytes
	if maxBytes <= 0 || maxBytes > deps.Config.Limits.MaxResponseBytes {
		maxBytes = deps.Config.Limits.MaxResponseBytes
	}
	result, err := api.SearchLog(ctx, in.Job, in.Build, in.Start, in.Query, maxBytes, pagination.BoundLimit(in.MaxMatches, 20, 200), pagination.BoundLimit(in.ContextLines, 0, 10))
	return SearchLogResponse{Result: result}, err
}

type TailLogRequest struct {
	Controller string `json:"controller,omitempty"`
	Job        string `json:"job"`
	Build      int    `json:"build"`
	Bytes      int64  `json:"bytes,omitempty"`
}
type TailLogResponse struct {
	Log model.LogChunk `json:"log"`
}

func TailLog(ctx context.Context, deps Deps, in TailLogRequest) (TailLogResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return TailLogResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return TailLogResponse{}, err
	}
	tailBytes := in.Bytes
	if tailBytes <= 0 || tailBytes > deps.Config.Limits.LogChunkBytes {
		tailBytes = deps.Config.Limits.LogChunkBytes
	}
	log, err := api.TailLog(ctx, in.Job, in.Build, tailBytes)
	return TailLogResponse{Log: log}, err
}

type TestReportRequest struct {
	Controller string `json:"controller,omitempty"`
	Job        string `json:"job"`
	Build      int    `json:"build"`
	FailedOnly bool   `json:"failedOnly,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}
type TestReportResponse struct {
	Report model.TestReport `json:"report"`
}

func TestReport(ctx context.Context, deps Deps, in TestReportRequest) (TestReportResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return TestReportResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return TestReportResponse{}, err
	}
	report, err := api.TestReport(ctx, in.Job, in.Build, in.FailedOnly, pagination.BoundLimit(in.Limit, 50, 500))
	return TestReportResponse{Report: report}, err
}

type PipelineRunResponse struct {
	Run model.PipelineRun `json:"run"`
}

func PipelineRun(ctx context.Context, deps Deps, in BuildRequest) (PipelineRunResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return PipelineRunResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return PipelineRunResponse{}, err
	}
	run, err := api.PipelineRun(ctx, in.Job, in.Build)
	return PipelineRunResponse{Run: run}, err
}

type PipelineStageRequest struct {
	Controller string `json:"controller,omitempty"`
	Job        string `json:"job"`
	Build      int    `json:"build"`
	StageID    string `json:"stageId"`
}
type PipelineStageResponse struct {
	Stage model.PipelineStageDetail `json:"stage"`
}

func PipelineStage(ctx context.Context, deps Deps, in PipelineStageRequest) (PipelineStageResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return PipelineStageResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return PipelineStageResponse{}, err
	}
	stage, err := api.PipelineStage(ctx, in.Job, in.Build, in.StageID)
	return PipelineStageResponse{Stage: stage}, err
}

type PipelineNodeLogRequest struct {
	Controller string `json:"controller,omitempty"`
	Job        string `json:"job"`
	Build      int    `json:"build"`
	NodeID     string `json:"nodeId"`
	MaxBytes   int64  `json:"maxBytes,omitempty"`
}
type PipelineNodeLogResponse struct {
	Log model.PipelineNodeLog `json:"log"`
}

func PipelineNodeLog(ctx context.Context, deps Deps, in PipelineNodeLogRequest) (PipelineNodeLogResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return PipelineNodeLogResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return PipelineNodeLogResponse{}, err
	}
	maxBytes := in.MaxBytes
	if maxBytes <= 0 || maxBytes > deps.Config.Limits.LogChunkBytes {
		maxBytes = deps.Config.Limits.LogChunkBytes
	}
	log, err := api.PipelineNodeLog(ctx, in.Job, in.Build, in.NodeID, maxBytes)
	return PipelineNodeLogResponse{Log: log}, err
}

type DownloadArtifactRequest struct {
	Controller   string `json:"controller,omitempty"`
	Job          string `json:"job"`
	Build        int    `json:"build"`
	RelativePath string `json:"relativePath"`
}

type ListArtifactsResponse struct {
	Artifacts []model.Artifact `json:"artifacts"`
}

func ListArtifacts(ctx context.Context, deps Deps, in BuildRequest) (ListArtifactsResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return ListArtifactsResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return ListArtifactsResponse{}, err
	}
	build, err := api.GetBuild(ctx, in.Job, in.Build)
	return ListArtifactsResponse{Artifacts: build.Artifacts}, err
}

type DownloadArtifactResponse struct {
	Download artifacts.DownloadResult `json:"download"`
}

func DownloadArtifact(ctx context.Context, deps Deps, in DownloadArtifactRequest) (DownloadArtifactResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return DownloadArtifactResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return DownloadArtifactResponse{}, err
	}
	d, err := artifacts.Download(ctx, deps.Config.Artifacts.DownloadDir, api, in.Job, in.Build, in.RelativePath)
	return DownloadArtifactResponse{Download: d}, err
}

type ReadArtifactRequest struct {
	Controller   string `json:"controller,omitempty"`
	Job          string `json:"job"`
	Build        int    `json:"build"`
	RelativePath string `json:"relativePath"`
	MaxBytes     int64  `json:"maxBytes,omitempty"`
}
type ReadArtifactResponse struct {
	Artifact model.ArtifactContent `json:"artifact"`
}

func ReadArtifact(ctx context.Context, deps Deps, in ReadArtifactRequest) (ReadArtifactResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return ReadArtifactResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return ReadArtifactResponse{}, err
	}
	maxBytes := in.MaxBytes
	if maxBytes <= 0 || maxBytes > deps.Config.Limits.InlineBytes {
		maxBytes = deps.Config.Limits.InlineBytes
	}
	artifact, err := api.ReadArtifact(ctx, in.Job, in.Build, in.RelativePath, maxBytes)
	return ReadArtifactResponse{Artifact: artifact}, err
}

type CoverageResponse struct {
	Report model.CoverageReport `json:"report"`
}

func Coverage(ctx context.Context, deps Deps, in BuildRequest) (CoverageResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return CoverageResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return CoverageResponse{}, err
	}
	report, err := api.CoverageReport(ctx, in.Job, in.Build)
	return CoverageResponse{Report: report}, err
}

type IssuesResponse struct {
	Report model.IssuesReport `json:"report"`
}

func Issues(ctx context.Context, deps Deps, in BuildRequest) (IssuesResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return IssuesResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return IssuesResponse{}, err
	}
	report, err := api.IssuesReport(ctx, in.Job, in.Build)
	return IssuesResponse{Report: report}, err
}

type ChangesResponse struct {
	ChangeSets []model.ChangeSet `json:"changeSets"`
	Truncated  bool              `json:"truncated"`
}

func Changes(ctx context.Context, deps Deps, in BuildRequest) (ChangesResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return ChangesResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return ChangesResponse{}, err
	}
	build, err := api.GetBuild(ctx, in.Job, in.Build)
	if err != nil {
		return ChangesResponse{}, err
	}
	return ChangesResponse{ChangeSets: build.ChangeSets}, nil
}

type WatchBuildRequest struct {
	Controller    string `json:"controller,omitempty"`
	Job           string `json:"job"`
	Build         int    `json:"build"`
	LastState     string `json:"lastState,omitempty"`
	WaitTimeoutMs int64  `json:"waitTimeoutMs,omitempty"`
}
type WatchBuildResponse struct {
	Watch model.BuildWatch `json:"watch"`
}

type watchState struct {
	Version int                `json:"v"`
	Target  watchTargetState   `json:"target"`
	Summary model.BuildSummary `json:"summary"`
	Build   watchBuildState    `json:"build"`
	Run     watchRunState      `json:"run"`
	Stages  []watchStageState  `json:"stages,omitempty"`
}

type watchTargetState struct {
	Controller string `json:"controller"`
	Job        string `json:"job"`
	Build      int    `json:"build"`
}

type watchBuildState struct {
	Building bool   `json:"building"`
	Result   string `json:"result,omitempty"`
}

type watchRunState struct {
	Status string `json:"status,omitempty"`
}

type watchStageState struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty"`
}

func WatchBuild(ctx context.Context, deps Deps, in WatchBuildRequest) (WatchBuildResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return WatchBuildResponse{}, err
	}
	controllerID := in.Controller
	if controllerID == "" {
		controllerID = deps.Config.DefaultController
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return WatchBuildResponse{}, err
	}
	previous, err := decodeWatchState(in.LastState)
	if err != nil {
		return WatchBuildResponse{}, err
	}
	if previous != nil && !watchTargetMatches(previous.Target, controllerID, in.Job, in.Build) {
		return WatchBuildResponse{}, apperrors.Wrap(apperrors.CodeInvalidRequest, "watch state does not match requested build", map[string]any{
			"controller": controllerID,
			"job":        in.Job,
			"build":      in.Build,
		})
	}
	waitTimeout := deps.Config.Watch.DefaultWaitTimeoutMs
	if in.WaitTimeoutMs > 0 {
		waitTimeout = in.WaitTimeoutMs
	}
	if waitTimeout > deps.Config.Watch.MaxWaitTimeoutMs {
		waitTimeout = deps.Config.Watch.MaxWaitTimeoutMs
	}
	deadline := time.Now().Add(time.Duration(waitTimeout) * time.Millisecond)
	consecutiveFailures := 0

	for {
		build, pipelinePtr, current, pipelineDegraded, fatal, err := fetchWatchState(ctx, api, controllerID, in.Job, in.Build)
		if fatal && err != nil {
			return WatchBuildResponse{}, err
		}
		if err == nil {
			consecutiveFailures = 0
			if pipelineDegraded && previous != nil {
				current.Run = previous.Run
				current.Stages = previous.Stages
				pipelinePtr = pipelineRunFromState(previous)
			}
			changed := previous == nil || !watchStatesEqual(*previous, current)
			complete := !build.Building
			if changed || complete || !time.Now().Before(deadline) {
				stateToken, err := encodeWatchState(current)
				if err != nil {
					return WatchBuildResponse{}, err
				}
				return WatchBuildResponse{Watch: model.BuildWatch{
					State:    stateToken,
					Build:    build.BuildSummary,
					Pipeline: pipelinePtr,
					Complete: complete,
					TimedOut: !changed && !complete,
				}}, nil
			}
		} else {
			if previous == nil && !time.Now().Before(deadline) {
				return WatchBuildResponse{}, apperrors.Wrap(apperrors.CodeUnavailable, "jenkins watch bootstrap timed out", map[string]any{
					"waitTimeoutMs": waitTimeout,
					"lastError":     err.Error(),
				})
			}
			if previous != nil && !time.Now().Before(deadline) {
				return WatchBuildResponse{Watch: model.BuildWatch{
					State:    in.LastState,
					Build:    previous.Summary,
					Pipeline: pipelineRunFromState(previous),
					Complete: !previous.Build.Building,
					TimedOut: true,
				}}, nil
			}
			consecutiveFailures++
			if consecutiveFailures >= deps.Config.Watch.MaxConsecutiveFailures {
				return WatchBuildResponse{}, apperrors.Wrap(apperrors.CodeUnavailable, "jenkins watch polling failed repeatedly", map[string]any{
					"consecutiveFailures": consecutiveFailures,
					"lastError":           err.Error(),
				})
			}
		}

		if err := sleepWithContext(ctx, watchSleepDuration(time.Until(deadline), deps.Config.Watch.PollIntervalMs)); err != nil {
			return WatchBuildResponse{}, err
		}
	}
}

func fetchWatchState(ctx context.Context, api *jenkinsapi.API, controllerID, job string, buildNumber int) (model.Build, *model.PipelineRun, watchState, bool, bool, error) {
	build, err := api.GetBuild(ctx, job, buildNumber)
	if err != nil {
		return model.Build{}, nil, watchState{}, false, false, err
	}
	pipeline, err := api.PipelineRun(ctx, job, buildNumber)
	if err != nil {
		if isDegradablePipelineError(err) {
			state := watchState{
				Version: 1,
				Target: watchTargetState{
					Controller: controllerID,
					Job:        job,
					Build:      buildNumber,
				},
				Summary: stableWatchSummary(build.BuildSummary),
				Build: watchBuildState{
					Building: build.Building,
					Result:   build.Result,
				},
				Run: watchRunState{},
			}
			return build, nil, state, true, false, nil
		}
		return model.Build{}, nil, watchState{}, false, true, err
	}
	pipelineCopy := pipeline
	return build, &pipelineCopy, newWatchState(controllerID, job, build, &pipeline), false, false, nil
}

func newWatchState(controllerID, job string, build model.Build, pipeline *model.PipelineRun) watchState {
	state := watchState{
		Version: 1,
		Target: watchTargetState{
			Controller: controllerID,
			Job:        job,
			Build:      build.Number,
		},
		Summary: stableWatchSummary(build.BuildSummary),
		Build: watchBuildState{
			Building: build.Building,
			Result:   build.Result,
		},
	}
	if pipeline == nil {
		return state
	}
	state.Run = watchRunState{
		Status: pipeline.Status,
	}
	stages := make([]watchStageState, 0, len(pipeline.Stages))
	for _, stage := range pipeline.Stages {
		stages = append(stages, watchStageState{
			ID:     stage.ID,
			Name:   stage.Name,
			Status: stage.Status,
		})
	}
	state.Stages = stages
	return state
}

func encodeWatchState(state watchState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", apperrors.Wrap(apperrors.CodeJenkins, "failed to encode watch state", err.Error())
	}
	compressed, err := compressWatchState(payload)
	if err != nil {
		return "", err
	}
	key, err := getWatchStateSigningKey()
	if err != nil {
		return "", err
	}
	payloadToken := base64.RawURLEncoding.EncodeToString(compressed)
	signature := signWatchStateToken(key, payloadToken)
	token := payloadToken + "." + signature
	if len(token) > maxWatchStateTokenBytes {
		return "", apperrors.Wrap(apperrors.CodeJenkins, "watch state too large to encode", map[string]any{"maxBytes": maxWatchStateTokenBytes})
	}
	return token, nil
}

func decodeWatchState(raw string) (*watchState, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if len(raw) > maxWatchStateTokenBytes {
		return nil, apperrors.Wrap(apperrors.CodeInvalidRequest, "watch state too large", map[string]any{"maxBytes": maxWatchStateTokenBytes})
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid watch state")
	}
	key, err := getWatchStateSigningKey()
	if err != nil {
		return nil, err
	}
	if !verifyWatchStateToken(key, parts[0], parts[1]) {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "watch state expired; you need to re-bootstrap")
	}
	compressed, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid watch state")
	}
	payload, err := decompressWatchState(compressed)
	if err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid watch state")
	}
	var state watchState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid watch state")
	}
	if state.Version != 1 {
		return nil, apperrors.Wrap(apperrors.CodeInvalidRequest, "unsupported watch state version", state.Version)
	}
	return &state, nil
}

func getWatchStateSigningKey() ([]byte, error) {
	watchStateSigningKeyOnce.Do(func() {
		watchStateSigningKey = make([]byte, 32)
		if _, err := rand.Read(watchStateSigningKey); err != nil {
			watchStateSigningKeyErr = apperrors.Wrap(apperrors.CodeUnavailable, "failed to initialize watch state signing key", err.Error())
		}
	})
	if watchStateSigningKeyErr != nil {
		return nil, watchStateSigningKeyErr
	}
	return watchStateSigningKey, nil
}

func signWatchStateToken(key []byte, payload string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyWatchStateToken(key []byte, payload, signature string) bool {
	got, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return hmac.Equal(got, mac.Sum(nil))
}

func compressWatchState(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, apperrors.Wrap(apperrors.CodeJenkins, "failed to compress watch state", err.Error())
	}
	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return nil, apperrors.Wrap(apperrors.CodeJenkins, "failed to compress watch state", err.Error())
	}
	if err := writer.Close(); err != nil {
		return nil, apperrors.Wrap(apperrors.CodeJenkins, "failed to compress watch state", err.Error())
	}
	return buf.Bytes(), nil
}

func decompressWatchState(payload []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()
	data, err := io.ReadAll(io.LimitReader(reader, maxWatchStateUncompressedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxWatchStateUncompressedBytes {
		return nil, fmt.Errorf("watch state exceeds maximum uncompressed size")
	}
	return data, nil
}

func watchStatesEqual(a, b watchState) bool {
	if a.Target != b.Target {
		return false
	}
	if a.Build != b.Build {
		return false
	}
	if a.Run != b.Run {
		return false
	}
	if len(a.Stages) != len(b.Stages) {
		return false
	}
	for i := range a.Stages {
		if a.Stages[i] != b.Stages[i] {
			return false
		}
	}
	return true
}

func watchTargetMatches(target watchTargetState, controllerID, job string, build int) bool {
	return target.Controller == controllerID && target.Job == job && target.Build == build
}

func stableWatchSummary(summary model.BuildSummary) model.BuildSummary {
	return model.BuildSummary{
		Number:   summary.Number,
		URL:      summary.URL,
		Result:   summary.Result,
		Building: summary.Building,
	}
}

func isDegradablePipelineError(err error) bool {
	appErr, ok := err.(*apperrors.Error)
	if !ok {
		return false
	}
	switch appErr.Code {
	case apperrors.CodeNotFound, apperrors.CodeUnavailable:
		return true
	case apperrors.CodeJenkins:
		return strings.HasPrefix(appErr.Message, "Jenkins returned HTTP 5")
	default:
		return false
	}
}

func pipelineRunFromState(state *watchState) *model.PipelineRun {
	if state == nil {
		return nil
	}
	if state.Run.Status == "" && len(state.Stages) == 0 {
		return nil
	}
	run := &model.PipelineRun{Status: state.Run.Status}
	if len(state.Stages) == 0 {
		return run
	}
	run.Stages = make([]model.PipelineStage, 0, len(state.Stages))
	for _, stage := range state.Stages {
		run.Stages = append(run.Stages, model.PipelineStage{
			ID:     stage.ID,
			Name:   stage.Name,
			Status: stage.Status,
		})
	}
	return run
}

func watchSleepDuration(remaining time.Duration, pollIntervalMs int64) time.Duration {
	interval := time.Duration(pollIntervalMs) * time.Millisecond
	if remaining < interval {
		return remaining
	}
	return interval
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type TriggerBuildRequest struct {
	Controller string            `json:"controller,omitempty"`
	Job        string            `json:"job"`
	Parameters map[string]string `json:"parameters,omitempty"`
}
type TriggerBuildResponse struct {
	QueueURL  string `json:"queueUrl,omitempty"`
	Triggered bool   `json:"triggered"`
}

func TriggerBuild(ctx context.Context, deps Deps, in TriggerBuildRequest) (TriggerBuildResponse, error) {
	if !deps.Config.Mutations.Enabled {
		return TriggerBuildResponse{}, apperrors.New(apperrors.CodeMutationDisabled, "mutating Jenkins tools are disabled")
	}
	if err := validation.JobPath(in.Job); err != nil {
		return TriggerBuildResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return TriggerBuildResponse{}, err
	}
	job, err := api.GetJob(ctx, in.Job)
	if err != nil {
		return TriggerBuildResponse{}, err
	}
	if err := validateTriggerParameters(job.Parameters, in.Parameters); err != nil {
		return TriggerBuildResponse{}, err
	}
	location, err := api.TriggerBuild(ctx, in.Job, in.Parameters)
	emit(deps, in.Controller, "trigger_build", in.Job, err)
	return TriggerBuildResponse{QueueURL: location, Triggered: err == nil}, err
}

func validateTriggerParameters(definitions []model.ParameterDefinition, parameters map[string]string) error {
	if len(definitions) == 0 {
		return nil
	}
	allowed := map[string]model.ParameterDefinition{}
	for _, definition := range definitions {
		allowed[definition.Name] = definition
	}
	for name := range parameters {
		if _, ok := allowed[name]; !ok {
			return apperrors.Wrap(apperrors.CodeInvalidRequest, "unknown build parameter", map[string]any{"parameter": name})
		}
	}
	for _, definition := range definitions {
		if !definition.Required {
			continue
		}
		if _, ok := parameters[definition.Name]; !ok {
			return apperrors.Wrap(apperrors.CodeInvalidRequest, "missing required build parameter", map[string]any{"parameter": definition.Name})
		}
	}
	return nil
}

type QueueItemRequest struct {
	Controller string `json:"controller,omitempty"`
	ID         int64  `json:"id"`
}
type QueueItemResponse struct {
	Item model.QueueItem `json:"item"`
}

type ListQueueResponse struct {
	Items []model.QueueItem `json:"items"`
}

func ListQueue(ctx context.Context, deps Deps, in BaseRequest) (ListQueueResponse, error) {
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return ListQueueResponse{}, err
	}
	items, err := api.ListQueue(ctx)
	return ListQueueResponse{Items: items}, err
}

func QueueItem(ctx context.Context, deps Deps, in QueueItemRequest) (QueueItemResponse, error) {
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return QueueItemResponse{}, err
	}
	item, err := api.QueueItem(ctx, in.ID)
	return QueueItemResponse{Item: item}, err
}

type CancelQueueItemResponse struct {
	Cancelled bool `json:"cancelled"`
}

func CancelQueueItem(ctx context.Context, deps Deps, in QueueItemRequest) (CancelQueueItemResponse, error) {
	if !deps.Config.Mutations.Enabled {
		return CancelQueueItemResponse{}, apperrors.New(apperrors.CodeMutationDisabled, "mutating Jenkins tools are disabled")
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return CancelQueueItemResponse{}, err
	}
	err = api.CancelQueueItem(ctx, in.ID)
	emit(deps, in.Controller, "cancel_queue_item", fmt.Sprintf("%d", in.ID), err)
	return CancelQueueItemResponse{Cancelled: err == nil}, err
}

type CancelBuildResponse struct {
	Cancelled bool `json:"cancelled"`
}

func CancelBuild(ctx context.Context, deps Deps, in BuildRequest) (CancelBuildResponse, error) {
	if !deps.Config.Mutations.Enabled {
		return CancelBuildResponse{}, apperrors.New(apperrors.CodeMutationDisabled, "mutating Jenkins tools are disabled")
	}
	if err := validateBuild(in.Job, in.Build); err != nil {
		return CancelBuildResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return CancelBuildResponse{}, err
	}
	err = api.CancelBuild(ctx, in.Job, in.Build)
	emit(deps, in.Controller, "cancel_build", fmt.Sprintf("%s#%d", in.Job, in.Build), err)
	return CancelBuildResponse{Cancelled: err == nil}, err
}

func apiFor(deps Deps, controller string) (*jenkinsapi.API, error) {
	if controller == "" {
		controller = deps.Config.DefaultController
	}
	api := deps.Jenkins[controller]
	if api == nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "unknown controller")
	}
	return api, nil
}
func validateBuild(job string, build int) error {
	if err := validation.JobPath(job); err != nil {
		return err
	}
	return validation.BuildNumber(build)
}
func emit(deps Deps, controller, action, target string, err error) {
	if controller == "" {
		controller = deps.Config.DefaultController
	}
	outcome := "success"
	msg := ""
	if err != nil {
		outcome = "error"
		msg = err.Error()
	}
	_ = deps.Audit.Emit(audit.Event{Time: time.Now().UTC(), Controller: controller, Action: action, Target: target, Outcome: outcome, Error: msg})
}
