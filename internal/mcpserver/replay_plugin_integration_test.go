//go:build !no_integration

package mcpserver

import (
	"strings"
	"testing"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/testutil/jenkinscontainer"
	jenkinstools "github.com/david/jenkins-mcp/internal/tools/jenkins"
	"github.com/stretchr/testify/require"
)

func TestIntegrationReplayPlugin(t *testing.T) {
	r := require.New(t)
	fixture := jenkinscontainer.Start(t)
	cfg, api := fixture.Controller(t, "admin")
	_, err := api.TriggerBuild(t.Context(), "example-replay-loaded", nil)
	r.NoError(err)
	build := jenkinscontainer.WaitForBuildResult(t, api, "example-replay-loaded", model.BuildResultSuccess)
	client, err := jenkinsclient.New(cfg.Controllers[0], nil)
	r.NoError(err)
	// Establish that the real plugin is installed and advertises the tool,
	// rather than accidentally passing through the HTML compatibility backend.
	result, available, err := client.CallMCPTool(t.Context(), "getReplayScripts", map[string]any{"jobFullName": "example-replay-loaded", "buildNumber": build})
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
	r.NotNil(replay.Replay.ScheduledBuild)
	jenkinscontainer.WaitForBuildNumberResult(t, api, "example-replay-loaded", replay.Replay.ScheduledBuild.Build, model.BuildResultSuccess)
	originalMain, originals, _, _, err := api.ReplayScripts(t.Context(), "example-replay-loaded", build)
	r.NoError(err)
	replayedMain, replayedLoaded, _, _, err := api.ReplayScripts(t.Context(), "example-replay-loaded", replay.Replay.ScheduledBuild.Build)
	r.NoError(err)
	r.Equal(originalMain, replayedMain, "omitted main override uses original source")
	originals[overrideKey] = "echo 'loaded one replacement'"
	r.Equal(originals, replayedLoaded, "only the selected loaded script changes")

	_, limited := fixture.Controller(t, "no-config-access")
	_, _, _, _, err = limited.ReplayScripts(t.Context(), "example-replay-loaded", build)
	var appErr *apperrors.Error
	r.ErrorAs(err, &appErr)
	r.Equal(apperrors.CodePermissionDenied, appErr.Code)
}
