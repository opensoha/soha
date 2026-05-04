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
- `web/src/theme/semi-theme.ts`: antd `ThemeConfig` plus shared CSS-variable baseline.

## Data and State Rules

- Keep server data in TanStack Query.
- Keep local runtime preferences and persistent scope in Zustand.
- Reuse shared scope helpers and the platform scope store for cluster and namespace state.
- Prefer aggregated backend DTOs. If the page needs data from many namespaces or resource kinds, push aggregation to the backend instead of adding client fan-out.
- Keep query keys stable and derived from route scope and filters.

## Navigation and Permissions

- When adding a route, update `index.tsx` and `meta.ts` together.
- If the route needs sidebar visibility, ensure it has a menu-aware `menuId` and the correct `permissionKey`.
- Frontend route visibility, backend visible menus, and backend authorization are separate gates. Keep all three aligned.
- Update `web/src/i18n/index.tsx` whenever user-facing route or page copy changes.

## Verification

- Run `npm run typecheck` in `web` after any non-trivial change.
- Run `npm run test` for affected feature behavior when tests exist.
- For navigation work, smoke test both light and dark modes and at least one narrow viewport.
