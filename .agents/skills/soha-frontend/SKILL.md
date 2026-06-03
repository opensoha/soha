---
name: soha-frontend
description: >-
  Implement and refactor the soha console in `web/src/**` with React 18,
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

# Soha Frontend

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

## Module Split Rules

- Keep route page files responsible for composition, hooks, UI state, permission-aware rendering, and event wiring. Move stable DTOs, option constants, payload builders, query-string helpers, status mappers, normalization functions, and other pure helpers into scoped sibling modules such as `*-model.ts`, `*-types.ts`, or `*-api.ts`.
- Prefer feature-local model modules before promoting code to shared folders. Use examples like `features/settings/ai-settings-model.ts`, `features/copilot/ai-gateway-model.ts`, `features/virtualization/virtualization-model.ts`, `features/platform/workloads-model.ts`, `features/platform/platform-management-model.ts`, and `features/system/system-model.ts` as the baseline pattern.
- Do not move JSX render helpers, hooks, mutation wiring, or table column factories out of a page file unless the new module has a clear UI-level ownership and the split reduces real complexity. Pure helpers belong in model files; UI components belong in page files or explicit shared component files.
- Keep compatibility exports when tests or existing imports depend on page-level helper exports. Re-export from the page while moving the implementation into the model module, then update imports only when the owning surface is ready.
- Split CSS by ownership. Put app-wide tokens and resets in `web/src/styles/globals.css`; put reusable shell/surface rules in `web/src/styles/shared-surfaces.css`; put component rules beside shared components; put feature/workbench rules beside their feature pages. Avoid adding new global selectors for one page or one antd component override.
- Route registration and route metadata stay separate. `web/src/routes/index.tsx` should keep the route tree; lazy page declarations and compatibility redirect components belong in a sibling route helper when they grow large. `web/src/routes/meta.ts` should own route lookup, permission, workspace, workbench, scope, and sidebar behavior; static route data belongs in a separate data module.
- Keep `web/src/types/index.ts` as a compatibility barrel only when global DTOs grow. Split global types by domain, for example `core.ts`, `platform.ts`, `delivery.ts`, and `access.ts`, and use type-only imports for cross-domain references.
- Keep `web/src/i18n/index.tsx` focused on provider, hook, `translate`, and dictionary registration. Locale dictionaries and i18n types belong in separate modules under `web/src/i18n/`.
- Remove historical naming residue during refactors. Do not leave names such as `Semi*`, legacy theme aliases, or old framework labels in active code when the implementation is native antd.
- Treat file-size reduction as a maintenance signal, not the goal by itself. A split is worthwhile only when it clarifies ownership, reduces page-bundle responsibility, improves testability, or prevents repeated edits in the same large file.

## Non-Negotiables

- Work in `web`. Do not treat `old_web` or `web_pro_backup` as active implementation targets.
- Import directly from `antd` and `@ant-design/icons`. Do not introduce Semi Design packages, compat layers, variables, or naming back into `web`.
- Keep `web/src/theme/app-theme.ts` as the single source for antd theme tokens and shared `--soha-*` CSS variables.
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
- In the k8s workbench, resource-scope controls belong in the app header instead of repeated page-level context bars. Use compact native antd `Select` controls, not a secondary popover/dropdown that hides the active scope.
- Namespace and cluster are independent scope controls. On namespace-scoped pages, place namespace first and cluster second inside one compact inline group; on cluster-only pages, show only the cluster selector and keep its width identical to the cluster control used in the namespace-plus-cluster group.
- Header scope controls should visually merge with the header/content background, avoid dark or high-contrast focus/open outlines, and never push, wrap, or overlap right-side header actions. Prefer tight gaps, stable widths, ellipsis, icon affordances, and tooltips over long inline labels such as "资源上下文".
- Favor list-first operational pages. Detail pages should expose actions, metrics, YAML, and diagnostics where the backend already supports them.
- Management list pages may use the clusters-page Pro-style pattern when a richer table workflow is needed: one outer operational surface, an independent compact antd query form above the table, a separate table management toolbar with batch/refresh/create/column actions, then the data table. Do not place the query form inside the table header or inside `AdminTable` toolbar when following this pattern.
- Management query panels must visually align with their paired management table: the first query field should share the table content's left baseline, and the query card should not drift farther right than the table's first column.
- Query panel fields in the same row should use fixed, intentional gaps instead of relying on leftover flex/grid space. Keep label widths and control widths stable so keyword, select, switch, and action buttons scan as one compact toolbar.
- The primary shell sidebar menu text baseline is 12px. Icons may be larger for legibility. Preserve this through shell/sidebar-owned typography and existing theme structure; do not add global `.ant-menu-title-content` overrides for it.
- Business data tables should follow the clusters-page table treatment: compact `AdminTable`, quiet border-only shell, small pagination, column settings in the toolbar, fixed right-side action column, and no decorative left-header table title. Do not pass `title` to `AdminTable` for ordinary business tables unless the table is one of several sibling tables in the same panel and the label is needed to distinguish them.
- Table result-count summaries such as `当前 x / y 条` belong on the left side of the table pagination row through `AdminTable.paginationSummary`; do not place them in the query-panel action area beside reset/search buttons.
- Fixed right-side action columns must be visually opaque over horizontally scrolling content. Cover the current Ant Design fixed-end classes as well as legacy fixed-right classes, including header, hover, selected-row, and shadow states, so underlying cells never show through.
- Action columns should center the header label and icon-button group. Shared action column presets should use centered alignment while remaining fixed to the right.
- Management table toolbar primary create buttons should stay compact and visually close to the query-panel button width. When an icon plus short text makes a create button much wider than nearby query actions, reduce only that button's icon gap and horizontal padding instead of inflating the whole toolbar.
- Management table utility icons must keep a stable order: density/compactness toggle first, refresh second, and column settings last. If create or batch business actions exist, keep those business actions before the utility icon group while preserving the utility order.
- List pages without search or create forms should still keep standard table utility controls such as refresh, density toggle, and column visibility in the table header when using the management table shell, unless the table is intentionally static or embedded.
- Scope-plus-table resource pages should keep the table inside the management table shell/card frame even when there is no query form or create action. Avoid bare tables directly on the page because status tags, pagination, and fixed action columns need the same visual boundary and alignment as richer management lists.
- Do not stack explanatory cards above simple operational resource lists when the sidebar, breadcrumb, scope selector, and table already explain the page. Remove redundant detail headers, business-line scope notes, and creation caveats unless they communicate a real permission boundary, unsupported capability, destructive risk, or next action.
- Overview and dashboard pages inside operational workbenches should stay dense. If sidebar, breadcrumb, and header scope already establish context, remove banner-like page descriptions and use compact metric cards, tighter panel spacing, and readable 3- or 4-column desktop grids instead of oversized 2-by-2 cards that dominate the first viewport.
- For resources that are not created from Soha, omit create controls and query panels that do not fit the workflow, but keep operational row actions and the table utility rail consistent with other management tables.
- Prefer `web/src/components/management-list.tsx` for the management-list baseline: compact query panel, table toolbar, batch bar, density/refresh buttons, and icon-only action controls. When following the clusters-page pattern, keep the query card and table footer compressed, align form labels and input placeholder text on the same vertical center line, avoid decorative table titles such as "查询表格" when the table is already obvious, do not enable default column sorting unless the user workflow explicitly needs it, render row operations as icon buttons with tooltips, and use antd `Popconfirm` for lightweight destructive confirmations instead of `Modal.confirm`.
- When adding forms or drawers, keep copy short and field grouping tight. Avoid decorative layouts that work against data density.
- Preserve light and dark behavior. Any visual change must work in both modes.

