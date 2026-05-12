---
stepsCompleted:
  - 1
  - 2
  - 3
  - 4
  - 5
  - 6
  - 7
  - 8
inputDocuments:
  - /Users/david/bmad/_bmad-output/planning-artifacts/prd.md
  - /Users/david/bmad/_bmad-output/planning-artifacts/prd-validation-report.md
  - /Users/david/bmad/_bmad-output/planning-artifacts/product-brief-bmad.md
workflowType: 'architecture'
lastStep: 8
status: 'complete'
completedAt: '2026-04-25'
project_name: 'Go Jenkins MCP Server'
user_name: 'David'
date: '2026-04-25'
---

# Architecture Decision Document

_This document builds collaboratively through step-by-step discovery. Sections are appended as we work through each architectural decision together._

## Project Context Analysis

### Requirements Overview

**Functional Requirements:**

The PRD defines 63 functional requirements across these capability areas:

- Jenkins controller configuration and discovery
- Job and build discovery
- Build output and running build inspection
- Pipeline stage/node/step evidence
- JUnit, coverage, and `recordIssues` evidence
- SCM and change evidence
- Artifact access
- Build actions and queue operations
- Capability discovery and structured error handling
- Packaging, documentation, and developer experience

Architecturally, this is an external integration server with a broad but coherent tool surface. The system needs a stable MCP-facing contract while hiding Jenkins API and plugin variability behind internal adapters.

**Non-Functional Requirements:**

The architecture is shaped by these NFRs:

- Bounded MCP responses for large Jenkins data, initially targeting 64 KiB text payloads.
- Incremental log retrieval, initially targeting 32-64 KiB log chunks.
- Running build watch with cursor-style continuation and 2-10 second polling guidance.
- Secure Jenkins credential handling with no credential leakage in logs or MCP responses.
- Explicit configuration gates for mutating actions.
- Audit records for trigger/cancel actions.
- Safe artifact download paths.
- Graceful degradation for missing Jenkins plugins or unavailable endpoints.
- Standalone Go binary and Docker packaging.
- Official Go MCP SDK preferred unless architecture identifies a concrete blocker.

**Scale & Complexity:**

- Primary domain: developer infrastructure / CI integration server
- Complexity level: medium-high
- Estimated architectural components: 8-10 major components

The complexity is not UI or business-domain complexity. It comes from Jenkins variability, plugin-dependent APIs, large data handling, secure mutation controls, and the need for a clean MCP tool contract.

### Technical Constraints & Dependencies

Known constraints:

- Must be external-only; no Jenkins plugin installation.
- Must be implemented in Go.
- Should use the official Go MCP SDK unless a blocker is documented.
- Must ship as both standalone binary and Docker image.
- Must support one or more Jenkins controllers with a default-controller path.
- Must rely on Jenkins permissions rather than bypassing Jenkins authorization.
- Must support plugin-aware behavior for Pipeline, JUnit, Coverage, and Warnings NG / `recordIssues`.
- Must support both local developer use and shared internal deployment.
- Must support JetBrains-oriented validation while remaining generally MCP-compatible.

Likely external dependencies:

- Jenkins Remote Access API
- Jenkins progressive log endpoints
- Jenkins queue/build/artifact APIs
- Pipeline REST or workflow-related endpoints where available
- JUnit report endpoints
- Coverage plugin APIs where available
- Warnings NG / `recordIssues` APIs where available
- Official Go MCP SDK

### Cross-Cutting Concerns Identified

- Capability discovery across controllers, job types, and plugins
- Consistent structured error taxonomy
- Bounded response shaping for model context limits
- Cursor/offset handling for logs and running build watch
- Credential redaction and secret-safe logging
- Mutating-action authorization, configuration, validation, and audit
- Artifact filesystem safety
- Jenkins API/plugin adapter isolation
- Tool schema stability before open-source readiness
- Representative fixtures for Jenkins data shapes and missing-plugin cases

## Starter Template Evaluation

### Primary Technology Domain

Backend/developer-tool integration server.

This is not a web app, mobile app, or UI-heavy tool. It is a Go-based MCP server with Jenkins integration, packaging, configuration, and test fixture needs.

### Starter Options Considered

**Option 1: Official Go MCP SDK example as the base**

Use a normal Go module and seed the MCP server from the official SDK examples. The SDK provides server/client APIs, transports including stdio, tool registration, JSON-RPC internals, auth primitives, and examples.

Pros:

- Directly aligns with PRD requirement to use official Go MCP SDK.
- Avoids unnecessary framework weight.
- Keeps MCP tool contracts central.
- Good fit for a stdio-first MCP server used by IDE agents.
- Lets architecture define clean internal packages around Jenkins adapters, tools, config, audit, and downloads.

