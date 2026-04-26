package jenkins

import (
	"context"
	"testing"

	"github.com/david/jenkins-mcp/internal/config"
)

func TestTriggerBuildRequiresMutationEnablement(t *testing.T) {
	_, err := TriggerBuild(context.Background(), Deps{
		Config: config.Config{
			DefaultController: "default",
			Controllers:       []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
		},
	}, TriggerBuildRequest{Job: "app"})
	if err == nil {
		t.Fatal("TriggerBuild() succeeded with mutations disabled")
	}
}
