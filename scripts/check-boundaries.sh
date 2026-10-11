#!/usr/bin/env sh
set -eu

# The inbound server and outbound client adapters own the MCP SDK. Keep domain
# and orchestration packages (including their tests) independent of SDK types.
violations=$(find internal -name '*.go' ! -path 'internal/mcpserver/*' ! -path 'internal/mcpclient/*' -exec grep -l 'github.com/modelcontextprotocol/go-sdk/mcp' {} + || true)
if [ -n "$violations" ]; then
  echo "$violations" >&2
  echo "MCP SDK import leaked outside internal/mcpserver or internal/mcpclient" >&2
  exit 1
fi

go test -tags=no_integration ./...