Cons:

- Requires us to define project structure ourselves.
- No full application scaffold.
- SDK API maturity remains an architecture risk that must be isolated.

**Option 2: Cobra CLI scaffold**

Cobra is current and widely used for Go CLIs; package docs show `v1.10.2`. It could help if the binary needs many subcommands or rich CLI UX.

Pros:

- Good CLI command/flag ergonomics.
- Useful if commands like `serve`, `validate-config`, `capabilities`, or `version` become real product needs.

Cons:

- Adds a dependency before the command surface exists.
- MCP servers initially need a narrow entrypoint: run stdio server with config.
- Native Go flags or a small config loader are likely enough for v1.

**Option 3: Generic Go service starter / standard project layout**

Use a general Go repository structure with `cmd/`, `internal/`, Dockerfile, CI, and tests.

Pros:

- Familiar structure for a Go backend.
- Good for larger projects.

Cons:

- "Standard layout" is a convention, not a maintained starter.
- Heavy service starters optimize for HTTP services, routers, dependency injection, observability stacks, and deployment patterns that are not the hard part of this product.

### Selected Starter: Custom Minimal Go Module Using Official Go MCP SDK

**Rationale for Selection:**

Use a custom minimal Go module seeded from the official Go MCP SDK examples. This keeps the architecture focused on MCP tool contracts and Jenkins integration rather than adopting a generic CLI or web-service framework.

The starter is minimal in dependencies, not casual in structure. Package boundaries should be intentional from day one because Jenkins APIs and MCP SDK behavior are both areas where implementation churn can otherwise spread.

Cobra should be deferred unless architecture later identifies multiple real shell commands. For v1, a single binary that runs an MCP server with documented configuration is enough.

**Initialization Command:**

```bash
mkdir jenkins-mcp-server
cd jenkins-mcp-server
go mod init github.com/your-org/jenkins-mcp-server
go get github.com/modelcontextprotocol/go-sdk@v1.5.0
```

**Architectural Decisions Provided by Starter:**

**Language & Runtime:**

- Go module.
- Official Go MCP SDK.
- Initial MCP transport is stdio only.
- Transport wiring must stay thin so HTTP/streamable transport can be added later without changing Jenkins/business logic.

**SDK Isolation:**

- Official Go MCP SDK usage must be isolated behind `internal/mcp`.
- SDK types must not leak into `internal/jenkins`, `internal/config`, `internal/artifacts`, `internal/audit`, or test fixtures.
- Jenkins client/adapters should return project-owned structs and project-owned structured errors.
- If SDK APIs churn or a blocker is found, replacement should primarily affect `internal/mcp` and `go.mod`.

**Build Tooling:**

- Standard Go toolchain.
- Dockerfile added early as a delivery artifact because Docker packaging is part of the product promise.
- GoReleaser may be evaluated later for release automation but is not part of the starter decision.

**Testing Framework:**

- Standard Go `testing` package initially.
- Fixture-driven tests for Jenkins response shapes.
- Jenkins API tests must run without a live Jenkins controller.
- No heavy test framework required at architecture start.

**Code Organization:**

```text
cmd/jenkins-mcp-server/main.go
internal/mcp/
internal/jenkins/
internal/config/
internal/artifacts/
internal/audit/
internal/errors/
testdata/
```

The entrypoint in `cmd/jenkins-mcp-server` should stay thin: load configuration, build dependencies, and start the MCP transport.

`internal/jenkins` may split into narrower packages once real tool work starts, such as `client`, `jobs`, `builds`, `queue`, and `logs`, when that separation is justified by tests and API complexity.

**Development Experience:**

- Small, inspectable Go project.
- Official SDK examples guide MCP server setup.
- First implementation slice should be possible test-first without touching `main.go`.
- Architecture remains free to add Cobra, Viper, GoReleaser, or additional transports later if justified.

**Note:** Project initialization using this command should be the first implementation story.

## Core Architectural Decisions

### Decision Priority Analysis

**Critical Decisions (Block Implementation):**

- Go runtime and module baseline
- MCP SDK boundary and transport approach
- Jenkins integration boundary
- Configuration model
- Credential and mutation safety model
- Error taxonomy
- Large-output and watch semantics
- Artifact download safety
- Test fixture strategy

**Important Decisions (Shape Architecture):**

- Package layout
- Logging and audit approach
- Docker packaging
- Capability discovery model
- Optional future transport path

**Deferred Decisions (Post-MVP):**

