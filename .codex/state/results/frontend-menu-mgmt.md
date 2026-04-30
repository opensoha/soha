# Frontend Menu Management Result

## Scope
- Frontend only.
- Updated `web/src/features/system/system-pages.tsx`.
- Added focused coverage in `web/src/features/system/system-pages.test.tsx`.

## Implemented
- Adapted the system menu-management page to explain the new default visibility model: menu visibility now derives from route `permissionKeys` in the common path.
- Added a `可见性策略` column that distinguishes:
  - `自动派生` for route-backed menus with known permission mappings
  - `显式覆盖` when menu role overrides are in use
  - `未映射` when no known frontend permission mapping exists and an explicit override may still be needed
- Updated the edit/create modal to support bounded operator control:
  - `可见性模式` switch between derived visibility and explicit override
  - read-only display of derived permission keys when available
  - role selector only when explicit override mode is chosen
- Kept transitional compatibility with current backend payloads by continuing to consume and submit `roleIds` only for explicit override mode.
- Added helper coverage for derived permission resolution and explicit/unmapped fallback behavior.

## Validation
- `npm test -- src/features/system/system-pages.test.tsx`
- `npm run typecheck`

## Residual Risk
- The frontend infers derived visibility from current route metadata unless the backend starts returning `derivedPermissionKeys` and/or `visibilityMode`, which this UI will also consume if present.
- Final operator wording assumes the backend track preserves explicit override semantics while making permission-derived visibility the default path.
