package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/jenkins/urlx"
	"github.com/david/jenkins-mcp/internal/security"
)

type API struct {
	id     string
	client *jenkinsclient.Client
}

func New(id string, c *jenkinsclient.Client) *API { return &API{id: id, client: c} }
func (a *API) BaseURL() string                    { return a.client.BaseURL() }

type controllerJSON struct {
	NodeName    string `json:"nodeName"`
	UseSecurity bool   `json:"useSecurity"`
	version     string
}

func (c *controllerJSON) SetVersion(v string) { c.version = v }

func (a *API) ControllerInfo(ctx context.Context) (model.ControllerInfo, error) {
	var out controllerJSON
	q := url.Values{"tree": {"nodeName,useSecurity"}}
	err := a.client.GetJSON(ctx, "api/json", q, &out)
	return model.ControllerInfo{ID: a.id, URL: a.BaseURL(), Version: out.version, UseSecurity: out.UseSecurity, NodeName: out.NodeName, Available: err == nil}, err
}

func (a *API) InstalledPlugins(ctx context.Context) ([]model.PluginInfo, error) {
	var raw struct {
		Plugins []model.PluginInfo `json:"plugins"`
	}
	err := a.client.GetJSON(ctx, "pluginManager/api/json", url.Values{"tree": {"plugins[shortName,version,active,enabled]"}}, &raw)
	return raw.Plugins, err
}

func (a *API) Capabilities(ctx context.Context, pluginDiscoveryEnabled bool) model.ControllerCapabilities {
	info, err := a.ControllerInfo(ctx)
	if err != nil {
		return model.ControllerCapabilities{
			Controller: model.ControllerInfo{ID: a.id, URL: a.BaseURL(), Available: false, Error: err.Error()},
			Features:   defaultFeatureMap(nil),
			Error:      err.Error(),
		}
	}
	if !pluginDiscoveryEnabled {
		return model.ControllerCapabilities{
			Controller: info,
			Features:   defaultFeatureMap(nil),
			Warnings: []model.CapabilityWarning{{
				Code:     "optional_plugin_discovery_disabled",
				Source:   "plugins",
				Optional: true,
				Message:  "Optional Jenkins plugin discovery was skipped because capabilities.pluginDiscoveryEnabled is false.",
			}},
		}
	}
	plugins, pluginErr := a.InstalledPlugins(ctx)
	caps := model.ControllerCapabilities{
		Controller: info,
		Features:   defaultFeatureMap(plugins),
		Plugins:    plugins,
	}
	if pluginErr != nil {
		caps.Error = pluginErr.Error()
		caps.Warnings = append(caps.Warnings, model.CapabilityWarning{
			Code:     "optional_plugin_discovery_failed",
			Source:   "plugins",
			Optional: true,
			Message:  "Optional Jenkins plugin discovery failed; the controller is available but plugin-derived feature detection used limited defaults.",
			Error:    pluginErr.Error(),
		})
	}
	return caps
}

func defaultFeatureMap(plugins []model.PluginInfo) map[string]bool {
	active := map[string]bool{}
	for _, plugin := range plugins {
		active[plugin.ShortName] = plugin.Active && plugin.Enabled
	}
	return map[string]bool{
		"jobs":         true,
		"builds":       true,
		"logs":         true,
		"artifacts":    true,
		"queue":        true,
		"junit":        len(plugins) == 0 || active["junit"],
		"pipeline":     len(plugins) == 0 || active["workflow-job"] || active["pipeline-rest-api"],
		"coverage":     active["coverage"] || active["jacoco"],
		"recordIssues": active["warnings-ng"],
	}
}

type jobJSON struct {
	Name                string     `json:"name"`
	URL                 string     `json:"url"`
	Color               string     `json:"color"`
	Class               string     `json:"_class"`
	Buildable           bool       `json:"buildable"`
	Disabled            *bool      `json:"disabled"`
	LastBuild           *buildJSON `json:"lastBuild"`
	LastCompletedBuild  *buildJSON `json:"lastCompletedBuild"`
	LastSuccessfulBuild *buildJSON `json:"lastSuccessfulBuild"`
	LastFailedBuild     *buildJSON `json:"lastFailedBuild"`
}
type jobsEnvelope struct {
	Jobs []jobJSON `json:"jobs"`
}

func (a *API) ListJobs(ctx context.Context, folder string) ([]model.Job, error) {
	path := "api/json"
	prefix := ""
	if folder != "" {
		prefix = strings.Trim(folder, "/")
		path = urlx.JobPath(folder) + "/api/json"
	}
	q := url.Values{"tree": {"jobs[name,url,color,_class,buildable,disabled,lastBuild[number,url,result,building,timestamp,duration],lastCompletedBuild[number,url,result,building,timestamp,duration],lastSuccessfulBuild[number,url,result,building,timestamp,duration],lastFailedBuild[number,url,result,building,timestamp,duration]]"}}
	var env jobsEnvelope
	if err := a.client.GetJSON(ctx, path, q, &env); err != nil {
		return nil, err
	}
	jobs := make([]model.Job, 0, len(env.Jobs))
	for _, j := range env.Jobs {
		full := j.Name
		if prefix != "" {
			full = prefix + "/" + j.Name
		}
		job := model.Job{
			Name:      j.Name,
			FullName:  full,
			URL:       j.URL,
			Color:     j.Color,
			Class:     j.Class,
			Buildable: j.Buildable,
			Disabled:  j.Disabled,
			Status:    jobStatus(j),
			Building:  j.LastBuild != nil && j.LastBuild.Building,
		}
		if j.LastBuild != nil {
			summary := summary(*j.LastBuild)
			job.LastBuild = &summary
		}
		if j.LastCompletedBuild != nil {
			summary := summary(*j.LastCompletedBuild)
			job.LastCompletedBuild = &summary
		}
		if j.LastSuccessfulBuild != nil {
			summary := summary(*j.LastSuccessfulBuild)
			job.LastSuccessfulBuild = &summary
		}
		if j.LastFailedBuild != nil {
			summary := summary(*j.LastFailedBuild)
			job.LastFailedBuild = &summary
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func jobStatus(j jobJSON) string {
	if j.Disabled != nil && *j.Disabled {
		return "disabled"
	}
	if status := jobStateFromColor(j.Color); status != "" {
		return status
	}
	if j.LastCompletedBuild != nil && j.LastCompletedBuild.Result != "" {
		return normalizeBuildResult(j.LastCompletedBuild.Result)
	}
	if j.LastBuild != nil && !j.LastBuild.Building && j.LastBuild.Result != "" {
		return normalizeBuildResult(j.LastBuild.Result)
	}
	if status := statusFromColor(j.Color); status != "" {
		return status
	}
	return "unknown"
}

func jobStateFromColor(color string) string {
	color = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(color)), "_anime")
	switch color {
	case "disabled":
		return "disabled"
	case "notbuilt", "not_built":
		return "not_built"
	default:
		return ""
	}
}

func normalizeBuildResult(result string) string {
	switch model.BuildResult(strings.ToUpper(strings.TrimSpace(result))) {
	case model.BuildResultSuccess:
		return "success"
	case model.BuildResultFailure:
		return "failed"
	case model.BuildResultUnstable:
		return "unstable"
	case model.BuildResultAborted:
		return "aborted"
	case model.BuildResultNotBuilt:
		return "not_built"
	case "":
		return ""
	default:
		return strings.ToLower(strings.TrimSpace(result))
	}
}

func statusFromColor(color string) string {
	color = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(color)), "_anime")
	switch color {
	case "blue":
		return "success"
	case "red":
		return "failed"
	case "yellow":
		return "unstable"
	case "aborted":
		return "aborted"
	case "notbuilt", "not_built":
		return "not_built"
	case "disabled":
		return "disabled"
	case "grey", "gray":
		return "unknown"
	case "":
		return ""
	default:
		return color
	}
}