- Cobra or richer CLI framework
- HTTP/streamable MCP transport
- GoReleaser release automation
- Advanced observability integrations
- Rich historical analytics and trend storage

### Data Architecture

**Decision:** No database for MVP.

**Rationale:** The server is an external protocol adapter and Jenkins evidence reader. Jenkins remains the source of truth. Persisting build/test/coverage data would introduce product scope and data-retention concerns that are not needed for v1.

**Data handled by the server:**

- In-memory request/response data
- Configuration loaded at startup
- Optional filesystem artifact downloads
- Audit records for mutating actions
- Test fixtures in `testdata/`

**Caching Strategy:**

- No durable cache in MVP.
- Short-lived in-memory caching is allowed only for capability discovery and controller metadata where it reduces repeated Jenkins calls.
- Cache entries must be bounded and safe to bypass.

**Migration Approach:**

- No database migrations in MVP.
- Configuration schema changes should be handled through documented config versioning or validation errors.

### Authentication & Security

**Decision:** Jenkins authentication is configured per controller and enforced by Jenkins permissions.

**Rationale:** The server must not bypass Jenkins authorization. It should act as a controlled MCP-facing client of Jenkins.

**Authentication Model:**

- Per-controller Jenkins URL and credentials.
- Support API token/basic-style Jenkins authentication as the baseline.
- Support Jenkins crumb/CSRF handling where required by Jenkins for mutating requests.
- Do not log credentials or return them in MCP responses.

**Authorization Model:**

- Server-side policy controls whether mutating MCP tools are enabled.
- Jenkins permissions remain authoritative for whether the authenticated user can perform an action.
- Mutating tools must be separate from inspection tools.

**Audit Model:**

- Trigger/cancel actions produce audit records with controller, job/build target, authenticated Jenkins user where available, requested action, outcome, and timestamp.
- Audit destination and retention must be deployment-configurable.

### API & Communication Patterns

**Decision:** MCP stdio transport for v1, implemented through official Go MCP SDK isolated behind `internal/mcp`.

**Rationale:** Stdio is the best first transport for local IDE/agent workflows. Keeping MCP SDK usage behind `internal/mcp` protects the rest of the codebase from SDK churn.

**MCP Tool Contract:**

- Tools are organized around developer diagnostic tasks, not raw Jenkins endpoint names.
- Tool schemas are the machine-readable source of truth.
- Tool outputs use project-owned response structs mapped to MCP content at the boundary.
- SDK types must not leak into `internal/jenkins`, `internal/config`, `internal/artifacts`, `internal/audit`, or fixtures.

**Jenkins Communication:**

- Jenkins API access lives behind `internal/jenkins`.
- Jenkins adapters return project-owned structs and project-owned structured errors.
- Jenkins plugin-dependent behavior is surfaced through capability discovery and typed unsupported-capability errors.

**Error Handling Standard:**

Structured errors must distinguish at least:

- Missing permission
- Missing plugin
- Unsupported job type
- Unavailable endpoint
- Expired build
- Deleted artifact
- Invalid parameters
- Unsafe artifact path
- Jenkins communication failure
- MCP/server failure

### Frontend Architecture

**Decision:** No frontend architecture for MVP.

**Rationale:** The product has no GUI in v1. User interaction happens through MCP clients and IDE/agent workflows.

**Implication:** UX expectations are expressed through tool names, schemas, response clarity, examples, and documentation.

### Infrastructure & Deployment

**Decision:** Ship as standalone Go binary and Docker image.

**Rationale:** Local developer use and shared internal deployment are both explicit PRD requirements.

**Runtime Baseline:**

- Target current Go release line, initially Go `1.26.x`.
- Use official Go MCP SDK `v1.5.0` as the initial MCP dependency unless architecture discovers a blocker.

**Configuration:**

- Configuration file and environment-variable support.
- Controller-aware config with optional default controller.
- Policy config for mutating tools and artifact download directories.
- Startup validation must fail clearly for invalid controller, credential, mutation, or artifact policy config.

**Logging:**

- Use Go standard library structured logging (`log/slog`) initially to avoid unnecessary logging dependencies.
- Logs must redact configured secrets and avoid emitting raw credentials.

**Docker:**

- Dockerfile is part of MVP packaging.
- Secrets must be supplied at runtime, never baked into image layers.
- Docker and binary modes must expose equivalent MCP capabilities.

### Decision Impact Analysis

**Implementation Sequence:**

