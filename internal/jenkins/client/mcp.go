package client

import (
	"context"
	"encoding/json"

	"github.com/david/jenkins-mcp/internal/mcpclient"
)

// CallMCPTool uses this controller's endpoint, credentials, and HTTP policy.
// SDK types remain inside the upstream adapter.
func (c *Client) CallMCPTool(ctx context.Context, name string, arguments any) (json.RawMessage, bool, error) {
	return c.CallMCPToolPrepared(ctx, name, func() (any, error) { return arguments, nil })
}

// CallMCPToolPrepared defers argument preparation until the tool is discovered.
func (c *Client) CallMCPToolPrepared(ctx context.Context, name string, prepare func() (any, error)) (json.RawMessage, bool, error) {
	if c.mcpPluginMode == "off" {
		return nil, false, nil
	}
	endpoint, err := c.endpointURL("mcp-server/mcp", nil)
	if err != nil {
		return nil, false, err
	}
	return mcpclient.CallToolPrepared(ctx, c.http, endpoint.String(), c.username, c.token, name, prepare)
}
