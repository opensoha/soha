# FRONTEND-NONPLATFORM-PRO-PAGES

## Scope
- Reworked active non-platform route entry pages under `web/src/pages/{delivery,observability,ai-observe,access,system,settings}.tsx`.
- Added a shared Pro-native workspace shell in `web/src/pages/nonplatform-workspace.tsx`.
- Kept `old_web` as reference only; no structural reuse.

## What Changed
- Replaced pathname-only wrapper pages with metadata-driven Pro workspaces built on `PageContainer`.
- Added per-section navigation buttons, route-context tags, workspace summaries, and lightweight top-level stats for:
  - delivery
  - observability
  - AI observe
  - access
  - system
  - settings
- Preserved existing feature-module rendering and backend/API behavior by continuing to render the current `web/src/features/**` pages inside each workspace.

## Behavior Notes
- No permission keys were changed.
- No route paths were changed.
- No backend contracts were changed.
- Settings entry now exposes `/settings/monitoring` in the active workspace shell, but underlying feature behavior remains owned by `settings-pages.tsx`.
- Access and system entry pages now provide a real workspace frame before delegating to the existing CRUD/log pages.

## Validation
- Ran `npm run tsc` in `web/`
- Result: passed

## Risks
- Several workspace summary stats query the same backend resources that downstream feature pages also query, so there is some duplicate top-level data fetching until shared query helpers are introduced.
- `SettingsCenterPage` still internally exposes only the existing tabs set; the route entry now surfaces monitoring in the outer workspace, but the inner feature-center composition has not yet been fully unified.

## Recommended Next Step
- Consolidate workspace-summary queries and inner feature-page queries behind shared TanStack Query helpers so the new entry shells do not add redundant fetches.
