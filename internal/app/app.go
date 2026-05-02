package app

import (
	"context"
	"log/slog"
	"os"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/david/jenkins-mcp/internal/mcpserver"
	stdiotransport "github.com/david/jenkins-mcp/internal/mcpserver/transport/stdio"
	"github.com/david/jenkins-mcp/internal/updatecheck"
)

var Version = "0.1.0-dev"

type Server struct {
	mcp           *mcpserver.Server
	updateChecker *updatecheck.Checker
}

func LoadConfigFromProcess(args []string, environ []string) (config.Config, error) {
	return config.Load(args, environ)
}

func InitConfigFromProcess(args []string, environ []string) (string, error) {
	return config.Init(args, environ)
}

func New(cfg config.Config) (*Server, error) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	clients := make(map[string]*jenkinsapi.API, len(cfg.Controllers))
	for _, controller := range cfg.Controllers {
		httpClient, err := jenkinsclient.New(controller, logger)
		if err != nil {
			return nil, err
		}
		clients[controller.ID] = jenkinsapi.New(controller.ID, httpClient)
	}
	auditer, err := audit.New(cfg.Audit)
	if err != nil {
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
		}),
		updateChecker: updateChecker,
	}, nil
}

func (s *Server) RunStdio(ctx context.Context) error {
	s.updateChecker.Start(ctx)
	return stdiotransport.Run(ctx, s.mcp.Raw())
}
