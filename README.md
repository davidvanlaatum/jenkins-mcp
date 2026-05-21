# Jenkins MCP Server

[![CI](https://github.com/davidvanlaatum/jenkins-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/davidvanlaatum/jenkins-mcp/actions/workflows/ci.yml)
[![Release](https://github.com/davidvanlaatum/jenkins-mcp/actions/workflows/release.yml/badge.svg)](https://github.com/davidvanlaatum/jenkins-mcp/actions/workflows/release.yml)
[![Latest Release](https://img.shields.io/github/v/release/davidvanlaatum/jenkins-mcp?sort=semver)](https://github.com/davidvanlaatum/jenkins-mcp/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/davidvanlaatum/jenkins-mcp)](https://github.com/davidvanlaatum/jenkins-mcp/blob/main/go.mod)

Go-based MCP server for Jenkins diagnostics and guarded build actions. It runs over stdio, talks to Jenkins using external APIs, and does not require a Jenkins plugin.

## Current Tool Surface

### Read Tools
- `jenkins_get_capabilities`: Discover configured Jenkins controllers, response limits, update-check status, optional capability warnings, and whether mutating tools are enabled. Agents should notify the user when `updates.updateAvailable` is `true`.
- `jenkins_resolve_build_url`: Resolve a Jenkins build URL to controller, job path, and build number.
- `jenkins_list_jobs`: List Jenkins jobs at the controller root or within a folder, with cursor pagination, optional recursive traversal, and filters for name, type, status, buildable/building state, last-build reference presence, last-build timestamps, last completed build JUnit summaries (`hasTests`, `hasFailedTests`, `hasSkippedTests`), whether the last completed build has Warnings NG issues, and whether the last completed build has coverage data (`hasCoverage`). JUnit, Warnings NG, and coverage filters are evaluated only when requested and use `lastCompletedBuild` summary probes. Each candidate job that survives cheaper filters may require summary probes until the requested page plus one extra match is found or candidates are exhausted; combine these filters with folder, name, type, status, buildable, building, or build metadata filters to reduce Jenkins API traffic. Job items include buildable status. Responses use `items`, `nextCursor`, `hasMore`, `truncated`, and `limit`.
- `jenkins_get_job`: Get Jenkins job metadata, recent build references, and parameter definitions.
- `jenkins_get_job_config`: Inspect Jenkins job configuration as a structured summary, best-effort redacted `config.xml`, or both. Falls back to safe job metadata when `config.xml` is not readable, such as when the caller lacks Job Configure or Extended Read permissions.
- `jenkins_list_builds`: List recent builds for a Jenkins job, with cursor pagination and filters for result, running/completed state, start timestamp, duration, estimated duration, keepLog, queueId, build number range, description text, and displayName text. Filters use fields from the build summary query and do not fetch full build details. Extended summaries include result, description, displayName, id, queueId, estimatedDuration, and keepLog. Responses use `items`, `nextCursor`, `hasMore`, `truncated`, and `limit`.
- `jenkins_get_build`: Get build details including result, causes, parameters, artifacts, changes, typed Warnings NG summary data when available, and optional typed coverage summaries from common coverage plugin endpoints.
- `jenkins_get_log`: Read a bounded progressive console log chunk. For Pipeline builds, prefer `jenkins_get_pipeline_node_log`.
- `jenkins_search_log`: Search a bounded console log chunk for text and return matching lines.
- `jenkins_tail_log`: Read the tail of a Jenkins console log using progressive log offsets.
- `jenkins_get_test_report`: Fetch JUnit test summary and bounded test case details when available.
- `jenkins_get_pipeline_run`: Fetch Pipeline stage evidence and pending input-step actions using the Jenkins Pipeline REST wfapi endpoint.
- `jenkins_get_pipeline_stage`: Fetch Pipeline stage details and child flow nodes for a stage id.
- `jenkins_get_pipeline_node_log`: Fetch bounded log output for a Pipeline flow node id.
- `jenkins_watch_build`: Long-poll a Jenkins build watcher for completion, stage-status changes, or pending input-step changes; keep `waitTimeoutMs` below any MCP host tool-call timeout.
- `jenkins_watch_queue_item`: Long-poll a Jenkins queue item watcher until stable queue fields change, it receives an executable build, is cancelled, disappears, or times out; Jenkins `why` text changes such as quiet-period countdowns do not wake the long poll by themselves, and `waitTimeoutMs` should stay below any MCP host tool-call timeout.
- `jenkins_list_issues`: List paged, typed Warnings NG issues for a build. The response includes discovered tools so callers can select a tool when a build has multiple Warnings NG results.
- `jenkins_get_changes`: Fetch SCM change sets for a Jenkins build.
- `jenkins_list_artifacts`: List artifacts for a Jenkins build.
- `jenkins_read_artifact`: Read a small text Jenkins artifact inline.
- `jenkins_list_queue`: List current Jenkins queue items.
- `jenkins_get_queue_item`: Inspect a Jenkins queue item by id.

### Local File Tools
- `jenkins_download_artifact`: Download a Jenkins artifact to the configured safe local directory. Does not require `mutations.enabled`.
- `jenkins_update_server`: Download, verify, and install or stage the latest released server binary. Requires `updates.selfUpdateEnabled`.

### Jenkins-Mutating Tools
- `jenkins_trigger_build`: Trigger a Jenkins build (parameterized or standard).
- `jenkins_cancel_queue_item`: Cancel a queued Jenkins item.
- `jenkins_cancel_build`: Cancel a running Jenkins build.

Jenkins-mutating tools are disabled unless explicitly enabled in configuration. `jenkins_download_artifact` does not change Jenkins state and is not gated by `mutations.enabled`, but it does write to the configured local artifact directory. `jenkins_update_server` is also separate from Jenkins mutations; it is disabled unless `updates.selfUpdateEnabled` is true because it writes to the local server installation.

> **Note:** When adding or modifying tools, ensure the tool definitions in `internal/mcpserver/server.go`, documentation in `docs/tools/jenkins.md`, and this list in `README.md` are all kept in sync.

## Quick Start

```bash
export JENKINS_URL="https://jenkins.example.com"
export JENKINS_USER="your-user"
export JENKINS_TOKEN="your-api-token"
go run ./cmd/jenkins-mcp-server
```

To explicitly update an installed binary from the latest GitHub release:

```bash
jenkins-mcp-server --self-update
```

For request URL troubleshooting, enable debug logs:

```bash
export JENKINS_MCP_LOG_LEVEL=debug
```

By default logs are written to stderr. To make MCP server logs easier to inspect from hosts that hide stderr, write them to a file:

```bash
export JENKINS_MCP_LOG_FILE=/tmp/jenkins-mcp-server.log
```

For MCP tool-call diagnostics, enable start/finish logging:

```bash
export JENKINS_MCP_LOG_TOOL_CALLS=true
```

Full tool arguments and responses are not logged unless explicitly opted in. This can expose Jenkins job names, parameters, log text, artifact text, and other sensitive data:

```bash
export JENKINS_MCP_LOG_TOOL_PAYLOADS=true
```

For Jenkins-mutating actions:

```bash
export JENKINS_MUTATIONS=true
export JENKINS_AUDIT_PATH=/tmp/jenkins-mcp-audit.jsonl
```

## Configuration

Configuration precedence is:

1. Flags
2. Environment variables
3. JSON config file
4. Defaults

Use `--config examples/config/config.json` for file-based configuration, or run `jenkins-mcp-server --init` to create a starter config file at the default location.

If `--config` and `JENKINS_MCP_CONFIG` are not set, the server tries default config paths in order and loads the first one that exists. Unix-like systems use `$XDG_CONFIG_HOME/jenkins-mcp/config.json`, then `~/.config/jenkins-mcp/config.json`; Windows uses `%APPDATA%\jenkins-mcp\config.json`, then `%USERPROFILE%\AppData\Roaming\jenkins-mcp\config.json`. Missing optional default files are ignored; normal validation still requires a configured Jenkins controller from a file or environment variables.

The server checks GitHub releases once at startup and then every 24 hours by default. Disable or retarget the check with:

```bash
export JENKINS_MCP_UPDATE_CHECK=false
export JENKINS_MCP_UPDATE_REPOSITORY=your-org/jenkins-mcp
export JENKINS_MCP_UPDATE_CHECK_INTERVAL_HOURS=24
```

`jenkins_get_capabilities` normally queries Jenkins `pluginManager` to derive plugin-backed feature flags. In restricted Jenkins deployments where normal users cannot access plugin metadata, disable this optional discovery with:

```bash
export JENKINS_MCP_PLUGIN_DISCOVERY=false
```

or:

```json
{
  "capabilities": {
    "pluginDiscoveryEnabled": false
  }
}
```

Logging can also be configured in JSON:

```json
{
  "logging": {
    "level": "info",
    "path": "/tmp/jenkins-mcp-server.log",
    "toolCalls": true,
    "toolPayloads": false
  }
}
```

## Development

```bash
pre-commit run --all-files
go test ./...
go build ./cmd/jenkins-mcp-server
```

`go test ./...` includes Docker-backed integration tests when Docker is
available. Those tests build a dedicated Jenkins LTS image with JCasC, Job DSL,
and the plugins needed by the MCP tool surface. Exclude them with
`go test -tags=no_integration ./...`.

GitHub Actions runs file hygiene, tidy/import checks, lint, tests with coverage, package-boundary checks, builds, GoReleaser snapshot validation, and an informational Gremlins mutation-testing baseline over selected non-integration utility packages. Tagged releases matching `v*` are built and published with GoReleaser.

See also:

- `docs/operations.md`
- `docs/release.md`
- `docs/security.md`
- `docs/tools/jenkins.md`
- `examples/mcp-client/`
