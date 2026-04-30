# Task Queue

Use this queue for bounded subtasks that can be handed to isolated threads or agents. Keep entries short, ordered by priority, and update status instead of duplicating rows.

Status values:

- `todo`
- `in_progress`
- `blocked`
- `ready_for_test`
- `ready_for_review`
- `done`

Owner role values:

- `main`
- `coder`
- `tester`
- `reviewer`
- `human`

Priority values:

- `P0`
- `P1`
- `P2`
- `P3`

| Task ID | Summary | Owner Role | Priority | Status | Depends On | Inputs | Expected Output |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `TASK-20260416-node-pod-runtime-fixes` | `Implement node detail timeout protection, pod metrics fallback, health semantics fix, and pod CPU/memory resource rendering` | `coder` | `P1` | `done` | `none` | `current_task + main-to-coder handoff + exact related files` | `.codex/state/results/TASK-20260416-node-pod-runtime-fixes--coder.md` |
| `TASK-20260416-node-pod-runtime-fixes-T1` | `Run only targeted validation for node detail, pod metrics fallback, and pods table rendering` | `tester` | `P1` | `done` | `TASK-20260416-node-pod-runtime-fixes` | `current_task + coder result + changed files` | `.codex/state/results/TASK-20260416-node-pod-runtime-fixes--tester.md` |
| `TASK-20260416-node-pod-runtime-fixes-R1` | `Review regression risk for Prometheus scoping, payload changes, and frontend semantics` | `reviewer` | `P2` | `done` | `TASK-20260416-node-pod-runtime-fixes` | `current_task + coder/tester results + changed files` | `.codex/state/results/TASK-20260416-node-pod-runtime-fixes--reviewer.md` |
| `TASK-20260416-node-pod-runtime-fixes-F1` | `Harden Prometheus pod-metrics fallback so missing cluster matcher does not accept ambiguous cross-cluster series` | `main` | `P2` | `blocked` | `TASK-20260416-node-pod-runtime-fixes-R1` | `current_task + reviewer result + internal/application/resource/metrics.go` | `scoped follow-up task snapshot + coder/tester/reviewer results + closure decision` |
| `TASK-20260416-node-pod-runtime-fixes-F1-T1` | `Run only targeted validation for the fallback-safety fix in internal/application/resource/metrics.go` | `tester` | `P2` | `done` | `TASK-20260416-node-pod-runtime-fixes-F1` | `current_task + follow-up coder result + internal/application/resource/metrics.go + internal/application/resource/metrics_test.go` | `.codex/state/results/TASK-20260416-node-pod-runtime-fixes-F1--tester.md` |
| `TASK-20260416-node-pod-runtime-fixes-F1-R1` | `Review only fallback correctness and ambiguity risk for the scoped metrics follow-up` | `reviewer` | `P2` | `done` | `TASK-20260416-node-pod-runtime-fixes-F1` | `current_task + follow-up coder/tester results + internal/application/resource/metrics.go` | `.codex/state/results/TASK-20260416-node-pod-runtime-fixes-F1--reviewer.md` |
| `TASK-20260416-node-pod-runtime-fixes-F1-C2` | `Revise the fallback guard so unscoped fallback is rejected when cluster-safe uniqueness cannot be proven` | `coder` | `P1` | `todo` | `TASK-20260416-node-pod-runtime-fixes-F1-R1` | `current_task + follow-up reviewer result + internal/application/resource/metrics.go + internal/application/resource/metrics_test.go` | `.codex/state/results/TASK-20260416-node-pod-runtime-fixes-F1--coder.md` |
| `TASK-20260416-platform-ui-menu-polish` | `Implement compact Pod metrics tab, menu-management submenu visibility/sort support, and icon-style table actions` | `coder` | `P1` | `done` | `none` | `current_task + main-to-coder handoff + exact related files` | `.codex/state/results/TASK-20260416-platform-ui-menu-polish--coder.md` |
| `TASK-20260416-platform-ui-menu-polish-T1` | `Run only targeted validation for Pod metrics compact mode, menu tree management, and icon-style table actions` | `tester` | `P1` | `done` | `TASK-20260416-platform-ui-menu-polish` | `current_task + coder result + changed files` | `.codex/state/results/TASK-20260416-platform-ui-menu-polish--tester.md` |
| `TASK-20260416-platform-ui-menu-polish-R1` | `Review scoped regression risk for compact Pod metrics, menu hierarchy management, and action-column rendering` | `reviewer` | `P1` | `done` | `TASK-20260416-platform-ui-menu-polish` | `current_task + coder/tester results + changed files` | `.codex/state/results/TASK-20260416-platform-ui-menu-polish--reviewer.md` |
| `TASK-20260416-platform-ui-menu-polish-C2` | `Suppress compact-mode Grafana CTA and normalize blank menu parentId before submit` | `coder` | `P2` | `done` | `TASK-20260416-platform-ui-menu-polish-R1` | `current_task + reviewer result + web/src/components/resource-metrics-panel.tsx + web/src/features/system/system-pages.tsx` | `.codex/state/results/TASK-20260416-platform-ui-menu-polish--coder.md` |
| `TASK-20260416-menu-hierarchy-pod-metrics-grid` | `Fix menu-management hierarchy drift and replace Pod metrics tabs with an adaptive chart grid` | `coder` | `P1` | `in_progress` | `none` | `current_task + main-to-coder handoff + exact related files` | `.codex/state/results/TASK-20260416-menu-hierarchy-pod-metrics-grid--coder.md` |
| `TASK-20260416-menu-hierarchy-pod-metrics-grid-T1` | `Run only targeted validation for menu hierarchy rendering and Pod metrics chart-grid layout` | `tester` | `P1` | `done` | `TASK-20260416-menu-hierarchy-pod-metrics-grid` | `current_task + coder result + changed files` | `.codex/state/results/TASK-20260416-menu-hierarchy-pod-metrics-grid--tester.md` |
| `TASK-20260416-menu-hierarchy-pod-metrics-grid-R1` | `Review scoped regression risk for menu hierarchy rendering and Pod metrics chart-grid layout` | `reviewer` | `P1` | `done` | `TASK-20260416-menu-hierarchy-pod-metrics-grid` | `current_task + coder/tester results + changed files` | `.codex/state/results/TASK-20260416-menu-hierarchy-pod-metrics-grid--reviewer.md` |
| `TASK-20260416-menu-hierarchy-pod-metrics-grid-C2` | `Stop paginating the flattened menu hierarchy so parent and child rows cannot be split across pages` | `coder` | `P1` | `todo` | `TASK-20260416-menu-hierarchy-pod-metrics-grid-R1` | `current_task + reviewer result + web/src/features/system/system-pages.tsx` | `.codex/state/results/TASK-20260416-menu-hierarchy-pod-metrics-grid--coder.md` |
| `TASK-20260416-frontend-white-screen-pod-metrics-visuals` | `Fix frontend white-screen regression and upgrade Pod metrics chart composition` | `main` | `P0` | `done` | `none` | `current_task + minimal handoffs + exact related files` | `scoped coder/tester/reviewer result files + closure decision` |
| `TASK-20260416-frontend-white-screen-pod-metrics-visuals-C1` | `Locate the smallest white-screen root cause in the frontend startup chain and implement the Pod metrics chart/baseline update with minimal backend exposure` | `coder` | `P0` | `done` | `TASK-20260416-frontend-white-screen-pod-metrics-visuals` | `current_task + main-to-coder handoff + exact related files` | `.codex/state/results/TASK-20260416-frontend-white-screen-pod-metrics-visuals--coder.md` |
| `TASK-20260416-frontend-white-screen-pod-metrics-visuals-T1` | `Run only targeted frontend/backend validation for the white-screen fix and Pod metrics visual changes` | `tester` | `P0` | `done` | `TASK-20260416-frontend-white-screen-pod-metrics-visuals-C1` | `current_task + coder result + changed files + main-to-tester handoff` | `.codex/state/results/TASK-20260416-frontend-white-screen-pod-metrics-visuals--tester.md` |
| `TASK-20260416-frontend-white-screen-pod-metrics-visuals-R1` | `Review regression risk for startup-chain changes and Pod metrics chart grouping/baseline handling` | `reviewer` | `P1` | `done` | `TASK-20260416-frontend-white-screen-pod-metrics-visuals-C1` | `current_task + coder result + optional tester result + main-to-reviewer handoff` | `.codex/state/results/TASK-20260416-frontend-white-screen-pod-metrics-visuals--reviewer.md` |
| `TASK-20260416-frontend-white-screen-pod-metrics-visuals-C2` | `Restore compact-mode Grafana access and align grouped Pod metric series by timestamp` | `coder` | `P1` | `done` | `TASK-20260416-frontend-white-screen-pod-metrics-visuals-R1` | `current_task + reviewer result + web/src/components/resource-metrics-panel.tsx` | `.codex/state/results/TASK-20260416-frontend-white-screen-pod-metrics-visuals--coder.md` |
| `TASK-20260416-frontend-white-screen-pod-metrics-visuals-T2` | `Run only targeted validation for the compact metrics follow-up` | `tester` | `P1` | `done` | `TASK-20260416-frontend-white-screen-pod-metrics-visuals-C2` | `current_task + follow-up coder result + web/src/components/resource-metrics-panel.tsx` | `.codex/state/results/TASK-20260416-frontend-white-screen-pod-metrics-visuals--tester.md` |
| `TASK-20260416-frontend-white-screen-pod-metrics-visuals-R2` | `Review only the compact metrics follow-up for regressions or residual risk` | `reviewer` | `P1` | `done` | `TASK-20260416-frontend-white-screen-pod-metrics-visuals-C2` | `current_task + follow-up coder/tester result + web/src/components/resource-metrics-panel.tsx` | `.codex/state/results/TASK-20260416-frontend-white-screen-pod-metrics-visuals--reviewer.md` |

