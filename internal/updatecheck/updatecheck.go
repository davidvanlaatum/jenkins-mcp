package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/david/jenkins-mcp/internal/config"
)

const defaultTimeout = 10 * time.Second

type releaseResponse struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

type Result struct {
	CurrentVersion  string
	LatestVersion   string
	ReleaseURL      string
	UpdateAvailable bool
}

type Status struct {
	Enabled            bool   `json:"enabled" jsonschema:"Whether update checks are enabled"`
	SelfUpdateEnabled  bool   `json:"selfUpdateEnabled" jsonschema:"Whether the jenkins_update_server tool is allowed to install or stage server updates"`
	Repository         string `json:"repository" jsonschema:"GitHub repository checked for releases"`
	CheckIntervalHours int64  `json:"checkIntervalHours" jsonschema:"Minimum interval between update checks in hours"`
	MaxDownloadBytes   int64  `json:"maxDownloadBytes" jsonschema:"Maximum release asset or checksum bytes the self-updater will download"`
	CurrentVersion     string `json:"currentVersion" jsonschema:"Current server version"`
	LatestVersion      string `json:"latestVersion,omitempty" jsonschema:"Latest available release version, when known"`
	ReleaseURL         string `json:"releaseUrl,omitempty" jsonschema:"URL for the latest available release"`
	UpdateAvailable    bool   `json:"updateAvailable" jsonschema:"Whether a newer release is available"`
	NotificationHint   string `json:"notificationHint,omitempty" jsonschema:"User-facing hint agents should show when an update is available"`
	CheckedAt          string `json:"checkedAt,omitempty" jsonschema:"Time the latest update check completed"`
	LastError          string `json:"lastError,omitempty" jsonschema:"Most recent update check error, when present"`
}

type Checker struct {
	cfg        config.UpdateCheckConfig
	version    string
	logger     *slog.Logger
	httpClient *http.Client
	releaseURL string
	mu         sync.RWMutex
	status     Status
}

func New(cfg config.UpdateCheckConfig, version string, logger *slog.Logger) *Checker {
	return &Checker{
		cfg:        cfg,
		version:    version,
		logger:     logger,
		httpClient: &http.Client{Timeout: defaultTimeout},
		releaseURL: cfg.ReleaseURL(),
		status: Status{
			Enabled:            cfg.Enabled,
			SelfUpdateEnabled:  cfg.SelfUpdateEnabled,
			Repository:         cfg.Repository,
			CheckIntervalHours: cfg.CheckIntervalHours,
			MaxDownloadBytes:   cfg.MaxDownloadBytes,
			CurrentVersion:     version,
		},
	}
}

func (c *Checker) Start(ctx context.Context) {
	if !c.cfg.Enabled {
		return
	}
	go c.run(ctx)
}

func (c *Checker) run(ctx context.Context) {
	c.checkAndLog(ctx)

	ticker := time.NewTicker(time.Duration(c.cfg.CheckIntervalHours) * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkAndLog(ctx)
		}
	}
}

func (c *Checker) checkAndLog(ctx context.Context) {
	result, err := c.Check(ctx)
	if err != nil {
		c.setError(err)
		c.logger.Debug("update check failed", "error", err)
		return
	}
	c.setResult(result)
	if result.UpdateAvailable {
		c.logger.Warn("new jenkins-mcp release available", "current", result.CurrentVersion, "latest", result.LatestVersion, "url", result.ReleaseURL)
	}
}

func (c *Checker) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *Checker) Check(ctx context.Context) (Result, error) {
	current, ok := normalizeVersion(c.version)
	if !ok {
		return Result{CurrentVersion: c.version}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.releaseURL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "jenkins-mcp-server/"+c.version)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("github release API returned %s", resp.Status)
	}

	var latest releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&latest); err != nil {
		return Result{}, err
	}
	latestVersion, ok := normalizeVersion(latest.TagName)
	if !ok {
		return Result{CurrentVersion: c.version, LatestVersion: latest.TagName, ReleaseURL: latest.HTMLURL}, nil
	}

	return Result{
		CurrentVersion:  c.version,
		LatestVersion:   latest.TagName,
		ReleaseURL:      latest.HTMLURL,
		UpdateAvailable: compareVersions(latestVersion, current) > 0,
	}, nil
}

func (c *Checker) setResult(result Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.CurrentVersion = result.CurrentVersion
	c.status.LatestVersion = result.LatestVersion
	c.status.ReleaseURL = result.ReleaseURL
	c.status.UpdateAvailable = result.UpdateAvailable
	c.status.NotificationHint = notificationHint(result)
	c.status.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	c.status.LastError = ""
}

func (c *Checker) setError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	c.status.LastError = err.Error()
}

func notificationHint(result Result) string {
	if !result.UpdateAvailable {
		return ""
	}
	return fmt.Sprintf("Notify the user that a newer jenkins-mcp release is available: current %s, latest %s. Release: %s", result.CurrentVersion, result.LatestVersion, result.ReleaseURL)
}

func normalizeVersion(v string) ([]int, bool) {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if v == "" || strings.Contains(v, "-") {
		return nil, false
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil, false
	}
	out := make([]int, 3)
	for i, part := range parts {
		if part == "" {
			return nil, false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return nil, false
			}
			out[i] = out[i]*10 + int(r-'0')
		}
	}
	return out, true
}

func compareVersions(a, b []int) int {
	for i := 0; i < 3; i++ {
		if a[i] > b[i] {
			return 1
		}
		if a[i] < b[i] {
			return -1
		}
	}
	return 0
}
