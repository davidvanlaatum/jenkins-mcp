package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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

type jobJSON struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Color string `json:"color"`
	Class string `json:"_class"`
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
	q := url.Values{"tree": {"jobs[name,url,color,_class]"}}
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
		jobs = append(jobs, model.Job{Name: j.Name, FullName: full, URL: j.URL, Color: j.Color, Class: j.Class})
	}
	return jobs, nil
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
	}
	if err := a.client.GetJSON(ctx, path, nil, &q); err != nil {
		return model.QueueItem{}, err
	}
	var ex *model.BuildSummary
	if q.Executable != nil {
		s := summary(*q.Executable)
		ex = &s
	}
	return model.QueueItem{ID: q.ID, URL: q.URL, Why: q.Why, Cancelled: q.Cancelled, Executable: ex}, nil
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
