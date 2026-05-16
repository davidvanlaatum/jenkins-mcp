---
stepsCompleted:
  - 1
  - 2
  - 3
  - 4
workflowType: 'epics-and-stories'
lastStep: 4
status: 'complete'
completedAt: '2026-04-25'
inputDocuments:
  - /Users/david/bmad/_bmad-output/planning-artifacts/prd.md
  - /Users/david/bmad/_bmad-output/planning-artifacts/architecture.md
  - /Users/david/bmad/_bmad-output/planning-artifacts/prd-validation-report.md
---

# Go Jenkins MCP Server - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for Go Jenkins MCP Server, decomposing the requirements from the PRD and Architecture requirements into implementable stories.

## Requirements Inventory

### Functional Requirements

FR1: Platform engineers can configure one or more Jenkins controllers for the MCP server.

FR2: Platform engineers can designate a default Jenkins controller for tools that do not specify a controller explicitly.

FR3: Platform engineers can configure Jenkins credentials without exposing secrets through MCP responses, logs, or documentation examples.

FR4: Platform engineers can configure whether mutating Jenkins actions are enabled.

FR5: Platform engineers can configure artifact download policy, including allowed filesystem locations.

FR6: AI agents can discover which Jenkins controllers are available through the MCP server.

FR7: AI agents can list Jenkins jobs across folders.

FR8: AI agents can list Jenkins jobs inside nested folders.

FR9: AI agents can identify common Jenkins job types, including pipeline and multibranch jobs where Jenkins exposes that information.

FR10: AI agents can retrieve job details needed for diagnosis.

FR11: AI agents can list builds for a job.

FR12: AI agents can retrieve build details, including result state, timestamps, duration, causes, parameters, and related metadata where available.

FR13: AI agents can inspect upstream and downstream build relationships where Jenkins exposes them.

FR14: AI agents can read large build console output in bounded slices.

FR15: AI agents can tail build output from a cursor or offset.

FR16: AI agents can search build output and retrieve matching context snippets.

FR17: AI agents can inspect the current state of a running build.

FR18: AI agents can watch running build progress through bounded updates.

FR19: AI agents can detect when a watched build completes.

FR20: AI agents can inspect queue and executor state for queued or running builds.

FR21: AI agents can inspect pipeline run structure where Jenkins exposes it.

FR22: AI agents can inspect pipeline stages and their status where Jenkins exposes them.

FR23: AI agents can inspect pipeline nodes, steps, or equivalent execution units where Jenkins exposes them.

FR24: AI agents can retrieve output for individual pipeline stages, nodes, or steps where Jenkins exposes it.

FR25: AI agents receive clear unsupported-capability responses when stage, node, or step data is unavailable.

FR26: AI agents can retrieve JUnit test summaries for a build where available.

FR27: AI agents can retrieve individual failed test details where available.

FR28: AI agents can retrieve test stdout/stderr or related failure output where available.

FR29: AI agents can retrieve coverage summaries where supported by Jenkins plugins.

FR30: AI agents can distinguish missing coverage plugin data from server failure because folded build responses omit unavailable coverage without failing.

FR31: AI agents can retrieve `recordIssues` / Warnings NG issue summaries where available.

FR32: AI agents can retrieve individual static-analysis issue details where available.

FR33: AI agents receive clear unsupported-capability responses when issue data is unavailable.

FR34: AI agents can retrieve SCM configuration details for a job or build where available.

FR35: AI agents can retrieve build change sets where available.

FR36: AI agents can inspect commit identifiers, authors, messages, affected files, and related change metadata where Jenkins exposes them.

FR37: AI agents can retrieve branch or change request metadata where Jenkins exposes it.

FR38: AI agents can list artifacts attached to a build.

FR39: AI agents can inspect artifact metadata before retrieval.

FR40: AI agents can read small text artifacts inline.

FR41: AI agents can download large or binary artifacts to the filesystem.

FR42: AI agents receive clear saved-path information after artifact download.

FR43: AI agents receive clear errors when artifacts are deleted, expired, inaccessible, or unsafe to write.

FR44: AI agents can trigger new builds for configured jobs where permitted.

FR45: AI agents can trigger parameterized builds with explicit parameters.

FR46: AI agents can inspect Jenkins queue items created by triggered builds.

FR47: AI agents can cancel queued builds where configured and permitted.

FR48: AI agents can cancel running builds where configured and permitted.

FR49: AI agents receive clear permission or policy errors when mutating actions are unavailable.

FR50: AI agents can discover server-supported Jenkins capability categories for a controller.

FR51: AI agents can discover plugin-dependent capability availability where Jenkins exposes enough information.

FR52: AI agents receive structured errors for missing permissions, missing plugins, unsupported job types, unavailable endpoints, expired builds, deleted artifacts, invalid parameters, and unsafe download paths.

FR53: AI agents can distinguish unavailable data from server failure.

FR54: AI agents can retrieve enough diagnostic metadata to decide which fallback evidence source to inspect next.

FR55: AI agents can inspect the authenticated Jenkins user identity and relevant permission context for a configured controller where Jenkins exposes it.

FR56: Developers can run the server as a standalone Go binary.

FR57: Developers or platform engineers can run the server as a Docker image.

FR58: Developers can configure the server for local MCP client use.

FR59: Platform engineers can configure the server for shared internal use.

FR60: Developers can read human-readable documentation for every v1 MCP tool.

FR61: Developers can follow setup documentation for binary usage, Docker usage, Jenkins authentication, and MCP client configuration.

FR62: Developers can follow documented examples for failed pipeline investigation, running build watch, artifact retrieval, and parameterized build triggering.

FR63: Developers can understand plugin-dependent behavior through a documented capability matrix.

### NonFunctional Requirements

NFR1: The server must return bounded responses for all tools that can access large Jenkins data, including console logs, stage logs, test results, coverage reports, issue lists, and artifacts.

NFR2: Default response payloads should stay under a documented configurable limit, initially targeting 64 KiB of text content per tool response unless a tool defines a smaller default.

NFR3: Large log tools must support incremental retrieval with explicit cursor or offset fields and documented default chunk sizing, initially targeting 32 KiB to 64 KiB per log chunk.

NFR4: Search and tail operations must return enough context to support diagnosis while respecting configured response-size limits, including documented limits for maximum matches and context lines.

NFR5: Running build watch operations must provide incremental progress updates without requiring clients to repeatedly fetch full build state.

NFR6: Watch responses should include a cursor or equivalent continuation token, and default polling guidance should target 2 to 10 second intervals unless the client or server config overrides it.

NFR7: Artifact retrieval must distinguish inline small-text reads from filesystem downloads for larger or binary files.

NFR8: Jenkins credentials must never be logged, returned in MCP responses, included in error messages, or baked into Docker images.

NFR9: The server must rely on Jenkins permissions for Jenkins-side authorization and must not bypass Jenkins access controls.

NFR10: Mutating tools, including build trigger, queue cancellation, and running build cancellation, must be explicitly enabled by configuration.

NFR11: Mutating tools must produce auditable records of requested action, target controller, job/build identifier, authenticated user where available, and result.

NFR12: Audit records must have a documented destination and retention policy in deployment configuration.

NFR13: Artifact downloads must be restricted to configured safe filesystem locations.

NFR14: Artifact downloads must avoid overwriting existing files unless explicitly requested.

NFR15: Returned Jenkins text may contain secrets already present in Jenkins logs; the product must document this risk and may support configurable redaction patterns.

NFR16: The server must return structured errors that distinguish missing permissions, missing plugins, unsupported job types, unavailable endpoints, expired builds, deleted artifacts, invalid parameters, unsafe download paths, and Jenkins/server communication failures.

NFR17: Plugin-dependent capabilities must degrade gracefully when Jenkins does not expose the required data.

