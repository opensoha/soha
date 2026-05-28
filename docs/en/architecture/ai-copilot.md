# AI Copilot

## Goal

soha now treats AI as a first-class workbench inside the platform shell.

The active target has two layers:

1. one workbench entry at `/ai-workbench`
2. a set of workbench child surfaces for investigation, automation, and tools

The AI layer should help with:

- alert-driven root-cause analysis
- performance fluctuation and capacity anomaly analysis
- trace hotspot and error-path analysis
- evidence aggregation across logs, events, audit, build, and release data
- inspection-to-investigation closure
- session-level assembly of tools, skills, and data sources

## Current Implemented Surface

The repository now includes a real AI workbench baseline.

- frontend routes:
  - `/ai-workbench`
  - `/ai-workbench/chat`
  - `/ai-workbench/root-cause`
  - `/ai-workbench/performance`
  - `/ai-workbench/inspection`
  - `/ai-workbench/tool-settings`
  - `/ai-workbench/model-settings`
- backend APIs:
  - `GET /api/v1/copilot/sessions`
  - `GET /api/v1/copilot/sessions/:sessionID`
  - `POST /api/v1/copilot/sessions`
  - `PATCH /api/v1/copilot/sessions/:sessionID`
  - `DELETE /api/v1/copilot/sessions/:sessionID`
  - `GET /api/v1/copilot/sessions/:sessionID/messages`
  - `POST /api/v1/copilot/sessions/:sessionID/messages`
- persistence:
  - `ai_sessions`
  - `ai_messages`

Legacy compatibility redirects still exist for:

- `/ai-observe`
- `/ai-observe/workbench`
- `/ai-observe/operations`
- `/ai-observe/tools`
- `/chat`
- `/ai-workbench/investigation`
- `/ai-workbench/automation`
- `/ai-workbench/tools`

Current reply generation remains platform-native and read-oriented. It uses live data already persisted in soha rather than calling an external model provider directly from the browser.

## Session Model

AI investigation is session-first.

Persistent base tables remain:

- `ai_sessions`
- `ai_messages`

`ai_sessions.metadata` now carries workbench metadata:

- `mode`
- `status`
- `scope`
- `pinnedContext`
- `toolset`
- `agentProviderId`
- `analysisRunRefs`
- `summary`
- `tags`
- `archivedAt`

Current session modes:

- `general`
- `root_cause`
- `performance`
- `trace`
- `inspection_review`

The `scope` object is also the standard monitoring-to-AI handoff contract:

- `alertId`
- `clusterId`
- `namespace`
- `workload`
- `timeRangeMinutes`

The `toolset` object is now honored by backend analysis execution:

- `enabledAdapterIds` limits the MCP adapters available to the session
- `disabledToolNames` blocks either a tool name or an `adapter.tool` qualified name
- `budgetOverrides.maxEvidenceItems` limits analysis evidence volume
- `budgetOverrides.timeoutSeconds` bounds a single analysis tool run
- `scopeOverrides` is merged into session scope before root-cause, performance, or trace analysis runs

Session tool assembly reads `/api/v1/copilot/workbench/catalog`, a safe catalog endpoint for `observe.ai.chat`, `observe.ai.view`, or `settings.ai.view` users. It returns adapter metadata, data-source readiness summaries, analysis-profile summaries, skill summaries, agent providers, capabilities, tool bindings, and skill bindings; full AI provider settings and secrets stay behind `settings.ai.view`.

When a scoped handoff is opened, the workbench can create a fresh investigation session for that scope instead of silently reusing an unrelated active session.

## Data Sources, Skills, And MCP

Current AIOps tool capability continues to be exposed through MCP adapters.

Registered adapters now include:

- `platform-native.v1`
- `logs.v1`
- `metrics.v1`
- `traces.v1`
- `delivery.v1`

Control-plane entry remains dual:

1. Settings > AI
   - global provider, data source, analysis profile, automation policy, and skill-definition configuration
2. `/ai-workbench/tools`
   - compatibility entry that redirects to `/ai-workbench/tool-settings`
3. `/ai-workbench/tool-settings`
   - session-level temporary toolset and skill assembly

