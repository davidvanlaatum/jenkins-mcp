package testutil

import (
	"context"
	"testing"

	"github.com/davidvanlaatum/dvgoutils/logging/testhandler"
)

// Context supplies the standard dvgoutils test logger and test lifetime.
func Context(t *testing.T) context.Context {
	t.Helper()
	ctx, _, _ := testhandler.SetupTestHandler(t)
	return ctx
}
