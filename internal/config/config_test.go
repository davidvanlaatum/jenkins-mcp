package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromEnvironment(t *testing.T) {
	cfg, err := Load(nil, []string{
		"JENKINS_URL=https://jenkins.example.com",
		"JENKINS_USER=alice",
		"JENKINS_TOKEN=secret",
		"JENKINS_MUTATIONS=true",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DefaultController != "default" {
		t.Fatalf("DefaultController = %q", cfg.DefaultController)
	}
	if len(cfg.Controllers) != 1 {
		t.Fatalf("controllers length = %d", len(cfg.Controllers))
	}
	controller := cfg.Controllers[0]
	if controller.URL != "https://jenkins.example.com" || controller.Username != "alice" || controller.Token != "secret" {
		t.Fatalf("controller = %+v", controller)
	}
	if !cfg.Mutations.Enabled {
		t.Fatal("mutations should be enabled")
	}
	if !cfg.Updates.Enabled {
		t.Fatal("update checks should be enabled by default")
	}
	if cfg.Redacted().Controllers[0].Token != "<redacted>" {
		t.Fatal("token was not redacted")
	}
}

func TestLoadUpdateCheckFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
		"defaultController": "default",
		"controllers": [{"id": "default", "url": "https://jenkins.example.com"}],
		"updates": {
			"enabled": false,
			"repository": "example/project",
			"checkIntervalHours": 6
		}
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load([]string{"--config", path}, nil)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Updates.Enabled {
		t.Fatal("updates.enabled should be configurable to false")
	}
	if cfg.Updates.Repository != "example/project" {
		t.Fatalf("updates.repository = %q", cfg.Updates.Repository)
	}
	if cfg.Updates.CheckIntervalHours != 6 {
		t.Fatalf("updates.checkIntervalHours = %d", cfg.Updates.CheckIntervalHours)
	}
}

func TestLoadUpdateCheckFromEnvironment(t *testing.T) {
	cfg, err := Load(nil, []string{
		"JENKINS_URL=https://jenkins.example.com",
		"JENKINS_MCP_UPDATE_CHECK=false",
		"JENKINS_MCP_UPDATE_REPOSITORY=example/project",
		"JENKINS_MCP_UPDATE_CHECK_INTERVAL_HOURS=12",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Updates.Enabled {
		t.Fatal("updates.enabled should be disabled by environment")
	}
	if cfg.Updates.Repository != "example/project" {
		t.Fatalf("updates.repository = %q", cfg.Updates.Repository)
	}
	if cfg.Updates.CheckIntervalHours != 12 {
		t.Fatalf("updates.checkIntervalHours = %d", cfg.Updates.CheckIntervalHours)
	}
}

func TestValidateRequiresController(t *testing.T) {
	cfg := Defaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() succeeded without controllers")
	}
}

func TestValidateRejectsInvalidUpdateRepositories(t *testing.T) {
	tests := []string{
		"",
		"owner/",
		"/repo",
		"owner/repo/extra",
		"owner /repo",
		"owner/repo ",
		"owner/repo?ref=main",
		"owner/repo#fragment",
		"https://github.com/owner/repo",
	}
	for _, repository := range tests {
		t.Run(repository, func(t *testing.T) {
			cfg := validTestConfig()
			cfg.Updates.Repository = repository
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() succeeded with invalid updates.repository")
			}
		})
	}
}

func TestValidateRejectsExcessiveUpdateCheckInterval(t *testing.T) {
	cfg := validTestConfig()
	cfg.Updates.CheckIntervalHours = maxUpdateCheckIntervalHours + 1
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() succeeded with excessive updates.checkIntervalHours")
	}
}

func TestUpdateReleaseURLUsesValidatedRepository(t *testing.T) {
	cfg := validTestConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	got := cfg.Updates.ReleaseURL()
	want := "https://api.github.com/repos/davidvanlaatum/jenkins-mcp/releases/latest"
	if got != want {
		t.Fatalf("ReleaseURL() = %q, want %q", got, want)
	}
}

func validTestConfig() Config {
	cfg := Defaults()
	cfg.Controllers = []ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}}
	return cfg
}
