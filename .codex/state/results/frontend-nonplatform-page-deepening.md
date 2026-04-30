# FRONTEND-NONPLATFORM-PAGE-DEEPENING

## Scope
- Deepened active non-platform entry pages under `web/src/pages/{delivery,observability,ai-observe,system,settings}.tsx`.
- Kept route paths, permission keys, and backend contracts unchanged.
- Limited edits to the workspace shell and the feature pages rendered inside those entry routes.

## What Changed
- Expanded `web/src/pages/nonplatform-workspace.tsx` from a thin `PageContainer` wrapper into a fuller workspace frame with:
  - route-context summary
  - section stats
  - workspace highlights
  - action-focus guidance
- Updated each non-platform entry page to supply clearer workspace-specific framing:
  - `delivery`
  - `observability`
  - `ai-observe`
  - `system`
  - `settings`
- Added `embedded` rendering support to nested feature pages that previously rendered their own top-level `PageHeader`, so the entry workspace is now the primary visible shell for:
  - delivery workflows / releases / registries / business lines / environments / application environments / release board / workflow templates
  - observability overview / alerts / notifications / events / oncall
  - AI observe root cause / performance / inspection
  - system online users / announcements / menus / audit / operations
  - settings monitoring / settings center

## Page Outcomes
- Delivery now reads as one Pro-native workbench that separates master-data setup from execution surfaces before entering the existing feature pages.
- Observability now frames alert pressure, notification coverage, and event review as one response workflow instead of a stack of independent shells.
- AI Observe now exposes the difference between ad hoc analysis, AI chat, and scheduled inspection directly in the route entry.
- System now separates control actions from audit/replay surfaces more clearly while preserving the current CRUD and permission behavior.
- Settings now removes the most obvious outer/inner shell duplication for monitoring and settings-center flows, while keeping existing form logic intact.

## Behavior Notes
- No backend API shape changed.
- No route path changed.
- No permission key changed.
- No menu visibility rule changed.
- Application-environment detail and a few standalone feature pages still keep their own page header because they are detail/workspace pages rather than shell-within-shell list entries.

## Validation
- Ran `cd web && npm run tsc`
- Result: passed
- Ran `cd web && npm run build`
- Result: passed

## Risks
- Workspace summary stats still issue some top-level queries that overlap with the inner feature pages, so there is still duplicate fetching until shared query helpers consolidate those reads.
- A few non-platform feature pages still use their own internal table titles or card titles even when embedded; the visual duplication is much lower, but not fully normalized into one shared section pattern yet.

## Recommended Next Step
- Consolidate the workspace-summary queries and embedded feature-page queries behind shared TanStack Query helpers so the deeper entry pages keep the improved framing without extra duplicate fetches.
