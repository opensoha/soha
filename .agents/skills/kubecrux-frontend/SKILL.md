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
  namespace scope, consume backend-aggregated DTOs, and avoid reintroducing
  any Semi Design residue.
---

# Kubecrux Frontend

## Overview

Implement console work inside the active Vite app under `web`. Keep the UI antd-first, operational, and visually aligned with the repo's neutral shadcn-like style rather than introducing a second component system.

## Workflow

1. Read `web/src/routes/index.tsx`, `web/src/routes/meta.ts`, and the target `web/src/features/**` module before editing.
2. Decide whether the change belongs in an existing route bundle, a shared component, or a store/query helper. Prefer the existing page bundle files until reuse pressure is real.
3. Fetch server data through TanStack Query and same-origin `/api/v1` helpers. Reuse persisted cluster and namespace scope from Zustand instead of creating page-local duplicates.
4. Build UI with native `antd` components plus the existing theme and CSS-variable system. Treat "shadcn style" as a visual direction, not as `shadcn/ui`.
5. Update route metadata, i18n strings, permission wiring, and tests together when navigation or behavior changes.
6. Clean up stale assets or scripts when they no longer match the active UI baseline; do not leave dead Semi-era static files around once usage is gone.
7. Validate with `npm run typecheck` in `web`, plus focused `npm run test` when semantics change.

## Non-Negotiables

- Work in `web`. Do not treat `old_web` or `web_pro_backup` as active implementation targets.
- Import directly from `antd` and `@ant-design/icons`. Do not introduce Semi Design packages, compat layers, variables, or naming back into `web`.
- Keep `web/src/theme/app-theme.ts` as the single source for antd theme tokens and shared `--kc-*` CSS variables.
- Do not reintroduce removed legacy theme assets, old static sync scripts, historical token aliases, or outdated file naming unless the user explicitly asks for legacy compatibility.
- Route registration lives in `web/src/routes/index.tsx`. Navigation ownership, breadcrumbs, and permission-aware metadata live in `web/src/routes/meta.ts`.
- Server state belongs to TanStack Query. Persisted UI and runtime preferences belong to Zustand. Avoid storing fetched API payloads in local stores.
- Prefer backend aggregation over browser fan-out. Do not issue one request per namespace when a platform aggregate endpoint exists or should exist.

## UI Direction

- Use Ant Design components with a shadcn-like grayscale treatment: neutral surfaces, restrained accents, crisp borders, compact square-edged controls, and quiet shadows.
- Do not import `shadcn/ui` or invent a parallel token system.
- Keep page chrome compact. Toolbars should usually live inside the table or card panel instead of in stacked external headers.
- Favor list-first operational pages. Detail pages should expose actions, metrics, YAML, and diagnostics where the backend already supports them.
- When adding forms or drawers, keep copy short and field grouping tight. Avoid decorative layouts that work against data density.
- Preserve light and dark behavior. Any visual change must work in both modes.

## Read These References When Needed

- `references/architecture.md`: folder ownership, route/runtime rules, query/state boundaries, and permission wiring.
- `references/ui-style.md`: antd plus shadcn-like UI rules, table/detail patterns, and visual do/don'ts.

## Done Criteria

- Routes, permissions, and visible menu behavior still align.
- Scope semantics remain obvious and persistent.
- No new frontend query fan-out or duplicated scope state is introduced.
- Affected pages typecheck, and tests are updated when semantics change.
