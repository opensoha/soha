# AI Copilot

## Goal

kubecrux should support two AI interaction surfaces:

1. an inline AI assistant inside the platform console
2. a standalone chat workspace for deeper cluster and business link analysis

The AI layer should help with:

- cluster diagnostics
- workload anomaly explanation
- event and audit correlation
- release risk review
- business link analysis across platform signals

## Current Implemented Surface

The repository now includes a read-only standalone chat workspace baseline.

- frontend route:
  - `/chat`
- backend APIs:
  - `GET /api/v1/copilot/sessions`
  - `POST /api/v1/copilot/sessions`
  - `GET /api/v1/copilot/sessions/:sessionID/messages`
  - `POST /api/v1/copilot/sessions/:sessionID/messages`
- persistence:
  - `ai_sessions`
  - `ai_messages`

Current reply generation is platform-native and read-only. It uses live data already persisted in kubecrux rather than calling an external model provider directly from the browser.

Current context sources:

- cluster summaries
- alert summary
- events
- audit logs
- applications
- build records

## Product Surfaces

### Inline Assistant

- not implemented in the current refactored frontend
- reserved for future page-scoped assistance

### Standalone Chat

- full-height page with session history on the left and chat area on the right
- supports saved conversations through `ai_sessions` and `ai_messages`
- can stitch together cluster, audit, event, alert, application, and build context

## Recommended Modules

### Backend

- `internal/application/copilot`
  - prompt assembly
  - context collection
  - tool calling policy
- `internal/infrastructure/ai`
  - model gateway
  - token and provider config
- `internal/infrastructure/mcp`
  - MCP adapters for tool context
- `internal/repository/copilot`
  - saved chats, prompt presets, feedback

### Frontend

- `web/src/features/copilot/chat-page.tsx`
  - standalone chat workspace
- future:
  - inline assistant surfaces
  - visible context panel
  - guarded action proposal UI

## Safety Model

The AI layer should start read-only.

Read context may include:

- cluster summaries
- workload views
- events
- audit logs
- alerts
- build and release records

Write or execution capabilities should require:

- explicit action intent
- RBAC + ABAC pass
- audit record
- optional approval gate for risky operations

## Data Flow Direction

1. frontend sends user message plus visible platform context
2. copilot service expands context from platform APIs and repositories
3. MCP adapters provide external tools when needed
4. model response returns explanation, recommended actions, or tool invocation proposals
5. platform records the conversation and any executed actions

## Next Step Reserve

The next increment should add:

- external model gateway configuration
- MCP-backed tool expansion
- inline assistant in cluster and workload pages
- guarded action proposals with approval checks
