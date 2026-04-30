---
stepsCompleted:
  - step-01-init
  - step-02-discovery
  - step-02b-vision
  - step-02c-executive-summary
  - step-03-success
  - step-04-journeys
  - step-05-domain
  - step-06-innovation
  - step-07-project-type
  - step-08-scoping
  - step-09-functional
  - step-10-nonfunctional
  - step-11-polish
  - step-12-complete
releaseMode: phased
date: '2026-04-25'
inputDocuments:
  - /Users/david/bmad/_bmad-output/planning-artifacts/product-brief-bmad.md
documentCounts:
  productBriefs: 1
  research: 0
  brainstorming: 0
  projectDocs: 0
classification:
  projectType: developer_tool
  domain: general developer infrastructure / CI tooling
  complexity: medium
  projectContext: greenfield
workflowType: 'prd'
---

# Product Requirements Document - Go Jenkins MCP Server

**Author:** David
**Date:** 2026-04-25

## Executive Summary

The Go Jenkins MCP Server is an external, pipeline-focused Model Context Protocol server for application developers and AI coding agents working with Jenkins. It gives agents structured access to Jenkins job, build, pipeline, test, coverage, issue, artifact, SCM, queue, and running-build evidence without requiring developers to open Jenkins, paste logs, or install a Jenkins-side plugin.

The product solves a practical CI diagnosis problem: Jenkins usually contains the evidence needed to understand a failed build, but that evidence is fragmented across console output, stage views, plugin-specific reports, artifacts, change sets, queue state, and build metadata. AI coding agents cannot reliably diagnose or verify CI failures from shallow status checks or copied log excerpts. This server turns Jenkins into a bounded, agent-readable evidence source that supports failure investigation, safe reruns, and developer workflow continuity.

The primary users are application developers and AI coding agents acting on their behalf. The initial product is internal developer infrastructure, designed with a stable path toward open source once the tool contract, safety model, and Jenkins compatibility behavior are proven.

### What Makes This Special

The core differentiator is pipeline-first diagnostic depth from outside Jenkins. The server does not try to become a Jenkins administration surface or require a Jenkins plugin. Instead, it normalizes the evidence developers need when a pipeline fails: progressive logs, stage and step output, test failures, coverage data, `recordIssues` findings, artifacts, SCM changes, queue state, parameters, and running-build progress.

The core insight is that Jenkins already has the evidence, but agents need a purpose-built MCP layer to access it safely and in context-sized slices. The product's first value moment is when an AI coding agent can move from a failed Jenkins build to a likely root cause, relevant evidence, and a safe rerun path without the developer leaving their coding environment.

## Project Classification

- **Project Type:** Developer tool
- **Domain:** Developer infrastructure / CI tooling
- **Complexity:** Medium
- **Project Context:** Greenfield
- **Primary Interface:** MCP tools exposed by an external Go server
- **Primary Users:** Application developers and AI coding agents acting on behalf of developers

## Success Criteria

### User Success

Application developers can hand a failed Jenkins build, job name, or build URL to an AI coding agent and receive a useful diagnosis without manually opening Jenkins. A successful user experience means the agent can identify the failed stage or step, retrieve the relevant log slice, inspect test/coverage/issue/artifact/SCM evidence when available, and explain the likely cause in terms a developer can act on.

The core user "aha" moment is when the agent moves from Jenkins failure signal to likely root cause and relevant evidence without requiring copied console output.

### Business Success

For the internal version, success means the tool becomes useful enough to include in normal developer-agent workflows for Jenkins-backed projects. It should reduce manual Jenkins UI visits during CI failure diagnosis and make agent-driven fix-and-verify loops practical.

Initial business success should be measured by internal adoption, repeated use during failed build investigations, and developer confidence that AI agents can inspect Jenkins evidence accurately and safely, without setting arbitrary early percentage targets.

### Technical Success

The server must expose a stable MCP tool interface over Jenkins APIs while handling Jenkins variability gracefully. It must support bounded responses for large logs, large test reports, large issue lists, coverage data, `recordIssues` findings, and artifacts; report missing plugins or unsupported endpoints clearly; and avoid turning Jenkins into an unsafe automation surface.

Mutating actions in v1 must be explicit, permission-aware, and auditable. Build triggering, queue inspection, and cancellation should work where configured, but the default product posture remains read-heavy.

