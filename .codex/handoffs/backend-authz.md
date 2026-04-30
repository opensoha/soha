# Backend Authz Handoff

## Ownership
- You own backend implementation only.
- Allowed write scope:
  - `internal/application/access/**`
  - `internal/application/scopegrant/**`
  - `internal/api/handlers/access.go`
  - `internal/api/handlers/scopegrant.go`
  - `internal/domain/access/**`
  - `internal/domain/identity/**` if required by the final permission model
  - `internal/repository/policy/**`
  - `internal/repository/user/**`
  - `internal/repository/menu/**`
  - `internal/bootstrap/**`
  - `migrations/**`
  - `docs/architecture/authorization.md`
  - `AGENTS.md`
  - `.codex/state/results/backend-authz.md`
- Do not edit `web/src/**`.

## Current Findings To Address
- `internal/application/access/management.go` still hard-codes write access through `ensureAdmin`.
- `internal/api/handlers/scopegrant.go` and `internal/application/scopegrant/service.go` are not principal-aware and currently have no explicit authorization enforcement.
- `internal/application/access/catalog.go` builds permission snapshots from `PermissionKeysForRoles(principal.Roles)`, which is static and prevents custom roles from formally controlling menus/APIs.
- Backend services elsewhere authorize with permission keys, so custom delegated roles need a persisted permission-key source that the backend can trust.

## Required Outcomes
- Formalize a persisted console-permission model for roles that can drive backend permission checks and permission snapshots.
- Keep RBAC resource action capabilities and console permission keys coherent. If you split them, make the contract explicit.
- Replace admin-only access-management write paths with explicit permission-key checks where appropriate.
- Add authorization to scope-grant list/create/update/delete.
- Preserve or improve existing ABAC/RBAC behavior for resource actions and scope grants.
- Update backend tests for the new behavior.

## Recommended Contract Direction
- Add explicit role-bound console permission keys rather than relying on static role-name maps.
- Ensure principals or authorization services can resolve effective permission keys for custom roles.
- Define explicit access-center permissions, including manage permissions, instead of only broad view keys.
- Record the final API/data contract in `.codex/state/results/backend-authz.md` for the frontend worker.

## Constraints
- You are not alone in the codebase. Do not revert unrelated edits.
- Coordinate by writing down the final contract; do not assume the frontend guessed it.