NFR18: The server must allow agents to distinguish unavailable Jenkins data from MCP server failure.

NFR19: Running build watch behavior must handle queue-to-executor transitions, build completion, cancellation, and Jenkins-side failures clearly.

NFR20: Filesystem artifact downloads must report the final saved path or a specific failure reason.

NFR21: The server should use the official Go MCP SDK unless architecture review identifies a concrete blocker.

NFR22: If an alternative to the official Go MCP SDK is chosen, the architecture document must record the blocker, evaluated alternatives, compatibility impact, and migration path back to the official SDK if feasible.

NFR23: The MCP tool interface must be compatible with MCP-capable clients generally, with primary validation against JetBrains IDE-based workflows.

NFR24: The server must support both standalone Go binary execution and Docker execution with equivalent MCP capabilities.

NFR25: The server must support configuration patterns suitable for local developer use and shared internal deployment.

NFR26: Jenkins integration must account for plugin variability across Pipeline, JUnit, Coverage, and Warnings NG / `recordIssues`.

NFR27: The server must expose capability discovery so agents and operators can understand what each configured Jenkins controller supports.

NFR28: The server must provide clear startup/configuration errors for missing controller configuration, invalid credentials, invalid artifact download policy, and disabled mutating actions.

NFR29: The server must include human-readable documentation for every v1 MCP tool.

NFR30: The server must include setup documentation for binary usage, Docker usage, Jenkins authentication, and MCP client configuration.

NFR31: The server must include troubleshooting guidance for common Jenkins and MCP integration failures.

NFR32: Tool schemas must be treated as the machine-readable source of truth for MCP clients.

NFR33: During internal v1, tool names and schemas may evolve, but the project must converge on stable names, schemas, and response patterns before open-source release.

NFR34: The codebase should keep Jenkins API/plugin-specific behavior isolated from MCP tool contract definitions so plugin compatibility can improve without unnecessary tool churn.

NFR35: The project should include representative test coverage for common Jenkins data shapes, plugin-available cases, and plugin-missing cases.

NFR36: Validation fixtures should cover freestyle or non-pipeline jobs, pipeline jobs, multibranch-style jobs, large logs, JUnit-present and JUnit-missing builds, coverage-present and coverage-missing builds, `recordIssues`-present and `recordIssues`-missing builds, artifacts-present and artifacts-missing builds, and permission-denied responses.

### Additional Requirements

- Initialize a custom minimal Go module using the official Go MCP SDK, currently `github.com/modelcontextprotocol/go-sdk@v1.5.0`.
- Target the current Go release line identified in architecture, initially Go `1.26.x`.
- Use stdio as the v1 MCP transport, while keeping transport wiring thin enough for future HTTP/streamable transport.
- Isolate all official Go MCP SDK imports and types behind `internal/mcpserver`.
- Keep Jenkins API/plugin behavior outside the MCP SDK boundary.
- Use project-owned request, response, domain, and error structs rather than leaking SDK or raw Jenkins DTOs across package boundaries.
- Ship as both a standalone Go binary and Docker image.
- Keep `cmd/jenkins-mcp-server/main.go` thin: load configuration, assemble dependencies, and start the server.
- Use `internal/app` for lifecycle and dependency assembly.
- Use `internal/mcpserver` for MCP SDK adaptation, tool registration, schema binding, transport, and error rendering.
- Use `internal/tools/<capability>` for tool request/response structs, validation calls, orchestration, and response shaping.
- Use `internal/jenkins/client`, `internal/jenkins/api`, `internal/jenkins/model`, and `internal/jenkins/urlx` to separate HTTP/auth/crumbs, typed endpoint calls, normalized models, and URL construction.
- Use `internal/config` for file/env/flag configuration, defaults, validation, and credential redaction metadata.
- Use `internal/artifacts` for inline/download policy, artifact metadata, response limits, and filesystem writes.
- Use `internal/audit` for mutation audit event emission.
- Use `internal/observability` for structured logs, request IDs, timing, redaction hooks, and diagnostic metadata.
- Use `internal/pagination` for cursor, limit, truncation, and continuation helpers.
- Use `internal/validation` for shared validation of job identity, build numbers, artifact paths, limits, and parameters.
- Use `internal/security` for path traversal checks, artifact filename policy, secret redaction, and URL safety checks.
- Use `internal/errors` for canonical application error model and error codes.
- Use `internal/contract` for MCP contract test helpers and schema snapshot helpers.
- Enforce dependency rules: no MCP SDK outside `internal/mcpserver`, no Jenkins HTTP calls outside `internal/jenkins/*`, no raw Jenkins JSON/XML returned as tool responses, no artifact writes outside `internal/artifacts`, and no trigger/cancel mutation path bypassing `internal/audit`.
- Establish stable MCP tool naming using lower snake case and the `jenkins_` prefix.
- Use canonical public identity fields: `jobFullName`, `buildNumber`, `queueId`, `artifactPath`, `cursor`, `limit`, and `url` only for Jenkins/browser-facing URLs.
- Ban public identity aliases such as `job`, `jobName`, `fullName`, `path`, `jobPath`, `jobUrl`, `buildId`, and `buildUrl`.
- Require every MCP tool to have named request and response structs.
- Require a documented tool contract registry with tool name, mutation flag, owner package, request struct, response struct, error codes, response limit behavior, and fixture coverage.
- Use a fixed paginated response shape with `items`, `nextCursor`, `hasMore`, `truncated`, and `limit`.
- Use a fixed log response shape with `text`, `cursor`, `nextCursor`, `hasMore`, `truncated`, `limit`, and `byteLimitExceeded`.
- Use a fixed error envelope with `code`, `message`, `retryable`, `jenkinsStatus`, and `details`.
- Include initial error codes for `not_found`, `unauthorized`, `forbidden`, `jenkins_unavailable`, `invalid_arguments`, `timeout`, `response_too_large`, `unsupported_capability`, `mutation_denied`, `conflict`, and `unsafe_artifact_path`.
- Implement mutation policy through `readOnly`, `mutationsEnabled`, and `allowedMutations`.
- Ensure every mutating tool passes through a shared mutation guard, produces an audit record, redacts sensitive parameters, and returns accepted/rejected status.
- Implement MVP watch as polling, not an event bus or long-lived in-memory subscription.
- Define capability discovery in two layers: `serverCapabilities` and `jenkinsCapabilities`.
- Use Go standard `testing` initially, fixture-driven Jenkins tests, and MCP golden contract tests.
- Store Jenkins fixtures under `testdata/jenkins/...` and MCP contract fixtures under `testdata/mcp/<tool_name>/...`.
- Add `scripts/check-boundaries.sh` or equivalent boundary enforcement.
- Document config precedence as flags > environment variables > config file > defaults.
- Ensure artifact downloads do not allow path traversal, absolute destination paths, implicit archive extraction, or accidental overwrites.
- Treat artifact download as not audit-requiring unless later policy changes.
- Treat response fields as additive by default; breaking changes require a new tool name or explicit versioning strategy.
- Track Jenkins plugin/API variance, especially Pipeline stage/step data, Coverage, and Warnings NG / `recordIssues`, as an early implementation risk.
- Confirm minimum supported Jenkins and plugin versions during implementation or documentation once endpoint behavior is validated.

### UX Design Requirements

No UX design document was found, and the PRD explicitly states that no graphical user interface is in MVP. UX expectations are captured through MCP tool names, schemas, response clarity, examples, and documentation.

### FR Coverage Map