1. Initialize minimal Go module with official Go MCP SDK.
2. Create thin `cmd/jenkins-mcp-server/main.go`.
3. Implement config loading and validation.
4. Establish `internal/mcp` boundary and first tool registration.
5. Establish `internal/jenkins` client with fixture-driven tests.
6. Implement structured error taxonomy.
7. Implement first diagnostic read tools before mutating tools.
8. Add artifact safety and audit foundations before artifact download or trigger/cancel tools.
9. Add Dockerfile once binary startup path is stable.

**Cross-Component Dependencies:**

- MCP tools depend on project-owned service interfaces, not direct Jenkins SDK/client internals.
- Jenkins adapters depend on config and errors, but not MCP SDK types.
- Mutating tools depend on policy config and audit.
- Artifact download depends on artifact policy and filesystem safety.
- Capability discovery informs plugin-dependent tool behavior and fallback errors.

## Implementation Patterns & Consistency Rules

### Pattern Categories Defined

**Critical Conflict Points Identified:**

- Stable MCP tool names and schemas
- Canonical Jenkins identity fields
- Package boundaries and forbidden dependencies
- Request/response struct ownership
- Pagination, truncation, and cursor shape
- Error envelope and error codes
- Large response limits
- Mutation policy and audit behavior
- Artifact safety
- Watch/polling semantics
- Capability discovery shape
- Fixture and contract test layout

### Core Rule

Stable external contract beats Jenkins convenience. Jenkins remains the source of truth, but raw Jenkins API shapes must not become the MCP contract.

Architecture compliance is tested, not trusted.

### Naming Patterns

**MCP Tool Naming:**

- Tool names are stable public identifiers.
- Tool names use lower snake case and the `jenkins_` prefix.
- Tool names are versionless; versioning belongs in capability metadata and schema metadata.
- Tool names must not be renamed without an explicit deprecation decision.

Examples:

- `jenkins_list_jobs`
- `jenkins_get_job`
- `jenkins_list_builds`
- `jenkins_get_build`
- `jenkins_get_build_log`
- `jenkins_watch_build`
- `jenkins_get_test_results`
- `jenkins_get_coverage`
- `jenkins_list_issues`
- `jenkins_download_artifact`
- `jenkins_trigger_build`
- `jenkins_cancel_build`

**Canonical Identity Fields:**

- `jobFullName`: URL-decoded, slash-separated Jenkins full name, no leading slash, case-sensitive.
- `buildNumber`: Jenkins build number.
- `queueId`: Jenkins queue item identity.
- `artifactPath`: Jenkins-relative artifact path.
- `cursor`: opaque continuation token.
- `limit`: bounded result size.
- `url`: Jenkins/browser-facing URL only, not internal routing identity.

Allowed examples:

- `backend/api-build`
- `folder/subfolder/job-name`

Banned public aliases:

- `job`
- `jobName`
- `fullName`
- `path`
- `jobPath`
- `jobUrl`
- `buildId`
- `buildUrl`

**Go Code Naming:**

- Go functions and types are idiomatic Go: `GetBuild`, `SearchLogs`, `TriggerBuild`.
- MCP tool names must not be derived automatically from Go function or package names.
- Every tool gets named request and response structs, for example `GetBuildRequest` and `GetBuildResponse`.
- Anonymous maps are allowed only for truly dynamic Jenkins build parameters.

### Structure Patterns

**Package Ownership:**

- `internal/mcp`: MCP SDK boundary, transport, tool registration, schema binding, MCP error mapping.
- `internal/tools`: request validation, orchestration, response shaping.
- `internal/jenkins`: Jenkins HTTP/API client and Jenkins domain adapter.
- `internal/config`: config and environment loading.
- `internal/artifacts`: artifact path validation and filesystem writes.
- `internal/audit`: mutation audit records.
- `internal/errors`: shared structured error model.
- `internal/testutil`: shared test helpers only.

**Forbidden Dependencies:**

- No official MCP SDK imports outside `internal/mcp`.
- No Jenkins HTTP calls outside `internal/jenkins`.
- `internal/jenkins` must not import `internal/mcp`.
- Tool handlers must not return raw Jenkins JSON/XML as the external response contract.

### Format Patterns

**Tool Contract Registry:**

Each MCP tool must have a documented contract with:

- Tool name
- Mutation flag
- Owner package
- Request struct
- Response struct
- Error codes
- Response limit behavior
- Fixture coverage

**Pagination and Truncation:**

Every paginated response returns:

- `items`
- `nextCursor`
- `hasMore`
- `truncated`
- `limit`

Cursors are opaque strings. Public clients must not receive raw Jenkins URLs, offsets, or page numbers as cursor contracts.

**Log Responses:**

Log tools use Jenkins progressive text semantics internally, but expose a stable response with:

