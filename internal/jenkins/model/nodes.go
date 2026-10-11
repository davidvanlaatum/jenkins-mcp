package model

type Node struct {
	DisplayName         string       `json:"displayName" jsonschema:"Jenkins computer display name"`
	URL                 string       `json:"url,omitempty" jsonschema:"Jenkins computer URL when reported"`
	Offline             bool         `json:"offline" jsonschema:"Whether the computer is offline"`
	TemporarilyOffline  bool         `json:"temporarilyOffline" jsonschema:"Whether the computer is temporarily marked offline"`
	OfflineReason       string       `json:"offlineReason,omitempty" jsonschema:"Jenkins explanation of the offline state"`
	ConfiguredExecutors int          `json:"configuredExecutors" jsonschema:"Configured number of regular executors"`
	BusyExecutors       int          `json:"busyExecutors" jsonschema:"Number of reported regular executors that are not idle"`
	IdleExecutors       int          `json:"idleExecutors" jsonschema:"Number of reported regular executors that are idle; offline executors are not necessarily available capacity"`
	OneOffExecutorCount int          `json:"oneOffExecutorCount" jsonschema:"Number of reported one-off executors, separate from regular executor counts"`
	Labels              []string     `json:"labels" jsonschema:"Assigned Jenkins label names"`
	Executors           []Executor   `json:"executors" jsonschema:"Regular executor state and current executable placement"`
	OneOffExecutors     []Executor   `json:"oneOffExecutors" jsonschema:"One-off executor state and current executable placement"`
	Monitor             *NodeMonitor `json:"monitor,omitempty" jsonschema:"Optional known monitor measurements; absent when unavailable or permission-filtered"`
}

type Executor struct {
	Number            int             `json:"number" jsonschema:"Executor number reported by Jenkins"`
	Idle              bool            `json:"idle" jsonschema:"Whether Jenkins reports the executor as idle"`
	LikelyStuck       bool            `json:"likelyStuck" jsonschema:"Whether Jenkins considers the executor likely stuck"`
	Progress          int             `json:"progress" jsonschema:"Jenkins progress percentage, or -1 when unknown"`
	CurrentExecutable *NodeExecutable `json:"currentExecutable,omitempty" jsonschema:"Current executable when exposed by Jenkins; some executable types expose only partial build metadata"`
}

type NodeExecutable struct {
	Number            int             `json:"number,omitempty" jsonschema:"Build number when the executable exposes it"`
	URL               string          `json:"url,omitempty" jsonschema:"Executable URL reported by Jenkins"`
	FullDisplayName   string          `json:"fullDisplayName,omitempty" jsonschema:"Executable display name including its job name when available"`
	Building          bool            `json:"building" jsonschema:"Whether Jenkins reports the executable as building"`
	Result            BuildResult     `json:"result,omitempty" jsonschema:"Build result when exposed and available"`
	Timestamp         int64           `json:"timestamp,omitempty" jsonschema:"Build start time in Unix epoch milliseconds when exposed"`
	Duration          int64           `json:"duration,omitempty" jsonschema:"Build duration in milliseconds when exposed"`
	EstimatedDuration int64           `json:"estimatedDuration,omitempty" jsonschema:"Estimated build duration in milliseconds when exposed"`
	QueueID           int64           `json:"queueId,omitempty" jsonschema:"Queue item id when exposed"`
	Build             *BuildReference `json:"build,omitempty" jsonschema:"Resolved configured-controller build reference when the executable URL matches a Jenkins build"`
}

type NodeMonitor struct {
	Architecture                 string `json:"architecture,omitempty" jsonschema:"Architecture reported by Jenkins' architecture monitor"`
	DiskFreeBytes                *int64 `json:"diskFreeBytes,omitempty" jsonschema:"Free bytes reported by Jenkins' disk space monitor"`
	TempFreeBytes                *int64 `json:"tempFreeBytes,omitempty" jsonschema:"Free bytes reported by Jenkins' temporary space monitor"`
	ResponseTimeMillis           *int64 `json:"responseTimeMillis,omitempty" jsonschema:"Average response time in milliseconds reported by Jenkins' response time monitor"`
	TotalPhysicalMemoryBytes     *int64 `json:"totalPhysicalMemoryBytes,omitempty" jsonschema:"Total physical memory in bytes reported by Jenkins' swap space monitor"`
	AvailablePhysicalMemoryBytes *int64 `json:"availablePhysicalMemoryBytes,omitempty" jsonschema:"Available physical memory in bytes reported by Jenkins' swap space monitor"`
	TotalSwapBytes               *int64 `json:"totalSwapBytes,omitempty" jsonschema:"Total swap space in bytes reported by Jenkins' swap space monitor"`
	AvailableSwapBytes           *int64 `json:"availableSwapBytes,omitempty" jsonschema:"Available swap space in bytes reported by Jenkins' swap space monitor"`
}
