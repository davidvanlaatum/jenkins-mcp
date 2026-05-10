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

Integration tests use `testcontainers-go` to build and start a Jenkins LTS
controller configured with JCasC and Job DSL. Docker must be available for the
Docker-backed tests to run; when Docker is unavailable, the testcontainers
provider check skips those tests.

To exclude Docker-backed integration tests explicitly, use the `no_integration`
build tag:

```bash
go test -tags=no_integration ./...
```