## Notes

- This task is suitable for split execution because coder writes code, while tester and reviewer can run after the coder result is available without sharing full parent-thread chat history.
- Keep the write scope centered on the listed frontend resource pages and backend resource service files.
- If Prometheus fallback requires broader monitoring-contract changes, stop and narrow the task again before widening scope.
- Recovery sync completed on `2026-04-16 11:09 CST`; coder/tester/reviewer artifacts were standardized from disk and no recoverable live agent id was found in repo-local state.
- No subagent re-run is required unless the main thread decides the reviewer `P2` findings need a new scoped fix.
- Main-thread triage completed on `2026-04-16 11:23 CST`.
- Decision:
  - `internal/application/resource/metrics.go` ambiguity risk is a required follow-up and is queued as `TASK-20260416-node-pod-runtime-fixes-F1`.
  - `internal/application/resource/nodes.go` 1.2s timeout risk is accepted now as an optional follow-up because the original hang-prevention requirement is already satisfied.
- Follow-up `TASK-20260416-node-pod-runtime-fixes-F1` was activated on `2026-04-16 11:26 CST` and narrowed to the fallback-safety path in `internal/application/resource/metrics.go` plus its targeted tests.
- First F1 implementation/test/review round completed on `2026-04-16 11:37 CST`.
- Outcome:
  - coder produced a scoped fallback guard and tests
  - tester passed the targeted test set
  - reviewer found a remaining `P1` correctness gap in the uniqueness proof
  - next smallest action is `TASK-20260416-node-pod-runtime-fixes-F1-C2`