### Measurable Outcomes

- An agent can inspect a failed pipeline and identify the relevant failing stage, step, or test without manual log pasting.
- Large console output can be read progressively through cursors, ranges, tailing, search, and context snippets.
- Running builds can be watched through bounded updates including queue/executor state, stage progress, log cursor, and completion state.
- The server can list jobs across folders and retrieve build/job details for common Jenkins job types, especially pipeline and multibranch jobs.
- The server can retrieve JUnit test results, coverage summaries, `recordIssues` findings, SCM changes, build parameters, causes, and artifacts when those data sources are available.
- Small text artifacts can be read inline, while larger or binary artifacts can be saved to the filesystem for external tools.
- The server clearly reports unavailable plugin-specific capabilities instead of returning ambiguous failures.
- Basic build triggering with parameters, queue inspection, and cancellation are available in v1 behind configuration and Jenkins permissions.

## Product Scope

### MVP - Minimum Viable Product

The MVP proves that an external Go MCP server can support real Jenkins pipeline diagnosis without a Jenkins plugin. It includes controller-aware configuration, job and folder discovery, build listing, build detail inspection, progressive log reading, running build watch support, pipeline stage/step inspection, JUnit failure retrieval, coverage summary retrieval, `recordIssues` issue retrieval, artifact listing, inline small-text artifact retrieval, filesystem download for large or binary artifacts, SCM/change set inspection, queue inspection, parameterized build triggering, and cancellation where allowed.

Coverage and `recordIssues` support are part of MVP with plugin-aware graceful degradation. If the required Jenkins plugins or endpoints are absent, the server must explain that clearly.

### Out of Scope for MVP

- Jenkins-side plugin installation.
- Full Jenkins administration.
- Creating, editing, or deleting Jenkins jobs.
- Jenkins node management, quiet-down, restart, or global configuration changes.
- Deep historical analytics beyond recent evidence needed for diagnosis.
- A graphical user interface.
- Broad Jenkins fleet administration beyond controller-aware configuration needed by the MCP server.

### Growth Features (Post-MVP)

Post-MVP work can improve depth, ergonomics, and compatibility: richer coverage drilldown, issue trends, flaky test history, stage retry/replay support where Jenkins allows it, broader plugin compatibility, stronger artifact handling, multi-controller management UX, generated troubleshooting summaries, and hardened audit/reporting for mutating actions.

### Vision (Future)

The long-term vision is a standard Jenkins MCP bridge for AI development tools. A developer can ask an agent to investigate a failed CI run, and the agent can gather Jenkins evidence, identify likely root cause, patch code, trigger or monitor the appropriate build, and report the result without forcing the developer through Jenkins UI workflows.

## User Journeys

### Journey 1: Developer Investigates a Failed Pipeline Through an AI Agent

Maya is an application developer working on a service change. Her Jenkins pipeline fails after she pushes a branch, but the Jenkins UI has too much scattered information: the console log is large, the failure may be inside a nested pipeline stage, and the relevant test report may not be obvious.

Instead of opening Jenkins, Maya asks her AI coding agent to investigate the failed build. The agent uses the Jenkins MCP server to inspect the job, build metadata, pipeline stages, progressive log output, JUnit failures, SCM changes, and any available coverage or `recordIssues` data. It identifies the failed stage, retrieves the relevant log slice, connects the failure to the likely code change, and summarizes the evidence.

The critical value moment is when Maya receives a focused explanation with links or identifiers for the exact stage, test, issue, artifact, or change set that matters. She can stay in her coding environment and decide whether to patch code, adjust tests, or rerun the build.

This journey reveals requirements for job/build lookup, pipeline stage inspection, progressive log reading, JUnit retrieval, SCM/change set inspection, issue/coverage retrieval, bounded summaries, and clear missing-capability reporting.

### Journey 2: AI Coding Agent Fixes and Verifies a CI Failure

An AI coding agent is working on behalf of a developer. It has made a code change and needs to verify whether Jenkins accepts it. The agent triggers a parameterized build through the MCP server, inspects the queue item, watches the running build, and reads incremental log/stage progress without flooding its context.

If the build fails, the agent gathers the relevant failure evidence: stage logs, failed tests, `recordIssues` findings, coverage output, artifacts, and SCM metadata. It uses that evidence to propose or apply a fix, then triggers or monitors another build where allowed.

