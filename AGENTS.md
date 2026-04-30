## Agent Notes (Project Development)

### Project Architecture
- **`cmd/jenkins-mcp-server`**: Entry point. Sets up logging, loads config, and starts the server.
- **`internal/app`**: Orchestrates dependency injection (config, audit, jenkins clients) and server lifecycle.
- **`internal/mcpserver`**: Defines the MCP server, registers tools, and handles error normalization.
- **`internal/tools/jenkins`**: Implements the tool handlers (business logic). This is the primary place for adding tool functionality.
- **`internal/jenkins/api`**: High-level Jenkins API logic, including tree-query construction and result parsing.
- **`internal/jenkins/client`**: Low-level HTTP client. Handles basic auth, Jenkins crumbs (CSRF), JSON decoding, and response size limits.
- **`internal/jenkins/model`**: Shared data structures used across the API and tool layers.
- **`internal/jenkins/urlx`**: Helpers for constructing Jenkins-specific URL paths safely.

### Workflow: Adding a New Tool
1.  **Define Models:** Add request/response types and data structures in `internal/jenkins/model/model.go`.
2.  **Implement API Call:** Add the corresponding method to `internal/jenkins/api/api.go`. Use tree queries (`?tree=...`) whenever possible to minimize data transfer.
3.  **Implement Handler:** Add the tool handler and request/response types to `internal/tools/jenkins/tools.go`.
4.  **Register Tool:** Register the new tool in `internal/mcpserver/server.go`. Ensure you use the appropriate `readOnlyTool`, `additiveMutationTool`, or `destructiveMutationTool` helper.
5.  **Update Documentation:** Update `docs/tools/jenkins.md` with the new tool details.
6.  **Update README:** Update the tool list and descriptions in `README.md`. **IMPORTANT:** All three locations (code, `docs/`, and `README.md`) must be kept in sync.

### Error Handling
- Use the `internal/errors` package for all domain-specific errors.
- Always use `apperrors.Wrap` to add context and machine-readable details.
- The `client.go` `classify` function maps HTTP status codes to internal `apperrors.Code` (e.g., 404 -> `CodeNotFound`).
- Errors returned to the MCP client are normalized and structured as JSON in the tool result.

### Testing & Regressions
- **Run all tests:** `./scripts/test.sh` or `go test ./...`.
- **Unit Tests:** Located alongside the code (e.g., `api_test.go`, `client_test.go`).
- **Integration Tests:** Use `mcp.NewInMemoryTransports()` for end-to-end tool testing in `server_test.go`.
- **Testdata:** Mocked Jenkins responses should be placed in `testdata/jenkins` and used for local verification when practical.
- When fixing a regression and adding a new test, prefer writing the regression test first when practical.
- Run the new regression test first to confirm it reproduces the bug before applying the fix.
- After the fix, rerun the relevant tests to confirm the regression is resolved.

### Coding Standards
- **Logging:** Use `log/slog`.
- **Validation:** Use the `internal/validation` package for common Jenkins inputs like job paths and build numbers.
- **Efficiency:**
    - For Pipeline builds, ALWAYS prefer stage-specific logs via `jenkins_get_pipeline_node_log`.
    - Use bounded readers (`readBounded`) and response limits from `config.LimitsConfig`.
- **Stability:** The `jenkins_watch_build` tool uses signed and compressed state tokens. Any changes to the `watchState` struct must be backward compatible if possible, or increment the version.

### Critical Considerations
- **CSRF:** The HTTP client automatically handles crumbs. Do not implement manual crumb fetching in the API layer.
- **Mutations:** Mutating tools must check `deps.Config.Mutations.Enabled` before proceeding.
- **Audit:** Emit audit events for all mutating operations using the `emit` helper in `tools.go`.
