package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldAbortCoverageProbeHonorsCallerContextOnly(t *testing.T) {
	r := require.New(t)

	r.False(shouldAbortCoverageProbe(t.Context(), context.DeadlineExceeded), "per-endpoint timeout should remain optional")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r.True(shouldAbortCoverageProbe(ctx, context.Canceled), "caller cancellation should abort coverage probing")
}
