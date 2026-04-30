# Backend Authz Harden Result

## Scope
- Backend-only hardening for runtime-facing console/API permission checks.
- No frontend files changed.
- RBAC action capabilities and ABAC/resource authorization logic were left unchanged.

## Changed Files
- `internal/application/access/permission_resolver.go`
- `internal/application/access/catalog.go`
- `internal/application/access/management.go`
- `internal/application/announcement/service.go`
- `internal/application/catalog/service.go`
- `internal/application/catalog/service_test.go`
- `internal/application/copilot/authz.go`
- `internal/application/identity/service.go`
- `internal/application/menu/service.go`
- `internal/application/monitoring/service.go`
- `internal/application/registry/service.go`
- `internal/application/release/service.go`
- `internal/application/release/service_test.go`
- `internal/application/resource/service.go`
- `internal/application/scopegrant/service.go`
- `internal/application/settings/service.go`
- `internal/application/workflow/service.go`
- `internal/api/handlers/upload.go`
- `internal/application/access/catalog_test.go`
- `internal/application/access/management_test.go`

## What Changed
- Added shared runtime authorization helpers in `internal/application/access/permission_resolver.go`:
  - `AuthorizeRuntimePermission(...)`
  - `RuntimePermissionKeys(...)`
- Runtime-facing permission checks now fail closed when the permission resolver is missing instead of silently falling back to static in-process role permission helpers.
- Switched runtime console/API permission gates in access, release, catalog, settings, upload, copilot, resource, workflow, monitoring, menu, identity, announcement, registry, and scope-grant services to the shared fail-closed helper.
- Added targeted tests that cover:
  - permission snapshot fails closed without a runtime resolver
  - access management fails closed without a runtime resolver
  - release trigger fails closed without a runtime resolver
- Updated older service tests that had implicitly relied on static fallback so they now use explicit permission resolvers.

## Commands Run
- `gofmt -w internal/application/access/permission_resolver.go internal/application/access/catalog.go internal/application/access/management.go internal/application/announcement/service.go internal/application/registry/service.go internal/application/menu/service.go internal/application/monitoring/service.go internal/application/identity/service.go internal/application/settings/service.go internal/application/scopegrant/service.go internal/application/workflow/service.go internal/application/release/service.go internal/application/resource/service.go internal/application/copilot/authz.go internal/application/catalog/service.go internal/api/handlers/upload.go internal/application/access/catalog_test.go internal/application/access/management_test.go internal/application/release/service_test.go`
- `gofmt -w internal/application/catalog/service_test.go internal/application/release/service_test.go`
- `go test ./internal/application/access ./internal/application/release ./internal/application/catalog ./internal/application/scopegrant`
- `rg -n "if .*permissions != nil|HasPermission\\(principal\\.Roles|PermissionKeysForRoles\\(principal\\.Roles" internal/application internal/api -g'*.go'`

## Results
- `go test ./internal/application/access ./internal/application/release ./internal/application/catalog ./internal/application/scopegrant` passed.
- Remaining runtime-facing static fallback branches for `principal.Roles` permission checks were removed from the scanned application/API call sites.

## Residual Risks
- One fallback remains in `internal/application/access/permission_resolver.go` when `ListRolePermissions(...)` succeeds but returns an empty matrix:
  - `PermissionKeys()` falls back to `PermissionKeysForRoles(principal.Roles)`.
  - This preserves built-in role defaults when the repository returns no persisted permission rows.
  - It does not reopen the nil-resolver wiring issue that was the main hardening target, because runtime service entry points now reject missing resolvers before reaching static helpers.
  - Custom roles still depend on persisted permission rows; an empty matrix will not grant custom-role permissions unless they have been loaded into the static matrix elsewhere in-process.
- Targeted validation covered the directly affected backend packages only. No full-repo backend test sweep was run in this worker task.
