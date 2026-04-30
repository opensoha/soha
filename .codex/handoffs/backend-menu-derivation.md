# Backend Menu Derivation Handoff

## Ownership
- You own backend implementation only.
- Allowed write scope:
  - `internal/application/menu/**`
  - `internal/repository/menu/**`
  - `internal/domain/menu/**`
  - `internal/bootstrap/**` if seed/default behavior must change
  - directly-related tests under backend scope
  - `.codex/state/results/backend-menu-derivation.md`
- Do not edit `web/src/**` or docs.

## Goal
- Implement a common-path model where menu visibility can be derived from persisted role `permissionKeys` rather than always requiring explicit `menu_role_bindings`.
- Preserve a compatible escape hatch for explicit menu overrides if still needed.

## Constraints
- Keep permission snapshot behavior coherent.
- Do not widen into unrelated auth refactors.
- Update backend tests for visible-menu derivation.
