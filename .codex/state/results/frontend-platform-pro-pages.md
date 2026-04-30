# FRONTEND-PLATFORM-PRO-PAGES

## Scope
- Active route-entry reconstruction only under `web/src/pages/platform/**` plus `web/src/pages/overview.tsx`
- `old_web` used as behavior reference only
- No backend contract changes

## What Changed
- Added a shared Pro-native platform workspace shell at `web/src/pages/platform/workspace-shell.tsx`
- Rebuilt active platform entry pages so they now render through `PageContainer` with:
  - route-aware section tabs
  - workspace summary metrics
  - contextual entry actions
  - preserved handoff into existing feature-page list/detail implementations
- Kept detail pages owned by existing feature modules instead of transplanting the old shell structure

## Page Outcomes
- `overview`
  - now lands inside a Pro entry workspace instead of a direct feature export
  - exposes overview-specific summary metrics and a direct handoff into pods
- `clusters`
  - now frames cluster inventory as a workspace with explicit registration-surface context
  - cluster detail remains a direct detail handoff
- `cluster-resources`
  - now provides Pro-native nodes/namespaces tabs and cluster-scope framing
  - node detail remains a direct detail handoff
- `workloads`
  - now provides a fuller workspace shell around overview and list pages
  - keeps detail pages separate so drill-down still feels operational
- `configuration`
  - now groups mutable config, quota, and webhook surfaces under one Pro workspace
  - configmap/secret detail pages remain direct drill-down workspaces
- `platform-access-control`
  - now exposes Kubernetes RBAC as a dedicated workspace with section tabs
- `network`
  - now frames topology as the primary entry and preserves service/detail drill-downs
- `storage`
  - now exposes PVC/PV/storageclass navigation as a storage workspace
- `extensions` and `helm`
  - now share one extension workspace shell with CRD and Helm section tabs

## Scope / Behavior Notes
- Scope semantics did not change
- Aggregation location did not change
- No old shell/page structure was transplanted wholesale
- Existing feature modules remain the source of page behavior and API usage

## Validation
- `cd web && npm run tsc -- --pretty false` passed

## Risks
- Some pages still render their own internal headers under the new route-level `PageContainer`, so a few workspaces may feel visually double-layered until feature modules are refactored further
- Tab coverage is intentionally curated for main workspace sections; lower-frequency routes such as ReplicaSets, EndpointSlices, and some configuration subtypes still remain reachable by URL/menu but are not all surfaced as top-level tabs

## Recommended Next Step
- Fold duplicated inner `PageHeader` usage out of the highest-traffic platform feature pages so the new Pro route shell becomes the single visible workspace header