- New mixed-task intake completed on `2026-04-16 17:05 CST` for Pod metrics UI simplification, menu-management submenu ordering, and icon-style table actions.
- First implementation/test/review round for `TASK-20260416-platform-ui-menu-polish` completed on `2026-04-16 17:35 CST`.
- Outcome:
  - coder delivered the scoped multi-file patch
  - tester passed targeted validation
  - reviewer found two remaining `P2` polish gaps
  - next smallest action is `TASK-20260416-platform-ui-menu-polish-C2`
- Second implementation/test/review round for `TASK-20260416-platform-ui-menu-polish` completed on `2026-04-16 17:43 CST`.
- Outcome:
  - coder fixed the two scoped `P2` findings in `resource-metrics-panel.tsx` and `system-pages.tsx`
  - tester re-verified the follow-up fixes successfully
  - reviewer found no new material issues
  - the task is now complete
- New mixed-task intake completed on `2026-04-16 17:53 CST` for menu-management hierarchy repair and Pod metrics chart-grid rendering.
- First implementation/test/review round for `TASK-20260416-menu-hierarchy-pod-metrics-grid` completed on `2026-04-16 18:02 CST`.
- Outcome:
  - coder fixed the Pod metrics grid gap and confirmed menu hierarchy/submenu seed coverage already existed in the reviewed worktree
  - tester partially validated the scope, with Go bootstrap validation blocked by sandbox/module access
  - reviewer found one remaining `P1` menu-management issue: pagination still splits parent/child rows
  - next smallest action is `TASK-20260416-menu-hierarchy-pod-metrics-grid-C2`
- New mixed-task intake completed on `2026-04-16 20:33 CST` for frontend white-screen repair and Pod metrics visual composition updates.
- This task is scoped for staged delegation:
  - coder first, because the white-screen root cause and exact write set must be identified before test/review can run with minimal context
  - tester and reviewer then consume the coder result without full chat history
- First implementation/test/review round for `TASK-20260416-frontend-white-screen-pod-metrics-visuals` completed on `2026-04-16 20:33 CST`.
- Outcome:
  - coder delivered the startup hardening, Pod request/limit exposure, and grouped Pod charts
  - tester passed targeted frontend build/typecheck and `go test ./internal/application/resource`
  - reviewer found two remaining `P2` issues in compact metrics rendering
  - next smallest action is `TASK-20260416-frontend-white-screen-pod-metrics-visuals-C2`
- Follow-up implementation/test/review round for `TASK-20260416-frontend-white-screen-pod-metrics-visuals` completed on `2026-04-16 20:33 CST`.
- Outcome:
  - coder fixed the compact Grafana regression and timestamp alignment for grouped charts
  - follow-up tester passed targeted frontend validation
  - follow-up reviewer found no material remaining issues
  - the task is now complete
