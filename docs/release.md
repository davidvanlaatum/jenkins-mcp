# Release

## Build

Build a local binary:

```bash
go build -trimpath -o dist/jenkins-mcp-server ./cmd/jenkins-mcp-server
```

Build a container image:

```bash
docker build -t jenkins-mcp-server:local .
```

## Validate

Run all configured checks before publishing:

```bash
pre-commit run --all-files
```

This runs formatting, Go imports, lint, tests, build, and package-boundary checks.

## Versioning

The binary currently reports `0.1.0-dev`. For a release, update `internal/app.Version` or replace it through a future linker-injected version variable.

Recommended tag format:

```bash
git tag v0.1.0
```

## Artifacts

Release artifacts should include:

- platform-specific binaries
- container image
- example config
- MCP client configuration examples
- tool and security documentation

GoReleaser can be added later if multi-platform binary publishing becomes routine.
