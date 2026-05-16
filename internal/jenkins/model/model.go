package model

type ControllerInfo struct {
	ID          string `json:"id" jsonschema:"Configured Jenkins controller id"`
	URL         string `json:"url,omitempty" jsonschema:"Base URL for the Jenkins controller"`
	Version     string `json:"version,omitempty" jsonschema:"Reported Jenkins version"`
	UseSecurity bool   `json:"useSecurity" jsonschema:"Whether Jenkins security is enabled on the controller"`
	NodeName    string `json:"nodeName,omitempty" jsonschema:"Reported Jenkins controller node name"`
	Available   bool   `json:"available" jsonschema:"Whether the controller was reachable and returned metadata"`
	Error       string `json:"error,omitempty" jsonschema:"Controller availability or capability error, when present"`
}

type PluginInfo struct {
	ShortName string `json:"shortName" jsonschema:"Jenkins plugin short name"`
	Version   string `json:"version,omitempty" jsonschema:"Installed plugin version"`
	Active    bool   `json:"active" jsonschema:"Whether the plugin is currently active"`
	Enabled   bool   `json:"enabled" jsonschema:"Whether the plugin is enabled"`
}

type CapabilityWarning struct {
	Code     string `json:"code" jsonschema:"Machine-readable warning code"`
	Source   string `json:"source" jsonschema:"Optional capability data source associated with the warning"`
	Optional bool   `json:"optional" jsonschema:"Whether the warning describes optional data that does not make the controller unavailable"`
	Message  string `json:"message" jsonschema:"Human-readable warning message"`
	Error    string `json:"error,omitempty" jsonschema:"Underlying Jenkins or network error, when available"`
}

type ControllerCapabilities struct {
	Controller ControllerInfo      `json:"controller" jsonschema:"Jenkins controller associated with these capabilities"`
	Features   map[string]bool     `json:"features" jsonschema:"Detected feature flags keyed by capability name"`
	Plugins    []PluginInfo        `json:"plugins,omitempty" jsonschema:"Installed Jenkins plugins relevant to MCP capabilities"`
	Warnings   []CapabilityWarning `json:"warnings,omitempty" jsonschema:"Structured warnings for optional capability data that could not be discovered or was skipped"`
	Error      string              `json:"error,omitempty" jsonschema:"Capability detection error, when present; optional data failures are also described in warnings"`
}

type Job struct {
	Name     string `json:"name" jsonschema:"Jenkins job display name"`
	FullName string `json:"fullName,omitempty" jsonschema:"Full Jenkins job path including folders"`
	URL      string `json:"url" jsonschema:"Jenkins job URL"`
	Color    string `json:"color,omitempty" jsonschema:"Raw Jenkins job color status"`
	Class    string `json:"class,omitempty" jsonschema:"Raw Jenkins job class name"`
	Disabled *bool  `json:"disabled,omitempty" jsonschema:"Whether the job is disabled, when Jenkins reports it"`
	Status   string `json:"status,omitempty" jsonschema:"Derived job status such as success, failed, unstable, aborted, disabled, not_built, or unknown"`
	Building bool   `json:"building" jsonschema:"Whether the latest build is currently building"`
}

type JobDetail struct {
	Job
	Description     string                `json:"description,omitempty" jsonschema:"Jenkins job description"`
	Buildable       bool                  `json:"buildable" jsonschema:"Whether Jenkins considers the job buildable"`
	InQueue         bool                  `json:"inQueue" jsonschema:"Whether the job currently has an item in the Jenkins queue"`
	NextBuildNumber int                   `json:"nextBuildNumber,omitempty" jsonschema:"Next build number Jenkins plans to assign"`
	LastBuild       *BuildSummary         `json:"lastBuild,omitempty" jsonschema:"Most recent build summary"`
	LastSuccessful  *BuildSummary         `json:"lastSuccessfulBuild,omitempty" jsonschema:"Most recent successful build summary"`
	LastFailed      *BuildSummary         `json:"lastFailedBuild,omitempty" jsonschema:"Most recent failed build summary"`
	Parameters      []ParameterDefinition `json:"parameters,omitempty" jsonschema:"Build parameter definitions for this job"`
}