FR1: Epic 1 - configure Jenkins controllers.
FR2: Epic 1 - designate a default Jenkins controller.
FR3: Epic 1 - configure Jenkins credentials safely.
FR4: Epic 1 - configure mutating action enablement.
FR5: Epic 1 - configure artifact download policy.
FR6: Epic 1 - discover available Jenkins controllers.
FR7: Epic 2 - list Jenkins jobs across folders.
FR8: Epic 2 - list jobs inside nested folders.
FR9: Epic 2 - identify common Jenkins job types.
FR10: Epic 2 - retrieve job details for diagnosis.
FR11: Epic 2 - list builds for a job.
FR12: Epic 2 - retrieve build details and metadata.
FR13: Epic 2 - inspect upstream/downstream build relationships.
FR14: Epic 3 - read large build output in bounded slices.
FR15: Epic 3 - tail build output from cursor or offset.
FR16: Epic 3 - search build output with context snippets.
FR17: Epic 3 - inspect current running build state.
FR18: Epic 3 - watch running build progress through bounded updates.
FR19: Epic 3 - detect watched build completion.
FR20: Epic 6 - inspect queue and executor state.
FR21: Epic 4 - inspect pipeline run structure.
FR22: Epic 4 - inspect pipeline stages and status.
FR23: Epic 4 - inspect pipeline nodes, steps, or execution units.
FR24: Epic 4 - retrieve output for pipeline stages, nodes, or steps.
FR25: Epic 4 - return unsupported-capability responses for unavailable pipeline data.
FR26: Epic 5 - retrieve JUnit test summaries.
FR27: Epic 5 - retrieve individual failed test details.
FR28: Epic 5 - retrieve test stdout/stderr or related failure output.
FR29: Epic 5 - retrieve coverage summaries.
FR30: Epic 5 - omit unavailable folded coverage data without failing build lookups.
FR31: Epic 5 - retrieve `recordIssues` / Warnings NG issue summaries.
FR32: Epic 5 - retrieve individual static-analysis issue details.
FR33: Epic 5 - return unsupported-capability responses for unavailable issue data.
FR34: Epic 5 - retrieve SCM configuration details.
FR35: Epic 5 - retrieve build change sets.
FR36: Epic 5 - inspect commit metadata.
FR37: Epic 5 - retrieve branch or change request metadata.
FR38: Epic 5 - list build artifacts.
FR39: Epic 5 - inspect artifact metadata.
FR40: Epic 5 - read small text artifacts inline.
FR41: Epic 5 - download large or binary artifacts to the filesystem.
FR42: Epic 5 - return saved-path information after artifact download.
FR43: Epic 5 - return clear artifact retrieval and safety errors.
FR44: Epic 6 - trigger new builds.
FR45: Epic 6 - trigger parameterized builds.
FR46: Epic 6 - inspect Jenkins queue items.
FR47: Epic 6 - cancel queued builds where allowed.
FR48: Epic 6 - cancel running builds where allowed.
FR49: Epic 6 - return permission or policy errors for unavailable mutating actions.
FR50: Epic 1 - discover server-supported Jenkins capability categories.
FR51: Epic 7 - document plugin-dependent capability availability and compatibility.
FR52: Epic 1 - return structured errors for core failure categories.
FR53: Epic 1 - distinguish unavailable data from server failure.
FR54: Epic 5 - provide diagnostic metadata for fallback evidence selection.
FR55: Epic 1 - inspect authenticated Jenkins user identity and permission context.
FR56: Epic 1 - run as a standalone Go binary.
FR57: Epic 7 - run as a Docker image.
FR58: Epic 1 - configure for local MCP client use.
FR59: Epic 1 - configure for shared internal use.
FR60: Epic 7 - provide human-readable documentation for every v1 MCP tool.
FR61: Epic 7 - provide setup documentation.
FR62: Epic 7 - provide workflow examples.
FR63: Epic 7 - provide plugin-dependent behavior capability matrix.

## Epic List

### Epic 1: Connect Safely to Jenkins and Expose Reliable MCP Capabilities

Developers and platform engineers can run the server, configure Jenkins access, verify the authenticated Jenkins identity, and use one stable end-to-end MCP tool path with structured errors and contract fixtures.

**User outcome:** The server can safely connect to Jenkins and prove that MCP, config, credentials, errors, fixtures, and basic capability reporting work before broader tool implementation begins.

**FRs covered:** FR1, FR2, FR3, FR4, FR5, FR6, FR50, FR52, FR53, FR55, FR56, FR58, FR59

**Implementation notes:** First vertical tool should be `jenkins_whoami` or equivalent. Include Go module setup, official MCP SDK boundary, stdio transport, config validation, canonical error envelope, and contract fixture harness. Binary runtime support belongs here; Docker/package polish can land in Epic 7.

### Epic 2: Find the Relevant Job, Build, and CI Context

AI agents can locate Jenkins jobs across folders, identify job types, inspect recent builds, and choose the correct job/build target using canonical identifiers.

**User outcome:** An agent can answer "which Jenkins job/build should I inspect next?" without ambiguity.

**FRs covered:** FR7, FR8, FR9, FR10, FR11, FR12, FR13

**Implementation notes:** Establish canonical `jobFullName` and `{ jobFullName, buildNumber }` usage across discovery tools. Include pagination, bounded summaries, stable Jenkins URLs where available, and contract fixtures.

### Epic 3: Inspect Running and Completed Build Logs Without Overload

AI agents can inspect completed and running build output through bounded log slices, tailing, search, and watch-style progress updates without flooding context.

**User outcome:** An agent can answer "what is happening or what failed in this build log?" using bounded evidence.

**FRs covered:** FR14, FR15, FR16, FR17, FR18, FR19

**Implementation notes:** Queue/executor state moves to Epic 6 unless needed only as read-only context in watch responses. Every log/watch tool must use the shared cursor, truncation, and response-limit patterns.

### Epic 4: Explain Pipeline Execution and Stage-Level Failures

AI agents can inspect pipeline structure, stages, nodes, steps, and stage/step output where Jenkins exposes it, with graceful fallback when plugin data is missing.

**User outcome:** An agent can answer "where in the pipeline did this fail?" and retrieve focused stage/step evidence.

**FRs covered:** FR21, FR22, FR23, FR24, FR25

**Implementation notes:** Keep plugin/endpoint absence as a first-class acceptance path. Stage/step tools must reuse log and build identity patterns from earlier epics.

### Epic 5: Collect Failure Evidence from Tests, Coverage, Issues, SCM, and Artifacts

AI agents can collect the non-log evidence needed for diagnosis: JUnit failures, coverage summaries, `recordIssues` findings, SCM changes, and build artifacts.

**User outcome:** An agent can answer "what evidence explains this failure or verifies the build output?" across tests, quality signals, changes, and artifacts.

**FRs covered:** FR26, FR27, FR28, FR29, FR30, FR31, FR32, FR33, FR34, FR35, FR36, FR37, FR38, FR39, FR40, FR41, FR42, FR43, FR54

**Implementation notes:** Story slicing should keep test evidence, quality evidence, SCM evidence, and artifact evidence separate. Artifact safety and unsupported plugin handling must be explicit.

### Epic 6: Trigger, Track, and Cancel Builds Under Explicit Policy

AI agents can trigger parameterized builds, inspect queue/executor state, track queued work, and cancel queued or running builds only when policy and Jenkins permissions allow it.

**User outcome:** An agent can safely complete a trigger-watch-verify loop without hidden writes.

**FRs covered:** FR20, FR44, FR45, FR46, FR47, FR48, FR49

**Implementation notes:** Default posture is read-only. Mutating tools must be opt-in, policy-gated, permission-aware, audited, and parameter-validated. Queue/executor state lands here.

### Epic 7: Document Agent Workflows, Setup, and Compatibility Boundaries

Developers and platform engineers can install, configure, troubleshoot, and understand the server's tool contracts and Jenkins compatibility behavior.

