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

### Release Number Policy

Release tags use `vMAJOR.MINOR.PATCH`. Choose the increment from the externally
observable `jenkins-mcp` contract, not from the size of the diff or the Jenkins
version used during development.

- **Patch** (`X.Y.Z+1`): backward-compatible bug fixes, security fixes,
  documentation-only changes, dependency or toolchain updates, packaging fixes,
  and internal implementation changes.
- **Minor** (`X.Y+1.0`): backward-compatible capabilities such as new tools,
  resources, prompts, optional inputs, optional output fields, CLI or
  configuration options, and supported Jenkins or plugin compatibility
  additions. Before `1.0.0`, breaking changes also use the minor component
  (`0.Y.0`); the `0.0.Z` line remains patch-only.
- **Major** (`X+1.0.0`): breaking changes after `1.0.0`, including removing or
  renaming a public tool, field, or CLI option; making an input required;
  changing output meanings, authentication, or safety requirements
  incompatibly; dropping a documented compatibility target; or changing a
  stable wire contract. `1.0.0` establishes the first stable contract and does
  not require a preceding `0.x` major release.

Introduce deprecations in a minor release and keep them documented for at least
one subsequent minor release before removal, unless a security issue or upstream
emergency requires otherwise. When a release contains several categories of
change, use the highest applicable increment. Pre-release and build-metadata tags
are not part of the current publishing policy.

Before selecting an exact version, preparing a release-specific change, creating
a tag, or pushing a tag, confirm the intended version and release scope with the
operator. Completed work and green CI do not authorize a release.

After that approval, create and push the authorized tag from an up-to-date
`main` commit:

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

## Self-Update

Installed binaries can be updated explicitly from the latest GitHub release:

```bash
jenkins-mcp-server --self-update
```

The updater selects the archive matching the current operating system and architecture, verifies the published SHA-256 checksum, extracts only the expected `jenkins-mcp-server` binary while ignoring safe GoReleaser metadata files such as README, LICENSE, and CHANGELOG, and then installs or stages it. Downloads are bounded by `updates.maxDownloadBytes`. macOS and Linux replace the current executable path with a verified temporary file. Windows stages `jenkins-mcp-server.exe.update` and a manifest next to the current executable so replacement can be completed after the IDE or MCP client exits.
