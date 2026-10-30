//go:build !no_integration

package mcpserver

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/david/jenkins-mcp/internal/config"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/testutil/jenkinscontainer"
	jenkinstools "github.com/david/jenkins-mcp/internal/tools/jenkins"
	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestIntegrationPipelineInput(t *testing.T) {
	ctx, _, _ := testhandler.SetupTestHandler(t)
	r := require.New(t)
	fixture := jenkinscontainer.Start(t)
	_, api := fixture.Controller(t, "admin")
	session, cleanup := connectIntegrationMCPWithConfig(t, fixture, "admin", func(cfg *config.Config) { cfg.Mutations.Enabled = true })
	defer cleanup()
	_, err := api.TriggerBuild(ctx, "example-input", nil)
	r.NoError(err)
	waitInput := func(id string) jenkinstools.PipelineInputsResponse {
		var inputs []model.PipelineInput
		r.Eventually(func() bool {
			var err error
			inputs, err = api.PipelineInputs(ctx, "example-input", 1)
			return err == nil && len(inputs) == 1 && inputs[0].ID == id
		}, 45*time.Second, 200*time.Millisecond, "wait for input %s", id)
		got := callIntegrationTool[jenkinstools.PipelineInputsResponse](t, session, "jenkins_get_pipeline_inputs", map[string]any{"job": "example-input", "build": 1})
		r.Len(got.Inputs, 1)
		r.Equal(id, got.Inputs[0].ID)
		return got
	}
	waitInput("Approve")
	args := map[string]any{"job": "example-input", "build": 1, "inputId": "Approve"}
	readSession, readCleanup := connectIntegrationMCPWithConfig(t, fixture, "read-only", func(cfg *config.Config) { cfg.Mutations.Enabled = true })
	defer readCleanup()
	denied, err := readSession.CallTool(ctx, &mcp.CallToolParams{Name: "jenkins_proceed_pipeline_input", Arguments: args})
	r.NoError(err)
	r.True(denied.IsError)
	readInputs := callIntegrationTool[jenkinstools.PipelineInputsResponse](t, readSession, "jenkins_get_pipeline_inputs", map[string]any{"job": "example-input", "build": 1})
	r.Len(readInputs.Inputs, 1)
	restrictedSession, restrictedCleanup := connectIntegrationMCPWithConfig(t, fixture, "build-only", func(cfg *config.Config) { cfg.Mutations.Enabled = true })
	defer restrictedCleanup()
	restricted, err := restrictedSession.CallTool(ctx, &mcp.CallToolParams{Name: "jenkins_proceed_pipeline_input", Arguments: args})
	r.NoError(err)
	r.True(restricted.IsError, "Job/Build permission must not bypass the submitter restriction")
	result := callIntegrationTool[model.PipelineInputResult](t, session, "jenkins_proceed_pipeline_input", args)
	r.True(result.Accepted)
	stale, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "jenkins_proceed_pipeline_input", Arguments: args})
	r.NoError(err)
	r.True(stale.IsError)
	got := waitInput("Values")
	r.Len(got.Inputs[0].Parameters, 5)
	for _, p := range got.Inputs[0].Parameters {
		if p.Name == "REGION" {
			r.Equal([]string{"east", "west"}, p.Choices)
		}
	}
	raw, err := json.Marshal(got)
	r.NoError(err)
	r.NotContains(string(raw), "fixture-default-secret")
	for _, p := range got.Inputs[0].Parameters {
		r.True(p.Supported, "parameter %s type %s", p.Name, p.Type)
	}
	args["inputId"] = "Values"
	args["parameters"] = map[string]any{"TARGET": "test-target", "CONFIRM": true, "REGION": "west", "NOTES": "line one\nline two", "SECRET": "fixture-submitted-secret"}
	result = callIntegrationTool[model.PipelineInputResult](t, session, "jenkins_proceed_pipeline_input", args)
	r.True(result.Accepted)
	waitInput("Reject")
	delete(args, "parameters")
	args["inputId"] = "Reject"
	result = callIntegrationTool[model.PipelineInputResult](t, session, "jenkins_abort_pipeline_input", args)
	r.True(result.Accepted)
	r.Eventually(func() bool {
		build, err := api.GetBuild(ctx, "example-input", 1)
		return err == nil && build.Result == model.BuildResultAborted
	}, 30*time.Second, 200*time.Millisecond)
}