**User outcome:** Teams can adopt and operate the MCP server without reverse-engineering tool behavior or plugin assumptions.

**FRs covered:** FR51, FR57, FR60, FR61, FR62, FR63

**Implementation notes:** Tool docs should be updated incrementally in earlier epics, while this epic completes Docker packaging, workflow examples, plugin matrix, troubleshooting, and compatibility guidance.

## Epic 1: Connect Safely to Jenkins and Expose Reliable MCP Capabilities

Developers and platform engineers can run the server, configure Jenkins access, verify the authenticated Jenkins identity, and use one stable end-to-end MCP tool path with structured errors and contract fixtures.

### Story 1.1: Set Up Initial Project from Starter Template

**Requirements covered:** FR56

As a developer,
I want a minimal Go MCP server project with the approved package boundaries,
So that future Jenkins tools can be implemented consistently.

**Acceptance Criteria:**

**Given** a clean repository
**When** the project is initialized
**Then** it contains a Go module, `cmd/jenkins-mcp-server/main.go`, `internal/app`, `internal/mcpserver`, and the approved internal package structure
**And** official Go MCP SDK usage is isolated behind `internal/mcpserver`
**And** the server can start over stdio with no Jenkins tools registered yet
**And** `go test ./...` passes
**And** a boundary check prevents MCP SDK imports outside `internal/mcpserver`

### Story 1.2: Load and Validate Jenkins Controller Configuration

**Requirements covered:** FR1, FR2, FR3, FR4, FR5, FR58, FR59

As a platform engineer,
I want to configure one or more Jenkins controllers and a default controller,
So that MCP tools can connect to the intended Jenkins instance safely.

**Acceptance Criteria:**

**Given** a config file and environment variables
**When** the server starts
**Then** it loads controller definitions, credentials references, default controller, mutation policy, artifact policy, and response limits
**And** config precedence is flags > environment variables > config file > defaults
**And** invalid controller IDs, missing required values, invalid artifact paths, or invalid mutation policy fail startup with structured errors
**And** credentials are never printed in logs or returned in errors
**And** config validation has unit tests

### Story 1.3: Create the Jenkins Client Connection and Identity Check

**Requirements covered:** FR55, FR52, FR53

As an AI agent,
I want to inspect the authenticated Jenkins user identity for a configured controller,
So that I can verify which Jenkins account and permission context the server is using.

**Acceptance Criteria:**

**Given** a configured Jenkins controller
**When** the agent calls `jenkins_whoami`
**Then** the server returns authenticated user identity and available permission context where Jenkins exposes it
**And** the tool accepts an optional `controllerId` and uses the default controller when omitted
**And** Jenkins authentication, crumb handling readiness, URL construction, and HTTP error classification live under `internal/jenkins/*`
**And** connection failure, unauthorized, forbidden, and unavailable-controller cases return the fixed error envelope
**And** Jenkins fixtures and MCP golden contract fixtures cover success and error cases

### Story 1.4: Expose Controller and Server Capability Discovery

**Requirements covered:** FR6, FR50, FR52, FR53

As an AI agent,
I want to discover configured controllers and server-level capabilities,
So that I know which tools and limits are safe to use.

**Acceptance Criteria:**

**Given** one or more configured controllers
**When** the agent calls capability or controller discovery tools
**Then** the server returns configured controller identifiers, default controller information, enabled tool categories, transport, schema version, server version, mutation mode, and configured response limits
**And** server capabilities are separated from Jenkins plugin-specific capabilities
**And** unavailable controller data is distinguishable from MCP server failure
**And** responses use canonical field names and bounded response shapes
**And** MCP contract fixtures cover single-controller, multi-controller, and invalid-controller cases

### Story 1.5: Establish Shared Structured Error and Contract Test Harness

**Requirements covered:** FR52, FR53

As a developer,
I want every MCP tool to use the same error envelope and contract fixture pattern,
So that future tools remain stable for AI agents.

**Acceptance Criteria:**

**Given** any tool handler returns an application error
**When** `internal/mcpserver` renders it
**Then** the MCP response uses `code`, `message`, `retryable`, `jenkinsStatus`, and `details` consistently
**And** initial error codes include `not_found`, `unauthorized`, `forbidden`, `jenkins_unavailable`, `invalid_arguments`, `timeout`, `response_too_large`, `unsupported_capability`, `mutation_denied`, `conflict`, and `unsafe_artifact_path`
**And** MCP golden fixtures live under `testdata/mcp/<tool_name>/`
**And** schema snapshots fail on accidental contract drift
**And** raw Jenkins JSON/XML is not returned as MCP output

### Story 1.6: Enforce Safe Runtime Defaults

**Requirements covered:** FR3, FR4, FR5, FR52

As a platform engineer,
I want the server to start in a safe read-oriented mode,
So that AI agents cannot perform hidden Jenkins mutations or unsafe artifact writes.

**Acceptance Criteria:**

**Given** default server configuration
**When** the server starts
**Then** mutating tools are disabled unless explicitly enabled
**And** artifact downloads are disabled or constrained to configured safe directories
**And** mutation policy fields support `readOnly`, `mutationsEnabled`, and `allowedMutations`
**And** disabled mutation or artifact actions return structured policy errors
**And** logs redact credential-like values using configured redaction metadata
**And** behavior is covered by unit and contract tests

## Epic 2: Find the Relevant Job, Build, and CI Context

AI agents can locate Jenkins jobs across folders, identify job types, inspect recent builds, and choose the correct job/build target using canonical identifiers.

### Story 2.1: List Jenkins Jobs Across Folders

**Requirements covered:** FR7, FR8, FR9

As an AI agent,
I want to list Jenkins jobs across folders using canonical job identifiers,
So that I can discover the build targets available for diagnosis.

**Acceptance Criteria:**

**Given** a configured Jenkins controller
**When** the agent calls `jenkins_list_jobs`
**Then** the server returns a bounded list of jobs with `jobFullName`, display name, job type where known, buildable status where available, Jenkins URL where available, and folder context
**And** nested folders can be traversed without requiring clients to construct Jenkins URLs
**And** responses use `items`, `nextCursor`, `hasMore`, `truncated`, and `limit`
**And** job identity aliases such as `jobName`, `path`, `fullName`, or `jobUrl` are not accepted as public input fields
**And** Jenkins and MCP fixtures cover root jobs, folder jobs, nested folders, multibranch jobs, encoded names, and permission-denied cases

### Story 2.2: Retrieve Job Details for Diagnosis

**Requirements covered:** FR10, FR9

As an AI agent,
I want to retrieve details for a specific Jenkins job,
So that I can understand whether it is the right target before inspecting builds.

**Acceptance Criteria:**

**Given** a `jobFullName`
**When** the agent calls `jenkins_get_job`
**Then** the server returns job metadata needed for diagnosis, including job type, display name, description where available, buildable status, disabled status, last build references, branch/change request metadata where available, and Jenkins URL where available
**And** the tool accepts `controllerId` optionally and uses the default controller when omitted
**And** missing, unauthorized, unsupported job type, and unavailable endpoint cases return structured errors
**And** raw Jenkins job JSON/XML is not exposed directly
**And** contract fixtures cover pipeline, freestyle, multibranch, folder, missing job, and permission-denied cases

### Story 2.3: List Builds for a Job

**Requirements covered:** FR11, FR12

As an AI agent,
I want to list recent builds for a Jenkins job,
So that I can choose the failed, running, or relevant build to inspect.

**Acceptance Criteria:**

**Given** a valid `jobFullName`
**When** the agent calls `jenkins_list_builds`
**Then** the server returns a bounded list of builds with `buildNumber`, result/state, timestamp, duration where available, building/running flag, Jenkins URL where available, and summary metadata
**And** responses use the shared pagination shape with opaque `nextCursor`
**And** clients do not need to provide raw Jenkins URLs or page offsets
**And** missing job, permission-denied, empty build history, and unavailable endpoint cases return structured responses or errors as appropriate
**And** fixtures cover successful build lists, running builds, empty build lists, and permission errors

