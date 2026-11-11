# Compute Workbench Table Standardization

## Scope

Standardize management tables under the compute workbench without changing routes, APIs, or
business behavior.

- Route-level management tables expose density, refresh, column settings, fixed action columns,
  horizontal overflow, and pagination summaries where pagination exists.
- Embedded detail tables keep the fixed action/overflow/pagination contract but do not gain a
  full route-level toolbar.
- Existing Docker table behavior remains the baseline.

## Design

Create one `VirtualizationAdminTable` in the virtualization domain shared layer. It composes the
global `AdminTable` with virtualization styling and the same toolbar contract already used by
`DockerAdminTable`: business actions first, then density, refresh, and column settings.

Virtualization route pages provide their query refetch callback and loading state. They no longer
define page-local table wrappers. The compute access page composes the shared management controls
directly because it belongs to neither virtualization nor Docker.

The global `AdminTable` remains domain-neutral. It continues to normalize action columns and own
column selection, pagination rendering, and horizontal scroll calculation.

## Verification

- Component tests assert the route-level controls and action-column contract.
- Typecheck, lint, focused tests, route/boundary checks, build, and graph diagnostics must pass.
- Browser checks cover representative virtualization, Docker, access, and embedded-detail tables.