func (a *API) GetJob(ctx context.Context, job string) (model.JobDetail, error) {
	path := urlx.JobPath(job) + "/api/json"
	tree := "name,fullName,url,color,_class,disabled,description,buildable,inQueue,nextBuildNumber,lastBuild[number,url,result,building,timestamp,duration],lastCompletedBuild[number,url,result,building,timestamp,duration],lastSuccessfulBuild[number,url,result,building,timestamp,duration],lastFailedBuild[number,url,result,building,timestamp,duration],property[parameterDefinitions[*]]"
	var raw struct {
		Name                string        `json:"name"`
		FullName            string        `json:"fullName"`
		URL                 string        `json:"url"`
		Color               string        `json:"color"`
		Class               string        `json:"_class"`
		Disabled            *bool         `json:"disabled"`
		Description         string        `json:"description"`
		Buildable           bool          `json:"buildable"`
		InQueue             bool          `json:"inQueue"`
		NextBuildNumber     int           `json:"nextBuildNumber"`
		LastBuild           *buildJSON    `json:"lastBuild"`
		LastCompletedBuild  *buildJSON    `json:"lastCompletedBuild"`
		LastSuccessfulBuild *buildJSON    `json:"lastSuccessfulBuild"`
		LastFailedBuild     *buildJSON    `json:"lastFailedBuild"`
		Properties          []jobProperty `json:"property"`
	}
	if err := a.client.GetJSON(ctx, path, url.Values{"tree": {tree}}, &raw); err != nil {
		return model.JobDetail{}, err
	}
	jobJSON := jobJSON{
		Name:               raw.Name,
		URL:                raw.URL,
		Color:              raw.Color,
		Class:              raw.Class,
		Buildable:          raw.Buildable,
		Disabled:           raw.Disabled,
		LastBuild:          raw.LastBuild,
		LastCompletedBuild: raw.LastCompletedBuild,
	}
	detail := model.JobDetail{
		Job: model.Job{
			Name:      raw.Name,
			FullName:  raw.FullName,
			URL:       raw.URL,
			Color:     raw.Color,
			Class:     raw.Class,
			Buildable: raw.Buildable,
			Disabled:  raw.Disabled,
			Status:    jobStatus(jobJSON),
			Building:  raw.LastBuild != nil && raw.LastBuild.Building,
		},
		Description:     raw.Description,
		InQueue:         raw.InQueue,
		NextBuildNumber: raw.NextBuildNumber,
		Parameters:      parseParameterDefinitions(raw.Properties),
	}
	if raw.LastBuild != nil {
		summary := summary(*raw.LastBuild)
		detail.LastBuild = &summary
	}
	if raw.LastSuccessfulBuild != nil {
		summary := summary(*raw.LastSuccessfulBuild)
		detail.LastSuccessful = &summary
	}
	if raw.LastFailedBuild != nil {
		summary := summary(*raw.LastFailedBuild)
		detail.LastFailed = &summary
	}
	return detail, nil
}

type jobProperty struct {
	ParameterDefinitions []parameterDefinitionJSON `json:"parameterDefinitions"`
}