- `text`
- `cursor`
- `nextCursor`
- `hasMore`
- `truncated`
- `limit`
- `byteLimitExceeded`

**Error Envelope:**

Errors use a fixed shape:

- `code`
- `message`
- `retryable`
- `jenkinsStatus`
- `details`

Initial error codes include:

- `not_found`
- `unauthorized`
- `forbidden`
- `jenkins_unavailable`
- `invalid_arguments`
- `timeout`
- `response_too_large`
- `unsupported_capability`
- `mutation_denied`
- `conflict`
- `unsafe_artifact_path`

### Process Patterns

**Mutation Policy:**

Mutating tools must declare policy explicitly.

Config supports:

- `readOnly`
- `mutationsEnabled`
- `allowedMutations`

Every mutating tool must:

- Pass through a shared mutation guard.
- Produce an audit record.
- Redact sensitive parameters.
- Return accepted/rejected status with queue/build reference where relevant.
- Support dry-run where useful.

**Watch Pattern:**

- MVP watch is polling, not an event bus.
- No long-lived in-memory subscriptions in MVP.
- Watch responses must include terminal state, partial updates, cursor, and polling guidance.
- Polling interval and timeout bounds are config-controlled.

**Capability Discovery:**

Capability discovery has two layers:

- `serverCapabilities`: server version, schema version, transport, enabled tools, mutation mode, configured limits.
- `jenkinsCapabilities`: detected Jenkins/plugin support for Pipeline, JUnit, Coverage, Warnings NG / `recordIssues`, artifacts, queue, and crumbs.

### Testing Patterns

**Fixture Layout:**

- Jenkins fixtures live under `testdata/jenkins/...`.
- MCP contract fixtures live under `testdata/mcp/<tool_name>/...`.

Examples:

- `testdata/jenkins/jobs/list_jobs.json`
- `testdata/jenkins/builds/get_build_success.json`
- `testdata/jenkins/logs/progressive_text_running.txt`
- `testdata/mcp/jenkins_get_build/success.json`
- `testdata/mcp/jenkins_get_build/error_forbidden.json`

Fixtures must cover ugly cases: folders, nested folders, spaces, encoded names, multibranch jobs, missing actions, missing plugins, empty test results, large logs, running builds, failed JUnit, and unsafe artifact paths.

**Contract Tests:**

Each MCP tool requires golden request/response tests before being considered complete:

- Input JSON
- Success response JSON
- Structured error response JSON

CI should fail on contract drift unless the fixture update is intentional.

### Enforcement Guidelines

**All AI Agents MUST:**

- Preserve stable MCP tool names.
- Use canonical identity fields.
- Keep SDK types inside `internal/mcp`.
- Keep Jenkins HTTP/API access inside `internal/jenkins`.
- Use named request/response structs per tool.
- Enforce bounded responses.
- Use the fixed error envelope.
- Add fixture-backed parsing tests for Jenkins data.
- Add MCP contract tests for tool response shapes.
- Treat missing plugins as typed capability errors.
- Add audit behavior before enabling mutating tools.

**Anti-Patterns:**

- Calling Jenkins directly from MCP registration code.
- Returning raw Jenkins API blobs as tool responses.
- Returning unbounded logs or issue lists.
- Exposing raw Jenkins URLs as cursors.
- Accepting both `jobFullName` and aliases.
- Adding a database in MVP.
- Adding an event bus for watch behavior.
- Allowing trigger/cancel tools without mutation policy and audit.

## Project Structure & Boundaries

### Requirements Mapping

- Jenkins controller/configuration -> `internal/config`, `internal/jenkins/client`, `internal/tools/system`
- Job and build discovery -> `internal/tools/jobs`, `internal/tools/builds`, `internal/jenkins/api`
- Build output/watch -> `internal/tools/logs`, `internal/tools/watch`, `internal/pagination`, `internal/jenkins/api`
- Pipeline stages/steps -> `internal/tools/pipeline`, `internal/jenkins/api`
- JUnit, coverage, issues -> `internal/tools/tests`, `internal/tools/quality`, `internal/jenkins/api`
- SCM/change sets -> `internal/tools/scm`, `internal/jenkins/api`
- Artifacts -> `internal/tools/artifacts`, `internal/artifacts`, `internal/security`, `internal/jenkins/api`
- Trigger/queue/cancel -> `internal/tools/queue`, `internal/audit`, `internal/jenkins/api`
- Capability discovery/errors -> `internal/tools/system`, `internal/errors`, `internal/contract`
- Packaging/docs -> `Dockerfile`, `docs`, `examples`

### Complete Project Directory Structure

