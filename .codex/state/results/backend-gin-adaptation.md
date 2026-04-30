# BACKEND-GIN-ADAPTATION

## Scope
- Adapted Gin auth/startup compatibility behavior for the rebuilt ant-design-pro frontend.
- Kept changes limited to `internal/api/**` plus directly related tests.

## Implemented Changes
- Normalized bearer token handling in principal middleware so `Authorization: Bearer <token>` is parsed and stored as the raw token value before auth/logout flows use it.
- Added Pro/scaffold-compatible auth endpoints:
  - `POST /api/login/account`
  - `POST /api/login/outLogin`
  - `GET /api/currentUser`
  - `GET /api/currentUserDetail`
  - `GET /api/accountSettingCurrentUser`
- Kept `/api/v1/auth/bootstrap` as the primary Pro runtime startup endpoint and confirmed it continues to return:
  - `currentUser`
  - `user`
  - `permissionSnapshot`
  - `branding`
- Added a Pro-compatible login response shape for `/api/login/account`:
  - `{ "status": "ok", "type": "...", "currentAuthority": "admin|user" }`
- Added a Pro-compatible current-user response mapper for remaining scaffold account utilities.
- Added a Pro-compatible logout response shape for `/api/login/outLogin`:
  - `{ "success": true }`

## Validation
- `gofmt -w` run on touched backend files.
- Passed:
  - `go test ./internal/api/handlers ./internal/api/middleware`
- Blocked:
  - `go test ./internal/api/routes`
  - Failure reason: `web/embed.go` expects embedded frontend build artifacts (`dist`) that are not present in the current worktree.

## Risks
- The compatibility current-user mapper returns placeholder Pro-profile fields (`signature`, `address`, `phone`, `geographic`, empty `notice`) because the backend domain model does not yet persist a richer profile object for those scaffold pages.
- `/api/login/account` now authenticates successfully for Pro compatibility but only returns scaffold-style authority metadata, not tokens. The rebuilt kubecrux login page should continue using `/api/v1/auth/login`.

## Recommended Next Step
- After the frontend rebuild settles, remove remaining Pro scaffold callers of `/api/currentUser*` and `/api/login/*` so the backend can collapse back to the versioned `/api/v1/auth/*` contract only.