The global skill registry now uses enterprise skill definitions, not just lightweight labels:

- `id`
- `name`
- `category`
- `ownerModule`
- `capabilityRefs`
- `blueprintRefs`
- `inputSchema`
- `outputSchema`
- `scopeRules`
- `enabled`

## Agent Runtime

soha now models AI execution through Agent Runtime instead of binding pages or analysis flows directly to Hermes.

Core objects:

- `AgentProvider`: executor catalog; current providers are `internal` and `hermes`
- `AgentCapability`: soha platform capabilities including `root_cause`, `performance`, `trace`, `inspection_review`, `delivery_failure`, `post_deploy_observation`, `platform_resource_diagnosis`, `docker_diagnosis`, `virtualization_diagnosis`, and `oncall_brief`
- `AgentToolBinding`: capability-to-MCP-adapter, platform read-tool, or provider-native-tool mapping
- `AgentSkillBinding`: soha skill mapping to Hermes skills, prompt templates, or future provider skill systems
- `AgentRun`: durable asynchronous execution record for external providers, carrying status, scope, toolset, skills, callback token, tool executions, and output artifacts

Hermes Agent is the first external provider. The AI workbench and automation policy select only `agentProviderId` and capability; the actual Hermes call is performed by an independent agent runner through claim/callback. Future OpenClaw, internal-agent, or other providers should extend provider adapters and runner executors without rewriting session pages, automation policy, or business analysis flows.

Agent Runtime output is written back as soha `AnalysisArtifact`, reusing evidence, hypotheses, recommendations, graph, tool-call records, and data-source snapshots. Permissions, menus, budgets, data redaction, audit, and high-risk operation boundaries remain owned by soha.

## Product Surfaces

### `/ai-workbench`

- recent investigations
- recent analysis runs
- risk radar
- quick entry into investigation, automation, and tools

### `/ai-workbench/chat`

- full-height investigation workspace with sessions on the left
- message flow in the center
- evidence, hypotheses, recommendations, and tool-chain details on the right
- can stitch together cluster, audit, event, alert, application, and build context
- when scope includes `alertId`, the user can jump back to the original monitoring alert detail
- supports `mode=trace` and `mode=inspection_review` in the same conversation canvas
- the session-level toolset drawer now shows the effective execution policy and edits adapter selection, `adapter.tool` disables, budget overrides, and scope overrides
- explicit analysis opens a confirmation modal before execution, so the user can choose provider, analysis profile, runnable analysis mode, edit the analysis target, and preview the session scope and toolset source; current runnable modes are `root_cause`, `performance`, `trace`, and `inspection_review`, the backend rejects `general`, and successful runs write the selected mode and `agentProviderId` back to session metadata
- selecting the `internal` provider uses soha in-process analysis; selecting `hermes` or another external provider creates an `AgentRun` and lets the runner write artifacts back asynchronously
- all `analysisArtifacts` carried by assistant messages are collected into an artifact history, so users can switch graph, evidence, and recommendation context across root-cause, performance, trace, and inspection-review artifacts

### `/ai-workbench/root-cause` And `/ai-workbench/performance`

Root-cause and performance entries reuse the same session canvas, but the path now carries the primary IA instead of relying on the legacy `investigation?mode=*` handoff.

### `/ai-workbench/inspection`