```text
jenkins-mcp-server/
├── README.md
├── LICENSE
├── go.mod
├── go.sum
├── Dockerfile
├── .dockerignore
├── .gitignore
├── Makefile
├── cmd/
│   └── jenkins-mcp-server/
│       └── main.go
├── internal/
│   ├── app/
│   │   ├── app.go
│   │   └── app_test.go
│   ├── mcpserver/
│   │   ├── server.go
│   │   ├── registry.go
│   │   ├── schemas.go
│   │   ├── errors.go
│   │   ├── transport/
│   │   │   └── stdio/
│   │   │       ├── stdio.go
│   │   │       └── stdio_test.go
│   │   └── *_test.go
│   ├── tools/
│   │   ├── contracts/
│   │   ├── jobs/
│   │   ├── builds/
│   │   ├── logs/
│   │   ├── watch/
│   │   ├── pipeline/
│   │   ├── tests/
│   │   ├── quality/
│   │   ├── scm/
│   │   ├── artifacts/
│   │   ├── queue/
│   │   └── system/
│   ├── jenkins/
│   │   ├── client/
│   │   ├── api/
│   │   ├── model/
│   │   └── urlx/
│   ├── config/
│   ├── artifacts/
│   ├── audit/
│   ├── observability/
│   ├── pagination/
│   ├── validation/
│   ├── security/
│   ├── errors/
│   ├── contract/
│   └── testutil/
├── testdata/
│   ├── jenkins/
│   │   ├── jobs/
│   │   ├── builds/
│   │   ├── logs/
│   │   ├── pipeline/
│   │   ├── tests/
│   │   ├── coverage/
│   │   ├── issues/
│   │   ├── scm/
│   │   ├── artifacts/
│   │   └── queue/
│   └── mcp/
│       └── <tool_name>/
├── docs/
│   ├── architecture.md
│   ├── tools/
│   ├── errors.md
│   ├── operations.md
│   ├── development.md
│   └── security.md
├── examples/
│   ├── config/
│   ├── docker/
│   ├── mcp-client/
│   └── tool-calls/
└── scripts/
    ├── check-boundaries.sh
    └── test.sh
```

### Package Ownership

- `cmd/jenkins-mcp-server`: process entrypoint only.
- `internal/app`: lifecycle, dependency assembly, startup and shutdown.
- `internal/mcpserver`: official Go MCP SDK boundary, tool registration, schema binding, error rendering.
- `internal/mcpserver/transport/stdio`: stdio transport wiring only.
- `internal/tools/<capability>`: tool request/response structs, validation calls, orchestration, response shaping.
- `internal/jenkins/client`: HTTP client, authentication, crumbs, retries, Jenkins HTTP error classification.
- `internal/jenkins/api`: typed Jenkins endpoint methods.
- `internal/jenkins/model`: normalized internal Jenkins models.
- `internal/jenkins/urlx`: Jenkins URL construction and escaping.
- `internal/config`: config files, environment parsing, defaults, validation, credential redaction metadata.
- `internal/artifacts`: artifact metadata, inline/download policy, size limits, filesystem write behavior.
- `internal/audit`: mutation audit event emission only.
- `internal/observability`: structured logs, request IDs, timing, redaction hooks, diagnostic metadata.
- `internal/pagination`: cursor, limit, truncation, and continuation helpers.
- `internal/validation`: shared validation for job identity, build numbers, artifact paths, limits, parameters.
- `internal/security`: path traversal checks, artifact filename policy, secret redaction, URL safety checks.
- `internal/errors`: canonical application error model and error codes.
- `internal/contract`: contract test helpers and schema snapshot helpers.
- `internal/testutil`: shared test fixtures and fake Jenkins helpers.

### Dependency Rules

| From | May Import | Must Not Import / Use |
|---|---|---|
| `cmd/jenkins-mcp-server` | `internal/app` | Jenkins business logic, MCP SDK directly |
| `internal/app` | config, mcpserver, tools, jenkins, audit, observability | Tool-specific business logic |
| `internal/mcpserver` | tools contracts, errors | Jenkins client internals, raw Jenkins DTOs |
| `internal/tools/*` | jenkins api/model, artifacts, audit, pagination, validation, errors | MCP SDK types, raw Jenkins HTTP clients |
| `internal/jenkins/*` | errors, observability, security helpers | mcpserver, tools |
| `internal/artifacts` | security, errors, observability | MCP transport details |
| `internal/audit` | observability, config types | Jenkins mutation execution |
| `internal/errors` | standard library only where practical | MCP SDK, Jenkins clients |

### Request Lifecycle

