package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadFromEnvironment(t *testing.T) {
	r := require.New(t)

	cfg, err := Load(nil, []string{
		"JENKINS_URL=https://jenkins.example.com",
		"JENKINS_USER=alice",
		"JENKINS_TOKEN=secret",
		"JENKINS_MUTATIONS=true",
	})
	r.NoError(err, "Load()")
	r.Equal("default", cfg.DefaultController, "DefaultController")
	r.Len(cfg.Controllers, 1, "controllers")
	controller := cfg.Controllers[0]
	r.Equal("https://jenkins.example.com", controller.URL, "controller URL")
	r.Equal("alice", controller.Username, "controller username")
	r.Equal("secret", controller.Token, "controller token")
	r.True(cfg.Mutations.Enabled, "mutations should be enabled")
	r.True(cfg.Updates.Enabled, "update checks should be enabled by default")
	r.True(cfg.Capabilities.PluginDiscoveryEnabled, "plugin discovery should be enabled by default")
	r.Equal("<redacted>", cfg.Redacted().Controllers[0].Token, "token should be redacted")
}

func TestLoadUpdateCheckFromFile(t *testing.T) {
	r := require.New(t)
	path := filepath.Join(t.TempDir(), "config.json")
	err := os.WriteFile(path, []byte(`{
		"defaultController": "default",
		"controllers": [{"id": "default", "url": "https://jenkins.example.com"}],
		"updates": {
			"enabled": false,
			"repository": "example/project",
			"checkIntervalHours": 6
		}
	}`), 0o600)
	r.NoError(err, "WriteFile()")

	cfg, err := Load([]string{"--config", path}, nil)
	r.NoError(err, "Load()")
	r.False(cfg.Updates.Enabled, "updates.enabled should be configurable to false")
	r.Equal("example/project", cfg.Updates.Repository, "updates.repository")
	r.Equal(int64(6), cfg.Updates.CheckIntervalHours, "updates.checkIntervalHours")
}

func TestLoadCapabilityConfigFromFile(t *testing.T) {
	r := require.New(t)
	path := filepath.Join(t.TempDir(), "config.json")
	err := os.WriteFile(path, []byte(`{
		"defaultController": "default",
		"controllers": [{"id": "default", "url": "https://jenkins.example.com"}],
		"capabilities": {
			"pluginDiscoveryEnabled": false
		}
	}`), 0o600)
	r.NoError(err, "WriteFile()")

	cfg, err := Load([]string{"--config", path}, nil)
	r.NoError(err, "Load()")
	r.False(cfg.Capabilities.PluginDiscoveryEnabled, "capabilities.pluginDiscoveryEnabled should be configurable to false")
}

func TestLoadFromDefaultConfigFile(t *testing.T) {
	r := require.New(t)
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "jenkins-mcp")
	err := os.MkdirAll(configDir, 0o700)
	r.NoError(err, "MkdirAll()")
	path := filepath.Join(configDir, "config.json")
	err = os.WriteFile(path, []byte(`{
		"defaultController": "default",
		"controllers": [{"id": "default", "url": "https://jenkins.example.com"}],
		"mutations": {"enabled": true}
	}`), 0o600)
	r.NoError(err, "WriteFile()")

	cfg, err := Load(nil, []string{"HOME=" + home})
	r.NoError(err, "Load()")
	r.Len(cfg.Controllers, 1, "controllers")
	r.Equal("https://jenkins.example.com", cfg.Controllers[0].URL, "controller URL")
	r.True(cfg.Mutations.Enabled, "mutations should be enabled from default config file")
}