- inspection and automation landing surface
- session-to-inspection and inspection-to-session loop
- create and edit inspection tasks directly inside the AI workbench
- scope-aware task form for platform, cluster, and namespace inspections
- inspection tasks can select a `mode=inspection` analysis profile, persisted as task metadata `analysisProfileId`, so backend execution can apply profile-driven playbooks and source constraints
- inspection tasks can now be deleted from the workbench; the backend gates the action with `observe.ai.inspection.manage`, keeps creator ownership checks, and relies on database cascade cleanup for related inspection runs
- task create/update, immediate execution, and run-to-session handoff stay in one workspace and remain gated by `observe.ai.inspection.manage`, `observe.ai.inspection.run`, and `observe.ai.chat`; run-to-session handoff also requires `observe.ai.view`, and session-to-inspection task generation also requires `observe.ai.chat`
- session-to-inspection task generation prefers the current available inspection profile, so the profile-backed inspection contract is not lost during handoff
- create and edit automation policies from the policy tab, including trigger type, analysis kinds, analysis profile, remediation policy, dedup window, cooldown, and enabled state
- automation policy read, create, edit, and delete operations continue to use the backend `/api/v1/copilot/automation-policies` contract and remain gated by `settings.ai.manage`; without that permission, the workbench must not proactively fetch the policy list and should show the permission boundary instead
- automation policy forms select `analysisProfileId` from safe workbench catalog profile summaries and `agentProviderId` from the agent provider catalog; the current runner only supports the `alert_webhook` trigger path, so trigger types without a runner must not be exposed as runnable policy options
- runnable automation analysis kinds include `root_cause`, `performance`, `trace`, and `inspection_review`; external providers can extend the same Agent Runtime into delivery failure analysis, post-deploy observation, platform resource diagnosis, Docker/virtualization diagnosis, and on-call briefing capabilities
- severity, status, minimum duration, label matching, analysis time range, and approval roles are persisted as structured `triggerConditions` / `approvalPolicy` instead of creating inert empty policies
- automation execution persists built-in root-cause, performance, and trace runs with a policy-prefixed `dedupKey`; external providers write the same dedup context into `AgentRun.input`. `dedupWindowSeconds` gates the same alert fingerprint, while `cooldownSeconds > 0` gates the whole policy across different alerts

Opening an inspection run as a session no longer creates an empty investigation:

- the backend copies the inspection task `clusterId` / `namespace` into session scope
- session `pinnedContext` keeps the `inspectionRunId`, `inspectionTaskId`, severity, and status
- the first assistant message carries an `inspection_review` analysis artifact
- that artifact converts findings into evidence, recommendations, and a left-to-right graph for the evidence panel

### `/ai-workbench/tool-settings`

- MCP adapters
- mirrored data-source inventory
- session-level toolset assembly
- global skill registry visibility
- Agent Provider / Capability / Tool Binding / Skill Binding safe catalog

Toolset editing must stay aligned with backend execution contracts:

- disabled tools are saved as `adapter.tool` names to avoid accidentally blocking same-named tools on other adapters
- an empty adapter list means automatic selection; the recommended preset prefers registered adapters backed by enabled data sources
- budget overrides only persist positive numbers, so `0` is not misread as an intentional limit
- scope overrides are structured as `clusterId`, `namespace`, `workload`, `service`, `alertId`, and `timeRangeMinutes`

### `/ai-workbench/model-settings`

- embedded global AI settings governed by `settings.ai.view` and `settings.ai.manage`

## Safety Model

The AI layer should stay analysis-first.

Current focus:

- aggregate context
- call read-oriented tools
- generate evidence, hypotheses, and recommendations
- persist tool calls and analysis artifacts into sessions
- convert external agent output into soha `AnalysisArtifact`

Application onboarding specification rendering and delivery bootstrap do not belong to the AI workbench itself. Those belong to the Delivery Workbench. AI only exposes discoverable MCP and skill capabilities that enterprise AI coding clients and Agent Runtime can call.

## Data Flow Direction

1. the frontend sends user input plus visible platform context
2. the copilot service expands context from platform APIs and repositories
3. MCP adapters provide external tools when needed
4. the model response returns explanation, recommendations, or tool-call proposals
5. the platform records the conversation and any executed actions

## Near-Term Expectations

After this phase, AI work should default to these rules:

- new AI-facing capabilities should land in the AI workbench instead of growing separate legacy pages
- session enhancements should prefer extending `metadata` before introducing new investigation entity models
- new data sources and tools should consider:
  - global configuration
  - session-level assembly
  - artifact persistence
- root cause, performance, trace, inspection review, and external agent analysis should keep converging on one artifact model
- adding an agent provider should extend provider adapters, tool bindings, skill bindings, and runner executors only; pages and business analysis flows must not depend directly on provider SDKs or CLIs
- monitoring-to-AI handoff should keep using the standard scope contract rather than page-specific ad hoc query params