```text
MCP SDK
  -> mcpserver registry
  -> typed request decode
  -> tool validation
  -> tool handler
  -> Jenkins API / artifact / audit dependency
  -> normalized response
  -> bounded MCP response envelope
```

### Error Mapping Flow

```text
Jenkins HTTP/API error
  -> jenkins/client classification
  -> internal/errors code
  -> tool-level context
  -> mcpserver error rendering
```

### Mutation Flow

```text
MCP request
  -> typed request decode
  -> mutation policy check
  -> Jenkins action
  -> audit event
  -> queue/build response
```

### Artifact Flow

```text
MCP request
  -> artifact identity validation
  -> Jenkins artifact fetch
  -> security path checks
  -> inline response or safe local write
  -> metadata response
```

### Configuration Rules

- Config precedence: flags > environment variables > config file > defaults.
- Config must validate controller IDs, default controller, credentials, mutation policy, artifact directories, and response limits at startup.
- Config owns credential references and redaction metadata, not runtime service lookup.

### Artifact Scope

- Inline artifact reads are bounded and include metadata.
- Downloaded artifacts are written only through `internal/artifacts`.
- No path traversal, absolute destination paths, or implicit archive extraction.
- Artifact download is not treated as a mutation audit event unless later policy requires it.

### Fixture Policy

- Jenkins fixtures are raw sanitized Jenkins payloads.
- MCP fixtures are golden request/response/error contracts.
- Fixtures must be named by scenario.
- Pair Jenkins input and MCP output fixtures where practical.
- Fixtures must cover truncation, empty states, permission failures, missing plugins, malformed Jenkins responses, nested folders, spaces, encoded names, large logs, and unsafe artifact paths.

### New Tool Checklist

1. Add request/response structs under `internal/tools/<capability>`.
2. Add handler in the same package.
3. Add Jenkins API method under `internal/jenkins/api` if needed.
4. Add or reuse normalized models under `internal/jenkins/model`.
5. Register the tool through `internal/mcpserver`.
6. Add MCP contract fixture under `testdata/mcp/<tool_name>`.
7. Add Jenkins fixture under `testdata/jenkins` if Jenkins parsing is involved.
8. Add docs under `docs/tools/`.
9. Verify boundary checks and `go test ./...`.

### Schema and Versioning Guidance

- Tool names are stable once released.
- Response fields are additive by default.
- Breaking changes require a new tool name or explicit versioned field strategy.
- Error codes are stable and documented.
- Cursor shape is universal and never tool-specific.
- Schema snapshots should fail on accidental contract drift.

## Architecture Validation Results

### Coherence Validation ✅

**Decision Compatibility:**

The selected decisions work together:

- Go module plus official Go MCP SDK fits the stdio-first MCP server model.
- SDK isolation in `internal/mcpserver` is consistent with the goal of protecting Jenkins and tool logic from SDK churn.
- No database for MVP aligns with Jenkins as source of truth.
- Binary and Docker packaging align with local developer use and shared internal deployment.
- Mutating actions are separated from inspection actions through policy and audit boundaries.

**Pattern Consistency:**

The implementation patterns support the architecture:

- Stable `jenkins_` tool names give AI agents a predictable MCP surface.
- Canonical identifiers like `jobFullName`, `buildNumber`, `queueId`, and `artifactPath` reduce ambiguity.
- Fixed error, cursor, truncation, and response-limit shapes prevent per-tool drift.
- Contract fixtures and boundary checks make the rules testable.

**Structure Alignment:**

The project structure supports the decisions:

- `internal/mcpserver` owns MCP transport and SDK adaptation.
- `internal/tools/<capability>` owns tool contracts and orchestration.
- `internal/jenkins/*` owns Jenkins API communication and normalization.
- `internal/artifacts`, `internal/audit`, `internal/security`, `internal/pagination`, and `internal/errors` give cross-cutting behavior clear homes.

### Requirements Coverage Validation ✅

**Functional Requirements Coverage:**

All PRD functional areas have architectural support:

- Controllers/configuration: `internal/config`, `internal/jenkins/client`, `internal/tools/system`
- Jobs/builds: `internal/tools/jobs`, `internal/tools/builds`, `internal/jenkins/api`
- Logs/watch: `internal/tools/logs`, `internal/tools/watch`, `internal/pagination`
- Pipeline stages/steps: `internal/tools/pipeline`, `internal/jenkins/api`
- JUnit/coverage/issues: `internal/tools/tests`, `internal/tools/quality`
- SCM/change sets: `internal/tools/scm`
- Artifacts: `internal/tools/artifacts`, `internal/artifacts`, `internal/security`
- Trigger/queue/cancel: `internal/tools/queue`, `internal/audit`
- Capability discovery/errors: `internal/tools/system`, `internal/errors`

