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
	"regexp"
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
	"github.com/david/jenkins-mcp/internal/selfupdate"
	"github.com/david/jenkins-mcp/internal/updatecheck"
	"github.com/david/jenkins-mcp/internal/validation"
)

type Deps struct {
	Config       config.Config
	Jenkins      map[string]*jenkinsapi.API
	Audit        *audit.Logger
	UpdateStatus func() updatecheck.Status
	SelfUpdate   func(context.Context, bool) (selfupdate.Result, error)
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
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
}

type JobConfigRequest struct {
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Mode       string `json:"mode,omitempty" jsonschema:"Configuration output mode: summary returns structured metadata, xml returns best-effort redacted config.xml text, both returns both; defaults to summary"`
	MaxBytes   int64  `json:"maxBytes,omitempty" jsonschema:"Maximum redacted XML bytes to return for xml or both mode; defaults to the configured inline response limit"`
}
type BuildRequest struct {
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build      int    `json:"build" jsonschema:"Jenkins build number"`
}

type ResolveBuildURLRequest struct {
	URL string `json:"url" jsonschema:"Full Jenkins build URL to resolve against configured controllers"`
}

type ResolveBuildURLResponse struct {
	Reference model.BuildReference `json:"reference" jsonschema:"Resolved Jenkins controller, job path, build number, and original URL"`
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
	Controllers      []model.ControllerInfo         `json:"controllers" jsonschema:"Configured Jenkins controllers and availability information"`
	Capabilities     []model.ControllerCapabilities `json:"capabilities" jsonschema:"Detected Jenkins controller capabilities and plugin information"`
	CapabilityConfig config.CapabilityConfig        `json:"capabilityConfig" jsonschema:"Configuration that controls optional capability discovery behavior"`
	MutationsEnabled bool                           `json:"mutationsEnabled" jsonschema:"Whether Jenkins-mutating tools are enabled by configuration"`
	Limits           config.LimitsConfig            `json:"limits" jsonschema:"Configured response and inline content limits"`
	Updates          updatecheck.Status             `json:"updates" jsonschema:"Update-check status for this MCP server"`
}

func Capabilities(ctx context.Context, deps Deps, in BaseRequest) (CapabilitiesResponse, error) {
	infos := []model.ControllerInfo{}
	capabilities := []model.ControllerCapabilities{}
	for _, c := range deps.Config.Controllers {
		api := deps.Jenkins[c.ID]
		caps := api.Capabilities(ctx, deps.Config.Capabilities.PluginDiscoveryEnabled)
		infos = append(infos, caps.Controller)
		capabilities = append(capabilities, caps)
	}
	var updates updatecheck.Status
	if deps.UpdateStatus != nil {
		updates = deps.UpdateStatus()
	}
	return CapabilitiesResponse{Controllers: infos, Capabilities: capabilities, CapabilityConfig: deps.Config.Capabilities, MutationsEnabled: deps.Config.Mutations.Enabled, Limits: deps.Config.Limits, Updates: updates}, nil
}

type UpdateServerRequest struct {
	Force bool `json:"force,omitempty" jsonschema:"Reinstall the latest release even when it is not newer than the current server version"`
}

type UpdateServerResponse struct {
	Update selfupdate.Result `json:"update" jsonschema:"Self-update installation or staging result"`
}

func UpdateServer(ctx context.Context, deps Deps, in UpdateServerRequest) (UpdateServerResponse, error) {
	if !deps.Config.Updates.SelfUpdateEnabled {
		return UpdateServerResponse{}, apperrors.New(apperrors.CodeMutationDisabled, "server self-update tool is disabled")
	}
	if deps.SelfUpdate == nil {
		return UpdateServerResponse{}, apperrors.New(apperrors.CodeUnsupported, "server self-update is not available")
	}
	result, err := deps.SelfUpdate(ctx, in.Force)
	emit(deps, "", "update_server", selfUpdateAuditTarget(result), err)
	return UpdateServerResponse{Update: result}, err
}

func selfUpdateAuditTarget(result selfupdate.Result) string {
	path := result.InstalledPath
	if path == "" {
		path = result.StagedPath
	}
	version := result.LatestVersion
	if version == "" {
		version = "latest"
	}
	if path == "" {
		return version
	}
	return fmt.Sprintf("%s %s", version, path)
}

