---
title: "Product Brief: Go Jenkins MCP Server"
status: "draft"
created: "2026-04-25T05:47:39Z"
updated: "2026-04-25T05:56:20Z"
inputs: []
---

# Product Brief: Go Jenkins MCP Server

## Executive Summary

Developers lose time when Jenkins pipelines fail because the useful evidence is scattered across job pages, large console logs, pipeline stage views, JUnit reports, coverage reports, static analysis plugins, artifacts, queue state, and SCM metadata. AI coding agents can help fix build failures, but only if they can inspect Jenkins with the same depth a developer would use manually. Today, that usually means shallow build status checks, brittle scraping, or asking the developer to paste logs.

The Go Jenkins MCP Server is an external, pipeline-focused Model Context Protocol server that gives AI coding agents and developers structured access to Jenkins job and build evidence. It connects to existing Jenkins controllers without requiring a Jenkins plugin, exposes safe read-heavy inspection tools, and includes controlled v1 actions for triggering, queue inspection, and canceling builds. The first useful moment is simple: an AI agent inspects a failed Jenkins pipeline deeply enough to identify the likely cause without the developer opening Jenkins.

Initially this is internal developer tooling, with a path to open source once the interface and safety model have proven stable. The product should be designed for multiple Jenkins controllers from the start to avoid locking the MCP interface into a single-instance assumption, while keeping one-controller setup simple for early adoption.

## The Problem

Jenkins remains a critical CI system, but its information model is fragmented. A single failed pipeline can require moving between folder hierarchy, job configuration, build history, stage graph, console output, JUnit failures, coverage deltas, Warnings NG issues, artifacts, SCM changes, upstream causes, and queue state. Developers often only need an answer to one question: what failed, why, and what should I inspect or change next?

AI agents are poorly served by Jenkins when they only receive build status or the last few console lines. Large logs exceed context limits. Pipeline stage output is hidden behind plugin-specific APIs. Test, coverage, and static-analysis data differ by plugin. Artifacts may need to be read inline when small text, or downloaded to disk when large or binary. Without a purpose-built MCP layer, agents either miss the important evidence or require manual developer intervention.

The cost is repeated context switching, slow failure diagnosis, weaker agent autonomy, and avoidable CI reruns.

## The Solution

Build an external Go MCP server that turns Jenkins into a structured, agent-friendly CI evidence source.

The server provides tools to discover jobs across folders, list builds, inspect job and build details, read large build output progressively, watch running builds, inspect pipeline stages and individual step logs, retrieve JUnit test results, examine coverage and `recordIssues` data, inspect SCM and change set details, list and download artifacts, trigger new builds with parameters, inspect queue state, and cancel queued or running work where configured.

The interface should be controller-aware from day one. Every tool should either accept a controller identifier or operate against a configured default, so adding more Jenkins instances later does not force a breaking MCP redesign. That does not mean v1 needs complex fleet management; it means the tool contract should not assume there is only one Jenkins. For artifacts, the server should support both inline retrieval for small text-like files and filesystem download for larger or binary files.

## What Makes This Different

This is not a Jenkins plugin and does not require changing Jenkins controllers. It sits outside Jenkins, authenticates through existing Jenkins APIs, and makes current Jenkins installations usable by MCP clients and AI coding agents.

The differentiator is pipeline-first diagnostic depth. Instead of exposing only generic job/build data, the server normalizes the evidence developers actually need during CI failure investigation: stage status, node logs, test failures, coverage deltas, static-analysis issues, artifacts, changes, queue state, and trigger parameters.

The product should also be safer than a broad Jenkins operations bridge. It is read-heavy by default, with mutating actions explicitly enabled, permission-aware, and auditable. Basic triggering, queue inspection, and canceling are in v1 because they complete the developer workflow, but they should be constrained by configuration, Jenkins permissions, parameter validation, and clear tool descriptions.

## Who This Serves

The primary users are application developers and AI coding agents acting on behalf of those developers. Developers want fewer trips into the Jenkins UI and faster answers when CI fails. AI agents need reliable, structured Jenkins context so they can diagnose failures, propose fixes, rerun builds, and verify outcomes.