### Story 2.4: Retrieve Build Details and Parameters

**Requirements covered:** FR12

As an AI agent,
I want to retrieve detailed metadata for a specific Jenkins build,
So that I can understand the build context before inspecting logs or evidence.

**Acceptance Criteria:**

**Given** a `jobFullName` and `buildNumber`
**When** the agent calls `jenkins_get_build`
**Then** the server returns result state, timestamps, duration, causes, parameters, build URL, queue reference where available, upstream/downstream references where available, and related metadata
**And** build identity is always `{ jobFullName, buildNumber }`
**And** parameters are represented without leaking credentials beyond Jenkins-provided masking behavior
**And** not found, expired build, permission-denied, and unavailable endpoint cases return structured errors
**And** MCP contract fixtures cover success, running build, failed build, parameterized build, missing build, and permission-denied cases

### Story 2.5: Inspect Upstream and Downstream Build Relationships

**Requirements covered:** FR13

As an AI agent,
I want to inspect upstream and downstream build relationships,
So that I can understand whether a failure was caused by or caused another CI run.

**Acceptance Criteria:**

**Given** a build with available relationship metadata
**When** the agent calls a build details tool or relationship-specific tool
**Then** the server returns upstream and downstream job/build references using canonical identifiers
**And** relationship references include Jenkins URLs where available but never use URLs as identity
**And** absent relationship data is returned as an empty relationship section, not a server failure
**And** missing permission or unavailable endpoint cases return structured errors
**And** fixtures cover upstream-only, downstream-only, both directions, and no-relationship builds

### Story 2.6: Normalize Jenkins Job and Build Identifiers

**Requirements covered:** FR7, FR8, FR10, FR11, FR12

As a developer,
I want shared validation and normalization for Jenkins job and build identifiers,
So that all future tools use the same public identity contract.

**Acceptance Criteria:**

**Given** public MCP inputs for job/build tools
**When** handlers validate identity fields
**Then** `jobFullName` is treated as URL-decoded, slash-separated, no leading slash, and case-sensitive
**And** `buildNumber` is validated as a Jenkins build number
**And** banned aliases such as `job`, `jobName`, `fullName`, `path`, `jobPath`, `jobUrl`, `buildId`, and `buildUrl` are rejected in public schemas
**And** Jenkins URL construction and escaping live under `internal/jenkins/urlx`
**And** unit tests cover folders, nested folders, spaces, encoded names, invalid names, and malformed build numbers

## Epic 3: Inspect Running and Completed Build Logs Without Overload

AI agents can inspect completed and running build output through bounded log slices, tailing, search, and watch-style progress updates without flooding context.

### Story 3.1: Read Bounded Build Console Output

**Requirements covered:** FR14

As an AI agent,
I want to read build console output in bounded slices,
So that I can inspect relevant log evidence without overwhelming context.

**Acceptance Criteria:**

**Given** a `jobFullName` and `buildNumber`
**When** the agent calls `jenkins_get_build_log` without a cursor
**Then** the server returns the first bounded log slice using the shared log response shape
**And** the response includes `text`, `cursor`, `nextCursor`, `hasMore`, `truncated`, `limit`, and `byteLimitExceeded`
**And** configured default and maximum byte limits are enforced server-side
**And** raw Jenkins progressive log offsets are not exposed as public contract details
**And** fixtures cover small log, large log, truncated log, missing build, permission-denied, and Jenkins unavailable cases

### Story 3.2: Continue and Tail Build Output

**Requirements covered:** FR15

As an AI agent,
I want to continue reading or tailing build output from a cursor,
So that I can follow log progress across multiple bounded calls.

**Acceptance Criteria:**

**Given** a previous log response with `nextCursor`
**When** the agent calls the log tool with that cursor
**Then** the server returns the next bounded log slice without duplicating prior content
**And** clients can request tail-oriented output for recent log content within configured limits
**And** invalid, expired, or mismatched cursors return structured errors
**And** cursor values are opaque and do not require clients to understand Jenkins internals
**And** fixtures cover continuation, tail output, empty continuation, running-build continuation, and invalid cursor cases

### Story 3.3: Search Build Output with Context

**Requirements covered:** FR16

As an AI agent,
I want to search build output and retrieve bounded context snippets,
So that I can find likely failure signals quickly.

**Acceptance Criteria:**

**Given** a `jobFullName`, `buildNumber`, and search pattern
**When** the agent calls `jenkins_search_build_log`
**Then** the server returns bounded matches with before/after context lines or text snippets
**And** maximum matches and context size are enforced by configuration
**And** the response indicates whether results were truncated
**And** invalid search patterns, oversized requests, missing builds, and permission errors return structured errors
**And** fixtures cover no matches, one match, many truncated matches, multiline context, and large log search cases

### Story 3.4: Inspect Running Build State

**Requirements covered:** FR17, FR19

As an AI agent,
I want to inspect the current state of a running build,
So that I can understand whether it is queued, executing, complete, failed, or canceled.

**Acceptance Criteria:**

**Given** a `jobFullName` and `buildNumber`
**When** the agent calls `jenkins_get_running_build` or equivalent running-state tool
**Then** the server returns current build state, result where available, running/building flag, timestamps, duration so far, estimated duration where available, and Jenkins URL where available
**And** completed builds are reported clearly rather than treated as errors
**And** queue/executor details that require queue-control semantics may be deferred to Epic 6
**And** Jenkins unavailable, missing build, and permission-denied cases return structured errors
**And** fixtures cover running, completed, failed, canceled, and inaccessible builds

### Story 3.5: Watch Running Build Progress with Bounded Updates

**Requirements covered:** FR18, FR19

As an AI agent,
I want to watch a running build through bounded polling updates,
So that I can monitor progress without repeatedly fetching full build state.

**Acceptance Criteria:**

**Given** a running build and optional watch cursor
**When** the agent calls `jenkins_watch_build`
**Then** the response includes current build state, terminal/completion status, polling guidance, log cursor metadata, and any progress summary available without exceeding response limits
**And** watch behavior uses polling semantics, not server-side subscriptions or an event bus
**And** queue-to-executor transitions, completion, cancellation, and Jenkins failures are represented clearly
**And** invalid cursors, completed builds, and unavailable data return structured responses or errors as appropriate
**And** fixtures cover first watch call, continued watch call, build completion, cancellation, and Jenkins communication failure

### Story 3.6: Apply Shared Log Limits and Redaction Hooks

**Requirements covered:** FR14, FR15, FR16, FR18

As a platform engineer,
I want log tools to enforce shared limits and optional redaction hooks,
So that Jenkins output remains safe and context-bounded.

**Acceptance Criteria:**

**Given** configured log limits and redaction patterns
**When** any log, search, tail, or watch response includes Jenkins text
**Then** the server enforces configured default and maximum limits
**And** optional redaction patterns are applied consistently before returning text
**And** the response indicates truncation or byte-limit exceedance clearly
**And** requests above server maximums are rejected with structured errors
**And** unit and contract tests cover limit enforcement, redaction behavior, and over-limit requests

## Epic 4: Explain Pipeline Execution and Stage-Level Failures

AI agents can inspect pipeline structure, stages, nodes, steps, and stage/step output where Jenkins exposes it, with graceful fallback when plugin data is missing.

### Story 4.1: Detect Pipeline Capability for a Build

**Requirements covered:** FR21, FR25

As an AI agent,
I want to know whether pipeline structure is available for a build,
So that I can choose the right diagnostic path.

**Acceptance Criteria:**

