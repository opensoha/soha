# Backend Authz Result

## Changed Files

- `internal/domain/access/models.go`
- `internal/api/dto/access.go`
- `internal/api/handlers/access.go`
- `internal/api/handlers/scopegrant.go`
- `internal/application/access/permissions.go`
- `internal/application/access/permission_resolver.go`
- `internal/application/access/catalog.go`
- `internal/application/access/management.go`
- `internal/application/access/catalog_test.go`
- `internal/application/access/management_test.go`
- `internal/application/scopegrant/service.go`
- `internal/application/scopegrant/service_test.go`
- `internal/repository/policy/repository.go`
- `internal/bootstrap/app.go`
- `internal/bootstrap/database.go`
- `migrations/0001_init.sql`
- `migrations/postgres/0001_init.sql`
- `migrations/0006_role_permission_keys.sql`
- `migrations/postgres/0006_role_permission_keys.sql`
- `docs/architecture/authorization.md`
- `AGENTS.md`

## Final Contract

- Roles now persist two distinct authorization fields:
  - `capabilities`: RBAC resource actions used by the existing RBAC + ABAC + scope-grant resource authorization flow.
  - `permissionKeys`: console/backend permission keys used for menu/API authorization and permission snapshots.
- `GET /access/permission-snapshot` now derives `permissionKeys` from persisted role assignments, not only from static built-in role-name maps.
- Access-center list/read surfaces require:
  - `access.users.view`
  - `access.roles.view`
  - `access.groups.view`
  - `access.policies.view`
  - `access.scope-grants.view`
- Access-center mutable operations require:
  - `access.users.manage`
  - `access.roles.manage`
  - `access.groups.manage`
  - `access.policies.manage`
  - `access.scope-grants.manage`
- Scope-grant CRUD is now principal-aware end to end:
  - handler reads principal from auth middleware
  - service enforces `access.scope-grants.view/manage`
  - unauthenticated-or-permissionless callers are denied server-side
- Role create/update payloads now accept and persist `permissionKeys` in addition to `capabilities`.
- Built-in permission maps still exist as bootstrap defaults and fallback defaults, but backend runtime permission checks can be driven by persisted custom-role `permissionKeys`.
- Existing RBAC/ABAC resource `allowedActions` behavior was preserved.

## Validation

- Ran:
  - `go test ./internal/application/access ./internal/application/scopegrant ./internal/api/handlers ./internal/repository/policy ./internal/bootstrap`
- Result:
  - `internal/application/access`: pass
  - `internal/application/scopegrant`: pass
  - remaining targeted packages: compile pass / no direct tests

## Residual Risks

- The runtime permission matrix is refreshed on permission snapshot resolution and role create/update/delete in-process. Persisted role permissions are authoritative in storage, but multi-process immediate propagation still depends on each process loading that persisted matrix during subsequent requests.
- I did not change frontend payload handling or UI forms. Frontend still needs to send `permissionKeys` on role create/update and consume the stricter access/scope-grant permission model.
