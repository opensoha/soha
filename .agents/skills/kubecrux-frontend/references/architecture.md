# Frontend Architecture

## Active Surface

- `web` is the only active frontend target.
- `web_pro_backup` and `old_web` are reference-only.
- `web/src/.umi` is generated output. Do not build new code on top of it.

## Ownership Map

- `web/src/App.tsx`: theme/bootstrap boundary.
- `web/src/layouts/app-layout.tsx`: shell, sidebar, header, breadcrumb, and global chrome.
- `web/src/routes/index.tsx`: active route registration and lazy page wiring.
- `web/src/routes/meta.ts`: titles, icons, breadcrumbs, parent-child navigation, permission keys, and runtime menu mapping.
- `web/src/features/**`: route-level business modules.
- `web/src/components/**`: shared reusable UI primitives and heavier widgets.
- `web/src/services/**`: API client and request helpers.
- `web/src/stores/**`: persisted auth, preference, and platform scope state.
- `web/src/theme/app-theme.ts`: antd `ThemeConfig` plus shared CSS-variable baseline.

## Workbench Map

- Platform workbench: resource dashboard, clusters, workloads, configuration, network, storage, CRDs, Helm, and Kubernetes RBAC surfaces.
- Delivery workbench: applications, application services and containers, build templates, workflow templates, release bundles, execution tasks, approval policies, release board, releases, registries, and catalog master data.
- Monitoring workbench: monitoring overview, alert rules, alert events, notification policies, healing policies, on-call collaboration, and events.
- AI workbench: `/ai-workbench` and canonical child routes for chat, root-cause, performance, inspection, tool settings, and model settings.
- Virtualization workbench: `/virtualization/**` routes for overview, VMs, VM detail, clusters, images, flavors, operations, and sync.
- Docker workbench: `/docker/**` routes for overview, hosts, projects, services, ports, templates, and operations.

## Data and State Rules

- Keep server data in TanStack Query.
- Keep local runtime preferences and persistent scope in Zustand.
- Reuse shared scope helpers and the platform scope store for cluster and namespace state.
- Prefer aggregated backend DTOs. If the page needs data from many namespaces or resource kinds, push aggregation to the backend instead of adding client fan-out.
- Keep query keys stable and derived from route scope and filters.
- Use mutation success handlers to invalidate TanStack Query keys rather than copying returned server objects into Zustand.
- Keep operation logs, execution artifacts, AI analysis artifacts, and VM console-heavy modules lazy where practical.

## Navigation and Permissions

- When adding a route, update `index.tsx` and `meta.ts` together.
- If the route needs sidebar visibility, ensure it has a menu-aware `menuId` and the correct `permissionKey`.
- Frontend route visibility, backend visible menus, and backend authorization are separate gates. Keep all three aligned.
- Update `web/src/i18n/index.tsx` whenever user-facing route or page copy changes.
- Workbench routing also depends on `WORKBENCH_DEFAULT_PATHS`, `getRouteWorkbenchId`, sidebar filtering, and backend module descriptors.
- For AI, Docker, virtualization, delivery execution, and monitoring navigation changes, update route tests and `web/src/features/auth/permission-catalog.ts` when permission keys change.
- Legacy AI paths should navigate to the canonical `/ai-workbench` routes instead of hosting a separate shell.

## Capability Boundaries

- Docker pages should display desired state, operation status, logs, and callback-derived runtime state. Do not show a successful runtime outcome before callbacks or runner status confirm it.
- Virtualization pages should distinguish unsupported provider actions, lab/runtime limitations, permission denial, pending sync, and truly empty data.
- AI pages should preserve session context in query parameters and metadata. Toolset changes are session-level unless the user is editing global model/provider settings.
- Monitoring on-call pages should consume backend active tasks and route resolution results; frontend filtering is secondary.

## Verification

- Run `npm run typecheck` in `web` after any non-trivial change.
- Run `npm run test` for affected feature behavior when tests exist.
- For navigation work, smoke test both light and dark modes and at least one narrow viewport.
- For workbench routing changes, run or update `web/src/routes/meta.test.ts` and the affected feature tests.