**Given** a `jobFullName` and `buildNumber`
**When** the agent requests pipeline capability or pipeline structure
**Then** the server detects whether Jenkins exposes pipeline run data for that build
**And** Pipeline-present responses include available capability metadata for stages, nodes, steps, and logs where known
**And** Pipeline-missing or endpoint-missing responses return `unsupported_capability` with fallback guidance toward build logs and other evidence
**And** missing permissions and Jenkins communication failures remain distinguishable from unsupported capability
**And** fixtures cover Pipeline job, non-pipeline job, multibranch pipeline, missing plugin/endpoint, and permission-denied cases

### Story 4.2: Inspect Pipeline Run Structure

**Requirements covered:** FR21, FR22

As an AI agent,
I want to inspect the pipeline run structure for a build,
So that I can understand the execution graph before drilling into failures.

**Acceptance Criteria:**

**Given** a build with pipeline run data
**When** the agent calls `jenkins_get_pipeline` or equivalent
**Then** the server returns bounded normalized pipeline structure with stage/node identifiers, display names, status, timing, and parent/child relationships where Jenkins exposes them
**And** the response uses canonical build identity and stable internal stage/node identifiers
**And** raw Jenkins Pipeline API JSON is not exposed directly
**And** unavailable or partial pipeline data is represented clearly
**And** contract fixtures cover simple pipeline, parallel stages, nested stages, partial data, and unsupported endpoint cases

### Story 4.3: Inspect Pipeline Stage Status

**Requirements covered:** FR22

As an AI agent,
I want to inspect pipeline stage status for a build,
So that I can identify which stage failed, is running, or was skipped.

**Acceptance Criteria:**

**Given** a pipeline build with stage data
**When** the agent requests stage details
**Then** the server returns stage name, stable stage identifier, status, duration, start time where available, error summary where available, and related Jenkins URL where available
**And** skipped, failed, unstable, running, successful, and aborted states are represented consistently
**And** the response is bounded and supports partial data if Jenkins omits fields
**And** missing stage identifiers or stale stage references return structured errors
**And** fixtures cover successful, failed, skipped, running, parallel, and missing-stage cases

### Story 4.4: Inspect Pipeline Nodes and Steps

**Requirements covered:** FR23

As an AI agent,
I want to inspect pipeline nodes and steps where Jenkins exposes them,
So that I can locate the specific execution unit that produced a failure.

**Acceptance Criteria:**

**Given** a pipeline build with node or step data
**When** the agent requests nodes or steps for a stage or build
**Then** the server returns bounded node/step details with stable identifiers, display labels, status, timing, parent context, and error information where available
**And** missing node/step plugin support returns `unsupported_capability` rather than raw Jenkins failure
**And** large step lists are paginated using the shared response shape
**And** raw Jenkins node/step JSON is not exposed directly
**And** fixtures cover node lists, step lists, large step lists, missing plugin, partial data, and permission-denied cases

### Story 4.5: Retrieve Stage, Node, or Step Output

**Requirements covered:** FR24, FR25

As an AI agent,
I want to retrieve output for an individual stage, node, or step,
So that I can inspect the focused log evidence around a pipeline failure.

**Acceptance Criteria:**

**Given** a pipeline stage, node, or step identifier
**When** the agent requests its output
**Then** the server returns bounded text output using the shared log response shape where Jenkins exposes scoped output
**And** if scoped output is unavailable, the server returns `unsupported_capability` with fallback guidance to full build log search or slices
**And** output retrieval applies shared log limits and redaction hooks
**And** invalid identifiers, missing build, permission-denied, and unavailable endpoint cases return structured errors
**And** fixtures cover scoped output success, unavailable scoped logs, large scoped output, redacted output, and invalid identifier cases

### Story 4.6: Add Pipeline Fallback Guidance to Capability Discovery

**Requirements covered:** FR25

As an AI agent,
I want pipeline capability responses to tell me what evidence sources remain useful,
So that I can continue diagnosis when pipeline-specific data is missing.

**Acceptance Criteria:**

**Given** a controller or build where pipeline support is absent or partial
**When** the agent inspects capabilities or receives an unsupported pipeline response
**Then** the response includes machine-readable fallback suggestions such as build log slices, log search, JUnit results, SCM changes, issues, or artifacts where available
**And** fallback suggestions do not claim support for tools that are disabled or unavailable
**And** plugin-dependent capability status is represented separately from server-supported tool categories
**And** contract fixtures cover full support, partial support, no support, and permission-limited support

## Epic 5: Collect Failure Evidence from Tests, Coverage, Issues, SCM, and Artifacts

AI agents can collect the non-log evidence needed for diagnosis: JUnit failures, coverage summaries, `recordIssues` findings, SCM changes, and build artifacts.

### Story 5.1: Retrieve JUnit Test Summaries

**Requirements covered:** FR26

As an AI agent,
I want to retrieve JUnit test summaries for a build,
So that I can quickly tell whether tests explain the build failure.

**Acceptance Criteria:**

**Given** a `jobFullName` and `buildNumber`
**When** the agent calls `jenkins_get_test_results`
**Then** the server returns bounded test summary data including total, passed, failed, skipped, duration where available, and failing suite/test counts
**And** the response includes Jenkins URL where available and canonical build identity
**And** missing JUnit data returns `unsupported_capability` or clearly unavailable data rather than server failure
**And** raw Jenkins test JSON/XML is not exposed directly
**And** fixtures cover tests present, no tests, missing JUnit endpoint, failed tests, large reports, and permission-denied cases

### Story 5.2: Retrieve Failed Test Details and Output

**Requirements covered:** FR27, FR28

As an AI agent,
I want to retrieve individual failed test details and related output,
So that I can connect a CI failure to a specific test and failure message.

**Acceptance Criteria:**

**Given** a build with failed JUnit tests
**When** the agent requests failed test details
**Then** the server returns bounded failed test records with class/suite name, test name, status, duration, failure message, stack trace or error details where available, and stdout/stderr where available
**And** large failure output is truncated with continuation or clear truncation metadata
**And** responses preserve enough identifiers for a follow-up request to a specific failed test where supported
**And** missing, stale, or unavailable test identifiers return structured errors
**And** fixtures cover single failure, multiple failures, large stack trace, missing stdout/stderr, and unavailable test detail cases

### Story 5.3: Retrieve Coverage Summaries with Plugin-Aware Fallback

**Requirements covered:** FR29, FR30

As an AI agent,
I want to retrieve coverage summaries where Jenkins exposes them,
So that I can understand whether coverage data contributed to the failure.

**Acceptance Criteria:**

**Given** a build with coverage plugin data
**When** the agent calls `jenkins_get_build`
**Then** the build response includes optional bounded normalized coverage summary data with metrics available from Jenkins, such as line, branch, instruction, class, method, or package coverage where supported
**And** missing coverage plugin data does not fail the build lookup
**And** coverage response shape remains stable even when Jenkins plugins expose different metric sets
**And** raw coverage plugin JSON/XML is not exposed directly
**And** fixtures cover coverage present, coverage missing, partial metrics, unsupported plugin, and permission-denied cases

### Story 5.4: Retrieve `recordIssues` / Warnings NG Issue Evidence

**Requirements covered:** FR31, FR32, FR33

As an AI agent,
I want to retrieve static-analysis issue summaries and details from `recordIssues` / Warnings NG,
So that I can understand whether build quality findings explain the failure.

**Acceptance Criteria:**

**Given** a build with Warnings NG or equivalent issue data
**When** the agent calls `jenkins_list_issues`
**Then** the server returns bounded issue summary data including totals, severity/category where available, tool/source where available, and affected files where available
**And** the agent can retrieve individual issue details where Jenkins exposes them
**And** large issue lists use the shared pagination shape
**And** missing plugin or endpoint data returns `unsupported_capability` with fallback guidance
**And** fixtures cover issues present, no issues, large issue list, missing plugin, individual issue details, and permission-denied cases

