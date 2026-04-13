# Frontend Structure

## Current Stack

- UI shell and page primitives use Semi Design via `@douyinfe/semi-ui` and `@douyinfe/semi-icons`
- official Semi theme packages are applied at runtime, and the active theme is persisted in client preferences
- Tailwind is kept for layout, spacing, and a small set of utility colors aligned to Semi tokens
- `web/tailwind.config.ts` maps common utilities to `--semi-*` CSS variables and disables `preflight`
- `web/src/styles/globals.css` defines the shared Semi-admin shell and page skeleton

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
  theme/
    semi-theme.ts
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
- sidebar grouping, titles, redirects, and breadcrumb metadata live in `web/src/routes/meta.ts`
- `web/src/layouts/app-layout.tsx` owns the Semi-based shell: `Layout`, `Nav`, `Breadcrumb`, theme switchers, user dropdown, and logout action
- page implementations are grouped by business domain under `web/src/features`
- platform pages intentionally stay bundled by capability today: `workloads-pages.tsx`, `network-storage-pages.tsx`, and `extensions-pages.tsx` each export multiple route-level pages
- HTTP access goes through `web/src/services/api-client.ts`, which targets same-origin `/api/v1` and retries once after token refresh
- persisted client state lives under `web/src/stores`; `preferences-store.ts` persists theme and sidebar preferences, while server state stays in TanStack Query
- `web/src/theme/semi-theme.ts` is the runtime theme bridge for official Semi theme packages and light/dark/system mode
- `components/` is now used for shared page skeleton parts such as `page-header.tsx` and `platform-scope-toolbar.tsx`
- `components/` also contains shared admin primitives such as `admin-table.tsx`, `stat-grid.tsx`, and `status-tag.tsx`
- `utils/time.ts` centralizes table-friendly date and relative-time formatting
- `utils/table-columns.ts` centralizes common table-column width and alignment presets
- `hooks/` is still intentionally absent today; only add it when reuse is concrete
- when adding a new page, update both `web/src/routes/index.tsx` and `web/src/routes/meta.ts`
- page code consumes aggregated DTOs only
