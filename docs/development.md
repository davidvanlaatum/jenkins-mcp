# Development

The project follows the package boundaries from the planning architecture:

- `cmd/jenkins-mcp-server`: thin process entrypoint.
- `internal/app`: dependency assembly.
- `internal/mcpserver`: official Go MCP SDK boundary.
- `internal/tools/jenkins`: tool contracts and orchestration.
- `internal/jenkins`: Jenkins HTTP client, endpoint adapters, and normalized models.

Run validation with:

```bash
go test ./...
```