type parameterDefinitionJSON struct {
	Class            string   `json:"_class"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	DefaultParameter rawValue `json:"defaultParameterValue"`
	DefaultValue     any      `json:"defaultValue"`
	Choices          []string `json:"choices"`
	IsRequired       bool     `json:"isRequired"`
	Trim             bool     `json:"trim"`
}

type rawValue struct {
	Value any `json:"value"`
}

func parseParameterDefinitions(properties []jobProperty) []model.ParameterDefinition {
	var out []model.ParameterDefinition
	for _, property := range properties {
		for _, param := range property.ParameterDefinitions {
			if param.Name == "" {
				continue
			}
			defaultValue := param.DefaultValue
			if defaultValue == nil {
				defaultValue = param.DefaultParameter.Value
			}
			out = append(out, model.ParameterDefinition{
				Name:        param.Name,
				Type:        param.Class,
				Description: param.Description,
				Default:     defaultValue,
				Choices:     param.Choices,
				Required:    param.IsRequired,
			})
		}
	}
	return out
}

type buildJSON struct {
	ID                string            `json:"id"`
	Number            int               `json:"number"`
	URL               string            `json:"url"`
	Result            string            `json:"result"`
	Building          bool              `json:"building"`
	Timestamp         int64             `json:"timestamp"`
	Duration          int64             `json:"duration"`
	Description       string            `json:"description"`
	DisplayName       string            `json:"displayName"`
	FullDisplayName   string            `json:"fullDisplayName"`
	QueueID           int64             `json:"queueId"`
	EstimatedDuration int64             `json:"estimatedDuration"`
	KeepLog           *bool             `json:"keepLog"`
	Actions           []json.RawMessage `json:"actions"`
	Artifacts         []model.Artifact  `json:"artifacts"`
	ChangeSets        []changeSetJSON   `json:"changeSets"`
}
type buildsEnvelope struct {
	Builds []buildJSON `json:"builds"`
}
type changeSetJSON struct {
	Kind  string       `json:"kind"`
	Items []changeJSON `json:"items"`
}
type changeJSON struct {
	CommitID      string   `json:"commitId"`
	Author        any      `json:"author"`
	Msg           string   `json:"msg"`
	Timestamp     int64    `json:"timestamp"`
	AffectedPaths []string `json:"affectedPaths"`
}

func (a *API) ListBuilds(ctx context.Context, job string, offset int, limit int) ([]model.BuildSummary, error) {
	path := urlx.JobPath(job) + "/api/json"
	tree := fmt.Sprintf("builds[id,number,url,result,building,timestamp,duration,description,displayName,queueId,estimatedDuration,keepLog]{%d,%d}", offset, offset+limit)
	var env buildsEnvelope
	if err := a.client.GetJSON(ctx, path, url.Values{"tree": {tree}}, &env); err != nil {
		return nil, err
	}
	out := make([]model.BuildSummary, 0, len(env.Builds))
	for _, b := range env.Builds {
		out = append(out, summary(b))
	}
	return out, nil
}

func (a *API) GetBuild(ctx context.Context, job string, number int) (model.Build, error) {
	path := urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/api/json"
	tree := "id,number,url,result,building,timestamp,duration,description,displayName,fullDisplayName,queueId,estimatedDuration,keepLog,artifacts[displayPath,fileName,relativePath],actions[causes[*],parameters[*]],changeSets[kind,items[commitId,author[fullName],msg,timestamp,affectedPaths]]"
	var b buildJSON
	if err := a.client.GetJSON(ctx, path, url.Values{"tree": {tree}}, &b); err != nil {
		return model.Build{}, err
	}
	build := model.Build{BuildSummary: summary(b), Description: b.Description, DisplayName: b.DisplayName, FullDisplayName: b.FullDisplayName, Artifacts: b.Artifacts}
	build.Causes, build.Parameters = parseActions(b.Actions)
	for _, cs := range b.ChangeSets {
		out := model.ChangeSet{Kind: cs.Kind}
		for _, it := range cs.Items {
			out.Items = append(out.Items, model.Change{CommitID: it.CommitID, Author: authorName(it.Author), Message: it.Msg, Timestamp: it.Timestamp, AffectedPaths: it.AffectedPaths})
		}
		build.ChangeSets = append(build.ChangeSets, out)
	}
	warningsSummary := a.WarningsNGSummary(ctx, job, number)
	if warningsSummary.Available {
		build.WarningsNGSummary = &warningsSummary
	}
	return build, nil
}

func (a *API) GetLog(ctx context.Context, job string, number int, start, max int64) (model.LogChunk, error) {
	path := urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/logText/progressiveText"
	status, body, headers, err := a.client.GetText(ctx, path, url.Values{"start": {strconv.FormatInt(start, 10)}})
	if err != nil {
		return model.LogChunk{}, err
	}
	if status < 200 || status > 299 {
		return model.LogChunk{}, fmt.Errorf("jenkins returned HTTP %d", status)
	}
	text := string(body)
	truncated := false
	if max > 0 && int64(len(text)) > max {
		text = text[:max]
		truncated = true
	}
	next, _ := strconv.ParseInt(headers.Get("X-Text-Size"), 10, 64)
	more := strings.EqualFold(headers.Get("X-More-Data"), "true")
	return model.LogChunk{Text: text, Start: start, NextStart: next, More: more, Truncated: truncated}, nil
}

func (a *API) SearchLog(ctx context.Context, job string, number int, start int64, query string, maxBytes int64, maxMatches int, contextLines int) (model.LogSearchResult, error) {
	if query == "" {
		return model.LogSearchResult{}, fmt.Errorf("query is required")
	}
	chunk, err := a.GetLog(ctx, job, number, start, maxBytes)
	if err != nil {
		return model.LogSearchResult{}, err
	}
	lines := strings.Split(chunk.Text, "\n")
	matches := make([]model.LogMatch, 0)
	for i, line := range lines {
		if !strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
			continue
		}
		match := model.LogMatch{Line: i + 1, Text: line}
		if contextLines > 0 {
			from := max(0, i-contextLines)
			to := min(len(lines), i+contextLines+1)
			match.Context = strings.Join(lines[from:to], "\n")
		}
		matches = append(matches, match)
		if maxMatches > 0 && len(matches) >= maxMatches {
			break
		}
	}
	return model.LogSearchResult{
		Query:        query,
		Matches:      matches,
		ScannedBytes: int64(len(chunk.Text)),
		NextStart:    chunk.NextStart,
		More:         chunk.More,
		Truncated:    chunk.Truncated || (maxMatches > 0 && len(matches) >= maxMatches),
	}, nil
}

func (a *API) TailLog(ctx context.Context, job string, number int, tailBytes int64) (model.LogChunk, error) {
	first, err := a.GetLog(ctx, job, number, 0, 1)
	if err != nil {
		return model.LogChunk{}, err
	}
	start := first.NextStart - tailBytes
	if start < 0 {
		start = 0
	}
	return a.GetLog(ctx, job, number, start, tailBytes)
}

func (a *API) TestReport(ctx context.Context, job string, number int, failedOnly bool, limit int) (model.TestReport, error) {
	path := urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/testReport/api/json"
	var raw struct {
		TotalCount int `json:"totalCount"`
		FailCount  int `json:"failCount"`
		SkipCount  int `json:"skipCount"`
		PassCount  int `json:"passCount"`
		Suites     []struct {
			Name  string           `json:"name"`
			Cases []model.TestCase `json:"cases"`
		} `json:"suites"`
	}
	if err := a.client.GetJSON(ctx, path, nil, &raw); err != nil {
		return model.TestReport{}, err
	}
	totalCount := raw.TotalCount
	if totalCount == 0 {
		totalCount = raw.PassCount + raw.FailCount + raw.SkipCount
	}
	report := model.TestReport{TotalCount: totalCount, FailCount: raw.FailCount, SkipCount: raw.SkipCount, PassCount: raw.PassCount}
	count := 0
	for _, s := range raw.Suites {
		suite := model.TestSuite{Name: s.Name}
		for _, c := range s.Cases {
			if failedOnly && c.Status != "FAILED" && c.Status != "REGRESSION" {
				continue
			}
			if limit > 0 && count >= limit {
				report.Truncated = true
				break
			}
			suite.Cases = append(suite.Cases, c)
			count++
		}
		if len(suite.Cases) > 0 || !failedOnly {
			report.Suites = append(report.Suites, suite)
		}
		if report.Truncated {
			break
		}
	}
	return report, nil
}

func (a *API) TestReportSummary(ctx context.Context, job string, number int) (model.TestReport, error) {
	path := urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/testReport/api/json"
	var raw struct {
		TotalCount int `json:"totalCount"`
		FailCount  int `json:"failCount"`
		SkipCount  int `json:"skipCount"`
		PassCount  int `json:"passCount"`
	}
	if err := a.client.GetJSON(ctx, path, url.Values{"tree": {"totalCount,failCount,skipCount,passCount"}}, &raw); err != nil {
		return model.TestReport{}, err
	}
	totalCount := raw.TotalCount
	if totalCount == 0 {
		totalCount = raw.PassCount + raw.FailCount + raw.SkipCount
	}
	return model.TestReport{TotalCount: totalCount, FailCount: raw.FailCount, SkipCount: raw.SkipCount, PassCount: raw.PassCount}, nil
}

func (a *API) PipelineRun(ctx context.Context, job string, number int) (model.PipelineRun, error) {
	path := urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/wfapi/describe"
	var raw struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Status         string `json:"status"`
		StartTime      int64  `json:"startTimeMillis"`
		EndTime        int64  `json:"endTimeMillis"`
		DurationMillis int64  `json:"durationMillis"`
		Stages         []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Status         string `json:"status"`
			StartTime      int64  `json:"startTimeMillis"`
			DurationMillis int64  `json:"durationMillis"`
			PauseMillis    int64  `json:"pauseDurationMillis"`
		} `json:"stages"`
	}
	if err := a.client.GetJSON(ctx, path, nil, &raw); err != nil {
		return model.PipelineRun{}, err
	}
	run := model.PipelineRun{
		ID:         raw.ID,
		Name:       raw.Name,
		Status:     model.PipelineStatus(raw.Status),
		StartTime:  raw.StartTime,
		EndTime:    raw.EndTime,
		DurationMS: raw.DurationMillis,
	}
	for _, stage := range raw.Stages {
		run.Stages = append(run.Stages, model.PipelineStage{
			ID:         stage.ID,
			Name:       stage.Name,
			Status:     model.PipelineStatus(stage.Status),
			StartTime:  stage.StartTime,
			DurationMS: stage.DurationMillis,
			PauseMS:    stage.PauseMillis,
		})
	}
	pending, err := a.PipelinePendingInputActions(ctx, job, number)
	if err != nil {
		if !isMissingPendingInputEndpoint(err) {
			run.PendingInputError = err.Error()
		}
	} else {
		run.PendingInputActions = pending
	}
	run.WaitingForInput = pipelineWaitingForInput(run.Status, run.Stages, run.PendingInputActions)
	return run, nil
}

func (a *API) PipelinePendingInputActions(ctx context.Context, job string, number int) ([]model.PendingInputAction, error) {
	path := urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/wfapi/pendingInputActions"
	var raw []struct {
		ID         string `json:"id"`
		Message    string `json:"message"`
		ProceedURL string `json:"proceedUrl"`
		AbortURL   string `json:"abortUrl"`
	}
	if err := a.client.GetJSON(ctx, path, nil, &raw); err != nil {
		return nil, err
	}
	actions := make([]model.PendingInputAction, 0, len(raw))
	for _, action := range raw {
		actions = append(actions, model.PendingInputAction{
			ID:         action.ID,
			Message:    action.Message,
			ProceedURL: action.ProceedURL,
			AbortURL:   action.AbortURL,
		})
	}
	return actions, nil
}

func pipelineWaitingForInput(status model.PipelineStatus, stages []model.PipelineStage, pending []model.PendingInputAction) bool {
	if status == model.PipelineStatusPausedPendingInput || len(pending) > 0 {
		return true
	}
	for _, stage := range stages {
		if stage.Status == model.PipelineStatusPausedPendingInput {
			return true
		}
	}
	return false
}

func isMissingPendingInputEndpoint(err error) bool {
	appErr, ok := err.(*apperrors.Error)
	if !ok {
		return false
	}
	return appErr.Code == apperrors.CodeNotFound
}

func (a *API) PipelineStage(ctx context.Context, job string, number int, stageID string) (model.PipelineStageDetail, error) {
	if stageID == "" {
		return model.PipelineStageDetail{}, fmt.Errorf("stage id is required")
	}
	path := urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/execution/node/" + url.PathEscape(stageID) + "/wfapi/describe"
	var raw struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Status         string `json:"status"`
		StartTime      int64  `json:"startTimeMillis"`
		DurationMillis int64  `json:"durationMillis"`
		PauseMillis    int64  `json:"pauseDurationMillis"`
		StageFlowNodes []struct {
			ID                   string   `json:"id"`
			Name                 string   `json:"name"`
			Status               string   `json:"status"`
			ParameterDescription string   `json:"parameterDescription"`
			StartTime            int64    `json:"startTimeMillis"`
			DurationMillis       int64    `json:"durationMillis"`
			PauseMillis          int64    `json:"pauseDurationMillis"`
			ParentNodes          []string `json:"parentNodes"`
			Links                struct {
				Log *struct {
					Href string `json:"href"`
				} `json:"log"`
			} `json:"_links"`
		} `json:"stageFlowNodes"`
	}
	if err := a.client.GetJSON(ctx, path, nil, &raw); err != nil {
		return model.PipelineStageDetail{}, err
	}
	detail := model.PipelineStageDetail{
		PipelineStage: model.PipelineStage{
			ID:         raw.ID,
			Name:       raw.Name,
			Status:     model.PipelineStatus(raw.Status),
			StartTime:  raw.StartTime,
			DurationMS: raw.DurationMillis,
			PauseMS:    raw.PauseMillis,
		},
	}
	for _, node := range raw.StageFlowNodes {
		detail.Nodes = append(detail.Nodes, model.PipelineNode{
			ID:                   node.ID,
			Name:                 node.Name,
			Status:               model.PipelineStatus(node.Status),
			ParameterDescription: node.ParameterDescription,
			StartTime:            node.StartTime,
			DurationMS:           node.DurationMillis,
			PauseMS:              node.PauseMillis,
			ParentNodes:          node.ParentNodes,
			HasLog:               node.Links.Log != nil,
		})
	}
	return detail, nil
}

func (a *API) PipelineNodeLog(ctx context.Context, job string, number int, nodeID string, maxBytes int64) (model.PipelineNodeLog, error) {
	if nodeID == "" {
		return model.PipelineNodeLog{}, fmt.Errorf("node id is required")
	}
	path := urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/execution/node/" + url.PathEscape(nodeID) + "/wfapi/log"
	var raw struct {
		NodeID     string `json:"nodeId"`
		NodeStatus string `json:"nodeStatus"`
		Text       string `json:"text"`
		Length     int64  `json:"length"`
		HasMore    bool   `json:"hasMore"`
	}
	if err := a.client.GetJSON(ctx, path, nil, &raw); err != nil {
		return model.PipelineNodeLog{}, err
	}
	truncated := false
	if maxBytes > 0 && int64(len(raw.Text)) > maxBytes {
		raw.Text = raw.Text[:maxBytes]
		truncated = true
	}
	return model.PipelineNodeLog{
		NodeID:     raw.NodeID,
		NodeStatus: model.PipelineStatus(raw.NodeStatus),
		Text:       raw.Text,
		Length:     raw.Length,
		HasMore:    raw.HasMore,
		Truncated:  truncated,
	}, nil
}

func (a *API) DownloadArtifact(ctx context.Context, job string, number int, rel string) ([]byte, error) {
	cleanRel, err := security.CleanRelativePath(rel)
	if err != nil {
		return nil, err
	}
	path := urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/artifact/" + urlx.RelativePath(cleanRel)
	status, body, _, err := a.client.GetText(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("jenkins returned HTTP %d", status)
	}
	return body, nil
}

func (a *API) ReadArtifact(ctx context.Context, job string, number int, rel string, maxBytes int64) (model.ArtifactContent, error) {
	body, err := a.DownloadArtifact(ctx, job, number, rel)
	if err != nil {
		return model.ArtifactContent{}, err
	}
	truncated := false
	if maxBytes > 0 && int64(len(body)) > maxBytes {
		body = body[:maxBytes]
		truncated = true
	}
	if !utf8.Valid(body) {
		return model.ArtifactContent{RelativePath: rel, Bytes: len(body), Inline: false, Truncated: truncated}, nil
	}
	return model.ArtifactContent{RelativePath: rel, Text: string(body), Bytes: len(body), Inline: true, Truncated: truncated}, nil
}

func (a *API) CoverageReport(ctx context.Context, job string, number int) (model.CoverageReport, error) {
	candidates := coverageCandidates(job, number)
	report := model.CoverageReport{CheckedEndpoints: candidates}
	for _, candidate := range candidates {
		var raw map[string]any
		if err := a.client.GetJSON(ctx, candidate, coverageProbeQuery(), &raw); err != nil {
			if shouldAbortCoverageProbe(ctx, err) {
				return model.CoverageReport{}, err
			}
			if !isNotFoundError(err) {
				report.Errors = append(report.Errors, coverageEndpointError(candidate, err))
			}
			continue
		}
		summary := normalizeCoverageSummary(candidate, raw)
		if coverageSummaryUseful(summary) {
			report.Summaries = append(report.Summaries, summary)
		}
	}
	report.Available = len(report.Summaries) > 0
	return report, nil
}

func coverageCandidates(job string, number int) []string {
	buildPath := urlx.JobPath(job) + "/" + strconv.Itoa(number)
	return []string{
		buildPath + "/coverage/api/json",
		buildPath + "/coverage/result/api/json",
		buildPath + "/jacoco/api/json",
	}
}

const coverageProbeTree = "lineCoverage[name,metric,type,covered,coveredCount,missed,missedCount,total,totalCount,percentage,percent,ratio,delta,change,status,state,qualityGateStatus]," +
	"branchCoverage[name,metric,type,covered,coveredCount,missed,missedCount,total,totalCount,percentage,percent,ratio,delta,change,status,state,qualityGateStatus]," +
	"instructionCoverage[name,metric,type,covered,coveredCount,missed,missedCount,total,totalCount,percentage,percent,ratio,delta,change,status,state,qualityGateStatus]," +
	"classCoverage[name,metric,type,covered,coveredCount,missed,missedCount,total,totalCount,percentage,percent,ratio,delta,change,status,state,qualityGateStatus]," +
	"methodCoverage[name,metric,type,covered,coveredCount,missed,missedCount,total,totalCount,percentage,percent,ratio,delta,change,status,state,qualityGateStatus]," +
	"complexityCoverage[name,metric,type,covered,coveredCount,missed,missedCount,total,totalCount,percentage,percent,ratio,delta,change,status,state,qualityGateStatus]," +
	"packageCoverage[name,metric,type,covered,coveredCount,missed,missedCount,total,totalCount,percentage,percent,ratio,delta,change,status,state,qualityGateStatus]," +
	"fileCoverage[name,metric,type,covered,coveredCount,missed,missedCount,total,totalCount,percentage,percent,ratio,delta,change,status,state,qualityGateStatus]," +
	"projectStatistics[*],projectDelta[*],modifiedFilesStatistics[*],modifiedLinesStatistics[*],modifiedFilesDelta[*],modifiedLinesDelta[*]," +
	"qualityGates[overallResult,resultItems[qualityGate,result,threshold,value]],referenceBuild," +
	"healthReport[description,score]," +
	"name,metric,type,covered,coveredCount,missed,missedCount,total,totalCount,percentage,percent,ratio,delta,change,status,state,qualityGateStatus"

func coverageProbeQuery() url.Values {
	return url.Values{"tree": {coverageProbeTree}}
}

func coverageEndpointError(endpoint string, err error) model.CoverageEndpointError {
	if appErr, ok := err.(*apperrors.Error); ok {
		return model.CoverageEndpointError{Endpoint: endpoint, Code: string(appErr.Code), Message: appErr.Message}
	}
	return model.CoverageEndpointError{Endpoint: endpoint, Code: string(apperrors.CodeJenkins), Message: err.Error()}
}

func normalizeCoverageSummary(endpoint string, raw map[string]any) model.CoverageSummary {
	source := coverageSource(endpoint)
	summary := model.CoverageSummary{
		Source:         source,
		Endpoint:       endpoint,
		TopLevelFields: sortedKeys(raw),
		HealthReports:  extractCoverageHealthReports(raw),
		Details:        extractCoverageDetails(raw),
	}
	var metrics []model.CoverageMetric
	collectCoverageMetrics(source, "", raw, &metrics)
	summary.Metrics = metrics
	return summary
}

func coverageSummaryUseful(summary model.CoverageSummary) bool {
	return len(summary.Metrics) > 0 || len(summary.HealthReports) > 0 || len(summary.Details) > 0
}

func coverageSource(endpoint string) string {
	switch {
	case strings.Contains(endpoint, "/coverage/result/"):
		return "coverage-result"
	case strings.Contains(endpoint, "/coverage/"):
		return "coverage"
	case strings.Contains(endpoint, "/jacoco/"):
		return "jacoco"
	default:
		return "unknown"
	}
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func extractCoverageHealthReports(raw map[string]any) []model.CoverageHealth {
	values, ok := raw["healthReport"].([]any)
	if !ok {
		return nil
	}
	reports := make([]model.CoverageHealth, 0, len(values))
	for _, value := range values {
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}
		report := model.CoverageHealth{}
		if description, ok := entry["description"].(string); ok {
			report.Description = description
		}
		if score, ok := intFromAny(entry["score"]); ok {
			report.Score = &score
		}
		if report.Description != "" || report.Score != nil {
			reports = append(reports, report)
		}
	}
	return reports
}

func extractCoverageDetails(raw map[string]any) []model.CoverageDetail {
	keys := sortedKeys(raw)
	details := make([]model.CoverageDetail, 0)
	for _, key := range keys {
		if len(details) >= 12 {
			break
		}
		switch value := raw[key].(type) {
		case string:
			if value != "" {
				details = append(details, model.CoverageDetail{Name: key, Value: limitCoverageDetail(value)})
			}
		case bool:
			details = append(details, model.CoverageDetail{Name: key, Value: strconv.FormatBool(value)})
		case float64:
			details = append(details, model.CoverageDetail{Name: key, Value: strconv.FormatFloat(value, 'f', -1, 64)})
		}
	}
	return details
}

func limitCoverageDetail(value string) string {
	const maxDetailLen = 200
	if len(value) <= maxDetailLen {
		return value
	}
	return value[:maxDetailLen]
}

func collectCoverageMetrics(rootName, path string, value any, metrics *[]model.CoverageMetric) {
	if len(*metrics) >= 64 {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		collectCoverageStatisticsMetrics(path, typed, metrics)
		if metric, ok := coverageMetricFromMap(rootName, path, typed); ok {
			*metrics = append(*metrics, metric)
		}
		for _, key := range sortedKeys(typed) {
			nextPath := key
			if path != "" {
				nextPath = path + "." + key
			}
			collectCoverageMetrics(rootName, nextPath, typed[key], metrics)
		}
	case []any:
		for _, item := range typed {
			collectCoverageMetrics(rootName, path, item, metrics)
		}
	}
}

var coverageStatisticsMetricPrefixes = map[string]string{
	"projectStatistics":       "project",
	"projectDelta":            "projectDelta",
	"modifiedFilesStatistics": "modifiedFiles",
	"modifiedLinesStatistics": "modifiedLines",
	"modifiedFilesDelta":      "modifiedFilesDelta",
	"modifiedLinesDelta":      "modifiedLinesDelta",
}

func collectCoverageStatisticsMetrics(path string, values map[string]any, metrics *[]model.CoverageMetric) {
	prefix, ok := coverageStatisticsMetricPrefixes[path]
	if !ok {
		return
	}
	isDelta := strings.HasSuffix(path, "Delta")
	for _, key := range sortedKeys(values) {
		if len(*metrics) >= 64 {
			return
		}
		number, ok := floatFromAny(values[key])
		if !ok {
			continue
		}
		metric := model.CoverageMetric{Name: prefix + "." + key}
		if isDelta {
			metric.Delta = &number
		} else if coverageStatisticIsPercentage(key, values[key]) {
			metric.Percentage = &number
		} else {
			metric.Total = &number
		}
		*metrics = append(*metrics, metric)
	}
}

var coveragePercentageStatisticKeys = map[string]struct{}{
	"branch":       {},
	"class":        {},
	"conditional":  {},
	"file":         {},
	"instruction":  {},
	"line":         {},
	"method":       {},
	"module":       {},
	"mutation":     {},
	"package":      {},
	"statement":    {},
	"testStrength": {},
}

func coverageStatisticIsPercentage(key string, value any) bool {
	if _, ok := coveragePercentageStatisticKeys[strings.ToLower(key)]; ok {
		return true
	}
	return coverageValueIsPercentage(value)
}

func coverageMetricFromMap(rootName, path string, values map[string]any) (model.CoverageMetric, bool) {
	covered, hasCovered := floatFromFirst(values, "covered", "coveredCount")
	missed, hasMissed := floatFromFirst(values, "missed", "missedCount")
	total, hasTotal := floatFromFirst(values, "total", "totalCount")
	percentageKeys := []string{"percentage", "percent", "ratio"}
	if strings.HasPrefix(path, "qualityGates.") {
		percentageKeys = append(percentageKeys, "value")
	}
	percentage, hasPercentage := floatFromFirst(values, percentageKeys...)
	delta, hasDelta := floatFromFirst(values, "delta", "change")
	if !hasTotal && hasCovered && hasMissed {
		derivedTotal := covered + missed
		total = derivedTotal
		hasTotal = true
	}
	if !hasPercentage && hasCovered && hasTotal && total > 0 {
		derivedPercentage := covered / total * 100
		percentage = derivedPercentage
		hasPercentage = true
	}
	if !hasCovered && !hasMissed && !hasTotal && !hasPercentage && !hasDelta {
		return model.CoverageMetric{}, false
	}
	name := coverageMetricName(rootName, path, values)
	metric := model.CoverageMetric{Name: name}
	if hasCovered {
		metric.Covered = &covered
	}
	if hasMissed {
		metric.Missed = &missed
	}
	if hasTotal {
		metric.Total = &total
	}
	if hasPercentage {
		metric.Percentage = &percentage
	}
	if hasDelta {
		metric.Delta = &delta
	}
	if status, ok := stringFromFirst(values, "status", "state", "qualityGateStatus", "result"); ok {
		metric.Status = status
	}
	return metric, true
}

func coverageMetricName(rootName, path string, values map[string]any) string {
	if name, ok := stringFromFirst(values, "name", "metric", "type", "qualityGate"); ok && name != "" {
		return name
	}
	if path == "" {
		if rootName != "" {
			return rootName
		}
		return "coverage"
	}
	parts := strings.Split(path, ".")
	name := parts[len(parts)-1]
	name = strings.TrimSuffix(name, "Coverage")
	name = strings.TrimSuffix(name, "coverage")
	if name == "" {
		return path
	}
	return name
}

func floatFromFirst(values map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		if value, ok := floatFromAny(values[key]); ok {
			return value, true
		}
	}
	return 0, false
}

func stringFromFirst(values map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value, true
		}
	}
	return "", false
}

func floatFromAny(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		trimmed := strings.TrimSpace(typed)
		trimmed = strings.TrimSuffix(trimmed, "%")
		trimmed = strings.TrimPrefix(trimmed, "+")
		parsed, err := strconv.ParseFloat(trimmed, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func coverageValueIsPercentage(value any) bool {
	text, ok := value.(string)
	return ok && strings.Contains(text, "%")
}

func intFromAny(value any) (int, bool) {
	number, ok := floatFromAny(value)
	if !ok {
		return 0, false
	}
	return int(number), true
}

func isNotFoundError(err error) bool {
	appErr, ok := err.(*apperrors.Error)
	return ok && appErr.Code == apperrors.CodeNotFound
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func shouldAbortCoverageProbe(ctx context.Context, err error) bool {
	return isContextError(err) && ctx.Err() != nil
}

type warningsNGSummaryJSON struct {
	Tools []warningsNGToolJSON `json:"tools"`
}

type warningsNGToolJSON struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	LatestURL       string `json:"latestUrl"`
	Size            int    `json:"size"`
	TotalSize       int    `json:"totalSize"`
	Total           int    `json:"total"`
	NewSize         int    `json:"newSize"`
	New             int    `json:"new"`
	FixedSize       int    `json:"fixedSize"`
	Fixed           int    `json:"fixed"`
	OutstandingSize int    `json:"outstandingSize"`
	Outstanding     int    `json:"outstanding"`
	ErrorSize       int    `json:"errorSize"`
	HighSize        int    `json:"highSize"`
	NormalSize      int    `json:"normalSize"`
	LowSize         int    `json:"lowSize"`
}

type warningsNGIssuesJSON struct {
	Size      int                  `json:"size"`
	TotalSize int                  `json:"totalSize"`
	Issues    []issueJSON          `json:"issues"`
	Tools     []warningsNGToolJSON `json:"tools"`
}

type issueJSON struct {
	AddedAt      int    `json:"addedAt"`
	AuthorEmail  string `json:"authorEmail"`
	AuthorName   string `json:"authorName"`
	BaseName     string `json:"baseName"`
	Severity     string `json:"severity"`
	Category     string `json:"category"`
	Type         string `json:"type"`
	Message      string `json:"message"`
	Description  string `json:"description"`
	File         string `json:"file"`
	FileName     string `json:"fileName"`
	FilePath     string `json:"filePath"`
	Package      string `json:"package"`
	PackageName  string `json:"packageName"`
	Module       string `json:"module"`
	ModuleName   string `json:"moduleName"`
	Line         int    `json:"line"`
	LineStart    int    `json:"lineStart"`
	LineEnd      int    `json:"lineEnd"`
	LineNumber   int    `json:"lineNumber"`
	Column       int    `json:"column"`
	ColumnStart  int    `json:"columnStart"`
	ColumnEnd    int    `json:"columnEnd"`
	ColumnNumber int    `json:"columnNumber"`
	Fingerprint  string `json:"fingerprint"`
	Reference    string `json:"reference"`
	Origin       string `json:"origin"`
	OriginName   string `json:"originName"`
	Commit       string `json:"commit"`
}

func (a *API) WarningsNGSummary(ctx context.Context, job string, number int) model.IssuesSummary {
	summary, err := a.WarningsNGSummaryStrict(ctx, job, number)
	if err != nil {
		return model.IssuesSummary{Available: false, CheckedEndpoints: summary.CheckedEndpoints, Message: optionalWarningsMessage(err)}
	}
	return summary
}

func (a *API) WarningsNGSummaryStrict(ctx context.Context, job string, number int) (model.IssuesSummary, error) {
	candidates := []string{urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/warnings-ng/api/json"}
	var raw warningsNGSummaryJSON
	if err := a.client.GetJSON(ctx, candidates[0], nil, &raw); err != nil {
		return model.IssuesSummary{Available: false, CheckedEndpoints: candidates, Message: optionalWarningsMessage(err)}, err
	}
	tools := issueToolSummaries(raw.Tools)
	if len(tools) == 0 {
		return model.IssuesSummary{Available: true, Endpoint: candidates[0], CheckedEndpoints: candidates, Message: "Warnings NG is available but did not report any issue tools."}, nil
	}
	return model.IssuesSummary{Available: true, Endpoint: candidates[0], CheckedEndpoints: candidates, Tools: tools}, nil
}

func (a *API) ListIssues(ctx context.Context, job string, number int, tool string, offset int, limit int) (model.IssuesPage, error) {
	summary := a.WarningsNGSummary(ctx, job, number)
	page := model.IssuesPage{
		Available:        summary.Available,
		Endpoint:         summary.Endpoint,
		CheckedEndpoints: append([]string{}, summary.CheckedEndpoints...),
		Tools:            summary.Tools,
		Message:          summary.Message,
	}
	if !summary.Available {
		return page, nil
	}

	selected := strings.TrimSpace(tool)
	if selected == "" {
		if len(summary.Tools) != 1 {
			if len(summary.Tools) > 1 {
				page.Message = "Multiple Warnings NG tools are available; provide a tool selector to list issues for one tool."
			}
			return page, nil
		}
		selected = summary.Tools[0].ID
	}

	endpoint := warningsNGIssuesEndpoint(job, number, selected)
	page.CheckedEndpoints = append(page.CheckedEndpoints, endpoint)
	var raw warningsNGIssuesJSON
	tree := fmt.Sprintf("size,totalSize,issues[addedAt,authorEmail,authorName,baseName,severity,category,type,message,description,file,fileName,filePath,package,packageName,module,moduleName,line,lineStart,lineEnd,lineNumber,column,columnStart,columnEnd,columnNumber,fingerprint,reference,origin,originName,commit]{%d,%d}", offset, offset+limit)
	if err := a.client.GetJSON(ctx, endpoint, url.Values{"tree": {tree}}, &raw); err != nil {
		if isOptionalMissing(err) {
			page.Endpoint = endpoint
			page.Message = optionalWarningsMessage(err)
			return page, nil
		}
		return model.IssuesPage{}, err
	}
	page.Available = true
	page.Endpoint = endpoint
	page.Message = ""
	page.Items = issuesFromJSON(raw.Issues)
	return page, nil
}

func warningsNGIssuesEndpoint(job string, number int, tool string) string {
	return urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/" + url.PathEscape(strings.Trim(tool, "/")) + "/all/api/json"
}

func issueToolSummaries(tools []warningsNGToolJSON) []model.IssueToolSummary {
	out := make([]model.IssueToolSummary, 0, len(tools))
	for _, tool := range tools {
		out = append(out, model.IssueToolSummary{
			ID:          firstNonEmpty(tool.ID, strings.Trim(tool.URL, "/")),
			Name:        tool.Name,
			URL:         tool.URL,
			LatestURL:   tool.LatestURL,
			Total:       firstPositive(tool.Total, tool.TotalSize, tool.Size),
			New:         firstPositive(tool.New, tool.NewSize),
			Fixed:       firstPositive(tool.Fixed, tool.FixedSize),
			Outstanding: firstPositive(tool.Outstanding, tool.OutstandingSize),
			Error:       tool.ErrorSize,
			High:        tool.HighSize,
			Normal:      tool.NormalSize,
			Low:         tool.LowSize,
		})
	}
	return out
}

func issuesFromJSON(issues []issueJSON) []model.Issue {
	out := make([]model.Issue, 0, len(issues))
	for _, issue := range issues {
		out = append(out, model.Issue{
			Severity:    issue.Severity,
			Category:    issue.Category,
			Type:        issue.Type,
			Message:     firstNonEmpty(issue.Message, issue.Description),
			Description: issue.Description,
			File:        firstNonEmpty(issue.File, issue.FileName, issue.FilePath),
			BaseName:    issue.BaseName,
			Package:     firstNonEmpty(issue.Package, issue.PackageName),
			Module:      firstNonEmpty(issue.Module, issue.ModuleName),
			Line:        firstPositive(issue.Line, issue.LineStart, issue.LineNumber),
			LineEnd:     issue.LineEnd,
			ColumnStart: firstPositive(issue.ColumnStart, issue.Column, issue.ColumnNumber),
			ColumnEnd:   issue.ColumnEnd,
			Fingerprint: issue.Fingerprint,
			Reference:   issue.Reference,
			Origin:      issue.Origin,
			OriginName:  issue.OriginName,
			AuthorName:  issue.AuthorName,
			AuthorEmail: issue.AuthorEmail,
			Commit:      issue.Commit,
			AddedAt:     issue.AddedAt,
		})
	}
	return out
}

func optionalWarningsMessage(err error) string {
	if isOptionalMissing(err) {
		return "Warnings NG data is not available for this build."
	}
	return err.Error()
}

func isOptionalMissing(err error) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*apperrors.Error); ok {
		return e.Code == apperrors.CodeNotFound || e.Code == apperrors.CodeUnsupported
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func (a *API) TriggerBuild(ctx context.Context, job string, params map[string]string) (string, error) {
	path := urlx.JobPath(job) + "/build"
	var form url.Values
	if len(params) > 0 {
		path = urlx.JobPath(job) + "/buildWithParameters"
		form = url.Values{}
		for k, v := range params {
			form.Set(k, v)
		}
	}
	status, _, headers, err := a.client.Post(ctx, path, nil, form)
	if err != nil {
		return "", err
	}
	if status < 200 || status > 399 {
		return "", jenkinsHTTPError(status)
	}
	return headers.Get("Location"), nil
}

func jenkinsHTTPError(status int) error {
	msg := fmt.Sprintf("Jenkins returned HTTP %d", status)
	detail := map[string]any{"status": status}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return apperrors.Wrap(apperrors.CodePermissionDenied, msg, detail)
	case http.StatusNotFound:
		return apperrors.Wrap(apperrors.CodeNotFound, msg, detail)
	default:
		return apperrors.Wrap(apperrors.CodeJenkins, msg, detail)
	}
}

func (a *API) QueueItem(ctx context.Context, id int64) (model.QueueItem, error) {
	path := "queue/item/" + strconv.FormatInt(id, 10) + "/api/json"
	var q struct {
		ID         int64      `json:"id"`
		URL        string     `json:"url"`
		Why        string     `json:"why"`
		Cancelled  bool       `json:"cancelled"`
		Executable *buildJSON `json:"executable"`
		Task       struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"task"`
	}
	if err := a.client.GetJSON(ctx, path, nil, &q); err != nil {
		return model.QueueItem{}, err
	}
	var ex *model.BuildSummary
	if q.Executable != nil {
		s := summary(*q.Executable)
		ex = &s
	}
	return model.QueueItem{ID: q.ID, URL: q.URL, Why: q.Why, Cancelled: q.Cancelled, TaskName: q.Task.Name, TaskURL: q.Task.URL, Executable: ex}, nil
}

