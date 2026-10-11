package client

import (
	"context"
	"encoding/json"

	"github.com/david/jenkins-mcp/internal/mcpclient"
)

// CallMCPTool uses this controller's endpoint, credentials, and HTTP policy.
// SDK types remain inside the upstream adapter.
func (c *Client) CallMCPTool(ctx context.Context, name string, arguments any) (json.RawMessage, bool, error) {
	if c.mcpPluginMode == "off" {
		return nil, false, nil
	}
	endpoint, err := c.endpointURL("mcp-server/mcp", nil)
	if err != nil {
		return nil, false, err
	}
	return mcpclient.CallTool(ctx, c.http, endpoint.String(), c.username, c.token, name, arguments)
}