The critical value moment is a closed loop: change, trigger, watch, diagnose, fix, and verify without manual Jenkins navigation.

This journey reveals requirements for parameter inspection, build triggering with parameters, queue inspection, running build watch support, progressive log cursors, stage progress, safe cancellation, artifact handling, and permission-aware mutating actions.

### Journey 3: Developer Handles Missing Plugin or Partial Jenkins Data

Sam is debugging a pipeline failure on a Jenkins controller that does not have every expected plugin installed, or where a job type exposes only partial pipeline metadata. The AI agent asks for coverage or stage-level details, but Jenkins cannot provide the requested data.

The MCP server returns a clear capability response instead of a generic failure. It explains that the endpoint or plugin data is unavailable for this controller/job/build and offers what can still be inspected: console logs, build result, test reports, artifacts, SCM changes, or raw job/build metadata.

The critical value moment is graceful degradation. Sam still gets useful diagnostic evidence, and the agent can adapt its investigation strategy instead of stopping on an ambiguous error.

This journey reveals requirements for controller capability discovery, plugin-aware feature detection, structured unsupported-capability errors, fallback evidence paths, and consistent error taxonomy.

### Journey 4: Platform Engineer Configures Safe Jenkins Access

Riley is a platform/build engineer responsible for letting AI tools access Jenkins safely. They need to configure one or more Jenkins controllers, credentials, default controller behavior, allowed mutating actions, artifact download locations, and audit expectations.

Riley sets up the MCP server with controller-aware configuration. Read-heavy tools are enabled by default. Triggering, canceling, and artifact downloads are explicitly configured and constrained by Jenkins permissions and server settings. Riley can verify which controllers and plugin capabilities are available, and can reason about what agents are allowed to do.

The critical value moment is confidence: Riley can enable developer-agent workflows without exposing broad Jenkins administration capabilities.

This journey reveals requirements for multi-controller-aware configuration, default controller selection, credential handling, mutating-action controls, filesystem download policy, audit logging, capability discovery, and clear operational documentation.

### Journey Requirements Summary

The journeys reveal these core capability areas:

- Controller-aware Jenkins configuration with a simple default-controller path.
- Job and folder discovery, including pipeline and multibranch jobs.
- Build listing, build details, build causes, parameters, SCM, and change sets.
- Progressive large-log inspection with cursors, ranges, tailing, search, and context snippets.
- Running build watch support with queue/executor state, stage progress, log cursor, and completion detection.
- Pipeline stage, node, and step inspection where Jenkins exposes it.
- JUnit, coverage, `recordIssues`, artifact, and SCM evidence retrieval.
- Inline small-text artifact reading and filesystem download for large or binary artifacts.
- Parameterized build triggering, queue inspection, and cancellation where configured and permitted.
- Capability discovery and graceful degradation when plugins/endpoints are unavailable.
- Permission-aware and auditable mutating actions.
- Clear error taxonomy for missing permissions, missing plugins, unsupported job types, expired builds, deleted artifacts, and unavailable data.

## Domain-Specific Requirements

### Compliance & Regulatory

No domain-specific regulatory compliance is required for v1. The product should still respect internal security and audit expectations for developer tooling that can inspect CI data and trigger Jenkins actions.

### Technical Constraints

- The server must not require Jenkins-side plugin installation.
- Jenkins credentials must be consumed from secure configuration and must not be logged, echoed, returned in MCP responses, or written to downloaded artifacts metadata.
- Mutating actions must respect Jenkins permissions and server-side configuration.
- Build triggering and cancellation must be explicit MCP tools, not side effects of inspection tools.
- Tool responses must be bounded to avoid flooding MCP clients with large logs, reports, or artifact contents.
- Artifact filesystem writes must use configured safe output directories and avoid accidental overwrites.
- The server must handle Jenkins plugin variability gracefully, especially for Pipeline, JUnit, Coverage, and Warnings NG / `recordIssues`.
- Errors must distinguish missing permissions, missing plugins, unsupported job types, unavailable endpoints, expired builds, and deleted artifacts.

### Integration Requirements