func (a *API) ListQueue(ctx context.Context) ([]model.QueueItem, error) {
	var raw struct {
		Items []struct {
			ID         int64      `json:"id"`
			URL        string     `json:"url"`
			Why        string     `json:"why"`
			Cancelled  bool       `json:"cancelled"`
			Executable *buildJSON `json:"executable"`
			Task       struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"task"`
		} `json:"items"`
	}
	tree := "items[id,url,why,cancelled,task[name,url],executable[number,url,result,building,timestamp,duration]]"
	if err := a.client.GetJSON(ctx, "queue/api/json", url.Values{"tree": {tree}}, &raw); err != nil {
		return nil, err
	}
	items := make([]model.QueueItem, 0, len(raw.Items))
	for _, item := range raw.Items {
		var ex *model.BuildSummary
		if item.Executable != nil {
			s := summary(*item.Executable)
			ex = &s
		}
		items = append(items, model.QueueItem{ID: item.ID, URL: item.URL, Why: item.Why, Cancelled: item.Cancelled, TaskName: item.Task.Name, TaskURL: item.Task.URL, Executable: ex})
	}
	return items, nil
}

func (a *API) CancelQueueItem(ctx context.Context, id int64) error {
	status, _, _, err := a.client.Post(ctx, "queue/cancelItem", url.Values{"id": {strconv.FormatInt(id, 10)}}, nil)
	if err != nil {
		return err
	}
	if status < 200 || status > 399 {
		return fmt.Errorf("jenkins returned HTTP %d", status)
	}
	return nil
}

func (a *API) CancelBuild(ctx context.Context, job string, number int) error {
	status, _, _, err := a.client.Post(ctx, urlx.JobPath(job)+"/"+strconv.Itoa(number)+"/stop", nil, nil)
	if err != nil {
		return err
	}
	if status < 200 || status > 399 {
		return fmt.Errorf("jenkins returned HTTP %d", status)
	}
	return nil
}

func summary(b buildJSON) model.BuildSummary {
	return model.BuildSummary{
		ID:                b.ID,
		Number:            b.Number,
		URL:               b.URL,
		Result:            model.BuildResult(b.Result),
		Building:          b.Building,
		Timestamp:         b.Timestamp,
		Duration:          b.Duration,
		Description:       b.Description,
		DisplayName:       b.DisplayName,
		QueueID:           b.QueueID,
		EstimatedDuration: b.EstimatedDuration,
		KeepLog:           b.KeepLog,
	}
}

func parseActions(actions []json.RawMessage) ([]model.Cause, map[string]any) {
	causes := []model.Cause{}
	params := map[string]any{}
	for _, raw := range actions {
		var a struct {
			Causes []struct {
				ShortDescription string `json:"shortDescription"`
				UserID           string `json:"userId"`
				UserName         string `json:"userName"`
			} `json:"causes"`
			Parameters []struct {
				Name  string `json:"name"`
				Value any    `json:"value"`
			} `json:"parameters"`
		}
		if json.Unmarshal(raw, &a) == nil {
			for _, c := range a.Causes {
				causes = append(causes, model.Cause{ShortDescription: c.ShortDescription, UserID: c.UserID, UserName: c.UserName})
			}
			for _, p := range a.Parameters {
				if p.Name != "" {
					params[p.Name] = p.Value
				}
			}
		}
	}
	if len(params) == 0 {
		params = nil
	}
	return causes, params
}
func authorName(v any) string {
	if m, ok := v.(map[string]any); ok {
		if s, ok := m["fullName"].(string); ok {
			return s
		}
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
