---
name: soha-frontend
description: Implement or review soha-web React and Ant Design UI, shared tables and tags, routing, data ownership, themes, and loading boundaries. Public API contracts belong to soha-contracts.
---

# Soha Frontend

## Workflow

1. Read the user task and actual affected implementation, callers, tests, styles and worktree
   state. Classify the work: maintenance (local fixes, copy or fields) stays scoped; explicitly
   authorized product redesign may reorganize the shell, layout, hierarchy and composition.
2. For UI organization and design review, load [soha-design](../soha-design/SKILL.md).
   Read [theme-system.md](references/theme-system.md) for styling/token ownership and
   [architecture-development.md](references/architecture-development.md) for route/data/state/CSS boundaries.
3. Preserve the task's API, permissions, state and engine lifecycle when implementing the design.
   For state changes, operation feedback or relationship exploration, apply
   [interaction semantics](../soha-design/references/interaction-feedback.md) at the actual
   API/Query owner and verify page integration, not only the shared control.
4. Reuse or adjust mature controls for that composition. Maintenance must not grow into an
   unsolicited redesign; authorized redesign must not shrink into color/radius/spacing changes
   because an old management template exists. Follow the `antd` skill for APIs: check the installed
   package version and query the CLI before writing component code. Do not pin a patch version here.
5. Take the current scope and sample from the user and the selected plan, not a fixed business
   domain. Foundation work can align rules, token ownership and conflicting CSS without starting
   a demo or page redesign. For an authorized page sample, complete its real task/action/feedback
   chain and review themes, long values, abnormal and interaction states. After user acceptance,
   extract demonstrated reuse and roll out complete business chains, not controls in bulk.
6. Verify behavior and visual outcome as well as applicable engineering gates. Keep actual
   progress and evidence in the selected initiative's single plan; no new planning system.
7. For cross-module questions, use existing `graphify-out/graph.json` when helpful and verify
   important findings against source. Ordinary lookup uses direct search; refresh only for
   requested graph maintenance or relevant structural changes, not styling or documentation.

## Boundaries

- Business code belongs in `src/features/<domain>/<capability>/`; promote shared code only for
  actual reuse, not file hierarchy symmetry.
- Feature manifests and the central registry own routes. Keep `src/routes/index.tsx` limited to
  shells; load heavy editors, charts, graphs, and terminals from their visible leaf.
- `src/services/api-client.ts` owns transport; feature API/query/mutation modules own wire calls,
  keys, caching, and invalidation. Pages must not bypass them or deep-import another feature's
  private implementation. TanStack Query owns server state; Zustand owns persistent UI context.
- Keep one app-level `ConfigProvider` in `src/main.tsx`; `src/theme/app-theme.ts` owns tokens.
  Shared components own reusable conventions. Explicitly scoped product pilots may compose their own layouts and select scene tokens from that same source.
- Import generic reading/request feedback from `components/request-feedback` and cache/write
  helpers from `utils/query-feedback`. Reuse `components/use-operation-mutation` for ordinary
  request fences and receipts; domain adapters retain task receipts and evidence routes.
  Reuse shared error presentation ownership instead of adding a second notification policy.
- Separate page composition, query behavior and table behavior. Reuse `AdminTable` over Ant Design
  `Table`; choose `ManagementDataPage` only when its management-list composition fits the task.
  Use `StatusTag` for states, `MetadataTag` for categories and `BooleanTag` for boolean labels;
  ordinary specifications, addresses and IDs use text. Do not build another universal table API.
- Preserve authorization, mutation confirmations, loading/error/no-permission states, keyboard
  access, and labels alongside color. Never put provider credentials in browser configuration.
- Preserve API/DTO, Query/mutation ownership, business state, path/route ID/menuId/iconKey and
  professional engine behavior. Hyphenated paths are not a visual defect. Keep React/Vite/TypeScript
  and Antd; no dependency migration or engine replacement without separate authorization.
- Navigation may already identify a collection: avoid repeating its large title/copy/icon in
  content, while preserving a semantic accessible heading. A useful resource identity/action
  region remains valid. Follow soha-design for the distinction and real branding.

## Table task contract

