package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadFromDefaultConfigFile(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "jenkins-mcp")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	path := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(path, []byte(`{
		"defaultController": "default",
		"controllers": [{"id": "default", "url": "https://jenkins.example.com"}],
		"mutations": {"enabled": true}
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(nil, []string{"HOME=" + home})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Controllers) != 1 || cfg.Controllers[0].URL != "https://jenkins.example.com" {
		t.Fatalf("controllers = %+v", cfg.Controllers)
	}
	if !cfg.Mutations.Enabled {
		t.Fatal("mutations should be enabled from default config file")
	}
}

func TestLoadFromXDGDefaultConfigFile(t *testing.T) {
	xdgConfigHome := t.TempDir()
	configDir := filepath.Join(xdgConfigHome, "jenkins-mcp")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	path := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(path, []byte(`{
		"defaultController": "default",
		"controllers": [{"id": "default", "url": "https://jenkins.example.com"}]
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(nil, []string{"HOME=" + t.TempDir(), "XDG_CONFIG_HOME=" + xdgConfigHome})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Controllers) != 1 || cfg.Controllers[0].URL != "https://jenkins.example.com" {
		t.Fatalf("controllers = %+v", cfg.Controllers)
	}
}

func TestLoadFallsBackToHomeDefaultConfigFile(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "jenkins-mcp")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	path := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(path, []byte(`{
		"defaultController": "default",
		"controllers": [{"id": "default", "url": "https://jenkins.example.com"}]
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(nil, []string{"HOME=" + home, "XDG_CONFIG_HOME=" + t.TempDir()})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Controllers) != 1 || cfg.Controllers[0].URL != "https://jenkins.example.com" {
		t.Fatalf("controllers = %+v", cfg.Controllers)
	}
}

func TestDefaultConfigPathsForWindows(t *testing.T) {
	appData := filepath.Join("D:", "Profiles", "alice", "Roaming")
	got := defaultConfigPathsForOS(map[string]string{
		"APPDATA":         appData,
		"USERPROFILE":     filepath.Join("C:", "Users", "alice"),
		"XDG_CONFIG_HOME": filepath.Join("C:", "msys64", "home", "alice", ".config"),
		"HOME":            filepath.Join("C:", "msys64", "home", "alice"),
	}, "windows")
	want := []string{
		filepath.Join(appData, "jenkins-mcp", "config.json"),
		filepath.Join("C:", "Users", "alice", "AppData", "Roaming", "jenkins-mcp", "config.json"),
	}
	if len(got) != len(want) {
		t.Fatalf("defaultConfigPathsForOS() length = %d, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("defaultConfigPathsForOS()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDefaultConfigPathsForWindowsFallsBackToUserProfile(t *testing.T) {
	userProfile := filepath.Join("C:", "Users", "alice")
	got := defaultConfigPathsForOS(map[string]string{"USERPROFILE": userProfile}, "windows")
	want := []string{filepath.Join(userProfile, "AppData", "Roaming", "jenkins-mcp", "config.json")}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("defaultConfigPathsForOS() = %q, want %q", got, want)
	}
}

func TestLoadIgnoresMissingDefaultConfigFile(t *testing.T) {
	cfg, err := Load(nil, []string{
		"HOME=" + t.TempDir(),
		"JENKINS_URL=https://jenkins.example.com",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Controllers) != 1 || cfg.Controllers[0].URL != "https://jenkins.example.com" {
		t.Fatalf("controllers = %+v", cfg.Controllers)
	}
}

func TestLoadErrorsForMissingExplicitConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if _, err := Load([]string{"--config", path}, nil); err == nil {
		t.Fatal("Load() succeeded with missing explicit config file")
	}
}

func TestInitCreatesDefaultConfigFile(t *testing.T) {
	home := t.TempDir()
	path, err := Init([]string{"--init"}, []string{"HOME=" + home})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	wantPath := filepath.Join(home, ".config", "jenkins-mcp", "config.json")
	if path != wantPath {
		t.Fatalf("Init() path = %q, want %q", path, wantPath)
	}
	cfg, err := loadFile(path)
	if err != nil {
		t.Fatalf("loadFile() error = %v", err)
	}
	if len(cfg.Controllers) != 1 || cfg.Controllers[0].URL != "https://jenkins.example.com" {
		t.Fatalf("controllers = %+v", cfg.Controllers)
	}
}

func TestInitCreatesExplicitConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.json")
	got, err := Init([]string{"--init", "--config", path}, nil)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if got != path {
		t.Fatalf("Init() path = %q, want %q", got, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
}

func TestInitDoesNotOverwriteExistingConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Init([]string{"--init", "--config", path}, nil); err == nil {
		t.Fatal("Init() succeeded with existing config file")
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
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() succeeded without controllers")
	}
	if !strings.Contains(err.Error(), "jenkins-mcp-server --init") {
		t.Fatalf("Validate() error = %q, want --init hint", err)
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
