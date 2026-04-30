# Backend Menu Derivation Result

## Status
- Completed

## Scope
- Implemented backend common-path menu visibility derivation in `internal/application/menu`.
- Added focused backend tests for derived visibility, ancestor inclusion, and explicit role-binding fallback.
- Did not edit `web/src/**` or docs.

## Changes
- Added `internal/application/menu/visibility.go` with a static menu-to-permission derivation table aligned to the current backend/frontend menu contract.
- Updated `internal/application/menu/service.go` so `ListVisible` now:
  - resolves runtime `permissionKeys` from the persisted role matrix
  - grants visibility automatically for mapped menus when the principal has the corresponding permission
  - preserves explicit `menu_role_bindings` as a fallback/escape hatch
  - preserves currently-unmapped menus without forcing new bindings
  - keeps ancestors visible when a child menu is visible
- Added `internal/application/menu/service_test.go`.

## Validation
- Ran:
  - `gofmt -w internal/application/menu/service.go internal/application/menu/visibility.go internal/application/menu/service_test.go`
  - `go test ./internal/application/menu ./internal/application/access`
- Result:
  - `ok github.com/kubecrux/kubecrux/internal/application/menu`
  - `ok github.com/kubecrux/kubecrux/internal/application/access`

## Notes
- The derivation table is intentionally bounded to the current known menu contract. Menus not yet mapped still keep prior behavior unless explicit bindings are absent, in which case they remain visible as before.
- Repo-level memory/docs updates remain for the docs/authz coordination track.