type ListJobsRequest struct {
	Controller   string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Folder       string `json:"folder,omitempty" jsonschema:"Optional Jenkins folder path to list, using / for nested folders"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum number of matching jobs to return; defaults to 100 and is capped at 500"`
	Cursor       string `json:"cursor,omitempty" jsonschema:"Opaque continuation cursor returned by a previous jenkins_list_jobs response"`
	Recursive    bool   `json:"recursive,omitempty" jsonschema:"When true, recursively traverse folder-like jobs and apply filters across descendants"`
	NameContains string `json:"nameContains,omitempty" jsonschema:"Case-insensitive substring filter matched against both name and fullName"`
	NameRegex    string `json:"nameRegex,omitempty" jsonschema:"Regular expression filter matched against both name and fullName"`
	Type         string `json:"type,omitempty" jsonschema:"Job type filter; accepts friendly names such as folder, pipeline, multibranch, freestyle, or raw Jenkins class names"`
	Status       string `json:"status,omitempty" jsonschema:"Derived job status filter such as success, failed, unstable, aborted, disabled, not_built, or unknown"`
	Building     *bool  `json:"building,omitempty" jsonschema:"Filter by whether lastBuild is currently building"`
}
type ListJobsResponse struct {
	Items      []model.Job `json:"items" jsonschema:"Matching Jenkins jobs for this page"`
	NextCursor string      `json:"nextCursor,omitempty" jsonschema:"Opaque cursor to request the next page when hasMore is true"`
	HasMore    bool        `json:"hasMore" jsonschema:"Whether additional matching jobs are available after this page"`
	Truncated  bool        `json:"truncated" jsonschema:"Whether additional matching jobs were omitted from this page due to the requested or configured limit"`
	Limit      int         `json:"limit" jsonschema:"Maximum number of matching jobs requested for this page after applying server caps"`
}

func ListJobs(ctx context.Context, deps Deps, in ListJobsRequest) (ListJobsResponse, error) {
	filter, err := newJobFilter(in)
	if err != nil {
		return ListJobsResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return ListJobsResponse{}, err
	}
	limit := pagination.BoundLimit(in.Limit, 100, 500)
	cursorSignature, err := listJobsCursorSignature(in)
	if err != nil {
		return ListJobsResponse{}, err
	}
	offset, gotSignature, err := pagination.DecodeCursor(in.Cursor, listJobsCursorKind)
	if err != nil {
		return ListJobsResponse{}, apperrors.Wrap(apperrors.CodeInvalidRequest, "invalid list jobs cursor", map[string]any{"cursor": in.Cursor, "reason": err.Error()})
	}
	if gotSignature != "" && gotSignature != cursorSignature {
		return ListJobsResponse{}, apperrors.Wrap(apperrors.CodeInvalidRequest, "list jobs cursor does not match request", map[string]any{"cursor": in.Cursor})
	}
	var jobs []model.Job
	if in.Recursive {
		jobs, err = listJobsRecursive(ctx, api, in.Folder, offset+limit+1, filter)
	} else {
		jobs, err = api.ListJobs(ctx, in.Folder)
	}
	if err != nil {
		return ListJobsResponse{}, err
	}
	if !in.Recursive {
		jobs = filterJobs(jobs, filter)
	}
	return listJobsPage(jobs, offset, limit, cursorSignature)
}

const listJobsCursorKind = "jenkins_list_jobs"

func listJobsPage(jobs []model.Job, offset int, limit int, signature string) (ListJobsResponse, error) {
	if offset > len(jobs) {
		offset = len(jobs)
	}
	end := offset + limit
	hasMore := len(jobs) > end
	if end > len(jobs) {
		end = len(jobs)
	}
	nextCursor := ""
	if hasMore {
		var err error
		nextCursor, err = pagination.EncodeCursor(listJobsCursorKind, end, signature)
		if err != nil {
			return ListJobsResponse{}, err
		}
	}
	return ListJobsResponse{Items: jobs[offset:end], NextCursor: nextCursor, HasMore: hasMore, Truncated: hasMore, Limit: limit}, nil
}

