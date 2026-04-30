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

GitHub Actions runs file hygiene, tidy/import checks, lint, tests with coverage, package-boundary checks, builds, and GoReleaser snapshot validation for pushes to `main` and pull requests.

## Versioning

The binary reports `0.1.0-dev` for local builds. GoReleaser injects the tag version at release time.

Recommended tag format:

```bash
git tag v0.1.0
git push origin v0.1.0
```

## Artifacts

GoReleaser publishes GitHub release artifacts for:

- Linux, macOS, and Windows
- amd64 and arm64
- checksums.txt

Run a local snapshot build before tagging if you want to inspect generated archives:

```bash
goreleaser release --snapshot --clean --skip=publish
```
