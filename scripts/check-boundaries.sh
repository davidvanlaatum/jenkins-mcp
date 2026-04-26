#!/usr/bin/env sh
set -eu

if grep -R "github.com/modelcontextprotocol/go-sdk/mcp" internal/tools internal/jenkins internal/config internal/artifacts internal/audit 2>/dev/null; then
  echo "MCP SDK import leaked outside internal/mcpserver" >&2
  exit 1
fi

go test ./...
