# Jenkins Tools

## Read Tools

- `jenkins_get_capabilities`: checks configured controllers, response limits, update-check status, installed plugins, optional capability warnings, and feature availability. When `updates.updateAvailable` is `true`, agents should notify the user using `updates.notificationHint`.
- `jenkins_resolve_build_url`: resolves a Jenkins build URL to controller, job path, and build number.
- `jenkins_list_jobs`: lists jobs at root or inside a folder, with optional recursive traversal, opaque cursor pagination, and filters for name, type, status, and building state. Responses use the shared paginated shape: `items`, `nextCursor`, `hasMore`, `truncated`, and `limit`; pass `nextCursor` back as `cursor` to continue with the same request filters. Cursors are signed with an in-memory key, so a server restart invalidates previously issued cursors. Name filters support case-insensitive `nameContains` matching and `nameRegex` matching against both `name` and `fullName`. Type filters accept friendly names such as `folder`, `pipeline`, `multibranch`, and `freestyle`, or raw Jenkins class names. Status filters use the derived `status` field, which treats Jenkins `disabled` and not-built `color` state first, then falls back to `lastCompletedBuild.result`, non-building `lastBuild.result`, and other Jenkins `color` values; `building` is derived from `lastBuild.building`. Recursive traversal scans until it either finds one additional matching job beyond the requested page or completes traversal, so `truncated` is accurate across folder boundaries when Jenkins can be scanned successfully.
- `jenkins_get_job`: returns job metadata, recent build references, and parameter definitions.
- `jenkins_list_builds`: lists recent builds for a job with opaque cursor pagination. Responses use the shared paginated shape: `items`, `nextCursor`, `hasMore`, `truncated`, and `limit`; pass `nextCursor` back as `cursor` to continue listing the same job. Cursors are signed with an in-memory key, so a server restart invalidates previously issued cursors; callers should restart pagination from the first page if a cursor becomes invalid. Build summaries include `result` (the Jenkins build status), `description`, `displayName`, `id`, `queueId`, `estimatedDuration`, and `keepLog`.
- `jenkins_get_build`: returns build metadata, causes, parameters, artifacts, and changes.
- `jenkins_get_log`: reads bounded progressive log chunks with `start` and `nextStart` offsets. For Pipeline builds, prefer `jenkins_get_pipeline_node_log` for specific stages.
- `jenkins_search_log`: searches a bounded log chunk and can include context lines.
- `jenkins_tail_log`: reads the tail of the console log using Jenkins progressive log offsets.
- `jenkins_get_test_report`: returns JUnit summary and bounded case details.
- `jenkins_get_pipeline_run`: returns Pipeline stages and pending input-step actions through Jenkins `wfapi` when the endpoints are available. Pipeline runs include `waitingForInput` and `pendingInputActions` when Jenkins reports a paused input step. If the supplemental pending-input endpoint fails after stage data is available, the run is still returned with `pendingInputError`.
- `jenkins_get_pipeline_stage`: returns a Pipeline stage and its child flow nodes.
- `jenkins_get_pipeline_node_log`: returns bounded log output for a Pipeline flow node. Prefer this over `jenkins_get_log` for Pipeline builds.
- `jenkins_watch_build`: bootstraps or long-polls for build completion, Pipeline stage-status changes, or pending input-step changes, returning an opaque watch state token plus current build and Pipeline status. The first call without `lastState` returns immediately to establish `watch.state`; callers should pass that token back as `lastState` on the next request, and can use `waitTimeoutMs` to bound how long a single subsequent long-poll call stays open. Invalid or expired `lastState` values return `invalid_request`; callers should restart from a fresh bootstrap call without `lastState`. Watch-state tokens are signed with an in-memory per-process key, so any server restart or rollout invalidates previously issued tokens and requires re-bootstrap. Prefer this over `jenkins_get_build` when waiting for a running build to progress; use `jenkins_get_build` for one-off snapshots.
- `jenkins_get_coverage`: probes common coverage plugin endpoints and returns a summary when available.
- `jenkins_get_issues`: probes common Warnings NG / analysis endpoints and returns a summary when available.
- `jenkins_get_changes`: returns SCM change sets exposed on the build.
- `jenkins_list_artifacts`: lists build artifacts without fetching content.
- `jenkins_read_artifact`: returns small UTF-8 text artifacts inline within configured size limits.
- `jenkins_list_queue`: lists current Jenkins queue items.
- `jenkins_get_queue_item`: inspects a queue item by id.

## Local File Tools

- `jenkins_download_artifact`: writes an artifact into the configured safe local directory. This does not change Jenkins state.

## Jenkins-Mutating Tools

- `jenkins_trigger_build`: triggers a build or parameterized build.
- `jenkins_cancel_queue_item`: cancels a queued Jenkins item.
- `jenkins_cancel_build`: stops a running build.

Jenkins-mutating tools require `mutations.enabled` or `JENKINS_MUTATIONS=true`. Trigger and cancel attempts emit JSONL audit events when `audit.path` is configured.

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
