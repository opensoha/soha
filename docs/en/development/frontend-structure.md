# Frontend Structure

## Current Stack

- UI shell and shared page primitives use native `antd` and `@ant-design/icons`
- Tailwind CSS 4 remains a utility layer for layout and spacing
- `web/src/styles/globals.css` defines the shared console shell and page skeleton
- route-driven pages consume TanStack Query server state and Zustand preference or scope state

```text
web/src/
  main.tsx
  App.tsx
  features/
    access/
      access-pages.tsx
    auth/
      auth-guard.tsx
      login-page.tsx
      oidc-callback-page.tsx
    copilot/
      chat-page.tsx
    delivery/
      delivery-pages.tsx
    docs/
      docs-page.tsx
    observability/
      monitoring-pages.tsx
    platform/
      cluster-detail-page.tsx
      clusters-page.tsx
      extensions-pages.tsx
      network-storage-pages.tsx
      overview-page.tsx
      workloads-pages.tsx
    settings/
      settings-pages.tsx
    system/
      system-pages.tsx
  layouts/
    app-layout.tsx
  routes/
    index.tsx
    meta.ts
  services/
    api-client.ts
  stores/
    auth-store.ts
    platform-scope-store.ts
    preferences-store.ts
  styles/
    globals.css
  components/
    admin-table.tsx
    page-header.tsx
    platform-scope-toolbar.tsx
    stat-grid.tsx
    status-tag.tsx
  types/
    index.ts
  utils/
    table-columns.ts
    time.ts
  vite-env.d.ts
```

## Frontend Rules

- `web/src/App.tsx` only mounts `AppRouter`; runtime composition starts in `web/src/main.tsx`
- route registration and lazy loading live in `web/src/routes/index.tsx`
- sidebar groups, titles, redirects, and breadcrumb metadata live in `web/src/routes/meta.ts`
- `web/src/layouts/app-layout.tsx` owns the Ant Design shell: `Layout`, `Menu`, `Breadcrumb`, theme controls, the user dropdown, and logout action
- page implementations are grouped by business domain under `web/src/features`
- platform pages intentionally remain bundled by capability today: `workloads-pages.tsx`, `network-storage-pages.tsx`, and `extensions-pages.tsx` each export multiple route-level pages
- HTTP access goes through `web/src/services/api-client.ts`, which targets same-origin `/api/v1` and retries once after token refresh
- persisted client state lives under `web/src/stores`; `preferences-store.ts` persists theme and sidebar preferences, while server state stays in TanStack Query
- `components/` is now used for shared page skeleton parts such as `page-header.tsx` and `platform-scope-toolbar.tsx`
- `components/` also contains shared admin primitives such as `admin-table.tsx`, `stat-grid.tsx`, and `status-tag.tsx`
- `utils/time.ts` centralizes table-friendly datetime and relative-time formatting
- `utils/table-columns.ts` centralizes common table-column width and alignment presets
- `hooks/` is still intentionally absent today; only add it when a real reuse pattern emerges
- when adding a new page, update both `web/src/routes/index.tsx` and `web/src/routes/meta.ts`
- page code consumes aggregated DTOs only