type JobConfig struct {
	Job              Job              `json:"job" jsonschema:"Jenkins job associated with the inspected configuration"`
	Mode             string           `json:"mode" jsonschema:"Configuration inspection mode used for the response: summary, xml, or both"`
	ConfigAccessible bool             `json:"configAccessible" jsonschema:"Whether Jenkins allowed reading the job config.xml endpoint"`
	Source           string           `json:"source" jsonschema:"Source used for the returned configuration information, such as config.xml or api/json fallback"`
	Summary          JobConfigSummary `json:"summary" jsonschema:"Structured summary extracted from job configuration or fallback job metadata"`
	XML              string           `json:"xml,omitempty" jsonschema:"Best-effort redacted config.xml content when mode requests XML output; plugin-specific fields may still require careful review"`
	Bytes            int              `json:"bytes,omitempty" jsonschema:"Number of bytes in the redacted XML content before tool-level truncation"`
	Truncated        bool             `json:"truncated" jsonschema:"Whether the redacted XML content was truncated to the requested or configured byte limit"`
	AccessError      string           `json:"accessError,omitempty" jsonschema:"Permission or retrieval error from config.xml when fallback metadata was returned"`
	Warnings         []ConfigWarning  `json:"warnings,omitempty" jsonschema:"Warnings about partial access, redaction, truncation, or parse limitations"`
}

type JobConfigSummary struct {
	RootElement          string                `json:"rootElement,omitempty" jsonschema:"Root XML element name from config.xml, when readable"`
	RootClass            string                `json:"rootClass,omitempty" jsonschema:"Class attribute from the root XML element, when present"`
	Plugin               string                `json:"plugin,omitempty" jsonschema:"Plugin attribute from the root XML element, when present"`
	Kind                 string                `json:"kind" jsonschema:"Derived Jenkins job configuration kind, such as branchJob, multibranchProject, organizationFolder, folder, pipeline, freestyle, or unknown"`
	Description          string                `json:"description,omitempty" jsonschema:"Job description from config XML or fallback job metadata"`
	Disabled             *bool                 `json:"disabled,omitempty" jsonschema:"Whether the job is disabled, when represented in config XML or job metadata"`
	Buildable            *bool                 `json:"buildable,omitempty" jsonschema:"Whether Jenkins considers the job buildable from fallback job metadata"`
	ScriptPath           string                `json:"scriptPath,omitempty" jsonschema:"Pipeline script path such as Jenkinsfile when found in config XML"`
	DefinitionClass      string                `json:"definitionClass,omitempty" jsonschema:"Pipeline definition class from config XML when found"`
	OrphanedItemStrategy string                `json:"orphanedItemStrategy,omitempty" jsonschema:"Multibranch or organization folder orphaned item strategy class when found"`
	Sources              []ConfigSource        `json:"sources,omitempty" jsonschema:"SCM sources, branch sources, or organization navigators discovered in config XML"`
	Traits               []ConfigComponent     `json:"traits,omitempty" jsonschema:"Branch source or navigator traits discovered in config XML"`
	Triggers             []ConfigComponent     `json:"triggers,omitempty" jsonschema:"Configured trigger components discovered in config XML"`
	JobProperties        []ConfigComponent     `json:"jobProperties,omitempty" jsonschema:"Configured job property components discovered in config XML or fallback metadata"`
	ProjectFactories     []ConfigComponent     `json:"projectFactories,omitempty" jsonschema:"Organization folder or multibranch project factories discovered in config XML"`
	Parameters           []ParameterDefinition `json:"parameters,omitempty" jsonschema:"Build parameter definitions from config XML fallback metadata when available"`
}

type ConfigSource struct {
	Kind          string   `json:"kind" jsonschema:"Source kind such as branchSource, navigator, scm, or unknown"`
	Class         string   `json:"class,omitempty" jsonschema:"Jenkins class name for the source or navigator"`
	Plugin        string   `json:"plugin,omitempty" jsonschema:"Plugin attribute associated with the source or navigator"`
	ID            string   `json:"id,omitempty" jsonschema:"Source identifier when present in config XML"`
	Remote        string   `json:"remote,omitempty" jsonschema:"SCM remote URL when present in config XML"`
	CredentialsID string   `json:"credentialsId,omitempty" jsonschema:"Redacted Jenkins credentials id when present in config XML"`
	RepoOwner     string   `json:"repoOwner,omitempty" jsonschema:"Repository owner or organization from branch source or navigator config"`
	Repository    string   `json:"repository,omitempty" jsonschema:"Repository name from branch source config"`
	ServerURL     string   `json:"serverUrl,omitempty" jsonschema:"SCM server or API URL when present in config XML"`
	Traits        []string `json:"traits,omitempty" jsonschema:"Trait class names attached to this source or navigator"`
}

