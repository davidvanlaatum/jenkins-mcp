package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

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

func (a *API) Capabilities(ctx context.Context) model.ControllerCapabilities {
	info, err := a.ControllerInfo(ctx)
	if err != nil {
		return model.ControllerCapabilities{
			Controller: model.ControllerInfo{ID: a.id, URL: a.BaseURL(), Available: false, Error: err.Error()},
			Features:   defaultFeatureMap(nil),
			Error:      err.Error(),
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
	Name               string     `json:"name"`
	URL                string     `json:"url"`
	Color              string     `json:"color"`
	Class              string     `json:"_class"`
	Disabled           *bool      `json:"disabled"`
	LastBuild          *buildJSON `json:"lastBuild"`
	LastCompletedBuild *buildJSON `json:"lastCompletedBuild"`
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
	q := url.Values{"tree": {"jobs[name,url,color,_class,disabled,lastBuild[number,result,building],lastCompletedBuild[number,result,building]]"}}
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
		jobs = append(jobs, model.Job{
			Name:     j.Name,
			FullName: full,
			URL:      j.URL,
			Color:    j.Color,
			Class:    j.Class,
			Disabled: j.Disabled,
			Status:   jobStatus(j),
			Building: j.LastBuild != nil && j.LastBuild.Building,
		})
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
	switch strings.ToUpper(strings.TrimSpace(result)) {
	case "SUCCESS":
		return "success"
	case "FAILURE":
		return "failed"
	case "UNSTABLE":
		return "unstable"
	case "ABORTED":
		return "aborted"
	case "NOT_BUILT":
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
		Disabled:           raw.Disabled,
		LastBuild:          raw.LastBuild,
		LastCompletedBuild: raw.LastCompletedBuild,
	}
	detail := model.JobDetail{
		Job: model.Job{
			Name:     raw.Name,
			FullName: raw.FullName,
			URL:      raw.URL,
			Color:    raw.Color,
			Class:    raw.Class,
			Disabled: raw.Disabled,
			Status:   jobStatus(jobJSON),
			Building: raw.LastBuild != nil && raw.LastBuild.Building,
		},
		Description:     raw.Description,
		Buildable:       raw.Buildable,
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
	Number          int               `json:"number"`
	URL             string            `json:"url"`
	Result          string            `json:"result"`
	Building        bool              `json:"building"`
	Timestamp       int64             `json:"timestamp"`
	Duration        int64             `json:"duration"`
	Description     string            `json:"description"`
	DisplayName     string            `json:"displayName"`
	FullDisplayName string            `json:"fullDisplayName"`
	Actions         []json.RawMessage `json:"actions"`
	Artifacts       []model.Artifact  `json:"artifacts"`
	ChangeSets      []changeSetJSON   `json:"changeSets"`
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

func (a *API) ListBuilds(ctx context.Context, job string, limit int) ([]model.BuildSummary, error) {
	path := urlx.JobPath(job) + "/api/json"
	tree := fmt.Sprintf("builds[number,url,result,building,timestamp,duration]{0,%d}", limit)
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
	tree := "number,url,result,building,timestamp,duration,description,displayName,fullDisplayName,artifacts[displayPath,fileName,relativePath],actions[causes[*],parameters[*]],changeSets[kind,items[commitId,author[fullName],msg,timestamp,affectedPaths]]"
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
		Status:     raw.Status,
		StartTime:  raw.StartTime,
		EndTime:    raw.EndTime,
		DurationMS: raw.DurationMillis,
	}
	for _, stage := range raw.Stages {
		run.Stages = append(run.Stages, model.PipelineStage{
			ID:         stage.ID,
			Name:       stage.Name,
			Status:     stage.Status,
			StartTime:  stage.StartTime,
			DurationMS: stage.DurationMillis,
			PauseMS:    stage.PauseMillis,
		})
	}
	return run, nil
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
			Status:     raw.Status,
			StartTime:  raw.StartTime,
			DurationMS: raw.DurationMillis,
			PauseMS:    raw.PauseMillis,
		},
	}
	for _, node := range raw.StageFlowNodes {
		detail.Nodes = append(detail.Nodes, model.PipelineNode{
			ID:                   node.ID,
			Name:                 node.Name,
			Status:               node.Status,
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
		NodeStatus: raw.NodeStatus,
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
	candidates := []string{
		urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/coverage/api/json",
		urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/coverage/result/api/json",
		urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/jacoco/api/json",
	}
	for _, candidate := range candidates {
		var raw map[string]any
		if err := a.client.GetJSON(ctx, candidate, nil, &raw); err == nil {
			return model.CoverageReport{Available: true, Endpoint: candidate, CheckedEndpoints: candidates, Summary: raw}, nil
		}
	}
	return model.CoverageReport{Available: false, CheckedEndpoints: candidates}, nil
}

func (a *API) IssuesReport(ctx context.Context, job string, number int) (model.IssuesReport, error) {
	candidates := []string{
		urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/warnings-ngResult/api/json",
		urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/warnings-ngResult/analysis/api/json",
		urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/analysisResult/api/json",
		urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/recordIssues/api/json",
		urlx.JobPath(job) + "/" + strconv.Itoa(number) + "/warnings/api/json",
	}
	for _, candidate := range candidates {
		var raw map[string]any
		if err := a.client.GetJSON(ctx, candidate, nil, &raw); err == nil {
			return model.IssuesReport{Available: true, Endpoint: candidate, CheckedEndpoints: candidates, Summary: raw}, nil
		}
	}
	return model.IssuesReport{Available: false, CheckedEndpoints: candidates}, nil
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
		return "", fmt.Errorf("jenkins returned HTTP %d", status)
	}
	return headers.Get("Location"), nil
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
	return model.BuildSummary{Number: b.Number, URL: b.URL, Result: b.Result, Building: b.Building, Timestamp: b.Timestamp, Duration: b.Duration}
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
