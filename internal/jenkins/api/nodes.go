package api

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/david/jenkins-mcp/internal/jenkins/model"
)

const nodeExecutableTree = "number,url,fullDisplayName,building,result,timestamp,duration,estimatedDuration,queueId"
const nodeExecutorTree = "number,idle,likelyStuck,progress,currentExecutable[" + nodeExecutableTree + "]"

// Tree pruning applies to map values, not monitor map keys. Select only known
// measurement fields; primitive architecture values are preserved by Jenkins.
const nodeMonitorTree = "size,average,totalPhysicalMemory,availablePhysicalMemory,totalSwapSpace,availableSwapSpace"
const nodesTree = "computer[displayName,url,offline,temporarilyOffline,offlineCauseReason,numExecutors,assignedLabels[name],executors[" + nodeExecutorTree + "],oneOffExecutors[" + nodeExecutorTree + "],monitorData[" + nodeMonitorTree + "]]"

// ListNodes inspects Computer state rather than the configured node inventory.
func (a *API) ListNodes(ctx context.Context) ([]model.Node, error) {
	var raw struct {
		Computers []struct {
			DisplayName        string `json:"displayName"`
			URL                string `json:"url"`
			Offline            bool   `json:"offline"`
			TemporarilyOffline bool   `json:"temporarilyOffline"`
			OfflineReason      string `json:"offlineCauseReason"`
			NumExecutors       int    `json:"numExecutors"`
			Labels             []struct {
				Name string `json:"name"`
			} `json:"assignedLabels"`
			Executors       []model.Executor           `json:"executors"`
			OneOffExecutors []model.Executor           `json:"oneOffExecutors"`
			MonitorData     map[string]json.RawMessage `json:"monitorData"`
		} `json:"computer"`
	}
	if err := a.client.GetJSON(ctx, "computer/api/json", url.Values{"tree": {nodesTree}}, &raw); err != nil {
		return nil, err
	}
	nodes := make([]model.Node, 0, len(raw.Computers))
	for _, computer := range raw.Computers {
		node := model.Node{
			DisplayName: computer.DisplayName, URL: computer.URL, Offline: computer.Offline,
			TemporarilyOffline: computer.TemporarilyOffline, OfflineReason: computer.OfflineReason,
			ConfiguredExecutors: computer.NumExecutors, OneOffExecutorCount: len(computer.OneOffExecutors),
			Labels: make([]string, 0, len(computer.Labels)), Executors: computer.Executors,
			OneOffExecutors: computer.OneOffExecutors, Monitor: normalizeNodeMonitor(computer.MonitorData),
		}
		if node.Executors == nil {
			node.Executors = []model.Executor{}
		}
		if node.OneOffExecutors == nil {
			node.OneOffExecutors = []model.Executor{}
		}
		for _, label := range computer.Labels {
			node.Labels = append(node.Labels, label.Name)
		}
		for _, executor := range node.Executors {
			if executor.Idle {
				node.IdleExecutors++
			} else {
				node.BusyExecutors++
			}
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// Optional monitors vary with permissions and plugin versions. Decode each known
// measurement independently so an unavailable monitor cannot hide executor state.
func normalizeNodeMonitor(data map[string]json.RawMessage) *model.NodeMonitor {
	var monitor model.NodeMonitor
	_ = json.Unmarshal(data["hudson.node_monitors.ArchitectureMonitor"], &monitor.Architecture)
	var disk, temp struct {
		Size *int64 `json:"size"`
	}
	var response struct {
		Average *int64 `json:"average"`
	}
	var swap struct {
		TotalPhysical     *int64 `json:"totalPhysicalMemory"`
		AvailablePhysical *int64 `json:"availablePhysicalMemory"`
		TotalSwap         *int64 `json:"totalSwapSpace"`
		AvailableSwap     *int64 `json:"availableSwapSpace"`
	}
	if json.Unmarshal(data["hudson.node_monitors.DiskSpaceMonitor"], &disk) == nil {
		monitor.DiskFreeBytes = disk.Size
	}
	if json.Unmarshal(data["hudson.node_monitors.TemporarySpaceMonitor"], &temp) == nil {
		monitor.TempFreeBytes = temp.Size
	}
	if json.Unmarshal(data["hudson.node_monitors.ResponseTimeMonitor"], &response) == nil {
		monitor.ResponseTimeMillis = response.Average
	}
	if json.Unmarshal(data["hudson.node_monitors.SwapSpaceMonitor"], &swap) == nil {
		monitor.TotalPhysicalMemoryBytes, monitor.AvailablePhysicalMemoryBytes = swap.TotalPhysical, swap.AvailablePhysical
		monitor.TotalSwapBytes, monitor.AvailableSwapBytes = swap.TotalSwap, swap.AvailableSwap
	}
	if monitor == (model.NodeMonitor{}) {
		return nil
	}
	return &monitor
}