type ConfigComponent struct {
	Name   string `json:"name,omitempty" jsonschema:"XML element name for the configuration component"`
	Class  string `json:"class,omitempty" jsonschema:"Jenkins class name for the configuration component"`
	Plugin string `json:"plugin,omitempty" jsonschema:"Plugin attribute associated with the configuration component"`
}

type ConfigWarning struct {
	Code    string `json:"code" jsonschema:"Machine-readable warning code for configuration inspection"`
	Message string `json:"message" jsonschema:"Human-readable warning message for configuration inspection"`
	Detail  string `json:"detail,omitempty" jsonschema:"Additional warning detail when available"`
}

type ParameterDefinition struct {
	Name        string   `json:"name" jsonschema:"Jenkins build parameter name"`
	Type        string   `json:"type,omitempty" jsonschema:"Jenkins build parameter type"`
	Description string   `json:"description,omitempty" jsonschema:"Build parameter description"`
	Default     any      `json:"default,omitempty" jsonschema:"Default build parameter value"`
	Choices     []string `json:"choices,omitempty" jsonschema:"Allowed choices for choice-like parameters"`
	Required    bool     `json:"required,omitempty" jsonschema:"Whether the parameter is required before triggering a build"`
}

type BuildResult string

const (
	BuildResultSuccess  BuildResult = "SUCCESS"
	BuildResultUnstable BuildResult = "UNSTABLE"
	BuildResultFailure  BuildResult = "FAILURE"
	BuildResultAborted  BuildResult = "ABORTED"
	BuildResultNotBuilt BuildResult = "NOT_BUILT"
)

type BuildSummary struct {
	ID                string      `json:"id,omitempty" jsonschema:"Jenkins build id string"`
	Number            int         `json:"number" jsonschema:"Jenkins build number"`
	URL               string      `json:"url" jsonschema:"Jenkins build URL"`
	Result            BuildResult `json:"result,omitempty" jsonschema:"Jenkins build result such as SUCCESS, FAILURE, UNSTABLE, ABORTED, or null while building"`
	Building          bool        `json:"building" jsonschema:"Whether the build is currently running"`
	Timestamp         int64       `json:"timestamp,omitempty" jsonschema:"Build start timestamp in Unix epoch milliseconds"`
	Duration          int64       `json:"duration,omitempty" jsonschema:"Build duration in milliseconds"`
	Description       string      `json:"description,omitempty" jsonschema:"Jenkins build description"`
	DisplayName       string      `json:"displayName,omitempty" jsonschema:"Jenkins build display name"`
	QueueID           int64       `json:"queueId,omitempty" jsonschema:"Jenkins queue item id that created this build, when available"`
	EstimatedDuration int64       `json:"estimatedDuration,omitempty" jsonschema:"Estimated build duration in milliseconds"`
	KeepLog           *bool       `json:"keepLog,omitempty" jsonschema:"Whether Jenkins is configured to keep this build log indefinitely"`
}

