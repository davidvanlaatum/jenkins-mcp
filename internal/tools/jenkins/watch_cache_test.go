package jenkins

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/stretchr/testify/require"
)

func TestWatchCacheLRUAndImmutableSnapshots(t *testing.T) {
	r := require.New(t)
	c := newWatchCache(2, 1024)
	a, err := c.put("build", map[string]string{"value": "a"})
	r.NoError(err)
	r.Len(a, 22)
	b, err := c.put("queue", map[string]string{"value": "b"})
	r.NoError(err)
	var got map[string]string
	r.NoError(c.get(a, "build", &got))
	got["value"] = "mutated"
	again, err := c.put("build", map[string]string{"value": "a"})
	r.NoError(err)
	r.Equal(a, again)
	_, err = c.put("build", map[string]string{"value": "c"})
	r.NoError(err)
	r.Error(c.get(b, "queue", &got))
	r.NoError(c.get(a, "build", &got))
	r.Equal("a", got["value"])
	r.Error(c.get(a, "queue", &got))
	r.Error(newWatchCache(2, 1024).get(a, "build", &got), "restart loses IDs")
}

func TestWatchCacheByteLimit(t *testing.T) {
	r := require.New(t)
	c := newWatchCache(10, 10)
	a, err := c.put("build", "1234")
	r.NoError(err)
	_, err = c.put("build", "5678")
	r.NoError(err)
	var got string
	r.Error(c.get(a, "build", &got))
	r.LessOrEqual(c.bytes, 10)
	_, err = c.put("build", "01234567890")
	r.Error(err)
}

func TestWatchCacheConcurrentAccess(t *testing.T) {
	c := newWatchCache(10, 1024)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			id, err := c.put("build", "same snapshot")
			if err != nil {
				t.Error(err)
				return
			}
			var got string
			if err := c.get(id, "build", &got); err != nil {
				t.Error(err)
			}
			if got != "same snapshot" {
				t.Errorf("unexpected snapshot %q", got)
			}
		})
	}
	wg.Wait()
	require.New(t).Equal(1, c.lru.Len())
}

func TestWatchBuildDefaultIgnoresStagesUntilCompletionOrTimeout(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprintf("complete=%v", complete), func(t *testing.T) {
			r := require.New(t)
			polls := 0
			deps := newWatchTestDeps(t, func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/job/app/42/api/json":
					polls++
					building := !complete || polls < 4
					result := ""
					if !building {
						result = "SUCCESS"
					}
					writeJSON(w, fmt.Sprintf(`{"number":42,"url":"https://jenkins.example.com/job/app/42/","building":%v,"result":%q}`, building, result))
				case "/job/app/42/wfapi/describe":
					status := "IN_PROGRESS"
					if polls > 1 {
						status = "SUCCESS"
					}
					writeJSON(w, fmt.Sprintf(`{"id":"42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":%q}]}`, status))
				default:
					http.NotFound(w, req)
				}
			}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 100, MaxWaitTimeoutMs: 100, MaxConsecutiveFailures: 3})
			first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
			r.NoError(err)
			r.Len(first.Watch.State, 22)
			second, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42, LastState: first.Watch.State})
			r.NoError(err)
			r.Equal(complete, second.Watch.Complete)
			r.Equal(!complete, second.Watch.TimedOut)
			if complete {
				r.NotNil(second.Watch.Build)
				r.Equal(first.Watch.Build.URL, second.Watch.Build.URL)
			} else {
				assertCompactWatchTimeout(t, second)
				cached, err := decodeWatchState(second.Watch.State)
				r.NoError(err)
				r.Equal(first.Watch.Build.URL, cached.Summary.URL)
				r.Equal("SUCCESS", string(cached.Stages[0].Status))
				resumed, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42, LastState: second.Watch.State, WaitFor: "change"})
				r.NoError(err)
				r.True(resumed.Watch.TimedOut, "resume uses latest cached stages")
			}
			r.NotEqual(first.Watch.State, second.Watch.State)
		})
	}
}

func TestWatchBuildRejectsInvalidWaitMode(t *testing.T) {
	_, err := WatchBuild(t.Context(), Deps{}, WatchBuildRequest{Job: "app", Build: 42, WaitFor: "bogus"})
	require.New(t).Error(err)
	assertAppErrorCode(t, err, apperrors.CodeInvalidRequest)
}

func TestWatchCacheExpiresWithoutRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := require.New(t)
		c := newWatchCache(10, 1024)
		id, err := c.put("build", "snapshot")
		r.NoError(err)
		time.Sleep(30 * time.Minute)
		synctest.Wait()
		c.mu.Lock()
		entries, ids, contents, bytes := c.lru.Len(), len(c.byID), len(c.byContent), c.bytes
		c.mu.Unlock()
		r.Zero(entries)
		r.Zero(ids)
		r.Zero(contents)
		r.Zero(bytes)
		var got string
		err = c.get(id, "build", &got)
		r.Error(err)
		r.Contains(err.Error(), "re-bootstrap")
		replacement, err := c.put("build", "snapshot")
		r.NoError(err)
		r.NotEqual(id, replacement, "expired content must receive a new ID")
	})
}

func TestWatchCacheRefreshesIdleExpiry(t *testing.T) {
	for _, access := range []string{"get", "put"} {
		t.Run(access, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := require.New(t)
				c := newWatchCache(10, 1024)
				id, err := c.put("build", "snapshot")
				r.NoError(err)
				time.Sleep(20 * time.Minute)
				var got string
				if access == "get" {
					r.NoError(c.get(id, "build", &got))
				} else {
					same, err := c.put("build", "snapshot")
					r.NoError(err)
					r.Equal(id, same)
				}
				time.Sleep(10 * time.Minute)
				synctest.Wait()
				c.mu.Lock()
				entries := c.lru.Len()
				c.mu.Unlock()
				r.Equal(1, entries, "use extends idle lifetime")
				time.Sleep(20 * time.Minute)
				synctest.Wait()
				r.Error(c.get(id, "build", &got), "expires 30 minutes after last use")
			})
		})
	}
}

func TestWatchBuildPreservesLatestObservationAcrossPollingFailures(t *testing.T) {
	for _, scenario := range []struct{ complete, initialInput bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		complete, initialInput := scenario.complete, scenario.initialInput
		t.Run(fmt.Sprintf("complete=%v/input=%v", complete, initialInput), func(t *testing.T) {
			r := require.New(t)
			polls := 0
			deps := newWatchTestDeps(t, func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/job/app/42/api/json":
					polls++
					if !complete && polls >= 3 {
						http.Error(w, "temporary failure", http.StatusBadGateway)
						return
					}
					building := polls < 3
					result := ""
					if !building {
						result = "SUCCESS"
					}
					writeJSON(w, fmt.Sprintf(`{"number":42,"url":"https://jenkins.example.com/job/app/42/","building":%v,"result":%q}`, building, result))
				case "/job/app/42/wfapi/describe":
					if polls >= 3 {
						http.Error(w, "temporary wfapi failure", http.StatusBadGateway)
						return
					}
					status := "IN_PROGRESS"
					if polls >= 2 {
						status = "SUCCESS"
					}
					writeJSON(w, fmt.Sprintf(`{"id":"42","status":"IN_PROGRESS","stages":[{"id":"1","name":"Build","status":%q}]}`, status))
				case "/job/app/42/wfapi/pendingInputActions":
					if initialInput && polls == 1 {
						writeJSON(w, `[{"id":"approve","message":"Continue?","proceedUrl":"input/approve/proceedEmpty"}]`)
					} else {
						writeJSON(w, `[]`)
					}
				default:
					http.NotFound(w, req)
				}
			}, config.WatchConfig{PollIntervalMs: 5, DefaultWaitTimeoutMs: 50, MaxWaitTimeoutMs: 50, MaxConsecutiveFailures: 100})
			first, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42})
			r.NoError(err)
			second, err := WatchBuild(t.Context(), deps, WatchBuildRequest{Job: "app", Build: 42, LastState: first.Watch.State})
			r.NoError(err)
			r.Equal(complete, second.Watch.Complete)
			r.Equal(!complete, second.Watch.TimedOut)
			cached, err := decodeWatchState(second.Watch.State)
			r.NoError(err)
			r.Empty(cached.Inputs, "do not resurrect previously cleared input")
			r.False(cached.Run.WaitingForInput)
			r.Len(cached.Stages, 1)
			r.Equal(model.PipelineStatusSuccess, cached.Stages[0].Status, "retain observed success after polling fails")
			if complete {
				r.NotNil(second.Watch.Pipeline)
				r.Equal(model.PipelineStatusSuccess, second.Watch.Pipeline.Stages[0].Status)
			} else {
				assertCompactWatchTimeout(t, second)
			}
		})
	}
}
