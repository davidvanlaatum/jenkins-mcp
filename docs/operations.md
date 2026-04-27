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

Useful environment variables:

- `JENKINS_MCP_CONFIG`: JSON config file path.
- `JENKINS_URL`: single default controller URL.
- `JENKINS_USER`: Jenkins username.
- `JENKINS_TOKEN`: Jenkins API token.
- `JENKINS_MUTATIONS`: set to `true` to enable mutating tools.
- `JENKINS_ARTIFACT_DIR`: local artifact download directory.
- `JENKINS_AUDIT_PATH`: JSONL audit path for mutating actions.

## Mutations

Mutating tools are disabled by default. Enable them only for trusted deployments:

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

## Log and Response Limits

Large responses are bounded to keep MCP payloads useful:

- `limits.maxResponseBytes`: general response budget.
- `limits.logChunkBytes`: progressive log chunk budget.
- `limits.inlineBytes`: inline artifact budget.

Use `jenkins_get_log`, `jenkins_tail_log`, and `jenkins_watch_build` with cursors rather than requesting complete logs.