### Story 5.5: Retrieve SCM Configuration and Build Change Sets

**Requirements covered:** FR34, FR35, FR36, FR37

As an AI agent,
I want to retrieve SCM configuration and change set details for a job or build,
So that I can connect a Jenkins result to the source changes that triggered it.

**Acceptance Criteria:**

**Given** a job or build with SCM data
**When** the agent calls SCM or change set tools
**Then** the server returns SCM configuration where available, branch/change request metadata where available, and build change sets with commit identifiers, authors, messages, affected files, and timestamps where Jenkins exposes them
**And** large change sets are bounded or paginated
**And** missing SCM data is represented as unavailable data, not server failure
**And** raw Jenkins SCM/change JSON/XML is not exposed directly
**And** fixtures cover Git SCM, multibranch metadata, pull/change request metadata, no changes, large change set, and permission-denied cases

### Story 5.6: List Artifacts and Inspect Artifact Metadata

**Requirements covered:** FR38, FR39, FR43

As an AI agent,
I want to list build artifacts and inspect artifact metadata before retrieval,
So that I can decide whether an artifact should be read inline or downloaded.

**Acceptance Criteria:**

**Given** a build with artifacts
**When** the agent calls `jenkins_list_artifacts` or artifact metadata tools
**Then** the server returns bounded artifact records with `artifactPath`, display name, size where available, content type where available, fingerprint/hash where available, and Jenkins URL where available
**And** artifact identity is always `{ jobFullName, buildNumber, artifactPath }`
**And** artifact paths are validated for traversal and unsafe forms before use
**And** missing, deleted, expired, inaccessible, or unsafe artifacts return structured errors
**And** fixtures cover no artifacts, text artifacts, binary artifacts, nested paths, missing size, deleted/expired artifact, unsafe path, and permission-denied cases

### Story 5.7: Read Small Text Artifacts Inline

**Requirements covered:** FR40, FR43

As an AI agent,
I want to read small text artifacts inline,
So that I can inspect diagnostic files without saving them to disk.

**Acceptance Criteria:**

**Given** a safe artifact path and configured inline size limit
**When** the agent requests inline artifact content
**Then** the server returns text content, content type, size metadata, truncation metadata, and checksum/hash where available
**And** binary or oversized artifacts are not returned inline and instead provide guidance to use download behavior
**And** inline content respects response-size limits and redaction hooks where configured
**And** missing, deleted, unsafe, binary, and oversized artifacts return structured responses or errors as appropriate
**And** fixtures cover small text, truncated text, binary artifact, oversized artifact, unsafe path, and permission-denied cases

### Story 5.8: Download Large or Binary Artifacts Safely

**Requirements covered:** FR41, FR42, FR43

As an AI agent,
I want to download large or binary artifacts to a configured filesystem location,
So that external tools can inspect build outputs that are not suitable for MCP inline responses.

**Acceptance Criteria:**

**Given** a configured artifact download policy and safe destination
**When** the agent requests artifact download
**Then** the server writes the artifact only through `internal/artifacts` to an allowed directory
**And** the response returns saved path, size, content type where available, checksum/hash where available, and source artifact identity
**And** downloads reject path traversal, absolute destination paths, implicit archive extraction, and overwrites unless explicitly allowed
**And** deleted, expired, inaccessible, unsafe, and filesystem failure cases return structured errors
**And** fixtures and unit tests cover allowed download, unsafe destination, overwrite denied, missing artifact, binary artifact, and filesystem error cases

### Story 5.9: Provide Evidence Fallback Metadata

**Requirements covered:** FR30, FR33, FR43, FR54

As an AI agent,
I want evidence tools to tell me which other evidence sources may be useful,
So that I can adapt diagnosis when a plugin or data source is unavailable.

**Acceptance Criteria:**

**Given** an evidence request succeeds, partially succeeds, or returns unsupported capability
**When** the server responds
**Then** the response includes diagnostic metadata that helps the agent choose fallback evidence sources where known
**And** fallback metadata only references enabled server tools and discovered/likely Jenkins capabilities
**And** omitted folded coverage and unsupported issue, test, SCM, or artifact data are distinguishable from server failure
**And** fallback suggestions use stable tool names and canonical identifiers
**And** contract fixtures cover successful evidence with next suggestions, partial evidence, unsupported capability, and permission-limited evidence

## Epic 6: Trigger, Track, and Cancel Builds Under Explicit Policy

AI agents can trigger parameterized builds, inspect queue/executor state, track queued work, and cancel queued or running builds only when policy and Jenkins permissions allow it.

### Story 6.1: Inspect Queue and Executor State

**Requirements covered:** FR20, FR46

As an AI agent,
I want to inspect Jenkins queue and executor state,
So that I can understand whether work is waiting, running, blocked, or assigned.

**Acceptance Criteria:**

**Given** a configured Jenkins controller
**When** the agent calls queue or executor inspection tools
**Then** the server returns bounded queue items and executor state where Jenkins exposes them
**And** queue items include `queueId`, task/job reference where available, blocked/buildable status, why text where available, and related Jenkins URL where available
**And** executor state includes running job/build references where available
**And** permission-denied or unavailable endpoint cases return structured errors
**And** fixtures cover empty queue, blocked queue item, buildable queue item, running executor, and permission-denied cases

### Story 6.2: Trigger a Non-Parameterized Build Under Policy

**Requirements covered:** FR44, FR49

As an AI agent,
I want to trigger a build only when mutation policy allows it,
So that I can start CI verification without hidden writes.

**Acceptance Criteria:**

**Given** mutating actions are enabled for build trigger
**When** the agent calls `jenkins_trigger_build` for a non-parameterized job
**Then** the server validates policy, Jenkins permissions, job identity, and crumb/CSRF requirements before triggering
**And** the response returns accepted/rejected status, `queueId` where available, job identity, Jenkins URL where available, and audit reference where available
**And** default read-only configuration rejects the request with `mutation_denied`
**And** unauthorized, forbidden, missing job, disabled job, and Jenkins unavailable cases return structured errors
**And** audit records redact sensitive values and include actor, tool, target, result, and timestamp

### Story 6.3: Trigger a Parameterized Build with Explicit Parameters

**Requirements covered:** FR45, FR49

As an AI agent,
I want to trigger parameterized Jenkins builds with explicit parameters,
So that I can run CI with the intended branch, environment, or build options.

**Acceptance Criteria:**

**Given** a parameterized job and enabled mutation policy
**When** the agent calls `jenkins_trigger_build` with parameters
**Then** the server validates parameter payload shape and sends explicit parameters to Jenkins
**And** sensitive parameter names or values are redacted from logs and audit records according to redaction rules
**And** unsupported, missing, or invalid parameter cases return structured validation errors
**And** the response returns queue/build tracking metadata where Jenkins provides it
**And** fixtures cover string, boolean, choice-like, missing, invalid, and sensitive parameters

### Story 6.4: Inspect Triggered Queue Items

**Requirements covered:** FR46

As an AI agent,
I want to inspect queue items created by triggered builds,
So that I can track whether Jenkins accepted, blocked, canceled, or started the build.

**Acceptance Criteria:**

**Given** a `queueId` returned from trigger
**When** the agent calls `jenkins_get_queue_item`
**Then** the server returns queue state, task/job reference, why text where available, executable build reference where available, cancellation state where available, and Jenkins URL where available
**And** if the queue item has started, the response includes canonical `{ jobFullName, buildNumber }` when derivable
**And** expired or missing queue items return structured `not_found` or unavailable-data responses
**And** permission-denied and unavailable endpoint cases return structured errors
**And** fixtures cover waiting, blocked, started, canceled, expired, and permission-denied queue items

