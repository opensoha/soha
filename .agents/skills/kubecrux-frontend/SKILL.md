---
name: kubecrux-frontend
description: >-
  Implement and refactor the kubecrux console in `web/src/**` with React 18,
  TypeScript 5, Vite, TanStack Query 5, Zustand 5, Tailwind 4, and native Ant
  Design 6. Use when adding or changing routes, feature pages, shared
  components, scoped queries, theme tokens, permission-aware navigation, or
  page-level UX. This skill enforces the active repo baseline: work only in
  `web`, keep `web_pro_backup` and `old_web` reference-only, use native `antd`
  and `@ant-design/icons`, preserve the existing shadcn-like grayscale visual
  language from `web/src/theme/app-theme.ts`, reuse persisted cluster and
  namespace scope, consume backend-aggregated DTOs, keep platform, delivery,
  monitoring, AI, virtualization, and Docker workbench routing aligned with
  backend menus and permission keys, and avoid reintroducing any Semi Design
  residue.
---

# Kubecrux Frontend

## Overview

Implement console work inside the active Vite app under `web`. Keep the UI antd-first, operational, and visually aligned with the repo's neutral shadcn-like style rather than introducing a second component system.

## Workflow

1. Read `web/src/routes/index.tsx`, `web/src/routes/meta.ts`, and the target `web/src/features/**` module before editing.
2. Decide whether the change belongs in an existing route bundle, a shared component, or a store/query helper. Prefer the existing page bundle files until reuse pressure is real.
3. Fetch server data through TanStack Query and same-origin `/api/v1` helpers. Reuse persisted cluster and namespace scope from Zustand instead of creating page-local duplicates.
4. Build UI with native `antd` components plus the existing theme and CSS-variable system. Treat "shadcn style" as a visual direction, not as `shadcn/ui`.
5. For workbench changes, keep route registration, route metadata, backend menu IDs, module visibility, and permission keys in sync before polishing the page UI.
6. Update route metadata, i18n strings, permission wiring, permission catalog, and tests together when navigation or behavior changes.
7. Clean up stale assets or scripts when they no longer match the active UI baseline; do not leave dead Semi-era static files around once usage is gone.
8. Validate with `npm run typecheck` in `web`, plus focused `npm run test` when semantics change.

## Non-Negotiables

- Work in `web`. Do not treat `old_web` or `web_pro_backup` as active implementation targets.
- Import directly from `antd` and `@ant-design/icons`. Do not introduce Semi Design packages, compat layers, variables, or naming back into `web`.
- Keep `web/src/theme/app-theme.ts` as the single source for antd theme tokens and shared `--kc-*` CSS variables.
- Do not reintroduce removed legacy theme assets, old static sync scripts, historical token aliases, or outdated file naming unless the user explicitly asks for legacy compatibility.
- Route registration lives in `web/src/routes/index.tsx`. Navigation ownership, breadcrumbs, and permission-aware metadata live in `web/src/routes/meta.ts`.
- Server state belongs to TanStack Query. Persisted UI and runtime preferences belong to Zustand. Avoid storing fetched API payloads in local stores.
- Prefer backend aggregation over browser fan-out. Do not issue one request per namespace when a platform aggregate endpoint exists or should exist.
- Frontend visibility is not authorization. Button hiding and disabled states must match backend permission keys, but backend APIs remain the source of enforcement.
- Do not model disabled modules as missing permissions. Module availability, menu visibility, route visibility, and backend authorization are separate gates.

## UI Direction

- Use Ant Design components with a shadcn-like grayscale treatment: neutral surfaces, restrained accents, crisp borders, compact square-edged controls, and quiet shadows.
- Do not import `shadcn/ui` or invent a parallel token system.
- Keep page chrome compact. Toolbars should usually live inside the table or card panel instead of in stacked external headers.
- Favor list-first operational pages. Detail pages should expose actions, metrics, YAML, and diagnostics where the backend already supports them.
- When adding forms or drawers, keep copy short and field grouping tight. Avoid decorative layouts that work against data density.
- Preserve light and dark behavior. Any visual change must work in both modes.

## Workbench Rules