type BuildReference struct {
	Controller string `json:"controller" jsonschema:"Configured Jenkins controller id"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build      int    `json:"build" jsonschema:"Jenkins build number"`
	URL        string `json:"url" jsonschema:"Original Jenkins build URL"`
}

type Build struct {
	BuildSummary
	Description       string          `json:"description,omitempty" jsonschema:"Jenkins build description"`
	DisplayName       string          `json:"displayName,omitempty" jsonschema:"Jenkins build display name"`
	FullDisplayName   string          `json:"fullDisplayName,omitempty" jsonschema:"Full Jenkins build display name including job context"`
	Causes            []Cause         `json:"causes,omitempty" jsonschema:"Causes that triggered the build"`
	Parameters        map[string]any  `json:"parameters,omitempty" jsonschema:"Build parameter values keyed by parameter name"`
	Artifacts         []Artifact      `json:"artifacts,omitempty" jsonschema:"Artifacts published by the build"`
	ChangeSets        []ChangeSet     `json:"changeSets,omitempty" jsonschema:"SCM change sets associated with the build"`
	Coverage          *CoverageReport `json:"coverage,omitempty" jsonschema:"Optional coverage summaries discovered from common Jenkins coverage plugin endpoints"`
	WarningsNGSummary *IssuesSummary  `json:"warningsNgSummary,omitempty" jsonschema:"Typed Warnings NG summary discovered from the build-level warnings-ng endpoint, when available"`
}
type Cause struct {
	ShortDescription string `json:"shortDescription" jsonschema:"Human-readable Jenkins cause description"`
	UserID           string `json:"userId,omitempty" jsonschema:"Jenkins user id associated with the cause"`
	UserName         string `json:"userName,omitempty" jsonschema:"Jenkins user display name associated with the cause"`
}
type Artifact struct {
	DisplayPath  string `json:"displayPath" jsonschema:"Jenkins artifact display path"`
	FileName     string `json:"fileName" jsonschema:"Artifact file name"`
	RelativePath string `json:"relativePath" jsonschema:"Artifact path relative to the build artifact root"`
	Size         int64  `json:"size,omitempty" jsonschema:"Artifact size in bytes, when reported by Jenkins"`
}
type ChangeSet struct {
	Kind  string   `json:"kind,omitempty" jsonschema:"SCM change set kind reported by Jenkins"`
	Items []Change `json:"items,omitempty" jsonschema:"SCM changes included in this change set"`
}
type Change struct {
	CommitID      string   `json:"commitId,omitempty" jsonschema:"SCM commit identifier"`
	Author        string   `json:"author,omitempty" jsonschema:"SCM change author"`
	Message       string   `json:"message,omitempty" jsonschema:"SCM commit message"`
	Timestamp     int64    `json:"timestamp,omitempty" jsonschema:"SCM change timestamp in Unix epoch milliseconds"`
	AffectedPaths []string `json:"affectedPaths,omitempty" jsonschema:"Paths affected by the SCM change"`
}
type LogChunk struct {
	Text      string `json:"text" jsonschema:"Console log text for this chunk"`
	Start     int64  `json:"start" jsonschema:"Byte offset where this log chunk starts"`
	NextStart int64  `json:"nextStart" jsonschema:"Byte offset to use for the next progressive log request"`
	More      bool   `json:"more" jsonschema:"Whether more log content is currently available after nextStart"`
	Truncated bool   `json:"truncated" jsonschema:"Whether the returned log text was truncated by configured limits"`
}

type LogMatch struct {
	Line    int    `json:"line" jsonschema:"One-based matching log line number"`
	Text    string `json:"text" jsonschema:"Matching log line text"`
	Context string `json:"context,omitempty" jsonschema:"Optional surrounding context lines for the match"`
}

type LogSearchResult struct {
	Query        string     `json:"query" jsonschema:"Search query used against the console log"`
	Matches      []LogMatch `json:"matches" jsonschema:"Matching log lines"`
	ScannedBytes int64      `json:"scannedBytes" jsonschema:"Number of log bytes scanned"`
	NextStart    int64      `json:"nextStart" jsonschema:"Byte offset to use for a subsequent search"`
	More         bool       `json:"more" jsonschema:"Whether more log content may be available after nextStart"`
	Truncated    bool       `json:"truncated" jsonschema:"Whether search results were truncated by limits"`
}
type TestReport struct {
	TotalCount int         `json:"totalCount" jsonschema:"Total number of test cases reported by Jenkins"`
	FailCount  int         `json:"failCount" jsonschema:"Number of failed test cases"`
	SkipCount  int         `json:"skipCount" jsonschema:"Number of skipped test cases"`
	PassCount  int         `json:"passCount" jsonschema:"Number of passing test cases"`
	Suites     []TestSuite `json:"suites,omitempty" jsonschema:"JUnit test suites and bounded test cases"`
	Truncated  bool        `json:"truncated" jsonschema:"Whether test case details were truncated by limits"`
}
type TestSuite struct {
	Name  string     `json:"name" jsonschema:"JUnit test suite name"`
	Cases []TestCase `json:"cases,omitempty" jsonschema:"Test cases in the suite"`
}
type TestCase struct {
	ClassName       string  `json:"className" jsonschema:"JUnit test class name"`
	Name            string  `json:"name" jsonschema:"JUnit test case name"`
	Status          string  `json:"status" jsonschema:"Test case status reported by Jenkins"`
	Duration        float64 `json:"duration,omitempty" jsonschema:"Test case duration in seconds"`
	ErrorDetails    string  `json:"errorDetails,omitempty" jsonschema:"Failure or error details for the test case"`
	ErrorStackTrace string  `json:"errorStackTrace,omitempty" jsonschema:"Failure or error stack trace for the test case"`
}
type QueueItem struct {
	ID         int64         `json:"id" jsonschema:"Jenkins queue item id"`
	URL        string        `json:"url,omitempty" jsonschema:"Jenkins queue item URL"`
	Why        string        `json:"why,omitempty" jsonschema:"Jenkins explanation for why the item remains queued"`
	Cancelled  bool          `json:"cancelled" jsonschema:"Whether the queue item has been cancelled"`
	TaskName   string        `json:"taskName,omitempty" jsonschema:"Queued task or job name"`
	TaskURL    string        `json:"taskUrl,omitempty" jsonschema:"Queued task or job URL"`
	Executable *BuildSummary `json:"executable,omitempty" jsonschema:"Build executable created from this queue item, when available"`
}

type QueueWatch struct {
	State       string          `json:"state" jsonschema:"Opaque queue watch state token to pass as lastState on the next jenkins_watch_queue_item call"`
	Status      string          `json:"status" jsonschema:"Queue watch status: queued, executable, cancelled, or disappeared"`
	Item        *QueueItem      `json:"item,omitempty" jsonschema:"Latest Jenkins queue item snapshot, when Jenkins still exposes it"`
	Build       *BuildReference `json:"build,omitempty" jsonschema:"Resolved build reference when Jenkins assigns an executable to the queue item"`
	TimedOut    bool            `json:"timedOut" jsonschema:"Whether the long-poll call reached waitTimeoutMs without a queue state change"`
	Terminal    bool            `json:"terminal" jsonschema:"Whether the queue item reached a terminal state and no further queue watching is needed"`
	Cancelled   bool            `json:"cancelled" jsonschema:"Whether Jenkins reports the queue item was cancelled"`
	Disappeared bool            `json:"disappeared" jsonschema:"Whether Jenkins no longer exposes the queue item before an executable could be resolved"`
}

type PipelineRun struct {
	ID                  string               `json:"id,omitempty" jsonschema:"Pipeline run id reported by Jenkins"`
	Name                string               `json:"name,omitempty" jsonschema:"Pipeline run name"`
	Status              PipelineStatus       `json:"status,omitempty" jsonschema:"Pipeline run status"`
	WaitingForInput     bool                 `json:"waitingForInput" jsonschema:"Whether the Pipeline run is currently paused waiting for Jenkins input-step approval"`
	PendingInputActions []PendingInputAction `json:"pendingInputActions,omitempty" jsonschema:"Pending Jenkins Pipeline input-step actions for this run"`
	PendingInputError   string               `json:"pendingInputError,omitempty" jsonschema:"Error encountered while fetching optional pending input-step actions, when stage data is still available"`
	StartTime           int64                `json:"startTimeMillis,omitempty" jsonschema:"Pipeline run start time in Unix epoch milliseconds"`
	EndTime             int64                `json:"endTimeMillis,omitempty" jsonschema:"Pipeline run end time in Unix epoch milliseconds"`
	DurationMS          int64                `json:"durationMillis,omitempty" jsonschema:"Pipeline run duration in milliseconds"`
	Stages              []PipelineStage      `json:"stages,omitempty" jsonschema:"Pipeline stage summaries"`
}

type PipelineStatus string

const (
	PipelineStatusSuccess            PipelineStatus = "SUCCESS"
	PipelineStatusFailed             PipelineStatus = "FAILED"
	PipelineStatusFailure            PipelineStatus = "FAILURE"
	PipelineStatusUnstable           PipelineStatus = "UNSTABLE"
	PipelineStatusAborted            PipelineStatus = "ABORTED"
	PipelineStatusNotExecuted        PipelineStatus = "NOT_EXECUTED"
	PipelineStatusInProgress         PipelineStatus = "IN_PROGRESS"
	PipelineStatusPausedPendingInput PipelineStatus = "PAUSED_PENDING_INPUT"
)

type PendingInputAction struct {
	ID         string `json:"id,omitempty" jsonschema:"Jenkins Pipeline input-step id"`
	Message    string `json:"message,omitempty" jsonschema:"Input-step prompt message shown by Jenkins"`
	ProceedURL string `json:"proceedUrl,omitempty" jsonschema:"Relative Jenkins URL used to proceed the input step"`
	AbortURL   string `json:"abortUrl,omitempty" jsonschema:"Relative Jenkins URL used to abort the input step"`
}

type PipelineStage struct {
	ID         string         `json:"id,omitempty" jsonschema:"Pipeline stage id"`
	Name       string         `json:"name,omitempty" jsonschema:"Pipeline stage name"`
	Status     PipelineStatus `json:"status,omitempty" jsonschema:"Pipeline stage status"`
	StartTime  int64          `json:"startTimeMillis,omitempty" jsonschema:"Pipeline stage start time in Unix epoch milliseconds"`
	DurationMS int64          `json:"durationMillis,omitempty" jsonschema:"Pipeline stage duration in milliseconds"`
	PauseMS    int64          `json:"pauseMillis,omitempty" jsonschema:"Pipeline stage paused duration in milliseconds"`
}

type PipelineStageDetail struct {
	PipelineStage
	Nodes []PipelineNode `json:"nodes,omitempty" jsonschema:"Pipeline flow nodes within the stage"`
}

type PipelineNode struct {
	ID                   string         `json:"id,omitempty" jsonschema:"Pipeline flow node id"`
	Name                 string         `json:"name,omitempty" jsonschema:"Pipeline flow node name"`
	Status               PipelineStatus `json:"status,omitempty" jsonschema:"Pipeline flow node status"`
	ParameterDescription string         `json:"parameterDescription,omitempty" jsonschema:"Pipeline flow node parameter description"`
	StartTime            int64          `json:"startTimeMillis,omitempty" jsonschema:"Pipeline flow node start time in Unix epoch milliseconds"`
	DurationMS           int64          `json:"durationMillis,omitempty" jsonschema:"Pipeline flow node duration in milliseconds"`
	PauseMS              int64          `json:"pauseMillis,omitempty" jsonschema:"Pipeline flow node paused duration in milliseconds"`
	ParentNodes          []string       `json:"parentNodes,omitempty" jsonschema:"Parent Pipeline flow node ids"`
	HasLog               bool           `json:"hasLog" jsonschema:"Whether this Pipeline flow node has log output"`
}

type PipelineNodeLog struct {
	NodeID     string         `json:"nodeId" jsonschema:"Pipeline flow node id"`
	NodeStatus PipelineStatus `json:"nodeStatus,omitempty" jsonschema:"Pipeline flow node status"`
	Text       string         `json:"text,omitempty" jsonschema:"Pipeline node log text"`
	Length     int64          `json:"length" jsonschema:"Number of log bytes returned"`
	HasMore    bool           `json:"hasMore" jsonschema:"Whether more node log output may be available"`
	Truncated  bool           `json:"truncated" jsonschema:"Whether node log output was truncated by limits"`
}

type ArtifactContent struct {
	RelativePath string `json:"relativePath" jsonschema:"Artifact path relative to the build artifact root"`
	Text         string `json:"text,omitempty" jsonschema:"Inline text content for the artifact"`
	Bytes        int    `json:"bytes" jsonschema:"Number of artifact bytes returned inline"`
	Inline       bool   `json:"inline" jsonschema:"Whether artifact content was returned inline"`
	Truncated    bool   `json:"truncated" jsonschema:"Whether artifact content was truncated by limits"`
}

type CoverageReport struct {
	Available        bool                    `json:"available" jsonschema:"Whether coverage data was found at any probed endpoint"`
	CheckedEndpoints []string                `json:"checkedEndpoints,omitempty" jsonschema:"Jenkins coverage endpoints checked in deterministic probing order"`
	Summaries        []CoverageSummary       `json:"summaries,omitempty" jsonschema:"Coverage summaries returned by Jenkins coverage-related plugin endpoints"`
	Errors           []CoverageEndpointError `json:"errors,omitempty" jsonschema:"Non-fatal coverage endpoint errors encountered while probing optional coverage data"`
}

type CoverageSummary struct {
	Source         string           `json:"source" jsonschema:"Stable coverage source identifier inferred from the Jenkins endpoint"`
	Endpoint       string           `json:"endpoint" jsonschema:"Jenkins endpoint that returned this coverage summary"`
	TopLevelFields []string         `json:"topLevelFields,omitempty" jsonschema:"Top-level JSON fields returned by the coverage endpoint, useful for identifying plugin response shape"`
	Metrics        []CoverageMetric `json:"metrics,omitempty" jsonschema:"Normalized coverage metrics discovered in the endpoint response"`
	HealthReports  []CoverageHealth `json:"healthReports,omitempty" jsonschema:"Jenkins health report entries returned with coverage data"`
	Details        []CoverageDetail `json:"details,omitempty" jsonschema:"Bounded typed fallback details for useful coverage fields that are not full metric objects"`
}

type CoverageMetric struct {
	Name       string   `json:"name" jsonschema:"Coverage metric name or type, such as line, branch, instruction, or class"`
	Covered    *float64 `json:"covered,omitempty" jsonschema:"Covered count for this metric when reported"`
	Missed     *float64 `json:"missed,omitempty" jsonschema:"Missed count for this metric when reported"`
	Total      *float64 `json:"total,omitempty" jsonschema:"Total count for this metric when reported or derivable"`
	Percentage *float64 `json:"percentage,omitempty" jsonschema:"Coverage percentage for this metric when reported or derivable"`
	Delta      *float64 `json:"delta,omitempty" jsonschema:"Coverage delta for this metric when reported"`
	Status     string   `json:"status,omitempty" jsonschema:"Coverage status or quality gate state for this metric when reported"`
}

type CoverageHealth struct {
	Description string `json:"description,omitempty" jsonschema:"Human-readable Jenkins health report description"`
	Score       *int   `json:"score,omitempty" jsonschema:"Jenkins health report score when reported"`
}

type CoverageDetail struct {
	Name  string `json:"name" jsonschema:"Coverage detail field name"`
	Value string `json:"value" jsonschema:"Bounded string representation of a useful non-metric coverage detail"`
}

type CoverageEndpointError struct {
	Endpoint string `json:"endpoint" jsonschema:"Jenkins coverage endpoint that failed while probing optional coverage data"`
	Code     string `json:"code" jsonschema:"Machine-readable error code for the non-fatal coverage endpoint failure"`
	Message  string `json:"message" jsonschema:"Human-readable error message for the non-fatal coverage endpoint failure"`
}

type IssuesSummary struct {
	Available        bool               `json:"available" jsonschema:"Whether Warnings NG data was found for the build"`
	Endpoint         string             `json:"endpoint,omitempty" jsonschema:"Jenkins endpoint that returned Warnings NG discovery data"`
	CheckedEndpoints []string           `json:"checkedEndpoints,omitempty" jsonschema:"Jenkins Warnings NG endpoints checked"`
	Tools            []IssueToolSummary `json:"tools,omitempty" jsonschema:"Warnings NG tools discovered for this build"`
	Message          string             `json:"message,omitempty" jsonschema:"Human-readable explanation when Warnings NG data is unavailable or empty"`
}

type IssueToolSummary struct {
	ID          string `json:"id,omitempty" jsonschema:"Warnings NG tool id used to request issue details"`
	Name        string `json:"name,omitempty" jsonschema:"Human-readable Warnings NG tool name"`
	URL         string `json:"url,omitempty" jsonschema:"Tool result URL or relative path reported by Jenkins"`
	LatestURL   string `json:"latestUrl,omitempty" jsonschema:"Latest tool result URL reported by Jenkins, when present"`
	Total       int    `json:"total,omitempty" jsonschema:"Total number of issues reported for this tool, when available"`
	New         int    `json:"new,omitempty" jsonschema:"Number of new issues reported for this tool, when available"`
	Fixed       int    `json:"fixed,omitempty" jsonschema:"Number of fixed issues reported for this tool, when available"`
	Outstanding int    `json:"outstanding,omitempty" jsonschema:"Number of outstanding issues reported for this tool, when available"`
	Error       int    `json:"error,omitempty" jsonschema:"Number of error-severity issues reported for this tool, when available"`
	High        int    `json:"high,omitempty" jsonschema:"Number of high-severity issues reported for this tool, when available"`
	Normal      int    `json:"normal,omitempty" jsonschema:"Number of normal-severity issues reported for this tool, when available"`
	Low         int    `json:"low,omitempty" jsonschema:"Number of low-severity issues reported for this tool, when available"`
}

type Issue struct {
	Severity    string `json:"severity,omitempty" jsonschema:"Issue severity reported by Warnings NG"`
	Category    string `json:"category,omitempty" jsonschema:"Issue category reported by Warnings NG"`
	Type        string `json:"type,omitempty" jsonschema:"Issue type reported by Warnings NG"`
	Message     string `json:"message,omitempty" jsonschema:"Issue message or description"`
	Description string `json:"description,omitempty" jsonschema:"Detailed issue description reported by Warnings NG, when available"`
	File        string `json:"file,omitempty" jsonschema:"Source file path associated with the issue"`
	BaseName    string `json:"baseName,omitempty" jsonschema:"Base source file name associated with the issue, when available"`
	Package     string `json:"package,omitempty" jsonschema:"Package or namespace associated with the issue"`
	Module      string `json:"module,omitempty" jsonschema:"Module associated with the issue"`
	Line        int    `json:"line,omitempty" jsonschema:"Source line number associated with the issue"`
	LineEnd     int    `json:"lineEnd,omitempty" jsonschema:"Ending source line number associated with the issue, when available"`
	ColumnStart int    `json:"columnStart,omitempty" jsonschema:"Starting source column number associated with the issue, when available"`
	ColumnEnd   int    `json:"columnEnd,omitempty" jsonschema:"Ending source column number associated with the issue, when available"`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"Stable Warnings NG issue fingerprint, when available"`
	Reference   string `json:"reference,omitempty" jsonschema:"Reference URL or identifier associated with the issue, when available"`
	Origin      string `json:"origin,omitempty" jsonschema:"Warnings NG origin or tool id associated with the issue, when available"`
	OriginName  string `json:"originName,omitempty" jsonschema:"Human-readable Warnings NG origin name associated with the issue, when available"`
	AuthorName  string `json:"authorName,omitempty" jsonschema:"SCM author name associated with the issue, when available"`
	AuthorEmail string `json:"authorEmail,omitempty" jsonschema:"SCM author email associated with the issue, when available"`
	Commit      string `json:"commit,omitempty" jsonschema:"SCM commit associated with the issue, when available"`
	AddedAt     int    `json:"addedAt,omitempty" jsonschema:"Warnings NG added-at build number or timestamp associated with the issue, when available"`
}