- Jenkins Remote Access API should be the baseline integration surface.
- Pipeline/stage/step inspection should use available Pipeline REST or workflow-related endpoints where installed.
- Test results should integrate with Jenkins JUnit report data where available.
- Coverage support should integrate with available Jenkins coverage plugin APIs where available.
- `recordIssues` support should integrate with Warnings NG or equivalent issue data where available.
- Artifact retrieval must support both inline small-text reads and filesystem download for large or binary artifacts.
- MCP tools should be controller-aware while allowing a simple default-controller configuration.

### Risk Mitigations

- **Risk:** Jenkins instances differ widely by plugin set and job type.
  **Mitigation:** Add capability discovery and structured unsupported-capability responses.
- **Risk:** Agents may accidentally perform unsafe Jenkins actions.
  **Mitigation:** Keep read-heavy defaults, explicitly configure mutating tools, validate parameters, and rely on Jenkins permissions.
- **Risk:** Large logs and reports can overwhelm model context or server memory.
  **Mitigation:** Use pagination, cursors, ranges, tailing, search, and bounded summaries.
- **Risk:** Artifact downloads could overwrite local files or place files in unsafe locations.
  **Mitigation:** Restrict downloads to configured directories, return saved paths clearly, and avoid overwrites unless explicitly requested.
- **Risk:** Secrets may appear in Jenkins logs or build metadata.
  **Mitigation:** Avoid additional secret exposure, document that Jenkins masking remains authoritative, and consider optional redaction patterns for returned text.

## Innovation & Novel Patterns

### Detected Innovation Areas

The product's innovative pattern is not a new CI system or a new Jenkins plugin. It is an agent-facing diagnostic layer that turns Jenkins into structured evidence for AI coding agents.

The novel combination is:

- Jenkins as the source of CI truth.
- MCP as the agent-facing tool protocol.
- Pipeline-first evidence gathering across logs, stages, tests, coverage, issues, artifacts, SCM, queue state, and running builds.
- Bounded tool responses designed for model context limits.
- Safe, explicit mutating actions for trigger, queue, watch, and cancel workflows.

This shifts Jenkins interaction from human navigation and manual log copying to agent-driven evidence retrieval and verification.

### Market Context & Competitive Landscape

Existing alternatives include direct Jenkins UI usage, Jenkins Remote Access API scripts, CI-specific IDE integrations, generic MCP wrappers, and Jenkins-side plugins. The differentiator is an external Go MCP server focused on developer-agent diagnosis, with no Jenkins plugin requirement and a tool contract designed around CI failure investigation.

The product should avoid competing with Jenkins administration tools. Its strongest position is as a developer workflow bridge between AI coding agents and existing Jenkins installations.

### Validation Approach

The innovation should be validated through realistic failed-pipeline scenarios:

- Give an agent only a Jenkins build URL or job/build identifier.
- Verify the agent can identify the relevant failing stage, test, issue, artifact, or SCM change.
- Verify the agent can inspect large logs without excessive context use.
- Verify graceful degradation when pipeline, coverage, or issue plugins are unavailable.
- Verify trigger, queue, watch, and cancel workflows behave safely under configured permissions.

Validation should focus on whether the tool makes agentic Jenkins diagnosis materially more useful than status-only checks or pasted logs.

### Risk Mitigation

- **Risk:** The MCP surface becomes a thin wrapper over Jenkins APIs instead of a diagnostic product.
  **Mitigation:** Organize tools around developer questions and failure-investigation workflows, not Jenkins endpoint names.
- **Risk:** The agent receives too much raw data.
  **Mitigation:** Design every large-data tool around bounded retrieval, cursors, search, and summaries.
- **Risk:** Jenkins plugin variability weakens the promise.
  **Mitigation:** Treat capability discovery and graceful degradation as core features.
- **Risk:** Build actions make the tool feel unsafe.
  **Mitigation:** Keep mutating actions explicit, configured, permission-aware, auditable, and easy to disable.

## Developer Tool Specific Requirements

### Project-Type Overview

The product is a developer infrastructure tool implemented as an external Go MCP server. Its primary interface is an MCP tool surface consumed by AI coding agents and developer environments, with initial emphasis on JetBrains IDE-based workflows while remaining compatible with any MCP-capable client.

The product should ship as both a standalone Go binary and a Docker image. The binary supports simple local installation and direct IDE/agent use. The Docker image supports repeatable internal deployment, isolated configuration, and easier use in shared developer infrastructure.

