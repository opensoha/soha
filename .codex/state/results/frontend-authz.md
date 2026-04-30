# Frontend Authz Result

## Changed files
- `web/src/features/auth/permission-snapshot.ts`
- `web/src/features/auth/permission-catalog.ts`
- `web/src/features/access/access-pages.tsx`
- `web/src/features/access/scope-grants-page.tsx`

## What changed
- Removed the frontend static role-to-permission fallback and stopped merging client-derived permissions into the backend snapshot.
- Kept authenticated route/menu/action authorization dependent on `/access/permission-snapshot` only.
- Extended the role-management UI to display and submit `permissionKeys` alongside existing `capabilities`.
- Added a frontend permission catalog so the role form exposes the real console permission-key surface instead of free-form guessing.
- Tightened access-center action visibility so create/edit/delete controls are not shown blindly when the page is merely visible.

## Actual contract findings
- Authoritative snapshot contract is present and stable on the frontend path:
  - `PermissionSnapshot` returns `permissionKeys`, `visibleMenuIds`, and `visibleMenus`.
- Role DTO/domain types already include `permissionKeys`.
- In this checkout, backend role write handling still drops `permissionKeys` before persistence:
  - `internal/api/dto/access.go` accepts `permissionKeys`.
  - `internal/domain/access/models.go` includes `RoleRecord.PermissionKeys` and `RoleInput.PermissionKeys`.
  - `internal/api/handlers/access.go` `mapRoleInput(...)` currently forwards only `id`, `name`, `scope`, and `capabilities`.

## Validation
- `pnpm exec tsc --noEmit` in `web/`: passed
- `pnpm exec vite build` in `web/`: passed

## Residual risks
- Role permission assignment UI is now forward-compatible, but persisted `permissionKeys` will not round-trip until the backend write path and repository/storage path are completed.
- Access-center mutation gating currently falls back to the existing `*.view` keys because this checkout does not yet expose distinct `access.*.manage` permission keys in the frontend-visible contract.
