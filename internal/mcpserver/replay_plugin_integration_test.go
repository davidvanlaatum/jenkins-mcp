//go:build !no_integration

package mcpserver

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/testutil/jenkinscontainer"
	jenkinstools "github.com/david/jenkins-mcp/internal/tools/jenkins"
	"github.com/stretchr/testify/require"
)

func TestIntegrationReplayPlugin(t *testing.T) {
	loggerCtx, _, _ := testhandler.SetupTestHandler(t)

	r := require.New(t)
	fixture := jenkinscontainer.Start(t)
	cfg, api := fixture.Controller(t, "admin")
	_, err := api.TriggerBuild(loggerCtx, "example-replay-loaded", nil)
	r.NoError(err)
	build := jenkinscontainer.WaitForBuildResult(t, api, "example-replay-loaded", model.BuildResultSuccess)
	client, err := jenkinsclient.New(loggerCtx, cfg.Controllers[0])
	r.NoError(err)
	// Establish that the real plugin is installed and advertises the tool,
	// rather than accidentally passing through the HTML compatibility backend.
	result, available, err := client.CallMCPTool(loggerCtx, "getReplayScripts", map[string]any{"jobFullName": "example-replay-loaded", "buildNumber": build})
	r.NoError(err)
	r.True(available, "fixture must provide getReplayScripts")
	r.NotEmpty(result)

	session, cleanup := connectIntegrationMCPWithConfig(t, fixture, "admin", func(cfg *config.Config) { cfg.Mutations.Enabled = true })
	defer cleanup()
	scripts := callIntegrationTool[jenkinstools.ReplayScriptsResponse](t, session, "jenkins_get_replay_scripts", map[string]any{"job": "example-replay-loaded", "build": build})
	r.Len(scripts.Scripts.Scripts, 3)
	var overrideKey string
	for _, script := range scripts.Scripts.Scripts {
		if script.Kind == "loaded" && strings.Contains(script.Content, "loaded one original") {
			overrideKey = script.OverrideKey
		}
	}
	r.NotEmpty(overrideKey)
	replay := callIntegrationTool[jenkinstools.ReplayBuildResponse](t, session, "jenkins_replay_build", map[string]any{
		"job": "example-replay-loaded", "build": build,
		"loadedScriptOverrides": map[string]string{overrideKey: "echo 'loaded one replacement'"},
	})
	r.True(replay.Replay.Replayed)
	r.False(replay.Replay.MainScriptOverridden)
	r.Nil(replay.Replay.ScheduledBuild)
	r.Equal("mcp_plugin", replay.Replay.Backend)
	r.True(replay.Replay.QueueAuthoritative)
	r.Greater(replay.Replay.QueueID, int64(0))
	replayBuild := resolveReplayQueue(t, session, replay.Replay.QueueID)
	jenkinscontainer.WaitForBuildNumberResult(t, api, "example-replay-loaded", replayBuild.Build, model.BuildResultSuccess)
	originalMain, originals, _, _, err := api.ReplayScripts(loggerCtx, "example-replay-loaded", build)
	r.NoError(err)
	replayedMain, replayedLoaded, _, _, err := api.ReplayScripts(loggerCtx, "example-replay-loaded", replayBuild.Build)
	r.NoError(err)
	r.Equal(originalMain, replayedMain, "omitted main override uses original source")
	originals[overrideKey] = "echo 'loaded one replacement'"
	r.Equal(originals, replayedLoaded, "only the selected loaded script changes")

	_, limited := fixture.Controller(t, "no-config-access")
	_, _, _, _, err = limited.ReplayScripts(loggerCtx, "example-replay-loaded", build)
	var appErr *apperrors.Error
	r.ErrorAs(err, &appErr)
	r.Equal(apperrors.CodePermissionDenied, appErr.Code)

	forcedSession, forcedCleanup := connectIntegrationMCPWithConfig(t, fixture, "admin", func(cfg *config.Config) {
		cfg.Mutations.Enabled = true
		cfg.Controllers[0].MCPPluginMode = "off"
	})
	defer forcedCleanup()
	forcedScripts := callIntegrationTool[jenkinstools.ReplayScriptsResponse](t, forcedSession, "jenkins_get_replay_scripts", map[string]any{"job": "example-replay-loaded", "build": build})
	r.Equal(scripts, forcedScripts, "forced-off mode uses real HTML even while the MCP plugin is available")
	_, available, err = client.CallMCPTool(loggerCtx, "getReplayScripts", map[string]any{"jobFullName": "example-replay-loaded", "buildNumber": build})
	r.NoError(err)
	r.True(available, "forced-off selection must not change the real plugin")

	// Disable the real plugin transport on this disposable controller. Jenkins
	// itself now returns 404; all Replay HTML and scheduling requests remain real.
	status, _, _, err := client.Post(loggerCtx, "scriptText", nil, url.Values{"script": {"io.jenkins.plugins.mcp.server.Endpoint.DISABLE_MCP_STREAMABLE = true; println('disabled')"}})
	r.NoError(err)
	r.Equal(http.StatusOK, status)
	_, available, err = client.CallMCPTool(loggerCtx, "getReplayScripts", map[string]any{"jobFullName": "example-replay-loaded", "buildNumber": build})
	r.NoError(err)
	r.False(available, "the real controller returns a missing MCP endpoint")
	htmlScripts := callIntegrationTool[jenkinstools.ReplayScriptsResponse](t, session, "jenkins_get_replay_scripts", map[string]any{"job": "example-replay-loaded", "build": build})
	r.Equal(scripts, htmlScripts, "HTML fallback returns exactly the plugin script identifiers, contents, digests and sizes")
	htmlReplay := callIntegrationTool[jenkinstools.ReplayBuildResponse](t, session, "jenkins_replay_build", map[string]any{
		"job": "example-replay-loaded", "build": build,
		"loadedScriptOverrides": map[string]string{overrideKey: "echo 'loaded one HTML replacement'"},
	})
	r.True(htmlReplay.Replay.Replayed)
	r.NotNil(htmlReplay.Replay.ScheduledBuild)
	r.Equal("native", htmlReplay.Replay.Backend)
	r.True(htmlReplay.Replay.ScheduledBuildPredicted)
	jenkinscontainer.WaitForBuildNumberResult(t, api, "example-replay-loaded", htmlReplay.Replay.ScheduledBuild.Build, model.BuildResultSuccess)
	htmlMain, htmlLoaded, _, _, err := api.ReplayScripts(loggerCtx, "example-replay-loaded", htmlReplay.Replay.ScheduledBuild.Build)
	r.NoError(err)
	r.Equal(originalMain, htmlMain)
	originals[overrideKey] = "echo 'loaded one HTML replacement'"
	r.Equal(originals, htmlLoaded, "HTML-backed replay preserves omitted originals and applies the loaded override")
	t.Log("Both real Jenkins backends return identical script metadata and successfully replay loaded-script-only overrides")
}

func resolveReplayQueue(t *testing.T, session *mcp.ClientSession, id int64) model.BuildReference {
	t.Helper()
	r := require.New(t)
	var resolved *model.BuildReference
	state := ""
	r.Eventually(func() bool {
		watch := callIntegrationTool[jenkinstools.WatchQueueItemResponse](t, session, "jenkins_watch_queue_item", map[string]any{"id": id, "lastState": state, "waitTimeoutMs": 1000})
		state = watch.Watch.State
		resolved = watch.Watch.Build
		return resolved != nil
	}, 60*time.Second, time.Millisecond, "authoritative queue must resolve to a build")
	return *resolved
}