**Non-Functional Requirements Coverage:**

- Performance: bounded responses, cursoring, pagination, truncation helpers.
- Security: credential redaction, mutation policy, artifact path safety, no raw secret logging.
- Reliability: structured errors, plugin-aware capability handling, fixture tests.
- Operability: logs, audit, Docker, examples, config validation.
- Maintainability: package boundaries, dependency rules, contract tests, new-tool checklist.

### Implementation Readiness Validation ✅

**Decision Completeness:**

Critical decisions are documented: Go runtime, official MCP SDK, stdio-first transport, no DB, SDK isolation, Jenkins adapter boundary, mutation audit, response limits, artifact safety, and fixture-driven testing.

**Structure Completeness:**

The structure is specific enough for AI implementation agents. It defines package ownership, dependency rules, request flow, error mapping flow, mutation flow, artifact flow, test fixture policy, and docs/examples layout.

**Pattern Completeness:**

The highest-risk conflict points are covered: naming, identifiers, tool contracts, error shape, pagination, truncation, artifact handling, plugin variance, mutation safety, and schema drift.

### Gap Analysis Results

**Critical Gaps:** None found.

**Important Gaps to Track in Early Implementation:**

- Jenkins plugin API variance remains the largest technical risk, especially Pipeline stage/step data, Coverage, and Warnings NG / `recordIssues`.
- Minimum supported Jenkins and plugin versions should be confirmed in implementation or documentation once real endpoint behavior is tested.
- The audit destination and retention model must be made concrete when `internal/audit` is implemented.
- Capability discovery should be implemented early enough to prevent plugin-dependent tools from failing opaquely.

**Nice-to-Have Future Enhancements:**

- HTTP/streamable MCP transport after stdio is stable.
- GoReleaser or equivalent release automation.
- Richer observability integrations.
- Broader Jenkins fixture corpus from real-world installations.

### Validation Issues Addressed

Party Mode refinements were incorporated before validation:

- `internal/mcp` was refined to `internal/mcpserver`.
- Tools were split by capability.
- Jenkins was split into `client`, `api`, `model`, and `urlx`.
- Added `observability`, `pagination`, `validation`, `security`, and `contract`.
- Added dependency rules, fixture policy, schema/versioning guidance, and new-tool checklist.

### Architecture Completeness Checklist

**✅ Requirements Analysis**

- [x] Project context thoroughly analyzed
- [x] Scale and complexity assessed
- [x] Technical constraints identified
- [x] Cross-cutting concerns mapped

**✅ Architectural Decisions**

- [x] Critical decisions documented with versions
- [x] Technology stack fully specified
- [x] Integration patterns defined
- [x] Performance considerations addressed

**✅ Implementation Patterns**

- [x] Naming conventions established
- [x] Structure patterns defined
- [x] Communication patterns specified
- [x] Process patterns documented

**✅ Project Structure**

- [x] Complete directory structure defined
- [x] Component boundaries established
- [x] Integration points mapped
- [x] Requirements to structure mapping complete

### Architecture Readiness Assessment

**Overall Status:** READY FOR IMPLEMENTATION

**Confidence Level:** High

The architecture has enough specificity for AI agents to implement consistently, with the main residual risk isolated to Jenkins/plugin API variability.

**Key Strengths:**

- Stable MCP contract is prioritized over Jenkins API convenience.
- SDK isolation is explicit and enforceable.
- Large-output handling is built into the design.
- Mutation paths are policy-gated and auditable.
- Fixtures and contract tests are first-class architecture concerns.

**Areas for Future Enhancement:**

- Broaden Jenkins/plugin compatibility matrix.
- Add optional HTTP transport.
- Add release automation.
- Expand operations documentation after first deployment.

### Implementation Handoff

**AI Agent Guidelines:**

- Follow all architectural decisions exactly as documented.
- Preserve MCP tool contracts and canonical identity fields.
- Respect package boundaries and forbidden imports.
- Add fixtures and contract tests with each tool.
- Treat missing Jenkins plugins as capability issues, not generic server failures.

**First Implementation Priority:**

Initialize the project with the minimal Go module and official Go MCP SDK:

```bash
mkdir jenkins-mcp-server
cd jenkins-mcp-server
go mod init github.com/your-org/jenkins-mcp-server
go get github.com/modelcontextprotocol/go-sdk@v1.5.0
```

Then create the thin entrypoint, config validation, `internal/mcpserver` boundary, and first read-only diagnostic tool with contract fixtures.
