# AI Copilot

## Goal

kubecrux now treats AI as a first-class workbench inside the platform shell.

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
  - `/ai-workbench/investigation`
  - `/ai-workbench/automation`
  - `/ai-workbench/tools`
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

Current reply generation remains platform-native and read-oriented. It uses live data already persisted in kubecrux rather than calling an external model provider directly from the browser.

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

## Product Surfaces

### `/ai-workbench`

- recent investigations
- recent analysis runs
- risk radar
- quick entry into investigation, automation, and tools

### `/ai-workbench/investigation`

- full-height investigation workspace with sessions on the left
- message flow in the center
- evidence, hypotheses, recommendations, and tool-chain details on the right
- can stitch together cluster, audit, event, alert, application, and build context
- when scope includes `alertId`, the user can jump back to the original monitoring alert detail

### `/ai-workbench/automation`

- inspection and automation landing surface
- session-to-inspection and inspection-to-session loop

### `/ai-workbench/tools`

- MCP adapters
- mirrored data-source inventory
- session-level toolset assembly
- global skill registry visibility

## Safety Model

The AI layer should stay analysis-first.

Current focus:

- aggregate context
- call read-oriented tools
- generate evidence, hypotheses, and recommendations
- persist tool calls and analysis artifacts into sessions

Application onboarding specification rendering and delivery bootstrap do not belong to the AI workbench itself. Those belong to the Delivery Workbench. AI only exposes discoverable MCP and skill capabilities that enterprise AI coding clients can call.

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
- root cause, performance, and trace analysis should keep converging on one artifact model
- monitoring-to-AI handoff should keep using the standard scope contract rather than page-specific ad hoc query params
