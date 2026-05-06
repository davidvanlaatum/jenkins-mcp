package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode"
)

const maxUpdateCheckIntervalHours int64 = 24 * 30
const starterConfig = `{
  "defaultController": "default",
  "controllers": [
    {
      "id": "default",
      "url": "https://jenkins.example.com",
      "username": "jenkins-user",
      "token": "jenkins-api-token"
    }
  ],
  "mutations": {
    "enabled": false
  }
}
`

type Config struct {
	Controllers       []ControllerConfig `json:"controllers"`
	DefaultController string             `json:"defaultController"`
	Mutations         MutationConfig     `json:"mutations"`
	Limits            LimitsConfig       `json:"limits"`
	Watch             WatchConfig        `json:"watch"`
	Artifacts         ArtifactConfig     `json:"artifacts"`
	Audit             AuditConfig        `json:"audit"`
	Logging           LoggingConfig      `json:"logging"`
	Updates           UpdateCheckConfig  `json:"updates"`
	Capabilities      CapabilityConfig   `json:"capabilities"`
}

type ControllerConfig struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Token    string `json:"token,omitempty"`
}

type MutationConfig struct {
	Enabled bool `json:"enabled"`
}

type LimitsConfig struct {
	MaxResponseBytes int64 `json:"maxResponseBytes" jsonschema:"Maximum bytes allowed in a bounded MCP response"`
	LogChunkBytes    int64 `json:"logChunkBytes" jsonschema:"Maximum console log bytes returned per log chunk"`
	InlineBytes      int64 `json:"inlineBytes" jsonschema:"Maximum artifact or inline content bytes returned directly in a tool response"`
}

type WatchConfig struct {
	PollIntervalMs         int64 `json:"pollIntervalMs"`
	DefaultWaitTimeoutMs   int64 `json:"defaultWaitTimeoutMs"`
	MaxWaitTimeoutMs       int64 `json:"maxWaitTimeoutMs"`
	MaxConsecutiveFailures int   `json:"maxConsecutiveFailures"`
}

type ArtifactConfig struct {
	DownloadDir string `json:"downloadDir"`
}

type AuditConfig struct {
	Path string `json:"path,omitempty"`
}

type LoggingConfig struct {
	Level        string `json:"level,omitempty"`
	Path         string `json:"path,omitempty"`
	ToolCalls    bool   `json:"toolCalls,omitempty"`
	ToolPayloads bool   `json:"toolPayloads,omitempty"`
}

type CapabilityConfig struct {
	PluginDiscoveryEnabled bool `json:"pluginDiscoveryEnabled" jsonschema:"Whether jenkins_get_capabilities should query Jenkins pluginManager for installed plugins and plugin-derived feature flags"`

	pluginDiscoveryEnabledSet bool
}

