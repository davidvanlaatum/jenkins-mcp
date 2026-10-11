//go:build !no_integration

package mcpserver

import (
	"testing"
	"time"

	"github.com/david/jenkins-mcp/internal/testutil"

	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/testutil/jenkinscontainer"
	jenkinstools "github.com/david/jenkins-mcp/internal/tools/jenkins"
	"github.com/stretchr/testify/require"
)

func TestIntegrationLockableResources(t *testing.T) {
	r := require.New(t)
	fixture := jenkinscontainer.Start(t)
	_, admin := fixture.Controller(t, "admin")
	build := jenkinscontainer.WaitForBuildResult(t, admin, "example-locks", model.BuildResultSuccess)
	session, cleanup := connectIntegrationMCP(t, fixture, "read-only")
	defer cleanup()

	listed := callIntegrationTool[jenkinstools.ListLockableResourcesResponse](t, session, "jenkins_list_lockable_resources", map[string]any{"label": "mobile"})
	r.Len(listed.Resources, 2)
	r.Len(listed.Labels, 2)
	r.Equal("lab-phone-1", listed.Resources[0].Name)
	r.True(*listed.Resources[0].Free)
	r.True(*listed.Resources[1].Reserved)
	r.Equal("admin", listed.Resources[1].ReservedBy)
	r.NotNil(listed.Resources[1].ReservedTimestamp)
	r.Equal(2, listed.Labels[0].ResourceCount)
	r.Equal(1, listed.Labels[0].FreeCount)
	r.Equal(1, listed.Labels[0].ReservedCount)

	// The input step keeps the resource locked without relying on a fixed sleep.
	_, err := admin.TriggerBuild(testutil.Context(t), "example-lock-holder", nil)
	r.NoError(err)
	r.Eventually(func() bool {
		resources, _, err := admin.ListLockableResources(testutil.Context(t))
		if err != nil {
			return false
		}
		for _, resource := range resources {
			if resource.Name == "lab-phone-1" && resource.Locked != nil && *resource.Locked {
				return true
			}
		}
		return false
	}, 45*time.Second, 250*time.Millisecond, "holder acquired resource")
	t.Cleanup(func() { _ = admin.CancelBuild(testutil.Context(t), "example-lock-holder", 1) })
	held := callIntegrationTool[jenkinstools.ListLockableResourcesResponse](t, session, "jenkins_list_lockable_resources", map[string]any{"state": "locked"})
	r.Len(held.Resources, 1)
	r.False(*held.Resources[0].Free)
	r.Equal("example-lock-holder #1", held.Resources[0].BuildName)
	r.NotEmpty(held.Resources[0].LockCause)
	t.Run("build usage", func(t *testing.T) {
		r := require.New(t)
		usage := callIntegrationTool[jenkinstools.BuildLockableResourcesResponse](t, session, "jenkins_get_build_lockable_resources", map[string]any{"job": "example-locks", "build": build})
		r.Equal("pipeline_wfapi", usage.Source)
		r.Equal(jenkinscontainer.ControllerID, usage.Build.Controller)
		r.Equal(fixture.BaseURL+"/job/example-locks/1/", usage.Build.URL)
		r.Len(usage.LockSteps, 3)
		r.Equal([]string{"lab-phone-1"}, usage.LockSteps[0].ResourceNames)
		r.False(usage.LockSteps[0].Ambiguous)
		r.Equal([]string{"lab-phone-1"}, usage.LockSteps[1].ResourceNames)
		r.False(usage.LockSteps[1].Ambiguous)
		r.True(usage.LockSteps[2].Ambiguous)
		r.Empty(usage.LockSteps[2].ParameterDescription, "wfapi omits arguments for the label lock")
		r.Contains(usage.Warnings[len(usage.Warnings)-1], "no parameter description")
		r.NotEmpty(usage.Warnings)
	})
}
