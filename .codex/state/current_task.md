# Current Task

## Title
Migrate the `web` frontend onto an `ant-design-pro` scaffold baseline while preserving existing product functionality and re-aligning backend contracts where the new shell expects different runtime integration points.

## Objective
The current `web` app already uses native `antd`, but it still runs on a custom Vite + React Router shell with bespoke layout, routing composition, and request bootstrapping.

This task should replace that shell with an `ant-design-pro` style scaffold baseline so the console aligns with Pro layout, route, access, and runtime conventions.

The migration must:
- discard the current page shell and page styling approach
- keep the current product capabilities, route coverage, and permission semantics
- re-host those capabilities inside an `ant-design-pro` style scaffold
- keep frontend and backend integrated cleanly after the shell swap

## Desired Direction
- Reuse the existing `web/src/features/**` business modules wherever possible.
- Move the runtime from the current custom Vite entry to a Pro-style runtime with:
  - Pro layout
  - route metadata mapped into Pro menus
  - centralized request/runtime bootstrap
  - initial-state driven auth and permission hydration
- Treat this as a scaffold migration, not a visual polish pass.
- Keep current APIs unless the Pro-style runtime requires small compatibility endpoints or response-shape adaptation.
- Do not preserve old shell CSS or custom layout semantics unless they are still needed for functionality.

## Required Outcomes
1. Frontend scaffold migration
   - switch `web` onto an `ant-design-pro` based scaffold/runtime baseline
   - replace the current custom shell, menu, breadcrumb, and login container implementation with Pro-aligned structure
   - map existing route metadata and permission/menu semantics into the new scaffold
2. Functional carry-over
   - preserve current feature coverage for platform, delivery, observability, access, system, settings, and auth flows
   - preserve cluster/namespace scope semantics and permission gating behavior
   - preserve same-origin backend integration for `/api/v1`
3. Backend adaptation
   - identify and implement any backend adjustments needed for the new shell bootstrapping flow
   - keep permission snapshot, visible menus, branding, and auth refresh/login behavior aligned with the new frontend runtime
4. Validation
   - run targeted frontend type/build validation
   - run targeted backend tests where contracts changed
5. Memory/docs
   - update `AGENTS.md`
   - update frontend structure docs if runtime or ownership boundaries changed materially

## Migration Specification
1. Runtime baseline
   - `@umijs/max` and `@ant-design/pro-components` become the shell/runtime baseline for `web`
   - React Router manual route registration is replaced by Pro/Max route config
   - request bootstrapping should centralize auth redirect, token usage, and error handling
2. Business-module preservation
   - keep `web/src/features/**` as the primary business implementation layer
   - pages may be wrapped, re-exported, or lightly adapted, but business queries and DTO consumption should stay close to existing modules
3. Navigation and access
   - existing `web/src/routes/meta.ts` remains the source of product route semantics unless and until a Pro-native route manifest fully supersedes it
   - backend permission snapshot plus visible menus remain the source of truth for menu visibility
   - Pro access rules must reflect current `permissionKeys` and `visibleMenuIds` behavior rather than introducing a second authorization model
4. Request and auth
   - keep backend base path at same-origin `/api/v1`
   - preserve login, refresh-token, logout, OIDC callback, and branding flows
   - do not regress current token refresh semantics during request migration
5. Styling constraints
   - old custom shell/page styles are allowed to be deleted or bypassed
   - new work should prefer Pro layout tokens and antd semantics over bespoke container styling
   - feature pages should converge toward Pro table/form/card/page-container patterns

## Hard Requirements
- Main thread must orchestrate and keep `.codex/state/**` current before spawning child threads.
- Workers are not alone in the codebase and must not revert unrelated edits.
- Frontend scaffold, frontend feature migration, and backend adaptation should use disjoint write ownership where possible.
- Do not silently break embedded asset serving; the backend still serves the built `web/dist`.

## Definition Of Done
- `web` boots on an `ant-design-pro` style scaffold/runtime.
- The old custom app shell is no longer the primary runtime shell.
- Existing features remain reachable through the new scaffold.
- Menu visibility and permission semantics remain aligned with backend snapshots.
- Any required backend contract adaptations are implemented and validated.
- Memory/docs are updated in the same task.
