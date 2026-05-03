package model

type ControllerInfo struct {
	ID          string `json:"id"`
	URL         string `json:"url,omitempty"`
	Version     string `json:"version,omitempty"`
	UseSecurity bool   `json:"useSecurity"`
	NodeName    string `json:"nodeName,omitempty"`
	Available   bool   `json:"available"`
	Error       string `json:"error,omitempty"`
}

type PluginInfo struct {
	ShortName string `json:"shortName"`
	Version   string `json:"version,omitempty"`
	Active    bool   `json:"active"`
	Enabled   bool   `json:"enabled"`
}

type ControllerCapabilities struct {
	Controller ControllerInfo  `json:"controller"`
	Features   map[string]bool `json:"features"`
	Plugins    []PluginInfo    `json:"plugins,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type Job struct {
	Name     string `json:"name"`
	FullName string `json:"fullName,omitempty"`
	URL      string `json:"url"`
	Color    string `json:"color,omitempty"`
	Class    string `json:"class,omitempty"`
	Disabled *bool  `json:"disabled,omitempty"`
	Status   string `json:"status,omitempty"`
	Building bool   `json:"building"`
}

type JobDetail struct {
	Job
	Description     string                `json:"description,omitempty"`
	Buildable       bool                  `json:"buildable"`
	InQueue         bool                  `json:"inQueue"`
	NextBuildNumber int                   `json:"nextBuildNumber,omitempty"`
	LastBuild       *BuildSummary         `json:"lastBuild,omitempty"`
	LastSuccessful  *BuildSummary         `json:"lastSuccessfulBuild,omitempty"`
	LastFailed      *BuildSummary         `json:"lastFailedBuild,omitempty"`
	Parameters      []ParameterDefinition `json:"parameters,omitempty"`
}

type ParameterDefinition struct {
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Default     any      `json:"default,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	Required    bool     `json:"required,omitempty"`
}

type BuildSummary struct {
	Number    int    `json:"number"`
	URL       string `json:"url"`
	Result    string `json:"result,omitempty"`
	Building  bool   `json:"building"`
	Timestamp int64  `json:"timestamp,omitempty"`
	Duration  int64  `json:"duration,omitempty"`
}

type BuildReference struct {
	Controller string `json:"controller"`
	Job        string `json:"job"`
	Build      int    `json:"build"`
	URL        string `json:"url"`
}

type Build struct {
	BuildSummary
	Description     string         `json:"description,omitempty"`
	DisplayName     string         `json:"displayName,omitempty"`
	FullDisplayName string         `json:"fullDisplayName,omitempty"`
	Causes          []Cause        `json:"causes,omitempty"`
	Parameters      map[string]any `json:"parameters,omitempty"`
	Artifacts       []Artifact     `json:"artifacts,omitempty"`
	ChangeSets      []ChangeSet    `json:"changeSets,omitempty"`
}
type Cause struct {
	ShortDescription string `json:"shortDescription"`
	UserID           string `json:"userId,omitempty"`
	UserName         string `json:"userName,omitempty"`
}
type Artifact struct {
	DisplayPath  string `json:"displayPath"`
	FileName     string `json:"fileName"`
	RelativePath string `json:"relativePath"`
	Size         int64  `json:"size,omitempty"`
}
type ChangeSet struct {
	Kind  string   `json:"kind,omitempty"`
	Items []Change `json:"items,omitempty"`
}
type Change struct {
	CommitID      string   `json:"commitId,omitempty"`
	Author        string   `json:"author,omitempty"`
	Message       string   `json:"message,omitempty"`
	Timestamp     int64    `json:"timestamp,omitempty"`
	AffectedPaths []string `json:"affectedPaths,omitempty"`
}
type LogChunk struct {
	Text      string `json:"text"`
	Start     int64  `json:"start"`
	NextStart int64  `json:"nextStart"`
	More      bool   `json:"more"`
	Truncated bool   `json:"truncated"`
}

type LogMatch struct {
	Line    int    `json:"line"`
	Text    string `json:"text"`
	Context string `json:"context,omitempty"`
}

