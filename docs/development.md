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

## Mutation testing

CI runs Gremlins mutation testing as an informational baseline job. The workflow
uses `go-gremlins/gremlins-action@v1` with Gremlins `v0.6.0` so the tool version
is fixed while still using the official action wrapper.

The initial package scope is intentionally narrow and avoids Docker-backed
integration tests:

- `internal/artifacts`
- `internal/jenkins/urlx`
- `internal/pagination`
- `internal/security`

Those packages are stable utility code with fast local unit tests, which makes
them useful for baseline mutation signal without making every pull request wait
on a full-repository mutation run. The GitHub Actions job has a 10 minute
timeout, uses two Gremlins workers, passes `-tags=no_integration`, and is marked
`continue-on-error` while baseline results are collected. Surviving mutations are
visible in the workflow logs and should be reviewed before deciding whether to
add a blocking threshold or expand package coverage.

Run the same scope locally with:

```bash
go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
for package in internal/artifacts internal/jenkins/urlx internal/pagination internal/security; do
  (cd "$package" && gremlins unleash --tags=no_integration --workers=2 --test-cpu=1 --timeout-coefficient=3 --output-statuses=lctv)
done
```
