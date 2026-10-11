package testutil

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging"
)

// Context supplies a quiet logger while preserving the test lifetime.
func Context(t *testing.T) context.Context {
	t.Helper()
	return logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}
