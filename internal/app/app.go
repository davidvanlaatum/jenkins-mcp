package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/david/jenkins-mcp/internal/mcpserver"
	stdiotransport "github.com/david/jenkins-mcp/internal/mcpserver/transport/stdio"
	"github.com/david/jenkins-mcp/internal/selfupdate"
	"github.com/david/jenkins-mcp/internal/updatecheck"
)

var Version = "0.1.0-dev"

type Server struct {
	mcp           *mcpserver.Server
	updateChecker *updatecheck.Checker
	logFile       *os.File
}

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

func New(cfg config.Config) (*Server, error) {
	logWriter, logFile, err := newLogWriter(cfg.Logging)
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: logLevel(cfg.Logging.Level)}))
	clients := make(map[string]*jenkinsapi.API, len(cfg.Controllers))
	for _, controller := range cfg.Controllers {
		httpClient, err := jenkinsclient.New(controller, logger)
		if err != nil {
			_ = closeLogFile(logFile)
			return nil, err
		}
		clients[controller.ID] = jenkinsapi.New(controller.ID, httpClient)
	}
	auditer, err := audit.New(cfg.Audit)
	if err != nil {
		_ = closeLogFile(logFile)
		return nil, err
	}
	updateChecker := updatecheck.New(cfg.Updates, Version, logger)
	return &Server{
		mcp: mcpserver.New(mcpserver.Dependencies{
			Config:       cfg,
			Jenkins:      clients,
			Audit:        auditer,
			Logger:       logger,
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
		logFile:       logFile,
	}, nil
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
	defer func() { _ = closeLogFile(s.logFile) }()
	s.updateChecker.Start(ctx)
	return stdiotransport.Run(ctx, s.mcp.Raw())
}