type IssuesPage struct {
	Available        bool               `json:"available" jsonschema:"Whether Warnings NG issue data was available"`
	Endpoint         string             `json:"endpoint,omitempty" jsonschema:"Jenkins endpoint used to return this issue page"`
	CheckedEndpoints []string           `json:"checkedEndpoints,omitempty" jsonschema:"Jenkins Warnings NG endpoints checked"`
	Tools            []IssueToolSummary `json:"tools,omitempty" jsonschema:"Warnings NG tools discovered for this build"`
	Items            []Issue            `json:"items,omitempty" jsonschema:"Warnings NG issues returned for this page"`
	Message          string             `json:"message,omitempty" jsonschema:"Human-readable explanation when Warnings NG data is unavailable or empty"`
}

type BuildWatch struct {
	State    string       `json:"state,omitempty" jsonschema:"Opaque state token to pass as lastState on the next watch call"`
	Build    BuildSummary `json:"build" jsonschema:"Current build summary"`
	Pipeline *PipelineRun `json:"pipeline,omitempty" jsonschema:"Current Pipeline run, stage state, and pending input-step state, when available"`
	Complete bool         `json:"complete" jsonschema:"Whether the watched build has completed"`
	TimedOut bool         `json:"timedOut" jsonschema:"Whether the watch call returned because the wait timeout elapsed"`
}