- Identify top-level versus embedded use, offset versus cursor navigation, server versus complete
  local sorting/filtering, total-count source, stable row keys and selection scope before editing.
  Reuse query, toolbar and column capabilities without changing those behaviors for visual reasons.
  A current-page count is not a server total; a local sorter must not imply global sorting.
- Select a real reference file and name the relevant behavior or visual region. Existing code is
  not automatically compliant; a rejected composition or historical screenshot is not a baseline.
- Check shared table and column APIs, then verify the actual page integration: permission gates,
  query reset, pagination/sort requests, actions, loading, empty and persistent error feedback.
  Shared theme tests do not establish that a new page is wired correctly.
- `scripts/check-table-boundaries.mjs` inventories production table imports and rejects additions.
  `--base <reviewed-ref>` or `TABLE_BOUNDARY_BASE_REF` identifies the base before HEAD; CI supplies the PR
  base or pre-push commit. Missing history fails instead of silently comparing against HEAD.
- The guard blocks new raw Table imports/re-exports, deep table entries and opaque whole-Antd
  namespace/dynamic imports. Import other controls by name; type-only imports remain valid.
  Only `src/components/admin-table.tsx` is the shared implementation exemption. Historical
  occurrences are counted at the Git baseline, not silently rewritten or blanket-exempted.
- Run `npm test -- scripts/check-table-boundaries.test.mjs` when changing this guard, and include
  negative cases. A necessary new exception needs an explicitly reviewed scope, reason and test;
  do not evade the rule with another wrapper or reset the baseline to make CI green.
- This import guard does not prove CSS, accessibility, pagination or visual compliance. Keep
  targeted component/page tests and actual visual review; preserve current accepted layouts.

K8s full-array resource lists compose `features/platform/shared/kubernetes-resource-table` over
AdminTable. Supply a stable resource key, display label, full unfiltered rows and a filter reset
key; keep resource columns, queries and permission-aware actions in their owner. The composition
owns continuous virtual browsing, truthful counts, layout preferences and refresh anchors.
Server-paginated catalogs, embedded detail tables and other workbenches retain their existing
AdminTable behavior. Do not copy the K8s control/viewport logic into each resource page.

## AI Workbench

- Reuse Ant Design X primitives (`Conversations`, `Welcome`, `Prompts`, `Bubble.List`, `Sender`,
  `ThoughtChain`); separate controller/data ownership from presentation.
- XRequest is for AI streaming/provider requests, not CRUD. Browser provider requests go through
  the same-origin backend proxy.
- Chat, DAG, topology, terminal, noVNC, and graphs may use scene-specific layouts, but still use
  shared tokens and feedback semantics. Layout follows the task rather than a fixed overview template.

## Verification

- The implementer runs the smallest effective self-check: affected tests where behavior needs
  proof, scoped lint/format checks and necessary page inspection. Copy or local styling does not
  automatically require new tests, a full Vitest run, E2E, build or independent audit.
  Use [testing entry points](../../../docs/testing.md) for scoped commands and resource constraints.
  After antd changes, run
  `antd lint <changed-path> --format json` (one path per invocation).
- Verify actual visual changes against soha-design using the themes, viewports, long values,
  zoom, keyboard/IME and error/permission states affected by the change; new or redesigned pages
  cover their applicable state/interaction matrix. Preserve data/console truthfulness.
  Test/build success does not establish visual acceptance; report unavailable checks explicitly.
- Independent tests and review happen at an agreed batch boundary, formal commit/integration
  preparation or explicit request, not after every development subtask or preview. Complex
  behavior needs success/failure/boundary scenarios before or early in implementation; use a
  suitable test-design role when helpful. Parallel developers need disjoint file ownership;
  parallel tests also need isolated runtime resources and stable acceptance inputs.
- `package.json` and `.github/workflows/*.yml` remain the executable gate source. Select lint,
  tests and build by impact and the required stage; a commit does not itself require full regression.
  Route, shared-boundary, dependency, architecture and release changes retain their applicable full
  gates, including coverage, route, boundary and bundle checks. Reuse unchanged evidence and rerun
  only affected checks after fixes unless a specific risk or mandatory gate requires more.
- Documentation-only changes require metadata, link, source-agreement, and diff checks, not
  application builds. Report failing gates separately from completed work; do not suppress type
  errors or change contracts/dependencies to finish an unrelated UI task.
