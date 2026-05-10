# Security

- Jenkins authentication is configured per controller and sent using Jenkins API-token/basic authentication.
- Credentials are not returned by configuration redaction helpers.
- Job configuration XML output is best-effort redacted and should still be treated as potentially sensitive because Jenkins plugins can store secrets in custom fields.
- Mutating MCP tools are disabled by default.
- Jenkins permissions remain authoritative for every request.
- Artifact downloads are constrained to the configured artifact directory and reject absolute paths or traversal.
- Trigger and cancel actions can emit append-only JSONL audit records.
