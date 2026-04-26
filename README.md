# Jenkins MCP Server

Go-based MCP server for Jenkins diagnostics and guarded build actions. It runs over stdio, talks to Jenkins using external APIs, and does not require a Jenkins plugin.

## Current Tool Surface

- `jenkins_get_capabilities`
- `jenkins_list_jobs`
- `jenkins_list_builds`
- `jenkins_get_build`
- `jenkins_get_log`
- `jenkins_search_log`
- `jenkins_tail_log`
- `jenkins_get_test_report`
- `jenkins_get_pipeline_run`
- `jenkins_get_pipeline_stage`
- `jenkins_get_pipeline_node_log`
- `jenkins_watch_build`
- `jenkins_get_coverage`
- `jenkins_get_issues`
- `jenkins_read_artifact`
- `jenkins_download_artifact`
- `jenkins_trigger_build`
- `jenkins_list_queue`
- `jenkins_get_queue_item`
- `jenkins_cancel_queue_item`
- `jenkins_cancel_build`

Mutating tools are disabled unless explicitly enabled in configuration.

## Quick Start

```bash
export JENKINS_URL="https://jenkins.example.com"
export JENKINS_USER="your-user"
export JENKINS_TOKEN="your-api-token"
go run ./cmd/jenkins-mcp-server
```

For mutating actions:

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

Use `--config examples/config/config.json` for file-based configuration.

## Development

```bash
go test ./...
go build ./cmd/jenkins-mcp-server
```
