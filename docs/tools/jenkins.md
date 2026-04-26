# Jenkins Tools

## Read Tools

- `jenkins_get_capabilities`: checks configured controllers and response limits.
- `jenkins_list_jobs`: lists jobs at root or inside a folder.
- `jenkins_list_builds`: lists recent builds for a job.
- `jenkins_get_build`: returns build metadata, causes, parameters, artifacts, and changes.
- `jenkins_get_log`: reads bounded progressive log chunks with `start` and `nextStart` offsets.
- `jenkins_get_test_report`: returns JUnit summary and bounded case details.
- `jenkins_get_pipeline_run`: returns Pipeline stages through Jenkins `wfapi` when the plugin endpoint is available.
- `jenkins_download_artifact`: writes an artifact into the configured safe local directory.
- `jenkins_get_queue_item`: inspects a queue item by id.

## Mutating Tools

- `jenkins_trigger_build`: triggers a build or parameterized build.
- `jenkins_cancel_build`: stops a running build.

Mutating tools require `mutations.enabled` or `JENKINS_MUTATIONS=true`. Trigger and cancel attempts emit JSONL audit events when `audit.path` is configured.
