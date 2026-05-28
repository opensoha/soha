# Frontend Structure

## Current Stack

- the active frontend target is `web`, running on `vite` + `react-router-dom`
- `web_pro_backup` preserves the previous frontend backup
- `old_web` remains in-repo as a feature-behavior reference after the reset
- UI shell and page primitives use native `antd`, `@ant-design/icons`, and the custom soha layout/theme system
- Tailwind CSS 4 is kept as a utility layer for layout and spacing
- `web/src/styles/globals.css` defines shared soha shell/page styling under the Vite app shell
- route-driven pages consume TanStack Query server state and Zustand preference or scope state

```text
web/src/
  App.tsx
  main.tsx
  layouts/
    app-layout.tsx
  routes/
    index.tsx
    meta.ts
  features/
    access/
      access-pages.tsx
    auth/
      auth-api.ts
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
  theme/
    app-theme.ts
  vite-env.d.ts
```

## Frontend Rules

- `web/src/routes/index.tsx` is the active route surface for the Vite shell
- `web/src/layouts/app-layout.tsx` owns the shared console shell, sidebar, header, breadcrumb, and theme-aware chrome
- page entry modules should stay thin and delegate business logic to `web/src/features/**`
- `web_pro_backup` and `old_web` are both reference-only and must not be treated as active shell structure
- HTTP access goes through soha auth helpers and same-origin `/api/v1`
- persisted client state lives under `web/src/stores`; `preferences-store.ts` persists theme and sidebar preferences, while server state stays in TanStack Query
- `components/` contains shared antd primitives and heavier reusable widgets
- `utils/time.ts` centralizes table-friendly date and relative-time formatting
- `utils/table-columns.ts` centralizes common table-column width and alignment presets
- `hooks/` is still intentionally absent today; only add it when reuse is concrete
- when adding a new active page, update `web/src/routes/index.tsx` and `web/src/routes/meta.ts` together
- page code consumes aggregated DTOs only