## Workbench Rules

- Platform pages share persisted cluster and namespace scope. List pages should keep scope filters, search, refresh, and batch actions in the table or panel toolbar.
- K8s workbench pages should derive header scope UI from route scope semantics: cluster-scoped pages show the cluster selector only, namespace-scoped pages show namespace plus cluster, and pages must not create their own duplicate scope selectors.
- Global breadcrumbs should be driven by route metadata plus runtime backend menu labels. Visible menu routes may prefer the current menu label, but detail or synthetic routes should keep their route title so entity/detail breadcrumbs are not collapsed into a parent menu name.
- Delivery pages should prefer backend aggregate endpoints for release boards, application detail, environment bindings, release bundles, execution tasks, logs, and artifacts instead of fan-out joins from the browser.
- AI workbench canonical routes are `/ai-workbench`, `/ai-workbench/chat`, `/ai-workbench/root-cause`, `/ai-workbench/performance`, `/ai-workbench/inspection`, `/ai-workbench/tool-settings`, and `/ai-workbench/model-settings`. Legacy `/ai-observe/**`, `/chat`, `/ai-workbench/investigation`, `/ai-workbench/automation`, and `/ai-workbench/tools` should remain redirects or compatibility routes only.
- AI investigation is session-first. Preserve session query parameters, mode selection, scope handoff, toolset drawer behavior, analysis artifact history, and inspection-to-session flows when changing the canvas.
- AI Agent Runtime UI must consume soha catalog contracts for `agentProviders`, `capabilities`, `toolBindings`, `skillBindings`, analysis profiles, and session toolsets. Do not call Hermes, OpenClaw, or provider-specific endpoints directly from pages.
- Provider selection belongs to session metadata, explicit-analysis requests, and automation policy forms as `agentProviderId`; pages should treat Hermes as one provider option, not as a special page mode.
- Agent output shown in the workbench should come from soha `AnalysisArtifact`, `ToolExecution`, and message metadata. Provider-native payloads should stay behind backend normalization.
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
- Styling with page-level hero cards, saturated gradients, or new token systems breaks the console baseline. Use the existing `soha-*` CSS variables and compact antd surfaces.
- Reintroducing page-level resource-scope bars in the k8s workbench makes operators switch context per page and visually competes with the shell. Keep scope in the header and keep list-page controls inside the table/panel surface.
- Applying backend menu labels to every breadcrumb segment can hide detail-route titles such as resource names or overview labels. Only replace labels for real visible menu routes.
- Oversized overview cards and repeated explanatory headers are regressions for operational console pages; compact scanability is preferred over marketing-style page composition.
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
- Refactors that split route pages, route metadata, global types, i18n, or CSS include focused tests for the touched behavior, `npm run typecheck`, and `npm run build` before completion.
- Browser checks cover representative workbench routes affected by the split and confirm pages render without login fallback, Vite error overlays, or console errors.
- Header, breadcrumb, and scope-selector changes are verified at the target browser viewport for wrapping, overlap, truncation, focus/open states, and parity between cluster-only and namespace-plus-cluster modes.
- Breadcrumb/menu/scope semantics changes include focused layout or route tests, especially around runtime menu labels and detail-route titles.
