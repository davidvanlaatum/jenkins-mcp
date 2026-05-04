# Operations

## Deployment Modes

The server is stdio-first for local IDE and agent integrations. Run it as a local binary or container process launched by an MCP client.

```bash
jenkins-mcp-server --config /path/to/config.json
```

For simple single-controller setups, environment variables are enough:

```bash
JENKINS_URL=https://jenkins.example.com \
JENKINS_USER=developer \
JENKINS_TOKEN=api-token \
jenkins-mcp-server
```

## Docker

Build:

```bash
docker build -t jenkins-mcp-server .
```

Run with a mounted config:

```bash
docker run --rm -i \
  -v "$PWD/examples/config/config.json:/config.json:ro" \
  -e JENKINS_MCP_CONFIG=/config.json \
  jenkins-mcp-server
```

## Configuration

Configuration precedence is:

1. flags
2. environment variables
3. config file
4. defaults

When `--config` and `JENKINS_MCP_CONFIG` are unset, the server tries default config paths in order and loads the first one that exists. Unix-like systems use `$XDG_CONFIG_HOME/jenkins-mcp/config.json`, then `~/.config/jenkins-mcp/config.json`; Windows uses `%APPDATA%\jenkins-mcp\config.json`, then `%USERPROFILE%\AppData\Roaming\jenkins-mcp\config.json`. Missing optional default files are ignored; normal validation still requires a configured Jenkins controller from a file or environment variables.

Run `jenkins-mcp-server --init` to create a starter config file at the default location, or `jenkins-mcp-server --init --config /path/to/config.json` to choose the path. Existing files are not overwritten.

Useful environment variables:

- `JENKINS_MCP_CONFIG`: JSON config file path.
- `JENKINS_URL`: single default controller URL.
- `JENKINS_USER`: Jenkins username.
- `JENKINS_TOKEN`: Jenkins API token.
- `JENKINS_MUTATIONS`: set to `true` to enable mutating tools.
- `JENKINS_ARTIFACT_DIR`: local artifact download directory.
- `JENKINS_AUDIT_PATH`: JSONL audit path for mutating actions.
- `JENKINS_MCP_LOG_LEVEL`: log verbosity; set to `debug` to include redacted Jenkins request URLs.
- `JENKINS_WATCH_POLL_INTERVAL_MS`: Jenkins polling interval during `jenkins_watch_build` long-polls in milliseconds. Default `3000`.
- `JENKINS_WATCH_DEFAULT_WAIT_TIMEOUT_MS`: default maximum duration for a `jenkins_watch_build` call in milliseconds when the request omits `waitTimeoutMs`. Default `120000`.
- `JENKINS_WATCH_MAX_WAIT_TIMEOUT_MS`: maximum allowed `waitTimeoutMs` for `jenkins_watch_build` in milliseconds. Default `900000`.
- `JENKINS_WATCH_MAX_CONSECUTIVE_FAILURES`: consecutive Jenkins poll failures tolerated before `jenkins_watch_build` returns an error. Default `3`.
- `JENKINS_MCP_UPDATE_CHECK`: set to `false` to disable periodic GitHub release checks. Default `true`.
- `JENKINS_MCP_UPDATE_REPOSITORY`: GitHub `owner/repo` used for release checks. Default `davidvanlaatum/jenkins-mcp`.
- `JENKINS_MCP_UPDATE_CHECK_INTERVAL_HOURS`: hours between release checks after startup. Default `24`.

The server checks the GitHub releases API at startup and periodically logs a warning when a newer release is available. The cached result is also returned by `jenkins_get_capabilities` under `updates`, so MCP clients and agents can surface it without reading process logs. When `updates.updateAvailable` is `true`, the response includes `updates.notificationHint` instructing agents to notify the user with the current version, latest version, and release URL. Release checks are best-effort: failures are logged at debug level and do not affect MCP requests. Disable this in network-restricted deployments:

```json
{
  "updates": {
    "enabled": false
  }
}
```

## Jenkins Mutations

Jenkins-mutating tools are disabled by default. Enable them only for trusted deployments:

```json
{
  "mutations": {
    "enabled": true
  },
  "audit": {
    "path": "/var/log/jenkins-mcp/audit.jsonl"
  }
}
```

Jenkins permissions are still authoritative. The server does not bypass Jenkins authorization.

`jenkins_download_artifact` is not gated by `mutations.enabled` because it does not change Jenkins state. It writes downloaded artifacts to the configured local artifact directory.

## Log and Response Limits

Large responses are bounded to keep MCP payloads useful:

- `limits.maxResponseBytes`: general response budget.
- `limits.logChunkBytes`: progressive log chunk budget.
- `limits.inlineBytes`: inline artifact budget.

Use `jenkins_get_log` and `jenkins_tail_log` with cursors rather than requesting complete logs. Use `jenkins_watch_build` for status-only long-polling on build completion or Pipeline stage-state changes.