### Technical Architecture Considerations

- The implementation language is Go.
- The server must expose MCP tools over a stable-enough v1 interface, while allowing breaking changes during the initial internal v1 phase.
- The tool surface should be hand-designed around developer diagnostic workflows rather than mechanically mirroring Jenkins endpoints.
- Tool schemas are the machine-readable source of truth for MCP clients.
- Every v1 tool should have human-readable documentation describing purpose, inputs, outputs, safety considerations, and common failure modes.
- The server should support configuration suitable for both local developer use and shared/internal deployment.
- Docker packaging must not assume secrets are baked into the image; credentials should be supplied through runtime configuration or environment-specific secret mechanisms.
- The binary and Docker image should expose equivalent MCP capabilities.

### Language Matrix

- **Implementation language:** Go
- **Runtime target:** Local developer machines and internal infrastructure hosts capable of running Go binaries or containers
- **Client ecosystem:** Any MCP-compatible client, with primary validation against JetBrains IDE-based agent workflows where possible

### Installation Methods

- Standalone Go binary for local use.
- Docker image for containerized use.
- Configuration file and/or environment-variable based setup for Jenkins controller definitions, credentials, default controller selection, mutating-action enablement, and artifact download policy.
- Documentation must include setup examples for at least one local binary flow and one Docker flow.

### API Surface

The MCP tool surface should be organized around developer tasks:

- Discover Jenkins controllers, jobs, folders, and builds.
- Inspect job and build details.
- Read large logs progressively.
- Watch running builds.
- Inspect pipeline stages, nodes, and step logs.
- Retrieve JUnit, coverage, `recordIssues`, SCM, change set, and artifact evidence.
- Read small text artifacts inline and download larger or binary artifacts to the filesystem.
- Trigger parameterized builds, inspect queue state, and cancel queued/running work where configured and permitted.
- Report controller/plugin capabilities and structured errors.

The v1 tool surface may evolve during internal development, but the product should converge on stable names, schemas, and response patterns before open-source release.

### Code Examples and Documentation

Documentation should include:

- Quick start for local binary use.
- Quick start for Docker use.
- Jenkins authentication and credential guidance.
- Example MCP client configuration, prioritizing JetBrains IDE workflows if available.
- Example failed pipeline investigation workflow.
- Example running build watch workflow.
- Example artifact retrieval workflow.
- Example parameterized build trigger workflow.
- Safety model documentation for mutating actions.
- Capability matrix explaining plugin-dependent behavior for Pipeline, JUnit, Coverage, and Warnings NG / `recordIssues`.
- Troubleshooting guidance for missing permissions, missing plugins, unsupported job types, expired builds, deleted artifacts, and unavailable endpoints.

### Migration and Adoption Guidance

Adoption guidance should explain how developers and AI agents move from existing Jenkins workflows to the MCP server:

- From manually opening Jenkins UI pages to asking an agent to inspect a job, build, stage, test, issue, artifact, or change set.
- From copying console log excerpts into chat to using bounded log, search, tail, and stage-output tools.
- From ad hoc Jenkins API scripts to documented MCP tools with stable schemas and structured errors.
- From manual rerun/watch workflows to explicit trigger, queue, watch, and cancellation tools where configured and permitted.
- From unknown plugin behavior to capability discovery and plugin-aware fallback paths.

The migration guide should include at least one before/after workflow for failed pipeline diagnosis and one before/after workflow for trigger-watch-verify loops.

## Project Scoping & Phased Development

### MVP Strategy & Philosophy

**MVP Approach:** Problem-solving MVP

The MVP must prove that an external Go MCP server can help AI coding agents diagnose Jenkins pipeline failures using structured evidence, without requiring developers to open Jenkins or paste logs. The first release is internal and may evolve quickly, but it must cover enough Jenkins evidence types to validate the core workflow: inspect, diagnose, trigger/watch, and verify.

**Resource Requirements:** The MVP requires Go backend development, Jenkins API/plugin familiarity, MCP tool design, CI/CD domain knowledge, and enough test infrastructure to exercise representative Jenkins job types and plugin combinations.

### MVP Feature Set (Phase 1)

**Core User Journeys Supported:**

