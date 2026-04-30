# Authz Docs Update Result

## Status
- Completed

## Scope
- Docs only.
- Updated:
  - `docs/architecture/authorization.md`
  - `docs/en/architecture/authorization.md`
  - `docs/operations/role-authorization-assignment.md`
  - `docs/en/operations/role-authorization-assignment.md`
- No product code changed.
- No docs navigation/config change was needed for this update.

## What Changed
- Rewrote the auth architecture guidance to reflect the landed menu model:
  - visible menus now derive from runtime `permissionKeys` on the common path
  - explicit `menu_role_bindings` are documented as exception/fallback behavior
  - backend ancestor inclusion and permission-snapshot `visibleMenuIds` flow are described
- Reworked the operator runbook around the new default workflow:
  - common case is now `permissionKeys` first, without mandatory manual menu-role binding
  - menu management states `自动派生` / `显式覆盖` / `未映射` are explained as operator signals
  - validation steps now tell operators when to expect both `permissionKeys` and `visibleMenuIds`
- Added explicit exception-path notes so the docs match the current implementation rather than an idealized model.

## Explicit Exception Paths Documented
- On mapped menus, explicit `roleIds` are additive fallback in the current backend; they do not hide a menu from principals who already satisfy the derived permission rule.
- Enabled unmapped menus with no explicit `roleIds` still remain visible for compatibility.
- The frontend menu-management UI can infer derived state from route metadata, but persisted backend menu records still only store `roleIds`.

## Validation
- Ran `git diff --check -- docs/architecture/authorization.md docs/en/architecture/authorization.md docs/operations/role-authorization-assignment.md docs/en/operations/role-authorization-assignment.md`
- Result: clean

## Notes
- `docs/operations/role-authorization-assignment.md` and `docs/en/operations/role-authorization-assignment.md` are currently untracked in the worktree. I updated them in place and did not change their tracking state.
- I did not edit `agents.md` because this worker handoff limited writes to `docs/**` plus this result file. The repo-level engineering memory still contains older menu-visibility wording and may need a separate owner if strict memory sync is required.