type LogSearchResult struct {
	Query        string     `json:"query"`
	Matches      []LogMatch `json:"matches"`
	ScannedBytes int64      `json:"scannedBytes"`
	NextStart    int64      `json:"nextStart"`
	More         bool       `json:"more"`
	Truncated    bool       `json:"truncated"`
}
type TestReport struct {
	TotalCount int         `json:"totalCount"`
	FailCount  int         `json:"failCount"`
	SkipCount  int         `json:"skipCount"`
	PassCount  int         `json:"passCount"`
	Suites     []TestSuite `json:"suites,omitempty"`
	Truncated  bool        `json:"truncated"`
}
type TestSuite struct {
	Name  string     `json:"name"`
	Cases []TestCase `json:"cases,omitempty"`
}
type TestCase struct {
	ClassName       string  `json:"className"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	Duration        float64 `json:"duration,omitempty"`
	ErrorDetails    string  `json:"errorDetails,omitempty"`
	ErrorStackTrace string  `json:"errorStackTrace,omitempty"`
}
type QueueItem struct {
	ID         int64         `json:"id"`
	URL        string        `json:"url,omitempty"`
	Why        string        `json:"why,omitempty"`
	Cancelled  bool          `json:"cancelled"`
	TaskName   string        `json:"taskName,omitempty"`
	TaskURL    string        `json:"taskUrl,omitempty"`
	Executable *BuildSummary `json:"executable,omitempty"`
}

type PipelineRun struct {
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Status     string          `json:"status,omitempty"`
	StartTime  int64           `json:"startTimeMillis,omitempty"`
	EndTime    int64           `json:"endTimeMillis,omitempty"`
	DurationMS int64           `json:"durationMillis,omitempty"`
	Stages     []PipelineStage `json:"stages,omitempty"`
}

type PipelineStage struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`
	Status     string `json:"status,omitempty"`
	StartTime  int64  `json:"startTimeMillis,omitempty"`
	DurationMS int64  `json:"durationMillis,omitempty"`
	PauseMS    int64  `json:"pauseMillis,omitempty"`
}

type PipelineStageDetail struct {
	PipelineStage
	Nodes []PipelineNode `json:"nodes,omitempty"`
}

type PipelineNode struct {
	ID                   string   `json:"id,omitempty"`
	Name                 string   `json:"name,omitempty"`
	Status               string   `json:"status,omitempty"`
	ParameterDescription string   `json:"parameterDescription,omitempty"`
	StartTime            int64    `json:"startTimeMillis,omitempty"`
	DurationMS           int64    `json:"durationMillis,omitempty"`
	PauseMS              int64    `json:"pauseMillis,omitempty"`
	ParentNodes          []string `json:"parentNodes,omitempty"`
	HasLog               bool     `json:"hasLog"`
}

type PipelineNodeLog struct {
	NodeID     string `json:"nodeId"`
	NodeStatus string `json:"nodeStatus,omitempty"`
	Text       string `json:"text,omitempty"`
	Length     int64  `json:"length"`
	HasMore    bool   `json:"hasMore"`
	Truncated  bool   `json:"truncated"`
}

type ArtifactContent struct {
	RelativePath string `json:"relativePath"`
	Text         string `json:"text,omitempty"`
	Bytes        int    `json:"bytes"`
	Inline       bool   `json:"inline"`
	Truncated    bool   `json:"truncated"`
}

type CoverageReport struct {
	Available        bool           `json:"available"`
	Endpoint         string         `json:"endpoint,omitempty"`
	CheckedEndpoints []string       `json:"checkedEndpoints,omitempty"`
	Summary          map[string]any `json:"summary,omitempty"`
}

type IssuesReport struct {
	Available        bool           `json:"available"`
	Endpoint         string         `json:"endpoint,omitempty"`
	CheckedEndpoints []string       `json:"checkedEndpoints,omitempty"`
	Summary          map[string]any `json:"summary,omitempty"`
}

type BuildWatch struct {
	State    string       `json:"state,omitempty"`
	Build    BuildSummary `json:"build"`
	Pipeline *PipelineRun `json:"pipeline,omitempty"`
	Complete bool         `json:"complete"`
	TimedOut bool         `json:"timedOut"`
}