func TestLoadFromXDGDefaultConfigFile(t *testing.T) {
	r := require.New(t)
	xdgConfigHome := t.TempDir()
	configDir := filepath.Join(xdgConfigHome, "jenkins-mcp")
	err := os.MkdirAll(configDir, 0o700)
	r.NoError(err, "MkdirAll()")
	path := filepath.Join(configDir, "config.json")
	err = os.WriteFile(path, []byte(`{
		"defaultController": "default",
		"controllers": [{"id": "default", "url": "https://jenkins.example.com"}]
	}`), 0o600)
	r.NoError(err, "WriteFile()")

	cfg, err := Load(nil, []string{"HOME=" + t.TempDir(), "XDG_CONFIG_HOME=" + xdgConfigHome})
	r.NoError(err, "Load()")
	r.Len(cfg.Controllers, 1, "controllers")
	r.Equal("https://jenkins.example.com", cfg.Controllers[0].URL, "controller URL")
}

func TestLoadFallsBackToHomeDefaultConfigFile(t *testing.T) {
	r := require.New(t)
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "jenkins-mcp")
	err := os.MkdirAll(configDir, 0o700)
	r.NoError(err, "MkdirAll()")
	path := filepath.Join(configDir, "config.json")
	err = os.WriteFile(path, []byte(`{
		"defaultController": "default",
		"controllers": [{"id": "default", "url": "https://jenkins.example.com"}]
	}`), 0o600)
	r.NoError(err, "WriteFile()")

	cfg, err := Load(nil, []string{"HOME=" + home, "XDG_CONFIG_HOME=" + t.TempDir()})
	r.NoError(err, "Load()")
	r.Len(cfg.Controllers, 1, "controllers")
	r.Equal("https://jenkins.example.com", cfg.Controllers[0].URL, "controller URL")
}

func TestDefaultConfigPathsForWindows(t *testing.T) {
	r := require.New(t)

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
	r.Equal(want, got, "defaultConfigPathsForOS()")
}

func TestDefaultConfigPathsForWindowsFallsBackToUserProfile(t *testing.T) {
	r := require.New(t)

	userProfile := filepath.Join("C:", "Users", "alice")
	got := defaultConfigPathsForOS(map[string]string{"USERPROFILE": userProfile}, "windows")
	want := []string{filepath.Join(userProfile, "AppData", "Roaming", "jenkins-mcp", "config.json")}
	r.Equal(want, got, "defaultConfigPathsForOS()")
}

func TestLoadIgnoresMissingDefaultConfigFile(t *testing.T) {
	r := require.New(t)

	cfg, err := Load(nil, []string{
		"HOME=" + t.TempDir(),
		"JENKINS_URL=https://jenkins.example.com",
	})
	r.NoError(err, "Load()")
	r.Len(cfg.Controllers, 1, "controllers")
	r.Equal("https://jenkins.example.com", cfg.Controllers[0].URL, "controller URL")
}

func TestLoadErrorsForMissingExplicitConfigFile(t *testing.T) {
	r := require.New(t)

	path := filepath.Join(t.TempDir(), "missing.json")
	_, err := Load([]string{"--config", path}, nil)
	r.Error(err, "Load() should fail with missing explicit config file")
}

func TestInitCreatesDefaultConfigFile(t *testing.T) {
	r := require.New(t)

	home := t.TempDir()
	path, err := Init([]string{"--init"}, []string{"HOME=" + home})
	r.NoError(err, "Init()")
	wantPath := filepath.Join(home, ".config", "jenkins-mcp", "config.json")
	r.Equal(wantPath, path, "Init() path")
	cfg, err := loadFile(path)
	r.NoError(err, "loadFile()")
	r.Len(cfg.Controllers, 1, "controllers")
	r.Equal("https://jenkins.example.com", cfg.Controllers[0].URL, "controller URL")
	b, err := os.ReadFile(path)
	r.NoError(err, "ReadFile()")
	var raw map[string]any
	err = json.Unmarshal(b, &raw)
	r.NoError(err, "Unmarshal()")
	mutations, ok := raw["mutations"].(map[string]any)
	r.True(ok, "mutations field should be an object")
	enabled, ok := mutations["enabled"].(bool)
	r.True(ok, "mutations.enabled should be a bool")
	r.False(enabled, "mutations.enabled")
	_, ok = raw["artifacts"]
	r.False(ok, "starter config should not include artifacts; downloadDir has an OS-specific default")
}

