# BACKEND-PRO-ADAPTATION Result

## Changed files
- `internal/api/handlers/auth.go`
- `internal/api/handlers/auth_test.go`
- `internal/api/routes/router.go`
- `internal/bootstrap/app.go`

## Exact API changes
- Added authenticated bootstrap endpoint: `GET /api/v1/auth/bootstrap`
- Response shape:
  - top-level envelope remains `{ "data": ... }`
  - `data.user`: existing principal payload
  - `data.currentUser`: alias of the same principal payload for ant-design-pro style runtime bootstrap
  - `data.permissionSnapshot`: existing permission snapshot model embedded in bootstrap
  - `data.branding`: existing branding settings model embedded in bootstrap
- Existing endpoints were not removed or changed:
  - `GET /api/v1/auth/me`
  - `GET /api/v1/access/permission-snapshot`
  - `GET /api/v1/settings/branding`
  - `GET /api/v1/menus/visible`

## Validation
- Ran: `go test ./internal/api/handlers ./internal/application/access ./internal/api/routes ./internal/bootstrap`
- Result: passed

## Risks
- Frontend scaffold work may expect a different field naming convention than `permissionSnapshot`; backend currently provides the nested object exactly under that key, but the in-progress Pro runtime integration still needs to consume it explicitly.
- `GET /api/v1/auth/bootstrap` aggregates auth, access, and branding reads into one request; if branding or permission snapshot authorization changes later, bootstrap behavior will inherit that coupling.

## Next step
- Frontend Pro scaffold should switch initial-state hydration to `GET /api/v1/auth/bootstrap` and verify that menu/access bootstrap uses `data.currentUser`, `data.permissionSnapshot`, and `data.branding` without extra round trips.
