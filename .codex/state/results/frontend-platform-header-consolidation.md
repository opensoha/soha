# FRONTEND-PLATFORM-HEADER-CONSOLIDATION

## Scope
- Active platform feature/header consolidation only under `web/src/features/platform/**`
- No backend contract changes
- No route-shell changes

## What Changed
- Removed duplicate inner `PageHeader` blocks from shell-managed, high-traffic platform list and overview pages so the route-level `PlatformWorkspaceShell` / `PageContainer` remains the single visible workspace header.
- Kept `PlatformScopeToolbar` in place for scope behavior continuity.
- Preserved detail-page headers where those pages render outside the route-level workspace shell.
- Moved create actions that previously lived in inner headers onto the table surface where needed:
  - `ClustersPage` create action now lives in `AdminTable.headerExtra`
  - `ClusterNamespacesPage` create action now lives in `AdminTable.headerExtra`
- Moved topology state badges previously shown in the removed header into the topology workspace filter/legend card.

## Pages Consolidated
- `OverviewPage`
- `WorkloadsOverviewPage`
- `ClustersPage`
- `ClusterNodesPage`
- `ClusterNamespacesPage`
- `NetworkTopologyPage`
- `NetworkServicesPage`
- `NetworkIngressesPage`
- `NetworkGatewaysPage`
- `StoragePvcPage`
- `StoragePvPage`
- `StorageClassesPage`
- generic shell-managed resource lists in `platform-management-pages.tsx`
- `CRDPage`
- `HelmReleasesPage`
- `HelmChartsPage`

## Behavior Notes
- Scope semantics did not change.
- Aggregation location did not change.
- Detail flows such as workload detail, service detail, cluster detail, config detail, node detail, and unsupported/empty detail states still keep their local headers because they are not all rendered inside the route-level workspace shell.

## Validation
- `cd web && npm run tsc` passed

## Risks
- Some lower-frequency platform detail or unsupported-state pages still intentionally render their own `PageHeader`; if any of those later move under a route-level shell, they should be reevaluated for the same duplication pattern.
- Generic resource list pages now rely on the outer Pro shell for title/description context; if one of those lists is reused outside the shell later, it may need an opt-in local header mode.

## Recommended Next Step
- Sweep remaining platform detail and placeholder routes to confirm which ones should stay standalone and which should eventually be wrapped by the Pro workspace shell before more header cleanup is attempted.