func TestInitCreatesExplicitConfigFile(t *testing.T) {
	r := require.New(t)

	path := filepath.Join(t.TempDir(), "custom.json")
	got, err := Init([]string{"--init", "--config", path}, nil)
	r.NoError(err, "Init()")
	r.Equal(path, got, "Init() path")
	_, err = os.Stat(path)
	r.NoError(err, "Stat()")
}

func TestInitDoesNotOverwriteExistingConfigFile(t *testing.T) {
	r := require.New(t)

	path := filepath.Join(t.TempDir(), "config.json")
	err := os.WriteFile(path, []byte("{}"), 0o600)
	r.NoError(err, "WriteFile()")
	_, err = Init([]string{"--init", "--config", path}, nil)
	r.Error(err, "Init() should fail with existing config file")
}

func TestLoadUpdateCheckFromEnvironment(t *testing.T) {
	r := require.New(t)

	cfg, err := Load(nil, []string{
		"JENKINS_URL=https://jenkins.example.com",
		"JENKINS_MCP_UPDATE_CHECK=false",
		"JENKINS_MCP_UPDATE_REPOSITORY=example/project",
		"JENKINS_MCP_UPDATE_CHECK_INTERVAL_HOURS=12",
		"JENKINS_MCP_SELF_UPDATE=true",
		"JENKINS_MCP_UPDATE_MAX_DOWNLOAD_BYTES=123456",
	})
	r.NoError(err, "Load()")
	r.False(cfg.Updates.Enabled, "updates.enabled should be disabled by environment")
	r.Equal("example/project", cfg.Updates.Repository, "updates.repository")
	r.Equal(int64(12), cfg.Updates.CheckIntervalHours, "updates.checkIntervalHours")
	r.True(cfg.Updates.SelfUpdateEnabled, "updates.selfUpdateEnabled")
	r.Equal(int64(123456), cfg.Updates.MaxDownloadBytes, "updates.maxDownloadBytes")
}

func TestLoadSelfUpdateDoesNotRequireController(t *testing.T) {
	r := require.New(t)

	cfg, selfUpdate, force, err := LoadSelfUpdate([]string{"--self-update"}, []string{
		"HOME=" + t.TempDir(),
		"JENKINS_MCP_UPDATE_REPOSITORY=example/project",
	})
	r.NoError(err, "LoadSelfUpdate()")
	r.Equal("example/project", cfg.Repository, "repository")
	r.True(selfUpdate, "selfUpdate")
	r.False(force, "force")
}

func TestLoadSelfUpdateParsesForceBoolForms(t *testing.T) {
	r := require.New(t)

	for _, args := range [][]string{
		{"--self-update", "--force"},
		{"--self-update", "--force=true"},
		{"--self-update", "-force=true"},
	} {
		_, selfUpdate, force, err := LoadSelfUpdate(args, []string{
			"HOME=" + t.TempDir(),
			"JENKINS_MCP_UPDATE_REPOSITORY=example/project",
		})
		r.NoError(err, "LoadSelfUpdate(%v)", args)
		r.True(selfUpdate, "selfUpdate for %v", args)
		r.True(force, "force for %v", args)
	}
}

func TestLoadSelfUpdateParsesExplicitFalse(t *testing.T) {
	r := require.New(t)

	_, selfUpdate, force, err := LoadSelfUpdate([]string{"--self-update=false", "--force=true"}, []string{
		"HOME=" + t.TempDir(),
		"JENKINS_MCP_UPDATE_REPOSITORY=example/project",
	})
	r.NoError(err, "LoadSelfUpdate()")
	r.False(selfUpdate, "selfUpdate")
	r.True(force, "force still parses")
}

