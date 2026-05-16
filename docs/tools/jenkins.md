# Jenkins Tools

## Read Tools

- `jenkins_get_capabilities`: checks configured controllers, response limits, update-check status, installed plugins, optional capability warnings, and feature availability. When `updates.updateAvailable` is `true`, agents should notify the user using `updates.notificationHint`.
- `jenkins_resolve_build_url`: resolves a Jenkins build URL to controller, job path, and build number.
- `jenkins_list_jobs`: lists jobs at root or inside a folder, with optional recursive traversal, opaque cursor pagination, and filters for name, type, status, and building state. Responses use the shared paginated shape: `items`, `nextCursor`, `hasMore`, `truncated`, and `limit`; pass `nextCursor` back as `cursor` to continue with the same request filters. Cursors are signed with an in-memory key, so a server restart invalidates previously issued cursors. Name filters support case-insensitive `nameContains` matching and `nameRegex` matching against both `name` and `fullName`. Type filters accept friendly names such as `folder`, `pipeline`, `multibranch`, and `freestyle`, or raw Jenkins class names. Status filters use the derived `status` field, which treats Jenkins `disabled` and not-built `color` state first, then falls back to `lastCompletedBuild.result`, non-building `lastBuild.result`, and other Jenkins `color` values; `building` is derived from `lastBuild.building`. Recursive traversal scans until it either finds one additional matching job beyond the requested page or completes traversal, so `truncated` is accurate across folder boundaries when Jenkins can be scanned successfully.
- `jenkins_get_job`: returns job metadata, recent build references, and parameter definitions.
- `jenkins_get_job_config`: inspects job configuration for classic and auto-discovered jobs. `mode` defaults to `summary`; use `xml` for bounded best-effort redacted `config.xml` text or `both` for both forms. XML mode redacts common sensitive and high-risk fields, including credentials, tokens, passwords, secrets, scripts, commands, and generic values, and returns an `xml_best_effort_redaction` warning because plugin-specific XML may still require careful review. When Jenkins denies `config.xml` access, such as without Job Configure or Extended Read permissions, the tool returns fallback metadata from `api/json` with `configAccessible=false` and a structured warning. The summary extracts common multibranch, organization folder, branch-source, navigator, project factory, trigger, trait, and Pipeline script-path details when present.
- `jenkins_list_builds`: lists recent builds for a job with opaque cursor pagination. Responses use the shared paginated shape: `items`, `nextCursor`, `hasMore`, `truncated`, and `limit`; pass `nextCursor` back as `cursor` to continue listing the same job. Cursors are signed with an in-memory key, so a server restart invalidates previously issued cursors; callers should restart pagination from the first page if a cursor becomes invalid. Build summaries include `result` (the Jenkins build status), `description`, `displayName`, `id`, `queueId`, `estimatedDuration`, and `keepLog`.
- `jenkins_get_build`: returns build metadata, causes, parameters, artifacts, changes, and optional typed coverage summaries. It probes common coverage endpoints (`coverage`, `coverage/result`, and `jacoco`) after loading the build, omits coverage when endpoints are missing, returns all available summaries in probe order, and records non-fatal coverage endpoint errors without failing the build lookup.
- `jenkins_get_log`: reads bounded progressive log chunks with `start` and `nextStart` offsets. For Pipeline builds, prefer `jenkins_get_pipeline_node_log` for specific stages.
- `jenkins_search_log`: searches a bounded log chunk and can include context lines.
- `jenkins_tail_log`: reads the tail of the console log using Jenkins progressive log offsets.
- `jenkins_get_test_report`: returns JUnit summary and bounded case details.
- `jenkins_get_pipeline_run`: returns Pipeline stages and pending input-step actions through Jenkins `wfapi` when the endpoints are available. Pipeline runs include `waitingForInput` and `pendingInputActions` when Jenkins reports a paused input step. If the supplemental pending-input endpoint fails after stage data is available, the run is still returned with `pendingInputError`.
- `jenkins_get_pipeline_stage`: returns a Pipeline stage and its child flow nodes.
- `jenkins_get_pipeline_node_log`: returns bounded log output for a Pipeline flow node. Prefer this over `jenkins_get_log` for Pipeline builds.
- `jenkins_watch_build`: bootstraps or long-polls for build completion, Pipeline stage-status changes, or pending input-step changes, returning an opaque watch state token plus current build and Pipeline status. The first call without `lastState` returns immediately to establish `watch.state`; callers should pass that token back as `lastState` on the next request, and can use `waitTimeoutMs` to bound how long a single subsequent long-poll call stays open. Invalid or expired `lastState` values return `invalid_request`; callers should restart from a fresh bootstrap call without `lastState`. Watch-state tokens are signed with an in-memory per-process key, so any server restart or rollout invalidates previously issued tokens and requires re-bootstrap. Prefer this over `jenkins_get_build` when waiting for a running build to progress; use `jenkins_get_build` for one-off snapshots.
- `jenkins_watch_queue_item`: bootstraps or long-polls a Jenkins queue item, returning an opaque `watch.state` token plus explicit queue status. The first call without `lastState` returns immediately; follow-up calls pass `lastState` and wait until the item changes, receives an executable build, is cancelled, disappears from Jenkins' queue API, or reaches `waitTimeoutMs`. Terminal statuses are `executable`, `cancelled`, and `disappeared`; executable responses include `watch.build` with `{ controller, job, build, url }` when Jenkins provides a build URL that matches a configured controller, so callers can immediately switch to `jenkins_watch_build`. Invalid or expired `lastState` values return `invalid_request` and require a fresh bootstrap call.
- `jenkins_get_issues`: probes common Warnings NG / analysis endpoints and returns a summary when available.
- `jenkins_get_changes`: returns SCM change sets exposed on the build.
- `jenkins_list_artifacts`: lists build artifacts without fetching content.
- `jenkins_read_artifact`: returns small UTF-8 text artifacts inline within configured size limits.
- `jenkins_list_queue`: lists current Jenkins queue items.
- `jenkins_get_queue_item`: inspects a queue item by id.