Secondary users include build and platform engineers who maintain Jenkins access patterns for teams. They benefit from a consistent MCP interface, controller-level configuration, and guardrails that prevent AI tooling from becoming an uncontrolled Jenkins automation surface.

## Success Criteria

The first success measure is diagnostic usefulness: for common pipeline failures, an agent can retrieve enough Jenkins evidence to summarize the likely cause and identify the relevant stage, step, test, issue, artifact, or change set without manual log pasting.

Operational success should be measured by:

- Percentage of failed pipeline investigations completed without opening Jenkins UI.
- Median time from failed build URL or job/build identifier to likely root cause summary.
- Coverage of common Jenkins evidence types: console logs, stage logs, JUnit, coverage, warnings/issues, artifacts, SCM changes, parameters, queue state.
- Clear capability reporting when a Jenkins plugin or endpoint is unavailable, so agents can adapt rather than fail mysteriously.
- Reliability against large logs, running builds, and large job folders without timing out or flooding MCP responses.
- Safe use of v1 mutating actions: successful parameterized build triggers, queue lookups, and cancellations with clear audit trails.

## V1 Scope

Version 1 should include:

- External Go MCP server with no Jenkins plugin requirement.
- Configuration for one or more Jenkins controllers, with a default controller for simple usage.
- Job discovery across folders, including pipeline and multibranch-friendly metadata where available.
- Build listing and build detail inspection.
- Progressive large-log reading with ranges, tailing, search, and context snippets.
- Running build watch support, including current result state, queue/executor state, elapsed time, progressive log cursor, stage progress, and completion detection.
- Pipeline run, stage, node, and step inspection using available Jenkins pipeline APIs.
- JUnit test summary and failure detail retrieval.
- Coverage summary retrieval, with plugin-aware graceful degradation.
- `recordIssues` / Warnings NG issue summary and detail retrieval, with plugin-aware graceful degradation.
- SCM, change set, build cause, parameters, and upstream/downstream metadata where Jenkins exposes it.
- Artifact listing plus inline small-text retrieval and filesystem download for large or binary artifacts.
- Build triggering with parameters, queue inspection, and cancellation of queued/running work where allowed.
- Capability discovery for controller features, installed plugins, available report types, and unsupported endpoints.
- Clear error reporting for permission failures, missing plugins, unavailable data, expired builds, and unsupported job types.

Explicitly out of scope for v1:

- Requiring a Jenkins-side plugin.
- Full Jenkins administration.
- Creating or editing jobs.
- Credential management beyond consuming configured credentials securely.
- Deep historical analytics beyond enough recent history to support diagnosis.
- Jenkins fleet administration, node management, quiet-down/restart operations, or global configuration changes.
- A graphical user interface.

## Technical Approach

The server should hide Jenkins API fragmentation behind stable MCP tools. It can use Jenkins Remote Access API as the baseline, Pipeline REST API or related workflow endpoints for stage and node data, JUnit endpoints for test reports, Coverage plugin APIs for coverage, Warnings NG APIs for `recordIssues`, and artifact endpoints for downloads. Because plugin availability varies, capability detection and graceful degradation are core product requirements, not polish.

Tool responses should be deliberately bounded. Large logs, artifacts, test suites, and issue lists need pagination, cursors, byte ranges, search, and summaries so the MCP client receives useful slices instead of unbounded dumps. Downloads should be explicit about where files are written and should avoid accidental overwrites.

## Vision

If successful, this becomes the standard bridge between AI development tools and Jenkins. A developer can ask an AI agent to investigate a failed CI run, and the agent can traverse Jenkins evidence, identify the likely issue, patch code, rerun the right build, and report the result without forcing the developer through the Jenkins UI.

Longer term, the project could expand from internal tooling into an open-source Jenkins MCP implementation with a stable tool contract, documented safety model, plugin capability matrix, and adapters for the Jenkins patterns teams actually run in production.
