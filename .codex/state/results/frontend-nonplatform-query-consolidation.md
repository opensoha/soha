# FRONTEND-NONPLATFORM-QUERY-CONSOLIDATION

## Scope
- Consolidated active non-platform TanStack Query helpers and query-key reuse for Delivery, Observability, Access, and Settings.
- Kept route behavior, permission keys, and backend contracts stable.
- Limited ownership to active non-platform query/helper consolidation only.

## What Changed
- Expanded `web/src/features/delivery/queries.ts` from three list helpers to the active delivery data surface used by shells and feature pages:
  - applications
  - business lines
  - delivery environments
  - application environments
  - workflow templates
  - builds
  - workflows
  - releases
  - registries
- Rewired `web/src/features/delivery/delivery-pages.tsx` and `web/src/features/delivery/delivery-catalog-pages.tsx` to consume shared delivery query option builders and shared invalidation keys instead of repeating ad hoc `queryKey` + `queryFn` blocks.
- Added `accessScopeGrantsQueryOptions` and `accessScopeGrantsQueryKey` in `web/src/features/access/queries.ts`, then switched `web/src/features/access/scope-grants-page.tsx` to that helper and to the shared delivery helpers it also depends on.
- Expanded `web/src/features/observability/queries.ts` with shared notification channel, route, and silence helpers, then rewired `web/src/features/observability/monitoring-pages.tsx` to use them.
- Added `web/src/features/settings/queries.ts` to centralize:
  - branding settings
  - identity settings
  - monitoring settings
  - AI settings
  - copilot data sources
  - copilot analysis profiles
  - copilot automation policies
  - copilot capability discovery
- Rewired `web/src/features/settings/settings-pages.tsx` to those settings/copilot helpers and shared invalidation keys.
- Kept outer workspace pages on the same helper/query-key lines already introduced by the earlier non-platform Pro-page work, so outer-shell summary queries and inner feature-page queries now resolve through shared helper ownership instead of parallel local definitions.

## Deduplicated Query Areas
- Delivery:
  - `/applications`
  - `/business-lines`
  - `/delivery-environments`
  - `/application-environments`
  - `/workflow-templates`
  - `/builds`
  - `/workflows`
  - `/releases`
  - `/registries`
- Observability:
  - `/monitoring/summary`
  - `/alerts`
  - `/events`
  - `/notification-channels`
  - `/alert-routes`
  - `/alert-silences`
- Access:
  - `/access/scope-grants`
  - shared supporting delivery master-data queries reused by scope-grant pages
- Settings:
  - `/settings/branding`
  - `/settings/identity`
  - `/settings/monitoring`
  - `/settings/ai`
  - `/copilot/data-sources`
  - `/copilot/analysis-profiles`
  - `/copilot/automation-policies`
  - `/copilot/data-source-capabilities`

## Behavior Notes
- No route paths changed.
- No permission keys changed.
- No backend API contracts changed.
- No non-platform workspace summary behavior was intentionally changed; this task consolidates helper ownership and query-key reuse rather than redesigning page content.

## Validation
- Ran `cd web && npm run tsc`
- Result: passed

## Risks
- Some outer workspace pages still intentionally issue summary queries in addition to the inner feature page’s main list query. The duplication is now consolidated at the cache/helper layer, but the workspace shell still owns its own summary data requirements.
- `web/src/features/access/access-pages.tsx` still contains some generic CRUD invalidation based on resource strings rather than a full per-resource query-helper module. The prioritized access scope-grant surface is consolidated, but the whole access domain is not yet normalized end-to-end.
- AI/copilot settings now share query helpers, but their server responses still rely on broad feature-local runtime shapes rather than a fully exported typed contract module.

## Recommended Next Step
- Finish the remaining Access-domain normalization by moving `access-pages.tsx` list queries and invalidations fully onto shared helper/query-key exports, then remove the last resource-string invalidation paths in that module.