func (c *CapabilityConfig) UnmarshalJSON(b []byte) error {
	type capabilityConfig CapabilityConfig
	var raw struct {
		capabilityConfig
		PluginDiscoveryEnabled *bool `json:"pluginDiscoveryEnabled"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*c = CapabilityConfig(raw.capabilityConfig)
	if raw.PluginDiscoveryEnabled != nil {
		c.PluginDiscoveryEnabled = *raw.PluginDiscoveryEnabled
		c.pluginDiscoveryEnabledSet = true
	}
	return nil
}

type UpdateCheckConfig struct {
	Enabled            bool   `json:"enabled"`
	Repository         string `json:"repository"`
	CheckIntervalHours int64  `json:"checkIntervalHours"`

	enabledSet bool
}

func (c *UpdateCheckConfig) UnmarshalJSON(b []byte) error {
	type updateCheckConfig UpdateCheckConfig
	var raw struct {
		updateCheckConfig
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*c = UpdateCheckConfig(raw.updateCheckConfig)
	if raw.Enabled != nil {
		c.Enabled = *raw.Enabled
		c.enabledSet = true
	}
	return nil
}

func (c UpdateCheckConfig) ReleaseURL() string {
	return "https://api.github.com/repos/" + c.Repository + "/releases/latest"
}

func Load(args []string, environ []string) (Config, error) {
	cfg := Defaults()
	env := envMap(environ)

	fs := flag.NewFlagSet("jenkins-mcp-server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", env["JENKINS_MCP_CONFIG"], "config file")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if *configPath != "" {
		fileCfg, err := loadFile(*configPath)
		if err != nil {
			return Config{}, err
		}
		cfg = merge(cfg, fileCfg)
	} else {
		fileCfg, found, err := loadDefaultConfig(defaultConfigPaths(env))
		if err != nil {
			return Config{}, err
		}
		if found {
			cfg = merge(cfg, fileCfg)
		}
	}

	applyEnv(&cfg, env)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Init(args []string, environ []string) (string, error) {
	env := envMap(environ)

	fs := flag.NewFlagSet("jenkins-mcp-server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", env["JENKINS_MCP_CONFIG"], "config file")
	initConfig := fs.Bool("init", false, "create a starter config file")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if !*initConfig {
		return "", errors.New("--init is required")
	}

	path := *configPath
	if path == "" {
		paths := defaultConfigPaths(env)
		if len(paths) == 0 {
			return "", errors.New("could not determine default config path; use --config PATH with --init")
		}
		path = paths[0]
	}

	if err := writeStarterConfig(path); err != nil {
		return "", err
	}
	return path, nil
}

func writeStarterConfig(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("config file already exists at %s", path)
		}
		return err
	}
	if _, err := f.WriteString(starterConfig); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func loadDefaultConfig(paths []string) (Config, bool, error) {
	for _, path := range paths {
		fileCfg, err := loadFile(path)
		if err == nil {
			return fileCfg, true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return Config{}, false, err
		}
	}
	return Config{}, false, nil
}

func defaultConfigPaths(env map[string]string) []string {
	return defaultConfigPathsForOS(env, runtime.GOOS)
}

func defaultConfigPathsForOS(env map[string]string, goos string) []string {
	var paths []string
	if goos == "windows" {
		if env["APPDATA"] != "" {
			paths = append(paths, filepath.Join(env["APPDATA"], "jenkins-mcp", "config.json"))
		}
		if env["USERPROFILE"] != "" {
			paths = append(paths, filepath.Join(env["USERPROFILE"], "AppData", "Roaming", "jenkins-mcp", "config.json"))
		}
		return paths
	}
	if env["XDG_CONFIG_HOME"] != "" {
		paths = append(paths, filepath.Join(env["XDG_CONFIG_HOME"], "jenkins-mcp", "config.json"))
	}
	if env["HOME"] != "" {
		paths = append(paths, filepath.Join(env["HOME"], ".config", "jenkins-mcp", "config.json"))
	}
	return paths
}

func Defaults() Config {
	return Config{
		DefaultController: "default",
		Limits:            LimitsConfig{MaxResponseBytes: 64 * 1024, LogChunkBytes: 64 * 1024, InlineBytes: 32 * 1024},
		Watch:             WatchConfig{PollIntervalMs: 3000, DefaultWaitTimeoutMs: 120000, MaxWaitTimeoutMs: 900000, MaxConsecutiveFailures: 3},
		Artifacts:         ArtifactConfig{DownloadDir: filepath.Join(os.TempDir(), "jenkins-mcp-artifacts")},
		Updates:           UpdateCheckConfig{Enabled: true, Repository: "davidvanlaatum/jenkins-mcp", CheckIntervalHours: 24},
		Capabilities:      CapabilityConfig{PluginDiscoveryEnabled: true},
	}
}

func (c Config) Validate() error {
	if len(c.Controllers) == 0 {
		return errors.New("at least one Jenkins controller is required; set JENKINS_URL or run jenkins-mcp-server --init to create a starter config file")
	}
	seen := map[string]bool{}
	hasDefault := false
	for _, controller := range c.Controllers {
		if strings.TrimSpace(controller.ID) == "" {
			return errors.New("controller id is required")
		}
		if seen[controller.ID] {
			return fmt.Errorf("duplicate controller id %q", controller.ID)
		}
		seen[controller.ID] = true
		if controller.ID == c.DefaultController {
			hasDefault = true
		}
		parsed, err := url.Parse(controller.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("controller %q has invalid url", controller.ID)
		}
	}
	if c.DefaultController == "" || !hasDefault {
		return fmt.Errorf("default controller %q is not configured", c.DefaultController)
	}
	if c.Limits.MaxResponseBytes <= 0 || c.Limits.LogChunkBytes <= 0 || c.Limits.InlineBytes <= 0 {
		return errors.New("limits must be positive")
	}
	if c.Watch.PollIntervalMs <= 0 || c.Watch.DefaultWaitTimeoutMs <= 0 || c.Watch.MaxWaitTimeoutMs <= 0 || c.Watch.MaxConsecutiveFailures <= 0 {
		return errors.New("watch settings must be positive")
	}
	if c.Watch.DefaultWaitTimeoutMs > c.Watch.MaxWaitTimeoutMs {
		return errors.New("watch.defaultWaitTimeoutMs must not exceed watch.maxWaitTimeoutMs")
	}
	if c.Artifacts.DownloadDir == "" {
		return errors.New("artifact downloadDir is required")
	}
	if !validGitHubRepository(c.Updates.Repository) {
		return errors.New("updates.repository must be in owner/repo format")
	}
	if c.Updates.CheckIntervalHours <= 0 {
		return errors.New("updates.checkIntervalHours must be positive")
	}
	if c.Updates.CheckIntervalHours > maxUpdateCheckIntervalHours {
		return fmt.Errorf("updates.checkIntervalHours must not exceed %d", maxUpdateCheckIntervalHours)
	}
	return nil
}

func validGitHubRepository(repository string) bool {
	if repository != strings.TrimSpace(repository) {
		return false
	}
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, part := range parts {
		for _, r := range part {
			if unicode.IsSpace(r) || r == '?' || r == '#' || r == '&' || r == '=' {
				return false
			}
		}
	}
	return true
}

func (c Config) Controller(id string) (ControllerConfig, bool) {
	if id == "" {
		id = c.DefaultController
	}
	for _, controller := range c.Controllers {
		if controller.ID == id {
			return controller, true
		}
	}
	return ControllerConfig{}, false
}

func (c Config) Redacted() Config {
	out := c
	out.Controllers = append([]ControllerConfig(nil), c.Controllers...)
	for i := range out.Controllers {
		if out.Controllers[i].Token != "" {
			out.Controllers[i].Token = "<redacted>"
		}
	}
	return out
}

func loadFile(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func merge(base, override Config) Config {
	if len(override.Controllers) > 0 {
		base.Controllers = override.Controllers
	}
	if override.DefaultController != "" {
		base.DefaultController = override.DefaultController
	}
	if override.Mutations.Enabled {
		base.Mutations.Enabled = true
	}
	if override.Limits.MaxResponseBytes != 0 {
		base.Limits.MaxResponseBytes = override.Limits.MaxResponseBytes
	}
	if override.Limits.LogChunkBytes != 0 {
		base.Limits.LogChunkBytes = override.Limits.LogChunkBytes
	}
	if override.Limits.InlineBytes != 0 {
		base.Limits.InlineBytes = override.Limits.InlineBytes
	}
	if override.Watch.PollIntervalMs != 0 {
		base.Watch.PollIntervalMs = override.Watch.PollIntervalMs
	}
	if override.Watch.DefaultWaitTimeoutMs != 0 {
		base.Watch.DefaultWaitTimeoutMs = override.Watch.DefaultWaitTimeoutMs
	}
	if override.Watch.MaxWaitTimeoutMs != 0 {
		base.Watch.MaxWaitTimeoutMs = override.Watch.MaxWaitTimeoutMs
	}
	if override.Watch.MaxConsecutiveFailures != 0 {
		base.Watch.MaxConsecutiveFailures = override.Watch.MaxConsecutiveFailures
	}
	if override.Artifacts.DownloadDir != "" {
		base.Artifacts.DownloadDir = override.Artifacts.DownloadDir
	}
	if override.Audit.Path != "" {
		base.Audit.Path = override.Audit.Path
	}
	if override.Logging.Level != "" {
		base.Logging.Level = override.Logging.Level
	}
	if override.Logging.Path != "" {
		base.Logging.Path = override.Logging.Path
	}
	if override.Logging.ToolCalls {
		base.Logging.ToolCalls = true
	}
	if override.Logging.ToolPayloads {
		base.Logging.ToolPayloads = true
	}
	if override.Updates.enabledSet {
		base.Updates.Enabled = override.Updates.Enabled
	}
	if override.Updates.Repository != "" {
		base.Updates.Repository = override.Updates.Repository
	}
	if override.Updates.CheckIntervalHours != 0 {
		base.Updates.CheckIntervalHours = override.Updates.CheckIntervalHours
	}
	if override.Capabilities.pluginDiscoveryEnabledSet {
		base.Capabilities.PluginDiscoveryEnabled = override.Capabilities.PluginDiscoveryEnabled
	}
	return base
}

func applyEnv(cfg *Config, env map[string]string) {
	if env["JENKINS_URL"] != "" {
		id := env["JENKINS_ID"]
		if id == "" {
			id = "default"
		}
		cfg.Controllers = []ControllerConfig{{ID: id, URL: env["JENKINS_URL"], Username: env["JENKINS_USER"], Token: env["JENKINS_TOKEN"]}}
		cfg.DefaultController = id
	}
	if v := env["JENKINS_MUTATIONS"]; v != "" {
		cfg.Mutations.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := env["JENKINS_ARTIFACT_DIR"]; v != "" {
		cfg.Artifacts.DownloadDir = v
	}
	if v := env["JENKINS_AUDIT_PATH"]; v != "" {
		cfg.Audit.Path = v
	}
	if v := env["JENKINS_MCP_LOG_LEVEL"]; v != "" {
		cfg.Logging.Level = v
	}
	if v := env["JENKINS_MCP_LOG_FILE"]; v != "" {
		cfg.Logging.Path = v
	}
	if v := env["JENKINS_MCP_LOG_TOOL_CALLS"]; v != "" {
		cfg.Logging.ToolCalls = parseBool(v)
	}
	if v := env["JENKINS_MCP_LOG_TOOL_PAYLOADS"]; v != "" {
		cfg.Logging.ToolPayloads = parseBool(v)
	}
	if v := env["JENKINS_MCP_UPDATE_CHECK"]; v != "" {
		cfg.Updates.Enabled = parseBool(v)
	}
	if v := env["JENKINS_MCP_UPDATE_REPOSITORY"]; v != "" {
		cfg.Updates.Repository = v
	}
	if v := env["JENKINS_MCP_UPDATE_CHECK_INTERVAL_HOURS"]; v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Updates.CheckIntervalHours = n
		}
	}
	if v := env["JENKINS_MCP_PLUGIN_DISCOVERY"]; v != "" {
		cfg.Capabilities.PluginDiscoveryEnabled = parseBool(v)
	}
	if v := env["JENKINS_MAX_RESPONSE_BYTES"]; v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Limits.MaxResponseBytes = n
		}
	}
	if v := env["JENKINS_LOG_CHUNK_BYTES"]; v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Limits.LogChunkBytes = n
		}
	}
	if v := env["JENKINS_WATCH_POLL_INTERVAL_MS"]; v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Watch.PollIntervalMs = n
		}
	}
	if v := env["JENKINS_WATCH_DEFAULT_WAIT_TIMEOUT_MS"]; v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Watch.DefaultWaitTimeoutMs = n
		}
	}
	if v := env["JENKINS_WATCH_MAX_WAIT_TIMEOUT_MS"]; v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Watch.MaxWaitTimeoutMs = n
		}
	}
	if v := env["JENKINS_WATCH_MAX_CONSECUTIVE_FAILURES"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Watch.MaxConsecutiveFailures = n
		}
	}
}

func parseBool(v string) bool {
	return strings.EqualFold(v, "true") || v == "1"
}

func envMap(environ []string) map[string]string {
	out := map[string]string{}
	for _, pair := range environ {
		k, v, ok := strings.Cut(pair, "=")
		if ok {
			out[k] = v
		}
	}
	return out
}
