package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/davidvanlaatum/dvgoutils/logging"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/david/jenkins-mcp/internal/jenkins/logcache"
	"github.com/david/jenkins-mcp/internal/mcpserver"
	stdiotransport "github.com/david/jenkins-mcp/internal/mcpserver/transport/stdio"
	"github.com/david/jenkins-mcp/internal/selfupdate"
	"github.com/david/jenkins-mcp/internal/updatecheck"
)

var Version = "0.1.0-dev"

type Server struct {
	mcp           *mcpserver.Server
	updateChecker *updatecheck.Checker
	logCache      *logcache.Cache
}

const maxLogCacheEntries = 32 * 1024

func LoadConfigFromProcess(args []string, environ []string) (config.Config, error) {
	return config.Load(args, environ)
}

func InitConfigFromProcess(args []string, environ []string) (string, error) {
	return config.Init(args, environ)
}

func LoadSelfUpdateConfigFromProcess(args []string, environ []string) (config.UpdateCheckConfig, bool, bool, error) {
	return config.LoadSelfUpdate(args, environ)
}

func SelfUpdate(ctx context.Context, cfg config.UpdateCheckConfig, force bool) (selfupdate.Result, error) {
	return selfupdate.Update(ctx, selfupdate.Options{
		Repository:       cfg.Repository,
		CurrentVersion:   Version,
		Force:            force,
		MaxDownloadBytes: cfg.MaxDownloadBytes,
	})
}

func New(ctx context.Context, cfg config.Config) (*Server, error) {
	var err error
	var sharedLogCache *logcache.Cache
	if cfg.LogCache.Enabled {
		sharedLogCache, err = logcache.New("", maxLogCacheEntries, cfg.LogCache.MaxBytes)
		if err != nil {
			return nil, err
		}
	}
	clients := make(map[string]*jenkinsapi.API, len(cfg.Controllers))
	for _, controller := range cfg.Controllers {
		httpClient, err := jenkinsclient.New(ctx, controller)
		if err != nil {
			_ = closeLogCache(sharedLogCache)
			return nil, err
		}
		clients[controller.ID] = jenkinsapi.NewWithLogCache(controller.ID, httpClient, sharedLogCache)
	}
	auditer, err := audit.New(cfg.Audit)
	if err != nil {
		_ = closeLogCache(sharedLogCache)
		return nil, err
	}
	updateChecker := updatecheck.New(cfg.Updates, Version)
	return &Server{
		mcp: mcpserver.New(ctx, mcpserver.Dependencies{
			Config:       cfg,
			Jenkins:      clients,
			Audit:        auditer,
			Version:      Version,
			UpdateStatus: updateChecker.Status,
			SelfUpdate: func(ctx context.Context, force bool) (selfupdate.Result, error) {
				return selfupdate.Update(ctx, selfupdate.Options{
					Repository:       cfg.Updates.Repository,
					CurrentVersion:   Version,
					Force:            force,
					MaxDownloadBytes: cfg.Updates.MaxDownloadBytes,
				})
			},
		}),
		updateChecker: updateChecker,
		logCache:      sharedLogCache,
	}, nil
}

// ConfigureLogging attaches the configured startup logger without retaining the
// context. The caller owns the returned cleanup function and passes the returned
// context to both construction and server operations.
func ConfigureLogging(ctx context.Context, cfg config.LoggingConfig) (context.Context, func() error, error) {
	logWriter, logFile, err := newLogWriter(cfg)
	if err != nil {
		return nil, nil, err
	}
	logger := slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: logLevel(cfg.Level)}))
	return logging.WithLogger(ctx, logger), func() error { return closeLogFile(logFile) }, nil
}

func newLogWriter(cfg config.LoggingConfig) (io.Writer, *os.File, error) {
	if strings.TrimSpace(cfg.Path) == "" {
		return os.Stderr, nil, nil
	}
	dir := filepath.Dir(cfg.Path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, nil, err
		}
	}
	f, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return f, f, nil
}

func closeLogFile(f *os.File) error {
	if f == nil {
		return nil
	}
	return f.Close()
}

func closeLogCache(cache *logcache.Cache) error {
	if cache == nil {
		return nil
	}
	return cache.Close()
}

func logLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (s *Server) RunStdio(ctx context.Context) error {
	defer func() { _ = closeLogCache(s.logCache) }()
	s.updateChecker.Start(ctx)
	return stdiotransport.Run(ctx, s.mcp.Raw())
}
