package observability

import "log/slog"

func NopLogger() *slog.Logger { return slog.Default() }