- Developer asks an AI agent to inspect a failed Jenkins pipeline.
- AI agent triggers, watches, diagnoses, and verifies a Jenkins build.
- Developer or agent handles missing Jenkins plugin data gracefully.
- Platform engineer configures safe Jenkins access for internal use.

**Must-Have Capabilities:**

- External Go MCP server with no Jenkins plugin requirement.
- Standalone Go binary and Docker image packaging.
- Controller-aware configuration with simple default-controller support.
- Secure Jenkins credential consumption.
- Job and folder discovery, including nested folders.
- Build listing and build detail inspection.
- Job detail inspection.
- Progressive large-log reading with bounded responses.
- Running build watch support.
- Pipeline stage, node, and step inspection where Jenkins exposes it.
- JUnit test result retrieval.
- Coverage data retrieval with plugin-aware graceful degradation.
- `recordIssues` / Warnings NG issue retrieval with plugin-aware graceful degradation.
- SCM and change set inspection.
- Artifact listing, inline small-text artifact retrieval, and filesystem download for large/binary artifacts.
- Parameterized build triggering.
- Queue inspection.
- Cancellation of queued/running work where configured and permitted.
- Controller/plugin capability discovery.
- Structured error taxonomy for missing permissions, missing plugins, unsupported job types, unavailable endpoints, expired builds, and deleted artifacts.
- Human-readable documentation for each v1 MCP tool.
- Basic setup documentation for binary, Docker, Jenkins authentication, and MCP client configuration.

### Post-MVP Features

**Phase 2: Hardening and Compatibility**

- Broader Jenkins job type and plugin compatibility.
- Richer coverage drilldown.
- Richer `recordIssues` filtering and trends.
- Flaky test history and test trend analysis.
- Stronger artifact handling and file safety controls.
- Improved multi-controller management ergonomics.
- Better generated diagnostic summaries.
- Stronger audit reporting for mutating actions.
- Expanded validation across JetBrains IDE workflows and other MCP clients.
- Tool contract stabilization before external release.

**Phase 3: Open-Source Readiness**

- Stable public MCP tool contract.
- Public documentation site or equivalent comprehensive docs.
- Public Docker image and release artifacts.
- Example configurations and sample workflows.
- Plugin capability matrix.
- Contribution guidelines and development setup.
- Versioning policy and compatibility guarantees.
- Security guidance for Jenkins credentials, logs, artifact downloads, and mutating actions.

### Risk Mitigation Strategy

**Technical Risks:** Jenkins API and plugin variability may make uniform behavior difficult. Mitigate with capability discovery, graceful degradation, typed errors, representative test fixtures, and clear documentation of supported data sources.

**Market/User Risks:** Developers may not trust agent-driven Jenkins diagnosis if evidence is incomplete or too verbose. Mitigate by focusing tools around developer questions, returning bounded evidence, and validating with realistic failed-pipeline workflows.

**Resource Risks:** Supporting every Jenkins plugin deeply would over-expand v1. Mitigate by requiring baseline support for core evidence types and plugin-aware graceful degradation, while reserving richer compatibility and trends for Phase 2.

**Safety Risks:** Triggering, cancellation, and artifact downloads could create operational risk. Mitigate with read-heavy defaults, explicit mutating tools, Jenkins permission checks, server-side configuration, parameter validation, audit logging, and safe artifact download paths.

## Functional Requirements

### Jenkins Controller and Configuration

- FR1: Platform engineers can configure one or more Jenkins controllers for the MCP server.
- FR2: Platform engineers can designate a default Jenkins controller for tools that do not specify a controller explicitly.
- FR3: Platform engineers can configure Jenkins credentials without exposing secrets through MCP responses, logs, or documentation examples.
- FR4: Platform engineers can configure whether mutating Jenkins actions are enabled.
- FR5: Platform engineers can configure artifact download policy, including allowed filesystem locations.
- FR6: AI agents can discover which Jenkins controllers are available through the MCP server.

### Job and Build Discovery

- FR7: AI agents can list Jenkins jobs across folders.
- FR8: AI agents can list Jenkins jobs inside nested folders.
- FR9: AI agents can identify common Jenkins job types, including pipeline and multibranch jobs where Jenkins exposes that information.
- FR10: AI agents can retrieve job details needed for diagnosis.
- FR11: AI agents can list builds for a job.
- FR12: AI agents can retrieve build details, including result state, timestamps, duration, causes, parameters, and related metadata where available.
- FR13: AI agents can inspect upstream and downstream build relationships where Jenkins exposes them.

