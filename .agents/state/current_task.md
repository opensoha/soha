# Current Task

Use this file as the canonical task snapshot for any new Codex thread or agent.

Rules:
- Replace placeholders with a concrete task before spawning execution subagents.
- Do not append long chat transcripts.
- The main orchestrator may update this file from a natural-language user request.
- Subagents must treat this file as the source of truth for task scope and next step.

- Task ID: `TASK-20260416-frontend-white-screen-pod-metrics-visuals`
- Task Type: `mixed task`
- Title: `Fix the frontend white-screen regression and upgrade Pod metrics chart composition`
- Status: `done`
- Owner: `main thread`
- Last Updated: `2026-04-16 20:33 CST`
- Source Handoff: `new user request`

## Goal

Resolve two scoped platform-console issues without widening the task:

- restore the web frontend so opening the console no longer results in a white screen, using the smallest fix in the current startup/render chain
- improve the Pod detail metrics tab so CPU and memory charts show request/limit baselines, disk read/write are combined into one chart, network in/out are shown together, and the ECharts palette is visually clearer

## Scope

### In Scope

- Investigate only the frontend boot/render path most likely to cause the reported white screen, centered on the current startup chain (`main.tsx`, route/bootstrap, layout/auth bootstrap, branding bootstrap, and directly related runtime helpers).
- Implement the smallest fix that restores renderability under the current local frontend boot path.
- Keep Pod metrics on the existing ECharts stack (`echarts-for-react`), not a chart-library swap.
- Upgrade compact Pod metrics rendering in `ResourceMetricsPanel` so related series can be grouped in a single chart where that improves readability.
- Use the already available Prometheus pod series for `network_rx`, `network_tx`, `disk_read`, and `disk_write`.
- If request/limit baselines are not present in current frontend payloads, add only the minimal backend/domain/type exposure required to draw CPU and memory request/limit reference lines for the Pod detail page.
- Preserve the existing Pod metrics range selector, empty state, and error messaging behavior.

### Out Of Scope

- Broad refactors of routing, auth, branding, menu, or settings systems beyond the white-screen fix.
- Replacing ECharts or redesigning metrics pages for Deployments, Services, or other resources.
- Broad monitoring-contract redesigns unrelated to Pod request/limit baseline exposure.
- Unrelated cleanup in already dirty frontend/backend files.

## Acceptance Criteria

- The frontend no longer renders as a blank white page on open under the current local boot path.
- `web` typecheck and production build succeed after the fix.
- The Pod metrics tab still preserves the range selector and current empty/error semantics.
- CPU and memory metrics render usage plus request/limit reference lines when those baselines are available, and degrade cleanly when they are not.
- Disk read/write render in a single chart.
- Network receive/transmit render in a single chart.
- Chart colors and line styles are clearly differentiated instead of looking visually flat or hard to distinguish.

## Current Known Facts

- `web` currently passes `npm --prefix web run typecheck`.
- `web` currently passes `npm --prefix web run build`, so the white-screen report is more likely a runtime/bootstrap regression than a compile-time break.
- Pod metrics charts currently use `echarts-for-react`.
- The backend already emits Pod metric series for `cpu`, `memory`, `network_rx`, `network_tx`, `disk_read`, `disk_write`, and `connections`.
- The current backend/frontend Pod detail payload does not expose Pod-level aggregated CPU/memory request/limit values, so drawing those reference lines may require a minimal backend/domain/type extension.

## Related Files

- `AGENTS.md`
- `.codex/handoffs/README.md`
- `.codex/state/current_task.md`
- `.codex/state/queue.md`
- `.codex/prompts/coder.md`
- `.codex/prompts/tester.md`
- `.codex/prompts/reviewer.md`
- `web/src/main.tsx`
- `web/src/routes/index.tsx`
- `web/src/layouts/app-layout.tsx`
- `web/src/features/auth/auth-guard.tsx`
- `web/src/features/auth/permission-snapshot.ts`
- `web/src/features/settings/use-branding-settings.ts`
- `web/src/utils/branding.ts`
- `web/src/components/resource-metrics-panel.tsx`
- `web/src/features/platform/workloads-pages.tsx`
- `web/src/types/index.ts`
- `internal/application/resource/metrics.go`
- `internal/application/resource/service.go`
- `internal/domain/resource/models.go`

## Test Checklist

- [ ] `npm --prefix web run typecheck`
- [ ] `npm --prefix web run build`
- [ ] Targeted runtime verification shows the frontend no longer blanks on open.
- [ ] Pod metrics compact view renders grouped disk/network charts and more distinct chart styling.
- [ ] Pod metrics CPU/memory charts render request/limit reference lines when available and remain stable when baseline values are absent.
- [ ] If backend/domain payloads change, run targeted Go validation for the touched resource package.

## Risks

- The white-screen report may be environment-specific or runtime-only; if local reproduction is partial, the fix may need to rely on the most likely startup failure path observed in code.
- `web/src/main.tsx`, `web/src/layouts/app-layout.tsx`, `web/src/routes/index.tsx`, `web/src/features/auth/permission-snapshot.ts`, `web/src/components/resource-metrics-panel.tsx`, and `web/src/features/platform/workloads-pages.tsx` already have worktree changes; follow-up edits must avoid reverting unrelated content.
- `ResourceMetricsPanel` is shared, so Pod-oriented visual grouping must not silently degrade non-Pod metrics consumers.
- If Pod baseline values are exposed from the backend, domain models, frontend types, and the detail page must stay aligned.

## Current Outcome

- Coder completed the primary white-screen and Pod metrics implementation round.
- Tester re-ran `npm --prefix web run typecheck`, `npm --prefix web run build`, and `go test ./internal/application/resource`; those targeted checks passed.
- A scoped follow-up in `web/src/components/resource-metrics-panel.tsx` restored Grafana access in compact Pod mode and aligned grouped series by timestamp instead of raw index.
- Follow-up tester validation passed and follow-up reviewer found no material remaining issues.

## Current Recommended Next Step

Optional manual browser smoke check: open the frontend from a clean session, then open a Pod detail metrics tab to visually confirm the white-screen symptom is gone and the grouped charts behave as expected with real data.
