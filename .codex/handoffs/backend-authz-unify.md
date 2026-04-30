# Backend Authz Unify Handoff

## Ownership
- You own backend implementation only.
- Allowed write scope:
  - backend service files that still enforce console/API permissions through `HasPermission(principal.Roles, ...)`
  - directly-related authorization helpers/tests
  - docs/memory only if the contract materially changes again
  - `.codex/state/results/backend-authz-unify.md`
- Do not edit `web/src/**`.

## Review Finding To Fix
- Persisted custom-role `permissionKeys` are not yet consumed uniformly because many services still check permissions through `HasPermission(principal.Roles, ...)`, which relies on the in-process role-permission matrix already being warmed.

## Required Outcomes
- Replace remaining relevant service-level console/API authorization paths with resolver-backed permission evaluation where needed for correctness.
- Keep RBAC resource `capabilities` and ABAC/scoped resource authorization behavior unchanged.
- Preserve existing bootstrap defaults as fallback only, not the authoritative runtime source for custom-role console/API permissions.
- Add/update targeted tests for the changed authorization helpers or services.
- Record changed files, validation, and any residual risks in `.codex/state/results/backend-authz-unify.md`.

## Starting Hints
- Prioritize services surfaced in review and similar helper patterns nearby:
  - `internal/application/announcement/service.go`
  - `internal/application/catalog/service.go`
  - `internal/application/release/service.go`
  - `internal/application/registry/service.go`
  - `internal/application/menu/service.go`
  - `internal/application/monitoring/service.go`
  - `internal/application/settings/service.go`
  - `internal/application/identity/service.go`
  - `internal/api/handlers/upload.go`
- If some paths are intentionally safe to leave on the static/default path, justify that explicitly in the result note.

## Constraints
- You are not alone in the codebase. Do not revert unrelated edits.
- Keep the change focused on uniform permission consumption, not a broad refactor.