- Platform pages share persisted cluster and namespace scope. List pages should keep scope filters, search, refresh, and batch actions in the table or panel toolbar.
- Delivery pages should prefer backend aggregate endpoints for release boards, application detail, environment bindings, release bundles, execution tasks, logs, and artifacts instead of fan-out joins from the browser.
- AI workbench canonical routes are `/ai-workbench`, `/ai-workbench/chat`, `/ai-workbench/root-cause`, `/ai-workbench/performance`, `/ai-workbench/inspection`, `/ai-workbench/tool-settings`, and `/ai-workbench/model-settings`. Legacy `/ai-observe/**`, `/chat`, `/ai-workbench/investigation`, `/ai-workbench/automation`, and `/ai-workbench/tools` should remain redirects or compatibility routes only.
- AI investigation is session-first. Preserve session query parameters, mode selection, scope handoff, toolset drawer behavior, analysis artifact history, and inspection-to-session flows when changing the canvas.
- AI Agent Runtime UI must consume kubecrux catalog contracts for `agentProviders`, `capabilities`, `toolBindings`, `skillBindings`, analysis profiles, and session toolsets. Do not call Hermes, OpenClaw, or provider-specific endpoints directly from pages.
- Provider selection belongs to session metadata, explicit-analysis requests, and automation policy forms as `agentProviderId`; pages should treat Hermes as one provider option, not as a special page mode.
- Agent output shown in the workbench should come from kubecrux `AnalysisArtifact`, `ToolExecution`, and message metadata. Provider-native payloads should stay behind backend normalization.
- Continuous analysis controls should stay automation-policy-driven. Frontend forms may choose provider, profile, analysis kinds, budgets, dedup, and cooldown, but should not expose Hermes cron as the platform scheduler.
- Keep permission, module, and empty-state boundaries explicit for Agent Runtime. External provider availability, runner absence, permission denial, unsupported capability, and truly empty data should not be presented as the same state.
- Docker workbench pages should show operation state and runner boundaries plainly. Do not imply Docker Engine or Compose has run until operation callbacks report runtime state.
- Virtualization pages should keep create, power, console, metrics, operation log, retry/cancel, and sync actions permission-aware. Surface KubeVirt/PVE or lab limitations as explicit status or empty states.
- Monitoring and on-call pages should consume backend route resolution and active alert-task data. Do not make frontend-only route matching the primary operational source.
- System and menu management changes must invalidate or refresh the permission snapshot so sidebar changes are visible immediately.

## Common Pitfalls

- Adding a route only in `index.tsx` without `meta.ts` makes breadcrumbs, sidebar selection, permission checks, and workbench filtering drift.
- Adding a menu or permission only in the frontend does not seed backend visibility. Keep backend seed menus and permission catalog aligned.
- Copying old `ai-observe` or legacy AI page structure into `/ai-workbench` will duplicate navigation; AI-specific switching belongs inside the workbench page chrome.
- Fetching raw Kubernetes rows to render dashboard cards usually means the backend overview DTO should be used or expanded.
- Storing server responses in Zustand creates stale and duplicated state. Use TanStack Query and invalidate keys after mutations.
- Styling with page-level hero cards, saturated gradients, or new token systems breaks the console baseline. Use the existing `kc-*` CSS variables and compact antd surfaces.
- Showing unavailable backend capabilities as normal empty tables confuses operators. Empty states must distinguish unsupported, backend-pending, permission-denied, and truly empty data.

## Read These References When Needed

- `references/architecture.md`: folder ownership, route/runtime rules, query/state boundaries, and permission wiring.
- `references/ui-style.md`: antd plus shadcn-like UI rules, table/detail patterns, and visual do/don'ts.

## Repo-specific reminders

- Settings navigation changes must update route metadata, menu wiring, i18n, and tests together.
- Login and callback pages must stay aligned with the backend auth-provider contract; do not hard-code a single OIDC-only assumption once `/auth/providers` exposes multiple enabled providers.
- If a backend capability is configuration-visible but runtime-incomplete, show that boundary plainly in UI copy instead of pretending the flow is available.
- Workbench routing currently includes platform, delivery, monitoring, AI, virtualization, and Docker. Keep `getRouteWorkbenchId`, sidebar filtering, and `WORKBENCH_DEFAULT_PATHS` aligned with any route additions.
- For AI, Docker, and virtualization routes, update `web/src/features/auth/permission-catalog.ts`, `web/src/routes/meta.test.ts`, and affected feature tests when permission keys change.
- Keep `docs/build`, `docs/.docusaurus`, and generated frontend artifacts out of hand-edited changes unless the user explicitly asks for built output.

## Done Criteria

- Routes, permissions, and visible menu behavior still align.
- Scope semantics remain obvious and persistent.
- No new frontend query fan-out or duplicated scope state is introduced.
- Workbench navigation, module visibility, and compatibility redirects still behave as expected.
- Affected pages typecheck, and tests are updated when semantics change.
