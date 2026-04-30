# Backend Authz Unify Result

## Summary
- Replaced remaining API-facing backend permission gates that relied only on `HasPermission(principal.Roles, ...)` with resolver-backed authorization where persisted role `permissionKeys` must be authoritative.
- Kept RBAC action capability derivation and ABAC/resource authorization behavior unchanged.
- Left static role-matrix checks only as fallback when a service/handler has no injected `PermissionResolver`.

## Changed Files
- `internal/bootstrap/app.go`
- `internal/api/handlers/settings.go`
- `internal/api/handlers/upload.go`
- `internal/application/announcement/service.go`
- `internal/application/catalog/service.go`
- `internal/application/catalog/service_test.go`
- `internal/application/copilot/analysis.go`
- `internal/application/copilot/authz.go`
- `internal/application/copilot/controlplane.go`
- `internal/application/copilot/inspection.go`
- `internal/application/copilot/service.go`
- `internal/application/identity/service.go`
- `internal/application/menu/service.go`
- `internal/application/monitoring/service.go`
- `internal/application/registry/service.go`
- `internal/application/release/service.go`
- `internal/application/release/service_test.go`
- `internal/application/resource/service.go`
- `internal/application/settings/service.go`
- `internal/application/workflow/service.go`

## Validation
- `go test ./internal/bootstrap ./internal/application/workflow ./internal/application/resource ./internal/application/copilot ./internal/application/monitoring ./internal/application/menu ./internal/application/announcement ./internal/application/registry ./internal/application/settings ./internal/application/catalog ./internal/application/release ./internal/application/scopegrant ./internal/application/access ./internal/application/identity ./internal/api/handlers`
- Added targeted tests proving delegated/custom roles resolved through persisted `permissionKeys` can pass:
  - catalog workflow-template manage gate
  - release trigger gate

## Residual Risks
- Some services still retain static fallback branches for nil-resolver safety. Runtime bootstrap now injects the resolver into the affected console/API services, but ad hoc construction in future tests or alternate wiring could still bypass persisted role permissions if the resolver is omitted.
- I did not widen this task into unrelated authz cleanup for helper packages that already centralize permission resolution (`internal/application/access/*`) or non-console capability evaluation paths.

## Docs / Memory
- No docs or memory files updated. Backend permission contract did not materially change again; this task unified runtime enforcement with the existing persisted-permission model.