func TestLoadCapabilityConfigFromEnvironment(t *testing.T) {
	r := require.New(t)

	cfg, err := Load(nil, []string{
		"JENKINS_URL=https://jenkins.example.com",
		"JENKINS_MCP_PLUGIN_DISCOVERY=false",
	})
	r.NoError(err, "Load()")
	r.False(cfg.Capabilities.PluginDiscoveryEnabled, "capabilities.pluginDiscoveryEnabled should be disabled by environment")
}

func TestLoadLoggingConfigFromEnvironment(t *testing.T) {
	r := require.New(t)

	cfg, err := Load(nil, []string{
		"JENKINS_URL=https://jenkins.example.com",
		"JENKINS_MCP_LOG_LEVEL=debug",
		"JENKINS_MCP_LOG_FILE=/tmp/jenkins-mcp.log",
		"JENKINS_MCP_LOG_TOOL_CALLS=true",
		"JENKINS_MCP_LOG_TOOL_PAYLOADS=1",
	})
	r.NoError(err, "Load()")
	r.Equal("debug", cfg.Logging.Level, "logging.level")
	r.Equal("/tmp/jenkins-mcp.log", cfg.Logging.Path, "logging.path")
	r.True(cfg.Logging.ToolCalls, "logging.toolCalls should be enabled by environment")
	r.True(cfg.Logging.ToolPayloads, "logging.toolPayloads should be enabled by environment")
}

func TestLoadLoggingConfigFromFile(t *testing.T) {
	r := require.New(t)
	path := filepath.Join(t.TempDir(), "config.json")
	err := os.WriteFile(path, []byte(`{
		"defaultController": "default",
		"controllers": [{"id": "default", "url": "https://jenkins.example.com"}],
		"logging": {
			"level": "warn",
			"path": "/tmp/jenkins-mcp.log",
			"toolCalls": true,
			"toolPayloads": true
		}
	}`), 0o600)
	r.NoError(err, "WriteFile()")

	cfg, err := Load([]string{"--config", path}, nil)
	r.NoError(err, "Load()")
	r.Equal("warn", cfg.Logging.Level, "logging.level")
	r.Equal("/tmp/jenkins-mcp.log", cfg.Logging.Path, "logging.path")
	r.True(cfg.Logging.ToolCalls, "logging.toolCalls should be enabled from file")
	r.True(cfg.Logging.ToolPayloads, "logging.toolPayloads should be enabled from file")
}

func TestValidateRequiresController(t *testing.T) {
	r := require.New(t)

	cfg := Defaults()
	err := cfg.Validate()
	r.Error(err, "Validate() should fail without controllers")
	r.Contains(err.Error(), "jenkins-mcp-server --init", "Validate() error should include init hint")
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
			r := require.New(t)

			cfg := validTestConfig()
			cfg.Updates.Repository = repository
			err := cfg.Validate()
			r.Error(err, "Validate() should fail with invalid updates.repository")
		})
	}
}

func TestValidateRejectsExcessiveUpdateCheckInterval(t *testing.T) {
	r := require.New(t)

	cfg := validTestConfig()
	cfg.Updates.CheckIntervalHours = maxUpdateCheckIntervalHours + 1
	err := cfg.Validate()
	r.Error(err, "Validate() should fail with excessive updates.checkIntervalHours")
}

func TestUpdateReleaseURLUsesValidatedRepository(t *testing.T) {
	r := require.New(t)

	cfg := validTestConfig()
	err := cfg.Validate()
	r.NoError(err, "Validate()")
	got := cfg.Updates.ReleaseURL()
	want := "https://api.github.com/repos/davidvanlaatum/jenkins-mcp/releases/latest"
	r.Equal(want, got, "ReleaseURL()")
}

func validTestConfig() Config {
	cfg := Defaults()
	cfg.Controllers = []ControllerConfig{{ID: "default", URL: "https://jenkins.example.com"}}
	return cfg
}