func listJobsCursorSignature(in ListJobsRequest) (string, error) {
	var building *bool
	if in.Building != nil {
		value := *in.Building
		building = &value
	}
	body, err := json.Marshal(struct {
		Controller   string `json:"controller,omitempty"`
		Folder       string `json:"folder,omitempty"`
		Recursive    bool   `json:"recursive,omitempty"`
		NameContains string `json:"nameContains,omitempty"`
		NameRegex    string `json:"nameRegex,omitempty"`
		Type         string `json:"type,omitempty"`
		Status       string `json:"status,omitempty"`
		Building     *bool  `json:"building,omitempty"`
	}{
		Controller:   in.Controller,
		Folder:       in.Folder,
		Recursive:    in.Recursive,
		NameContains: in.NameContains,
		NameRegex:    in.NameRegex,
		Type:         in.Type,
		Status:       in.Status,
		Building:     building,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

type jobFilter struct {
	nameContains string
	nameRegex    *regexp.Regexp
	jobType      string
	status       string
	building     *bool
}

func newJobFilter(in ListJobsRequest) (jobFilter, error) {
	var filter jobFilter
	filter.nameContains = strings.ToLower(strings.TrimSpace(in.NameContains))
	filter.jobType = strings.ToLower(strings.TrimSpace(in.Type))
	filter.status = normalizeStatusFilter(in.Status)
	filter.building = in.Building
	if strings.TrimSpace(in.NameRegex) != "" {
		expr, err := regexp.Compile(in.NameRegex)
		if err != nil {
			return jobFilter{}, apperrors.Wrap(apperrors.CodeInvalidRequest, "invalid job name regex", map[string]any{
				"regex": in.NameRegex,
				"error": err.Error(),
			})
		}
		filter.nameRegex = expr
	}
	return filter, nil
}

func filterJobs(jobs []model.Job, filter jobFilter) []model.Job {
	if filter == (jobFilter{}) {
		return jobs
	}
	out := make([]model.Job, 0, len(jobs))
	for _, job := range jobs {
		if jobMatchesFilter(job, filter) {
			out = append(out, job)
		}
	}
	return out
}

func jobMatchesFilter(job model.Job, filter jobFilter) bool {
	if filter.nameContains != "" && !strings.Contains(strings.ToLower(job.Name), filter.nameContains) && !strings.Contains(strings.ToLower(job.FullName), filter.nameContains) {
		return false
	}
	if filter.nameRegex != nil && !filter.nameRegex.MatchString(job.Name) && !filter.nameRegex.MatchString(job.FullName) {
		return false
	}
	if filter.jobType != "" && !jobMatchesType(job.Class, filter.jobType) {
		return false
	}
	if filter.status != "" && normalizeStatusFilter(job.Status) != filter.status {
		return false
	}
	if filter.building != nil && job.Building != *filter.building {
		return false
	}
	return true
}

func normalizeStatusFilter(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failure":
		return "failed"
	case "notbuilt", "not-built":
		return "not_built"
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}

func jobMatchesType(class string, typ string) bool {
	class = strings.ToLower(strings.TrimSpace(class))
	typ = strings.ToLower(strings.TrimSpace(typ))
	if class == "" || typ == "" {
		return false
	}
	if class == typ || strings.HasSuffix(class, "."+typ) {
		return true
	}
	switch typ {
	case "folder":
		return strings.Contains(class, "folder") && !strings.Contains(class, "multibranch")
	case "pipeline", "workflow":
		return strings.Contains(class, "workflowjob")
	case "multibranch", "multi-branch", "multi_branch":
		return strings.Contains(class, "multibranch")
	case "freestyle", "free-style", "free_style":
		return strings.Contains(class, "freestyleproject")
	default:
		return false
	}
}

func listJobsRecursive(ctx context.Context, api *jenkinsapi.API, folder string, maxMatches int, filter jobFilter) ([]model.Job, error) {
	seen := map[string]bool{}
	var out []model.Job
	var walk func(string) error
	walk = func(current string) error {
		if maxMatches > 0 && len(out) >= maxMatches {
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
			if jobMatchesFilter(job, filter) {
				out = append(out, job)
				if maxMatches > 0 && len(out) >= maxMatches {
					return nil
				}
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
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum number of recent builds to return; defaults to 20 and is capped at 100"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"Opaque continuation cursor returned by a previous jenkins_list_builds response"`
}
type ListBuildsResponse struct {
	Items      []model.BuildSummary `json:"items" jsonschema:"Recent Jenkins builds for this page"`
	NextCursor string               `json:"nextCursor,omitempty" jsonschema:"Opaque cursor to request the next page when hasMore is true"`
	HasMore    bool                 `json:"hasMore" jsonschema:"Whether additional builds are available after this page"`
	Truncated  bool                 `json:"truncated" jsonschema:"Whether additional builds were omitted from this page due to the requested or configured limit"`
	Limit      int                  `json:"limit" jsonschema:"Maximum number of builds requested for this page after applying server caps"`
}

func ListBuilds(ctx context.Context, deps Deps, in ListBuildsRequest) (ListBuildsResponse, error) {
	if err := validation.JobPath(in.Job); err != nil {
		return ListBuildsResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return ListBuildsResponse{}, err
	}
	limit := pagination.BoundLimit(in.Limit, 20, 100)
	cursorSignature, err := listBuildsCursorSignature(in)
	if err != nil {
		return ListBuildsResponse{}, err
	}
	offset, gotSignature, err := pagination.DecodeCursor(in.Cursor, listBuildsCursorKind)
	if err != nil {
		return ListBuildsResponse{}, apperrors.Wrap(apperrors.CodeInvalidRequest, "invalid list builds cursor", map[string]any{"cursor": in.Cursor, "reason": err.Error()})
	}
	if gotSignature != "" && gotSignature != cursorSignature {
		return ListBuildsResponse{}, apperrors.Wrap(apperrors.CodeInvalidRequest, "list builds cursor does not match request", map[string]any{"cursor": in.Cursor})
	}
	builds, err := api.ListBuilds(ctx, in.Job, offset, limit+1)
	if err != nil {
		return ListBuildsResponse{}, err
	}
	return listBuildsPage(builds, offset, limit, cursorSignature)
}

const listBuildsCursorKind = "jenkins_list_builds"

func listBuildsPage(builds []model.BuildSummary, offset int, limit int, signature string) (ListBuildsResponse, error) {
	hasMore := len(builds) > limit
	if hasMore {
		builds = builds[:limit]
	}
	nextCursor := ""
	if hasMore {
		var err error
		nextCursor, err = pagination.EncodeCursor(listBuildsCursorKind, offset+limit, signature)
		if err != nil {
			return ListBuildsResponse{}, err
		}
	}
	return ListBuildsResponse{Items: builds, NextCursor: nextCursor, HasMore: hasMore, Truncated: hasMore, Limit: limit}, nil
}

func listBuildsCursorSignature(in ListBuildsRequest) (string, error) {
	body, err := json.Marshal(struct {
		Controller string `json:"controller,omitempty"`
		Job        string `json:"job"`
	}{
		Controller: in.Controller,
		Job:        in.Job,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

type GetBuildResponse struct {
	Build model.Build `json:"build" jsonschema:"Detailed Jenkins build information"`
}

type GetJobResponse struct {
	Job model.JobDetail `json:"job" jsonschema:"Detailed Jenkins job metadata"`
}

type GetJobConfigResponse struct {
	Config model.JobConfig `json:"config" jsonschema:"Jenkins job configuration inspection result with redacted XML or structured summary"`
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

func GetJobConfig(ctx context.Context, deps Deps, in JobConfigRequest) (GetJobConfigResponse, error) {
	if err := validation.JobPath(in.Job); err != nil {
		return GetJobConfigResponse{}, err
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "summary"
	}
	if mode != "summary" && mode != "xml" && mode != "both" {
		return GetJobConfigResponse{}, apperrors.Wrap(apperrors.CodeInvalidRequest, "invalid job config mode", map[string]any{"mode": in.Mode, "allowed": []string{"summary", "xml", "both"}})
	}
	maxBytes := in.MaxBytes
	if maxBytes <= 0 || maxBytes > deps.Config.Limits.InlineBytes {
		maxBytes = deps.Config.Limits.InlineBytes
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return GetJobConfigResponse{}, err
	}
	config, err := api.GetJobConfig(ctx, in.Job, mode, maxBytes)
	return GetJobConfigResponse{Config: config}, err
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
	if err != nil {
		return GetBuildResponse{}, err
	}
	coverage, err := api.CoverageReport(ctx, in.Job, in.Build)
	if err != nil {
		return GetBuildResponse{}, err
	}
	if coverage.Available || len(coverage.Errors) > 0 {
		build.Coverage = &coverage
	}
	return GetBuildResponse{Build: build}, err
}

type GetLogRequest struct {
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build      int    `json:"build" jsonschema:"Jenkins build number"`
	Start      int64  `json:"start,omitempty" jsonschema:"Progressive log byte offset to start reading from"`
	MaxBytes   int64  `json:"maxBytes,omitempty" jsonschema:"Maximum log bytes to return; defaults to the configured log chunk limit"`
}
type GetLogResponse struct {
	Log model.LogChunk `json:"log" jsonschema:"Bounded Jenkins console log chunk"`
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
	Controller   string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job          string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build        int    `json:"build" jsonschema:"Jenkins build number"`
	Query        string `json:"query" jsonschema:"Text to search for in the console log"`
	Start        int64  `json:"start,omitempty" jsonschema:"Progressive log byte offset to start searching from"`
	MaxBytes     int64  `json:"maxBytes,omitempty" jsonschema:"Maximum log bytes to search; defaults to the configured response limit"`
	MaxMatches   int    `json:"maxMatches,omitempty" jsonschema:"Maximum matching log lines to return; defaults to 20 and is capped at 200"`
	ContextLines int    `json:"contextLines,omitempty" jsonschema:"Number of surrounding context lines to include per match; capped at 10"`
}
type SearchLogResponse struct {
	Result model.LogSearchResult `json:"result" jsonschema:"Console log search matches and pagination state"`
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
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build      int    `json:"build" jsonschema:"Jenkins build number"`
	Bytes      int64  `json:"bytes,omitempty" jsonschema:"Maximum tail bytes to return; defaults to the configured log chunk limit"`
}
type TailLogResponse struct {
	Log model.LogChunk `json:"log" jsonschema:"Tail chunk from the Jenkins console log"`
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
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build      int    `json:"build" jsonschema:"Jenkins build number"`
	FailedOnly bool   `json:"failedOnly,omitempty" jsonschema:"When true, return only failed test cases"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum number of test cases to return; defaults to 50 and is capped at 500"`
}
type TestReportResponse struct {
	Report model.TestReport `json:"report" jsonschema:"JUnit test summary and bounded test case details"`
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
	Run model.PipelineRun `json:"run" jsonschema:"Pipeline run status, stage summary, and pending input-step state"`
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
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build      int    `json:"build" jsonschema:"Jenkins build number"`
	StageID    string `json:"stageId" jsonschema:"Pipeline stage id from jenkins_get_pipeline_run"`
}
type PipelineStageResponse struct {
	Stage model.PipelineStageDetail `json:"stage" jsonschema:"Pipeline stage detail and child flow nodes"`
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
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build      int    `json:"build" jsonschema:"Jenkins build number"`
	NodeID     string `json:"nodeId" jsonschema:"Pipeline node id from stage or run details"`
	MaxBytes   int64  `json:"maxBytes,omitempty" jsonschema:"Maximum node log bytes to return; defaults to the configured log chunk limit"`
}
type PipelineNodeLogResponse struct {
	Log model.PipelineNodeLog `json:"log" jsonschema:"Bounded Pipeline node log output"`
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
	Controller   string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job          string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build        int    `json:"build" jsonschema:"Jenkins build number"`
	RelativePath string `json:"relativePath" jsonschema:"Artifact relative path from the Jenkins build artifacts list"`
}

type ListArtifactsResponse struct {
	Artifacts []model.Artifact `json:"artifacts" jsonschema:"Artifacts published by the Jenkins build"`
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
	Download artifacts.DownloadResult `json:"download" jsonschema:"Local artifact download result"`
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
	Controller   string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job          string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build        int    `json:"build" jsonschema:"Jenkins build number"`
	RelativePath string `json:"relativePath" jsonschema:"Artifact relative path from the Jenkins build artifacts list"`
	MaxBytes     int64  `json:"maxBytes,omitempty" jsonschema:"Maximum artifact bytes to read inline; defaults to the configured inline artifact limit"`
}
type ReadArtifactResponse struct {
	Artifact model.ArtifactContent `json:"artifact" jsonschema:"Inline artifact content and truncation state"`
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

type ListIssuesRequest struct {
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build      int    `json:"build" jsonschema:"Jenkins build number"`
	Tool       string `json:"tool,omitempty" jsonschema:"Optional Warnings NG tool id or URL segment to list issues for; when omitted, the response returns discovered tools and lists issues only if exactly one tool is available"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum number of issues to return; defaults to 50 and is capped at 200"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"Opaque continuation cursor returned by a previous jenkins_list_issues response"`
}

type ListIssuesResponse struct {
	Page       model.IssuesPage `json:"page" jsonschema:"Warnings NG issue discovery data and issue items for this page"`
	NextCursor string           `json:"nextCursor,omitempty" jsonschema:"Opaque cursor to request the next page when hasMore is true"`
	HasMore    bool             `json:"hasMore" jsonschema:"Whether additional issues are available after this page"`
	Truncated  bool             `json:"truncated" jsonschema:"Whether additional issues were omitted from this page due to the requested or configured limit"`
	Limit      int              `json:"limit" jsonschema:"Maximum number of issues requested for this page after applying server caps"`
}

func ListIssues(ctx context.Context, deps Deps, in ListIssuesRequest) (ListIssuesResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return ListIssuesResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return ListIssuesResponse{}, err
	}
	limit := pagination.BoundLimit(in.Limit, 50, 200)
	cursorSignature, err := listIssuesCursorSignature(in)
	if err != nil {
		return ListIssuesResponse{}, err
	}
	offset, gotSignature, err := pagination.DecodeCursor(in.Cursor, listIssuesCursorKind)
	if err != nil {
		return ListIssuesResponse{}, apperrors.Wrap(apperrors.CodeInvalidRequest, "invalid list issues cursor", map[string]any{"cursor": in.Cursor, "reason": err.Error()})
	}
	if gotSignature != "" && gotSignature != cursorSignature {
		return ListIssuesResponse{}, apperrors.Wrap(apperrors.CodeInvalidRequest, "list issues cursor does not match request", map[string]any{"cursor": in.Cursor})
	}
	page, err := api.ListIssues(ctx, in.Job, in.Build, in.Tool, offset, limit+1)
	if err != nil {
		return ListIssuesResponse{}, err
	}
	return listIssuesPage(page, offset, limit, cursorSignature)
}

const listIssuesCursorKind = "jenkins_list_issues"

func listIssuesPage(page model.IssuesPage, offset int, limit int, signature string) (ListIssuesResponse, error) {
	hasMore := len(page.Items) > limit
	if hasMore {
		page.Items = page.Items[:limit]
	}
	nextCursor := ""
	if hasMore {
		var err error
		nextCursor, err = pagination.EncodeCursor(listIssuesCursorKind, offset+limit, signature)
		if err != nil {
			return ListIssuesResponse{}, err
		}
	}
	return ListIssuesResponse{Page: page, NextCursor: nextCursor, HasMore: hasMore, Truncated: hasMore, Limit: limit}, nil
}

func listIssuesCursorSignature(in ListIssuesRequest) (string, error) {
	body, err := json.Marshal(struct {
		Controller string `json:"controller,omitempty"`
		Job        string `json:"job"`
		Build      int    `json:"build"`
		Tool       string `json:"tool,omitempty"`
	}{
		Controller: in.Controller,
		Job:        in.Job,
		Build:      in.Build,
		Tool:       strings.TrimSpace(in.Tool),
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

type ChangesResponse struct {
	ChangeSets []model.ChangeSet `json:"changeSets" jsonschema:"SCM change sets associated with the Jenkins build"`
	Truncated  bool              `json:"truncated" jsonschema:"Whether additional change data was omitted"`
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
	Controller    string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job           string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build         int    `json:"build" jsonschema:"Jenkins build number"`
	LastState     string `json:"lastState,omitempty" jsonschema:"Opaque watch state token returned by a previous jenkins_watch_build call"`
	WaitTimeoutMs int64  `json:"waitTimeoutMs,omitempty" jsonschema:"Maximum milliseconds to wait for build completion, Pipeline stage-status changes, or pending input-step changes; MCP hosts may cancel the tool call sooner"`
}
type WatchBuildResponse struct {
	Watch model.BuildWatch `json:"watch" jsonschema:"Current build watch state, progress, and completion status"`
}

type watchState struct {
	Version int                      `json:"v"`
	Target  watchTargetState         `json:"target"`
	Summary model.BuildSummary       `json:"summary"`
	Build   watchBuildState          `json:"build"`
	Run     watchRunState            `json:"run"`
	Inputs  []watchPendingInputState `json:"inputs,omitempty"`
	Stages  []watchStageState        `json:"stages,omitempty"`
}

type watchTargetState struct {
	Controller string `json:"controller"`
	Job        string `json:"job"`
	Build      int    `json:"build"`
}

type watchBuildState struct {
	Building bool              `json:"building"`
	Result   model.BuildResult `json:"result,omitempty"`
}

type watchRunState struct {
	Status          model.PipelineStatus `json:"status,omitempty"`
	WaitingForInput bool                 `json:"waitingForInput,omitempty"`
}

type watchPendingInputState struct {
	ID         string `json:"id,omitempty"`
	Message    string `json:"message,omitempty"`
	ProceedURL string `json:"proceedUrl,omitempty"`
	AbortURL   string `json:"abortUrl,omitempty"`
}

type watchStageState struct {
	ID     string               `json:"id,omitempty"`
	Name   string               `json:"name,omitempty"`
	Status model.PipelineStatus `json:"status,omitempty"`
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
		if ctxErr := ctx.Err(); ctxErr != nil {
			return WatchBuildResponse{}, watchContextError(ctxErr)
		}
		if fatal && err != nil {
			return WatchBuildResponse{}, err
		}
		if err == nil {
			consecutiveFailures = 0
			if pipelineDegraded && previous != nil {
				current.Run = previous.Run
				current.Inputs = previous.Inputs
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
		Status:          pipeline.Status,
		WaitingForInput: pipeline.WaitingForInput,
	}
	inputs := make([]watchPendingInputState, 0, len(pipeline.PendingInputActions))
	for _, input := range pipeline.PendingInputActions {
		inputs = append(inputs, watchPendingInputState{
			ID:         input.ID,
			Message:    input.Message,
			ProceedURL: input.ProceedURL,
			AbortURL:   input.AbortURL,
		})
	}
	state.Inputs = inputs
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
	if len(a.Inputs) != len(b.Inputs) {
		return false
	}
	for i := range a.Inputs {
		if a.Inputs[i] != b.Inputs[i] {
			return false
		}
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
	if state.Run.Status == "" && len(state.Stages) == 0 && len(state.Inputs) == 0 {
		return nil
	}
	run := &model.PipelineRun{
		Status:          state.Run.Status,
		WaitingForInput: state.Run.WaitingForInput || state.Run.Status == model.PipelineStatusPausedPendingInput || len(state.Inputs) > 0,
	}
	for _, stage := range state.Stages {
		if stage.Status == model.PipelineStatusPausedPendingInput {
			run.WaitingForInput = true
			break
		}
	}
	if len(state.Inputs) > 0 {
		run.PendingInputActions = make([]model.PendingInputAction, 0, len(state.Inputs))
		for _, input := range state.Inputs {
			run.PendingInputActions = append(run.PendingInputActions, model.PendingInputAction{
				ID:         input.ID,
				Message:    input.Message,
				ProceedURL: input.ProceedURL,
				AbortURL:   input.AbortURL,
			})
		}
	}
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
			return watchContextError(ctx.Err())
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return watchContextError(ctx.Err())
	case <-timer.C:
		return nil
	}
}

func watchContextError(err error) error {
	if appErr, ok := apperrors.FromContext(err); ok {
		return appErr
	}
	return err
}

type TriggerBuildRequest struct {
	Controller string            `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job        string            `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Parameters map[string]string `json:"parameters,omitempty" jsonschema:"Build parameters keyed by Jenkins parameter name"`
}
type TriggerBuildResponse struct {
	QueueURL  string `json:"queueUrl,omitempty" jsonschema:"Jenkins queue item URL returned after triggering the build"`
	Triggered bool   `json:"triggered" jsonschema:"Whether Jenkins accepted the build trigger request"`
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
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	ID         int64  `json:"id" jsonschema:"Jenkins queue item id"`
}
type QueueItemResponse struct {
	Item model.QueueItem `json:"item" jsonschema:"Jenkins queue item detail"`
}

type WatchQueueItemRequest struct {
	Controller    string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	ID            int64  `json:"id" jsonschema:"Jenkins queue item id to watch"`
	LastState     string `json:"lastState,omitempty" jsonschema:"Opaque queue watch state token returned by a previous jenkins_watch_queue_item call"`
	WaitTimeoutMs int64  `json:"waitTimeoutMs,omitempty" jsonschema:"Maximum milliseconds to wait for queue assignment, cancellation, disappearance, or another queue state change; MCP hosts may cancel the tool call sooner"`
}
type WatchQueueItemResponse struct {
	Watch model.QueueWatch `json:"watch" jsonschema:"Current queue watch state, terminal status, and resolved build reference when available"`
}

type queueWatchState struct {
	Version int                   `json:"v"`
	Target  queueWatchTargetState `json:"target"`
	Status  string                `json:"status"`
	Item    *model.QueueItem      `json:"item,omitempty"`
	Build   *model.BuildReference `json:"build,omitempty"`
}

type queueWatchTargetState struct {
	Controller string `json:"controller"`
	ID         int64  `json:"id"`
}

type ListQueueResponse struct {
	Items []model.QueueItem `json:"items" jsonschema:"Current Jenkins queue items"`
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

func WatchQueueItem(ctx context.Context, deps Deps, in WatchQueueItemRequest) (WatchQueueItemResponse, error) {
	if in.ID <= 0 {
		return WatchQueueItemResponse{}, apperrors.New(apperrors.CodeInvalidRequest, "queue item id must be positive")
	}
	controllerID := in.Controller
	if controllerID == "" {
		controllerID = deps.Config.DefaultController
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return WatchQueueItemResponse{}, err
	}
	previous, err := decodeQueueWatchState(in.LastState)
	if err != nil {
		return WatchQueueItemResponse{}, err
	}
	if previous != nil && (previous.Target.Controller != controllerID || previous.Target.ID != in.ID) {
		return WatchQueueItemResponse{}, apperrors.Wrap(apperrors.CodeInvalidRequest, "watch state does not match requested queue item", map[string]any{
			"controller": controllerID,
			"id":         in.ID,
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
		current, err := fetchQueueWatchState(ctx, deps.Config, api, controllerID, in.ID)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return WatchQueueItemResponse{}, watchContextError(ctxErr)
		}
		if err == nil {
			consecutiveFailures = 0
			changed := previous == nil || !queueWatchStatesEqual(*previous, current)
			terminal := queueWatchStateTerminal(current)
			if changed || terminal || !time.Now().Before(deadline) {
				stateToken, err := encodeQueueWatchState(current)
				if err != nil {
					return WatchQueueItemResponse{}, err
				}
				return WatchQueueItemResponse{Watch: queueWatchResponse(current, stateToken, !changed && !terminal)}, nil
			}
		} else {
			if previous == nil && !time.Now().Before(deadline) {
				return WatchQueueItemResponse{}, apperrors.Wrap(apperrors.CodeUnavailable, "jenkins queue watch bootstrap timed out", map[string]any{
					"waitTimeoutMs": waitTimeout,
					"lastError":     err.Error(),
				})
			}
			if previous != nil && !time.Now().Before(deadline) {
				return WatchQueueItemResponse{Watch: queueWatchResponse(*previous, in.LastState, true)}, nil
			}
			consecutiveFailures++
			if consecutiveFailures >= deps.Config.Watch.MaxConsecutiveFailures {
				return WatchQueueItemResponse{}, apperrors.Wrap(apperrors.CodeUnavailable, "jenkins queue watch polling failed repeatedly", map[string]any{
					"consecutiveFailures": consecutiveFailures,
					"lastError":           err.Error(),
				})
			}
		}

		if err := sleepWithContext(ctx, watchSleepDuration(time.Until(deadline), deps.Config.Watch.PollIntervalMs)); err != nil {
			return WatchQueueItemResponse{}, err
		}
	}
}

func fetchQueueWatchState(ctx context.Context, cfg config.Config, api *jenkinsapi.API, controllerID string, id int64) (queueWatchState, error) {
	item, err := api.QueueItem(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return queueWatchState{
				Version: 1,
				Target:  queueWatchTargetState{Controller: controllerID, ID: id},
				Status:  "disappeared",
			}, nil
		}
		return queueWatchState{}, err
	}
	state := queueWatchState{
		Version: 1,
		Target:  queueWatchTargetState{Controller: controllerID, ID: id},
		Status:  "queued",
		Item:    &item,
	}
	if item.Cancelled {
		state.Status = "cancelled"
	}
	if item.Executable != nil {
		state.Status = "executable"
		if item.Executable.URL != "" {
			if ref, err := resolveBuildURL(cfg, item.Executable.URL); err == nil {
				state.Build = &ref
			}
		}
	}
	return state, nil
}

func encodeQueueWatchState(state queueWatchState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", apperrors.Wrap(apperrors.CodeJenkins, "failed to encode queue watch state", err.Error())
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
		return "", apperrors.Wrap(apperrors.CodeJenkins, "queue watch state too large to encode", map[string]any{"maxBytes": maxWatchStateTokenBytes})
	}
	return token, nil
}

func decodeQueueWatchState(raw string) (*queueWatchState, error) {
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
	var state queueWatchState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, "invalid watch state")
	}
	if state.Version != 1 {
		return nil, apperrors.Wrap(apperrors.CodeInvalidRequest, "unsupported watch state version", state.Version)
	}
	return &state, nil
}

func queueWatchStatesEqual(a, b queueWatchState) bool {
	return a.Target == b.Target &&
		a.Status == b.Status &&
		queueItemsEqual(a.Item, b.Item) &&
		buildReferencesEqual(a.Build, b.Build)
}

func queueItemsEqual(a, b *model.QueueItem) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.ID != b.ID || a.URL != b.URL || a.Cancelled != b.Cancelled || a.TaskName != b.TaskName || a.TaskURL != b.TaskURL {
		return false
	}
	if a.Executable == nil || b.Executable == nil {
		return a.Executable == b.Executable
	}
	return stableWatchSummary(*a.Executable) == stableWatchSummary(*b.Executable)
}

func buildReferencesEqual(a, b *model.BuildReference) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func queueWatchStateTerminal(state queueWatchState) bool {
	return state.Status == "executable" || state.Status == "cancelled" || state.Status == "disappeared"
}

func queueWatchResponse(state queueWatchState, token string, timedOut bool) model.QueueWatch {
	watch := model.QueueWatch{
		State:       token,
		Status:      state.Status,
		Item:        state.Item,
		Build:       state.Build,
		TimedOut:    timedOut,
		Terminal:    queueWatchStateTerminal(state),
		Cancelled:   state.Status == "cancelled",
		Disappeared: state.Status == "disappeared",
	}
	return watch
}

func isNotFound(err error) bool {
	appErr, ok := err.(*apperrors.Error)
	return ok && appErr.Code == apperrors.CodeNotFound
}

type CancelQueueItemResponse struct {
	Cancelled bool `json:"cancelled" jsonschema:"Whether Jenkins accepted the queue item cancellation request"`
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
	Cancelled bool `json:"cancelled" jsonschema:"Whether Jenkins accepted the build cancellation request"`
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
	if deps.Audit == nil {
		return
	}
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
