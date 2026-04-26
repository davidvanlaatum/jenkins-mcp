package stdio

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context, server *mcp.Server) error {
	return server.Run(ctx, &mcp.StdioTransport{})
}
