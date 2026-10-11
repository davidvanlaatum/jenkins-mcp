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
	ctx, closeLogging, err := ConfigureLogging(t.Context(), cfg.Logging)
	r.NoError(err)
	t.Cleanup(func() { _ = closeLogging() })
	_, err = New(ctx, cfg)
	r.NoError(err)
	logger := logging.FromContext(ctx)
	r.False(logger.Enabled(ctx, slog.LevelInfo))
	r.True(logger.Enabled(ctx, slog.LevelWarn))
	logger.InfoContext(ctx, "suppressed info")
	logger.WarnContext(ctx, "configured warning")
	logs, err := os.ReadFile(cfg.Logging.Path)
	r.NoError(err)
	r.Contains(string(logs), "configured warning")
	r.NotContains(string(logs), "suppressed info")
}
