package app

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/david/jenkins-mcp/internal/config"
	"github.com/davidvanlaatum/dvgoutils/logging"
	"github.com/stretchr/testify/require"
)

func TestStartupContextUsesConfiguredLogFileAndLevel(t *testing.T) {
	r := require.New(t)
	cfg := config.Defaults()
	cfg.Logging.Path = filepath.Join(t.TempDir(), "logs", "server.log")
	cfg.Logging.Level = "warn"
	cfg.LogCache.Enabled = false
	server, err := New(t.Context(), cfg)
	r.NoError(err)
	t.Cleanup(func() { _ = closeLogFile(server.logFile) })
	logger := logging.FromContext(server.ctx)
	r.False(logger.Enabled(server.ctx, slog.LevelInfo))
	r.True(logger.Enabled(server.ctx, slog.LevelWarn))
	logger.InfoContext(server.ctx, "suppressed info")
	logger.WarnContext(server.ctx, "configured warning")
	logs, err := os.ReadFile(cfg.Logging.Path)
	r.NoError(err)
	r.Contains(string(logs), "configured warning")
	r.NotContains(string(logs), "suppressed info")
}
