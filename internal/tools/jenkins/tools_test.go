package jenkins

import (
	"context"
	"testing"

	"github.com/david/jenkins-mcp/internal/config"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
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

func TestResolveBuildURL(t *testing.T) {
	ref, err := resolveBuildURL(config.Config{
		Controllers: []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
	}, "https://jenkins.example.com/job/weather-station/job/weather-station-server/job/main/104/")
	if err != nil {
		t.Fatalf("resolveBuildURL() error = %v", err)
	}
	if ref.Controller != "default" || ref.Job != "weather-station/weather-station-server/main" || ref.Build != 104 {
		t.Fatalf("reference = %+v", ref)
	}
}

func TestResolveBuildURLRejectsUnknownController(t *testing.T) {
	_, err := resolveBuildURL(config.Config{
		Controllers: []config.ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}},
	}, "https://other.example.com/job/app/1/")
	if err == nil {
		t.Fatal("resolveBuildURL() accepted unknown controller")
	}
}

func TestValidateTriggerParametersRejectsUnknown(t *testing.T) {
	err := validateTriggerParameters([]model.ParameterDefinition{{Name: "BRANCH"}}, map[string]string{"UNKNOWN": "main"})
	if err == nil {
		t.Fatal("validateTriggerParameters() accepted unknown parameter")
	}
}

func TestValidateTriggerParametersRequiresRequired(t *testing.T) {
	err := validateTriggerParameters([]model.ParameterDefinition{{Name: "BRANCH", Required: true}}, nil)
	if err == nil {
		t.Fatal("validateTriggerParameters() accepted missing required parameter")
	}
}

func TestValidateTriggerParametersAcceptsKnown(t *testing.T) {
	err := validateTriggerParameters([]model.ParameterDefinition{{Name: "BRANCH", Required: true}}, map[string]string{"BRANCH": "main"})
	if err != nil {
		t.Fatalf("validateTriggerParameters() error = %v", err)
	}
}