### Build Output and Running Build Inspection

- FR14: AI agents can read large build console output in bounded slices.
- FR15: AI agents can tail build output from a cursor or offset.
- FR16: AI agents can search build output and retrieve matching context snippets.
- FR17: AI agents can inspect the current state of a running build.
- FR18: AI agents can watch running build progress through bounded updates.
- FR19: AI agents can detect when a watched build completes.
- FR20: AI agents can inspect queue and executor state for queued or running builds.

### Pipeline Structure and Stage Evidence

- FR21: AI agents can inspect pipeline run structure where Jenkins exposes it.
- FR22: AI agents can inspect pipeline stages and their status where Jenkins exposes them.
- FR23: AI agents can inspect pipeline nodes, steps, or equivalent execution units where Jenkins exposes them.
- FR24: AI agents can retrieve output for individual pipeline stages, nodes, or steps where Jenkins exposes it.
- FR25: AI agents receive clear unsupported-capability responses when stage, node, or step data is unavailable.

### Test, Coverage, and Issue Evidence

- FR26: AI agents can retrieve JUnit test summaries for a build where available.
- FR27: AI agents can retrieve individual failed test details where available.
- FR28: AI agents can retrieve test stdout/stderr or related failure output where available.
- FR29: AI agents can retrieve coverage summaries where supported by Jenkins plugins.
- FR30: AI agents receive clear unsupported-capability responses when coverage data is unavailable.
- FR31: AI agents can retrieve `recordIssues` / Warnings NG issue summaries where available.
- FR32: AI agents can retrieve individual static-analysis issue details where available.
- FR33: AI agents receive clear unsupported-capability responses when issue data is unavailable.

### SCM and Change Evidence

- FR34: AI agents can retrieve SCM configuration details for a job or build where available.
- FR35: AI agents can retrieve build change sets where available.
- FR36: AI agents can inspect commit identifiers, authors, messages, affected files, and related change metadata where Jenkins exposes them.
- FR37: AI agents can retrieve branch or change request metadata where Jenkins exposes it.

### Artifact Access

- FR38: AI agents can list artifacts attached to a build.
- FR39: AI agents can inspect artifact metadata before retrieval.
- FR40: AI agents can read small text artifacts inline.
- FR41: AI agents can download large or binary artifacts to the filesystem.
- FR42: AI agents receive clear saved-path information after artifact download.
- FR43: AI agents receive clear errors when artifacts are deleted, expired, inaccessible, or unsafe to write.

### Build Actions and Queue Operations

- FR44: AI agents can trigger new builds for configured jobs where permitted.
- FR45: AI agents can trigger parameterized builds with explicit parameters.
- FR46: AI agents can inspect Jenkins queue items created by triggered builds.
- FR47: AI agents can cancel queued builds where configured and permitted.
- FR48: AI agents can cancel running builds where configured and permitted.
- FR49: AI agents receive clear permission or policy errors when mutating actions are unavailable.

### Capability Discovery and Error Handling

- FR50: AI agents can discover server-supported Jenkins capability categories for a controller.
- FR51: AI agents can discover plugin-dependent capability availability where Jenkins exposes enough information.
- FR52: AI agents receive structured errors for missing permissions, missing plugins, unsupported job types, unavailable endpoints, expired builds, deleted artifacts, invalid parameters, and unsafe download paths.
- FR53: AI agents can distinguish unavailable data from server failure.
- FR54: AI agents can retrieve enough diagnostic metadata to decide which fallback evidence source to inspect next.
- FR55: AI agents can inspect the authenticated Jenkins user identity and relevant permission context for a configured controller where Jenkins exposes it.

### Packaging, Documentation, and Developer Experience

- FR56: Developers can run the server as a standalone Go binary.
- FR57: Developers or platform engineers can run the server as a Docker image.
- FR58: Developers can configure the server for local MCP client use.
- FR59: Platform engineers can configure the server for shared internal use.
- FR60: Developers can read human-readable documentation for every v1 MCP tool.
- FR61: Developers can follow setup documentation for binary usage, Docker usage, Jenkins authentication, and MCP client configuration.
- FR62: Developers can follow documented examples for failed pipeline investigation, running build watch, artifact retrieval, and parameterized build triggering.
- FR63: Developers can understand plugin-dependent behavior through a documented capability matrix.

