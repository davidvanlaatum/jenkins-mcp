package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListNodes(t *testing.T) {
	r := require.New(t)
	api := newTestAPI(t, func(w http.ResponseWriter, req *http.Request) {
		r.Equal("/computer/api/json", req.URL.Path)
		r.Equal(nodesTree, req.URL.Query().Get("tree"))
		_, _ = w.Write([]byte(`{"computer":[{
		"displayName":"worker","url":"http://jenkins/computer/worker/","offline":false,"numExecutors":2,
		"assignedLabels":[{"name":"linux"}],
		"executors":[{"number":0,"idle":false,"likelyStuck":true,"progress":41,"currentExecutable":{"number":7,"url":"http://jenkins/job/app/7/","fullDisplayName":"app #7","building":true,"timestamp":1234,"duration":300,"estimatedDuration":500,"queueId":12}}, {"number":1,"idle":true,"progress":-1,"currentExecutable":null}],
		"oneOffExecutors":[{"number":-1,"idle":false,"progress":-1,"currentExecutable":{"url":"http://jenkins/job/pipeline/8/","number":8}}],
		"monitorData":{"hudson.node_monitors.ArchitectureMonitor":"Linux (aarch64)","hudson.node_monitors.DiskSpaceMonitor":{"size":0,"path":"private"},"hudson.node_monitors.TemporarySpaceMonitor":{"size":100},"hudson.node_monitors.ResponseTimeMonitor":{"average":3},"hudson.node_monitors.SwapSpaceMonitor":{"totalPhysicalMemory":800,"availablePhysicalMemory":400,"totalSwapSpace":20,"availableSwapSpace":10},"unknown.monitor":{"secret":"omit"}}
		},{"displayName":"offline","offline":true,"temporarilyOffline":true,"offlineCauseReason":"maintenance","numExecutors":1,"monitorData":{}}]}`))
	})
	nodes, err := api.ListNodes(t.Context())
	r.NoError(err)
	r.Len(nodes, 2)
	node := nodes[0]
	r.Equal("worker", node.DisplayName)
	r.Equal("http://jenkins/computer/worker/", node.URL)
	r.False(node.Offline)
	r.Equal(2, node.ConfiguredExecutors)
	r.Equal(1, node.BusyExecutors)
	r.Equal(1, node.IdleExecutors)
	r.Equal(1, node.OneOffExecutorCount)
	r.Equal([]string{"linux"}, node.Labels)
	r.Len(node.Executors, 2)
	r.True(node.Executors[0].LikelyStuck)
	r.Equal(41, node.Executors[0].Progress)
	executable := node.Executors[0].CurrentExecutable
	r.NotNil(executable)
	r.Equal(7, executable.Number)
	r.Equal("app #7", executable.FullDisplayName)
	r.True(executable.Building)
	r.Equal(int64(1234), executable.Timestamp)
	r.Equal(int64(300), executable.Duration)
	r.Equal(int64(500), executable.EstimatedDuration)
	r.Equal(int64(12), executable.QueueID)
	r.Nil(node.Executors[1].CurrentExecutable)
	r.Equal(8, node.OneOffExecutors[0].CurrentExecutable.Number)
	r.NotNil(node.Monitor)
	r.Equal("Linux (aarch64)", node.Monitor.Architecture)
	r.NotNil(node.Monitor.DiskFreeBytes)
	r.Zero(*node.Monitor.DiskFreeBytes, "zero measurement must be retained")
	r.Equal(int64(100), *node.Monitor.TempFreeBytes)
	r.Equal(int64(3), *node.Monitor.ResponseTimeMillis)
	r.Equal(int64(800), *node.Monitor.TotalPhysicalMemoryBytes)
	r.Equal(int64(400), *node.Monitor.AvailablePhysicalMemoryBytes)
	r.Equal(int64(20), *node.Monitor.TotalSwapBytes)
	r.Equal(int64(10), *node.Monitor.AvailableSwapBytes)
	r.True(nodes[1].Offline)
	r.True(nodes[1].TemporarilyOffline)
	r.Equal("maintenance", nodes[1].OfflineReason)
	r.Nil(nodes[1].Monitor)
	r.NotNil(nodes[1].Labels)
	r.NotNil(nodes[1].Executors)
	r.NotNil(nodes[1].OneOffExecutors)
}

func TestListNodesOptionalMonitors(t *testing.T) {
	for _, data := range []string{`{}`, `null`, `{"unknown":{"nested":"ignored"}}`, `{"hudson.node_monitors.DiskSpaceMonitor":{"size":"unavailable"}}`} {
		t.Run(data, func(t *testing.T) {
			r := require.New(t)
			api := newTestAPI(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"computer":[{"displayName":"worker","monitorData":` + data + `}]}`))
			})
			nodes, err := api.ListNodes(t.Context())
			r.NoError(err)
			r.Len(nodes, 1)
			r.Nil(nodes[0].Monitor)
		})
	}
}

func TestListNodesEmptyAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantError  bool
	}{
		{"empty", `{"computer":[]}`, 200, false},
		{"forbidden", `not authorized`, 403, true},
		{"malformed", `{`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			api := newTestAPI(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			nodes, err := api.ListNodes(t.Context())
			if tc.wantError {
				r.Error(err)
				r.Nil(nodes)
			} else {
				r.NoError(err)
				r.NotNil(nodes)
				r.Empty(nodes)
			}
		})
	}
}
