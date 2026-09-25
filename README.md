# Jenkins MCP Server

[![CI](https://github.com/davidvanlaatum/jenkins-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/davidvanlaatum/jenkins-mcp/actions/workflows/ci.yml)
[![Release](https://github.com/davidvanlaatum/jenkins-mcp/actions/workflows/release.yml/badge.svg)](https://github.com/davidvanlaatum/jenkins-mcp/actions/workflows/release.yml)
[![Latest Release](https://img.shields.io/github/v/release/davidvanlaatum/jenkins-mcp?sort=semver)](https://github.com/davidvanlaatum/jenkins-mcp/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/davidvanlaatum/jenkins-mcp)](https://github.com/davidvanlaatum/jenkins-mcp/blob/main/go.mod)

Go-based MCP server for Jenkins diagnostics and guarded build actions. It runs over stdio, talks to Jenkins using external APIs, and does not require a Jenkins plugin.

## Current Tool Surface

### Read Tools
- `jenkins_get_capabilities`: Discover configured Jenkins controllers, response and log-cache limits, update-check status, optional capability warnings, and whether mutating tools are enabled. Agents should notify the user when `updates.updateAvailable` is `true`.
- `jenkins_resolve_build_url`: Resolve a Jenkins build URL to controller, job path, and build number.
- `jenkins_list_jobs`: List Jenkins jobs at the controller root or within a folder, with cursor pagination, optional recursive traversal, and filters for name, type, status, buildable/building state, last-build reference presence, last-build timestamps, last completed build JUnit summaries (`hasTests`, `hasFailedTests`, `hasSkippedTests`), whether the last completed build has Warnings NG issues, and whether the last completed build has coverage data (`hasCoverage`). JUnit, Warnings NG, and coverage filters are evaluated only when requested and use `lastCompletedBuild` summary probes. Each candidate job that survives cheaper filters may require summary probes until the requested page plus one extra match is found or candidates are exhausted; combine these filters with folder, name, type, status, buildable, building, or build metadata filters to reduce Jenkins API traffic. Job items include buildable status. Responses use `items`, `nextCursor`, `hasMore`, `truncated`, and `limit`.
- `jenkins_get_job`: Get Jenkins job metadata, recent build references, and parameter definitions.
- `jenkins_get_job_config`: Inspect Jenkins job configuration as a structured summary, best-effort redacted `config.xml`, or both. Falls back to safe job metadata when `config.xml` is not readable, such as when the caller lacks Job Configure or Extended Read permissions.
- `jenkins_list_builds`: List recent builds for a Jenkins job, with cursor pagination and filters for result, running/completed state, start timestamp, duration, estimated duration, keepLog, queueId, build number range, description text, and displayName text. Filters use fields from the build summary query and do not fetch full build details. Extended summaries include result, description, displayName, id, queueId, estimatedDuration, and keepLog. Responses use `items`, `nextCursor`, `hasMore`, `truncated`, and `limit`.
- `jenkins_get_build`: Get build details including result, causes, parameters, artifacts, changes, typed Warnings NG summary data when available, and optional typed coverage summaries from common coverage plugin endpoints.
- `jenkins_get_log`: Read a bounded progressive console log chunk, reusing the process-scoped disk cache when enabled. For Pipeline builds, prefer `jenkins_get_pipeline_node_log`.
- `jenkins_search_log`: Search the progressive console log across bounded server-side pages and return matching lines with optional context. Different searches reuse cached raw log pages. Use `maxScanBytes` to bound total bytes scanned; it defaults to 8 MiB and is capped at 64 MiB.
- `jenkins_tail_log`: Read the tail of a Jenkins console log using progressive log offsets and cached raw pages when available.
- `jenkins_get_test_report`: Fetch JUnit test summary and bounded compact test case metadata when available, with optional filters for status, exact suite/class/case name, substring or regex suite/case/class name, and duration. Filters apply before `limit`; summary counts remain full-report Jenkins counts. Broad requests omit bulky failure text, while exact `className` or `caseName` follow-up requests try to fetch failure details and stack traces for the returned matches and report whether detail enrichment succeeded with `failureDetailsIncluded`. If Jenkins does not expose a usable detail URL for a compact match, the compact match is still returned with `failureDetailsIncluded: false`.
- `jenkins_get_flaky_test_stats`: Analyze compact JUnit status histories across selected builds of one job, dropping builds with no JUnit data, counting state transitions only across reported observations, and returning sorted flaky-test stats plus failed-build references for targeted follow-up.
- `jenkins_get_pipeline_run`: Fetch Pipeline stage evidence and pending input-step actions using the Jenkins Pipeline REST wfapi endpoint.
- `jenkins_get_pipeline_stage`: Fetch Pipeline stage details and child flow nodes for a stage id.
- `jenkins_get_pipeline_node_log`: Fetch the bounded tail of a Pipeline flow node log through Jenkins' progressive-text endpoint, coalescing concurrent requests and reusing disk-cached raw pages when log caching is enabled.
- `jenkins_get_replay_scripts`: Fetch the native Jenkins Pipeline Replay script set for a build, including primary and loaded-script identifiers, script content, truncation metadata, and digests.
- `jenkins_watch_build`: Long-poll with short server-held state IDs (30-minute idle expiry) for completion or required input by default; use `waitFor: "change"` for stage-status changes; timeout responses omit build and Pipeline snapshots; prefer the configured wait default (2 minutes) or longer supported waits, shortening `waitTimeoutMs` only for known or observed host timeouts.
- `jenkins_watch_queue_item`: Uses short server-held state IDs (30-minute idle expiry) and omits item/build snapshots on timeout. Long-poll a Jenkins queue item watcher until stable queue fields change, it receives an executable build, is cancelled, disappears, or times out; Jenkins `why` text changes such as quiet-period countdowns do not wake the long poll by themselves, and prefer the configured wait default (2 minutes) or longer supported waits, shortening `waitTimeoutMs` only for known or observed host timeouts.
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
- `jenkins_replay_build`: Replay a Jenkins Pipeline build through native Pipeline Replay, optionally with full primary and loaded-script overrides.
- `jenkins_cancel_queue_item`: Cancel a queued Jenkins item. The tool is annotated as idempotent because repeating the cancellation cannot cause another queue-state transition.
- `jenkins_cancel_build`: Cancel a running Jenkins build. The tool is annotated as idempotent because repeating the cancellation cannot cause another build-state transition.

Jenkins-mutating tools are disabled unless explicitly enabled in configuration. `jenkins_download_artifact` does not change Jenkins state and is not gated by `mutations.enabled`, but it does write to the configured local artifact directory. `jenkins_update_server` is also separate from Jenkins mutations; it is disabled unless `updates.selfUpdateEnabled` is true because it writes to the local server installation.

> **Note:** When adding or modifying tools, ensure the tool definitions in `internal/mcpserver/server.go`, documentation in `docs/tools/jenkins.md`, and this list in `README.md` are all kept in sync.

MCP discovery identifies the server with a human-readable title, description, project URL, and usage instructions. Because the registered tool definitions are fixed for the lifetime of the process and are identical for every caller, the server advertises `tools.listChanged=false` and gives `tools/list` a one-hour public cache TTL. This caches tool definitions only; Jenkins data and tool-call results are not cached by MCP.

## Quick Start

```bash
export JENKINS_URL="https://jenkins.example.com"
export JENKINS_USER="your-user"
export JENKINS_TOKEN="your-api-token"
go run ./cmd/jenkins-mcp-server
```

Or run the published container image:

```bash
docker run --rm -i \
  -e JENKINS_URL \
  -e JENKINS_USER \
  -e JENKINS_TOKEN \
  ghcr.io/davidvanlaatum/jenkins-mcp:latest
```

See [Docker MCP client configuration](docs/operations.md#docker-mcp-client-configuration)
for a copy-paste stdio client definition, version pinning, config-file mounts,
and persistent artifact downloads.

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

Progressive console-log pages are cached in a private, process-scoped temporary directory by default. Concurrent requests for the same page share one Jenkins fetch, and later `jenkins_get_log`, `jenkins_search_log`, and `jenkins_tail_log` calls can reuse the downloaded bytes. The cache is bounded to 1 GiB by default, uses LRU eviction, and is removed during normal server shutdown. Configure or disable it with:

```json
{
  "logCache": {
    "enabled": true,
    "maxBytes": 1073741824
  }
}
```

The equivalent environment variables are `JENKINS_LOG_CACHE_ENABLED` and `JENKINS_LOG_CACHE_MAX_BYTES`. Cache files can contain Jenkins console output and should be treated as sensitive even though the directory and files use private permissions.

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

GitHub Actions runs file hygiene, tidy/import checks, lint, tests with coverage, package-boundary checks, builds, GoReleaser snapshot validation, and an informational Gremlins mutation-testing baseline over selected non-integration utility packages. Canonical `vMAJOR.MINOR.PATCH` tags are built and published with GoReleaser; other `v*` tags are rejected before publication. Releases also publish `ghcr.io/davidvanlaatum/jenkins-mcp` for Linux amd64 and arm64 with the release version and `latest` tags.

Release versions follow the [release number policy](docs/release.md#release-number-policy): patches are backward-compatible maintenance, minors add compatible capabilities (and carry pre-`1.0.0` breaking changes), and majors represent breaking stable-contract changes after `1.0.0`. Selecting or publishing an exact release version and commit requires explicit operator confirmation.

See also:

- `docs/operations.md`
- `docs/release.md`
- `docs/security.md`
- `docs/tools/jenkins.md`
- `examples/mcp-client/`