## Non-Functional Requirements

### Performance

- The server must return bounded responses for all tools that can access large Jenkins data, including console logs, stage logs, test results, coverage reports, issue lists, and artifacts. Default response payloads should stay under a documented configurable limit, initially targeting 64 KiB of text content per tool response unless a tool defines a smaller default.
- Large log tools must support incremental retrieval with explicit cursor or offset fields and documented default chunk sizing, initially targeting 32 KiB to 64 KiB per log chunk.
- Search and tail operations must return enough context to support diagnosis while respecting configured response-size limits, including documented limits for maximum matches and context lines.
- Running build watch operations must provide incremental progress updates without requiring clients to repeatedly fetch full build state. Watch responses should include a cursor or equivalent continuation token, and default polling guidance should target 2 to 10 second intervals unless the client or server config overrides it.
- Artifact retrieval must distinguish inline small-text reads from filesystem downloads for larger or binary files.

### Security

- Jenkins credentials must never be logged, returned in MCP responses, included in error messages, or baked into Docker images.
- The server must rely on Jenkins permissions for Jenkins-side authorization and must not bypass Jenkins access controls.
- Mutating tools, including build trigger, queue cancellation, and running build cancellation, must be explicitly enabled by configuration.
- Mutating tools must produce auditable records of requested action, target controller, job/build identifier, authenticated user where available, and result. Audit records must have a documented destination and retention policy in deployment configuration.
- Artifact downloads must be restricted to configured safe filesystem locations.
- Artifact downloads must avoid overwriting existing files unless explicitly requested.
- Returned Jenkins text may contain secrets already present in Jenkins logs; the product must document this risk and may support configurable redaction patterns.

### Reliability and Error Handling

- The server must return structured errors that distinguish missing permissions, missing plugins, unsupported job types, unavailable endpoints, expired builds, deleted artifacts, invalid parameters, unsafe download paths, and Jenkins/server communication failures.
- Plugin-dependent capabilities must degrade gracefully when Jenkins does not expose the required data.
- The server must allow agents to distinguish unavailable Jenkins data from MCP server failure.
- Running build watch behavior must handle queue-to-executor transitions, build completion, cancellation, and Jenkins-side failures clearly.
- Filesystem artifact downloads must report the final saved path or a specific failure reason.

### Integration Compatibility

- The server should use the official Go MCP SDK unless architecture review identifies a concrete blocker. If an alternative is chosen, the architecture document must record the blocker, evaluated alternatives, compatibility impact, and migration path back to the official SDK if feasible.
- The MCP tool interface must be compatible with MCP-capable clients generally, with primary validation against JetBrains IDE-based workflows.
- The server must support both standalone Go binary execution and Docker execution with equivalent MCP capabilities.
- The server must support configuration patterns suitable for local developer use and shared internal deployment.
- Jenkins integration must account for plugin variability across Pipeline, JUnit, Coverage, and Warnings NG / `recordIssues`.

### Operability

- The server must expose capability discovery so agents and operators can understand what each configured Jenkins controller supports.
- The server must provide clear startup/configuration errors for missing controller configuration, invalid credentials, invalid artifact download policy, and disabled mutating actions.
- The server must include human-readable documentation for every v1 MCP tool.
- The server must include setup documentation for binary usage, Docker usage, Jenkins authentication, and MCP client configuration.
- The server must include troubleshooting guidance for common Jenkins and MCP integration failures.

### Maintainability

- Tool schemas must be treated as the machine-readable source of truth for MCP clients.
- During internal v1, tool names and schemas may evolve, but the project must converge on stable names, schemas, and response patterns before open-source release.
- The codebase should keep Jenkins API/plugin-specific behavior isolated from MCP tool contract definitions so plugin compatibility can improve without unnecessary tool churn.
- The project should include representative test coverage for common Jenkins data shapes, plugin-available cases, and plugin-missing cases. At minimum, validation fixtures should cover freestyle or non-pipeline jobs, pipeline jobs, multibranch-style jobs, large logs, JUnit-present and JUnit-missing builds, coverage-present and coverage-missing builds, `recordIssues`-present and `recordIssues`-missing builds, artifacts-present and artifacts-missing builds, and permission-denied responses.
