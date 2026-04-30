# FRONTEND-PLATFORM-DETAIL-OWNERSHIP

## Scope
- Active platform detail and placeholder shell ownership only under `web/src/pages/platform/**` and selected `web/src/features/platform/**`
- No backend contract changes
- No unrelated route or menu changes

## Ownership Decisions

### Fold into route-level Pro shells
- `network/services/:serviceName`
  - kept inside the `Network` `PlatformWorkspaceShell`
  - reason: this page is a service drill-down on top of the existing network workspace scope and tabs, not a separate top-level operational area
- `configuration/configmaps/:configMapName`
  - kept inside the `Configuration` `PlatformWorkspaceShell`
  - reason: this page is a focused resource inspector/YAML editor within the configuration workspace and benefits from the parent shell context
- `configuration/secrets/:secretName`
  - kept inside the `Configuration` `PlatformWorkspaceShell`
  - reason: same ownership model as ConfigMap detail; it is route content within configuration operations rather than its own workspace

### Keep as standalone workspaces
- `clusters/:clusterId`
  - remains standalone
  - reason: cluster detail is a cross-workspace handoff surface with its own summary, diagnostics, and navigation into nodes and workloads
- `cluster-resources/nodes/:nodeName`
  - remains standalone
  - reason: node detail is a full operational workspace with editing, YAML, and scheduled pod context
- `workloads/**/:name` detail routes
  - remain standalone
  - reason: workload detail pages already act as deeper operational workspaces with multi-tab runtime flows, logs, exec/terminal, rollout history, and YAML

## What Changed
- Removed the route-level early escape for service detail from `web/src/pages/platform/network.tsx`
- Removed the route-level early escape for ConfigMap and Secret detail from `web/src/pages/platform/configuration.tsx`
- Added an `embedded` mode to:
  - `ServiceDetailPage`
  - `ConfigMapDetailPage`
  - `SecretDetailPage`
- In embedded mode, those pages suppress their standalone `PageHeader` and render lightweight inner summary cards instead, so the outer `PlatformWorkspaceShell` remains the visible workspace owner

## Behavior Notes
- Scope semantics did not change
- Aggregation location did not change
- The detail pages listed above still keep their internal tabs and actions; only shell ownership and header responsibility changed
- Standalone ownership for cluster, node, and workload details is intentional and unchanged

## Validation
- `cd web && npm run tsc`
- `cd web && npm run build`

## Risks
- Embedded detail pages now show a compact summary card instead of a standalone page header; if the team later wants breadcrumb-style inner titles, that should be designed consistently across shell-owned detail routes
- Other low-frequency platform detail/placeholder routes such as port-forward or future unsupported states may still need the same ownership review once they move closer to active Pro shell usage

## Recommended Next Step
- Apply the same ownership pass to remaining shell-adjacent placeholder/detail routes, especially lower-frequency configuration/network operational pages that still use local `PageHeader` patterns but are no longer true standalone workspaces