## Local File Tools

- `jenkins_download_artifact`: writes an artifact into the configured safe local directory. This does not change Jenkins state and does not require `mutations.enabled`; it is annotated as a non-destructive side-effect tool because it writes to local disk.

## Jenkins-Mutating Tools

- `jenkins_trigger_build`: triggers a build or parameterized build.
- `jenkins_cancel_queue_item`: cancels a queued Jenkins item.
- `jenkins_cancel_build`: stops a running build.

Jenkins-mutating tools require `mutations.enabled` or `JENKINS_MUTATIONS=true`. Trigger and cancel attempts emit JSONL audit events when `audit.path` is configured. Artifact downloads are local file side effects rather than Jenkins mutations, so they remain available when Jenkins mutations are disabled.

## Diagnostics Logging

Server logs go to stderr by default. Set `JENKINS_MCP_LOG_FILE` or `logging.path` to append logs to a file, which can be easier to inspect when MCP hosts hide stdio server logs.

Set `JENKINS_MCP_LOG_TOOL_CALLS=true` or `logging.toolCalls=true` to log MCP tool call start and finish events with tool name, duration, success/error state, normalized error code, and response size. Full tool arguments and responses are only logged when `JENKINS_MCP_LOG_TOOL_PAYLOADS=true` or `logging.toolPayloads=true`; this is intended for local debugging and may include sensitive Jenkins data such as job names, build parameters, console log excerpts, and artifact content.

## Capability Discovery

`jenkins_get_capabilities` treats Jenkins plugin metadata as optional. If the configured Jenkins user cannot access `pluginManager`, the controller remains available and the response includes a structured `warnings` entry with `optional: true`; the legacy `error` field remains populated for compatibility.

Disable optional plugin discovery in restricted Jenkins deployments with `JENKINS_MCP_PLUGIN_DISCOVERY=false` or:

```json
{
  "capabilities": {
    "pluginDiscoveryEnabled": false
  }
}
```