### Story 6.5: Cancel Queued Builds Under Policy

**Requirements covered:** FR47, FR49

As an AI agent,
I want to cancel queued builds only when policy and Jenkins permissions allow it,
So that unwanted queued work can be stopped safely.

**Acceptance Criteria:**

**Given** a queued build item and enabled cancel policy
**When** the agent calls `jenkins_cancel_queue_item`
**Then** the server validates mutation policy, `queueId`, Jenkins permissions, and crumb/CSRF requirements before cancellation
**And** the response returns accepted/rejected status, queue identity, target job where available, and audit reference where available
**And** default read-only configuration rejects the request with `mutation_denied`
**And** missing, already-started, already-canceled, forbidden, and Jenkins unavailable cases return structured responses or errors
**And** audit records include actor, tool, target, result, and timestamp with sensitive fields redacted

### Story 6.6: Cancel Running Builds Under Policy

**Requirements covered:** FR48, FR49

As an AI agent,
I want to cancel running builds only when policy and Jenkins permissions allow it,
So that runaway or obsolete CI work can be stopped safely.

**Acceptance Criteria:**

**Given** a running build identified by `jobFullName` and `buildNumber`
**When** the agent calls `jenkins_cancel_build`
**Then** the server validates mutation policy, build identity, Jenkins permissions, and crumb/CSRF requirements before cancellation
**And** the response returns accepted/rejected status, build identity, latest known build state, Jenkins URL where available, and audit reference where available
**And** completed builds are not canceled and return a clear non-mutating response
**And** default read-only configuration rejects the request with `mutation_denied`
**And** missing, forbidden, completed, already-canceled, and Jenkins unavailable cases return structured responses or errors

### Story 6.7: Audit Mutating Actions Consistently

**Requirements covered:** FR44, FR45, FR47, FR48, FR49

As a platform engineer,
I want every mutating Jenkins action to emit consistent audit records,
So that trigger and cancel behavior can be reviewed after use.

**Acceptance Criteria:**

**Given** any trigger or cancel tool is called
**When** the action is accepted, rejected, fails, or is denied by policy
**Then** the server emits an audit record with timestamp, controller, authenticated Jenkins user where available, MCP tool name, target identity, redacted parameters, result, and error code where relevant
**And** audit destination and retention behavior are documented in configuration
**And** audit emission failures are handled according to configured fail-open/fail-closed behavior
**And** audit tests cover accepted, denied, failed, and redacted-parameter cases
**And** no read-only inspection tool emits mutation audit records unless later policy explicitly requires it

## Epic 7: Document Agent Workflows, Setup, and Compatibility Boundaries

Developers and platform engineers can install, configure, troubleshoot, and understand the server's tool contracts and Jenkins compatibility behavior.

### Story 7.1: Package the Server as a Docker Image

**Requirements covered:** FR57

As a platform engineer,
I want to run the Jenkins MCP server as a Docker image,
So that shared internal deployments can use the same capabilities as the standalone binary.

**Acceptance Criteria:**

**Given** the server builds as a standalone Go binary
**When** the Docker image is built
**Then** the image contains the server binary and can run the stdio MCP server entrypoint
**And** secrets are supplied only at runtime and are not baked into image layers
**And** Docker execution exposes equivalent MCP capabilities to binary execution
**And** image build instructions and runtime configuration examples are documented
**And** tests or build checks verify the Docker image can start with example-safe configuration

### Story 7.2: Document All v1 MCP Tool Contracts

**Requirements covered:** FR60

As a developer,
I want human-readable documentation for every v1 MCP tool,
So that I can understand each tool's purpose, inputs, outputs, limits, and failure modes.

**Acceptance Criteria:**

**Given** all v1 tools implemented through earlier epics
**When** a developer reads `docs/tools/`
**Then** every v1 MCP tool has documentation covering purpose, input fields, output fields, response limits, error codes, safety considerations, and examples
**And** docs use canonical names like `jobFullName`, `buildNumber`, `queueId`, `artifactPath`, `cursor`, and `limit`
**And** documentation reflects the actual contract fixtures and schema snapshots
**And** docs do not include real credentials, private Jenkins URLs, or unsafe examples
**And** a doc check or review step verifies all registered tools have docs

### Story 7.3: Write Binary and Docker Setup Guides

**Requirements covered:** FR61

As a developer or platform engineer,
I want setup documentation for local binary and Docker use,
So that I can configure the server correctly for my environment.

**Acceptance Criteria:**

**Given** a user wants to run the server locally or in a shared environment
**When** they follow setup documentation
**Then** the docs explain binary usage, Docker usage, config file structure, environment variables, credential references, default controller configuration, artifact policy, mutation policy, and response limits
**And** examples include local MCP client configuration, prioritizing JetBrains-compatible MCP usage where possible
**And** the docs explain config precedence: flags > environment variables > config file > defaults
**And** docs include startup troubleshooting for missing controller configuration, invalid credentials, invalid artifact policy, and disabled mutations
**And** examples use placeholder-safe values only

### Story 7.4: Document Diagnostic Agent Workflows

**Requirements covered:** FR62

As an application developer,
I want example agent workflows for common Jenkins investigations,
So that I can see how the MCP tools help diagnose and verify CI failures.

**Acceptance Criteria:**

**Given** the server supports read and action workflows
**When** a developer reads workflow examples
**Then** examples cover failed pipeline investigation, running build watch, artifact retrieval, and parameterized build trigger workflows
**And** examples show tool-call sequences rather than raw Jenkins endpoint calls
**And** examples demonstrate bounded log use, fallback behavior, and evidence gathering from tests, issues, SCM, and artifacts
**And** trigger/cancel examples clearly show mutation policy and audit expectations
**And** examples are suitable for AI coding agents and human developers

### Story 7.5: Publish Jenkins Plugin Capability Matrix

**Requirements covered:** FR51, FR63

As a platform engineer,
I want a documented Jenkins capability matrix,
So that I understand which features depend on Jenkins job types, plugins, or endpoints.

**Acceptance Criteria:**

**Given** Jenkins capability behavior implemented across tool families
**When** a platform engineer reads the capability matrix
**Then** it explains baseline Jenkins Remote API support and plugin-dependent support for Pipeline, JUnit, Coverage, Warnings NG / `recordIssues`, artifacts, queue, and crumbs
**And** the matrix distinguishes server-supported tool categories from detected Jenkins capabilities
**And** missing-plugin, unsupported-job-type, permission-limited, and unavailable-endpoint behaviors are documented
**And** fallback evidence paths are listed for unavailable pipeline, coverage, issue, test, SCM, and artifact data
**And** minimum supported Jenkins/plugin assumptions discovered during implementation are documented

### Story 7.6: Write Security, Audit, and Troubleshooting Guidance

**Requirements covered:** FR61, FR63

As a platform engineer,
I want security, audit, and troubleshooting guidance,
So that I can operate the server safely and diagnose integration failures.

**Acceptance Criteria:**

**Given** the server handles credentials, logs, artifacts, and mutating actions
**When** the operator reads security and operations docs
**Then** the docs explain credential handling, secret redaction limits, Jenkins permission reliance, artifact download safety, mutating-action policy, audit destination/retention, and Docker secret handling
**And** troubleshooting covers missing permissions, missing plugins, unsupported job types, expired builds, deleted artifacts, unavailable endpoints, invalid parameters, unsafe download paths, and Jenkins/server communication failures
**And** the docs explain that Jenkins log masking remains authoritative and returned Jenkins text may contain secrets already present in logs
**And** the docs explain audit fail-open/fail-closed behavior if implemented
**And** examples avoid exposing real secrets or internal hostnames
