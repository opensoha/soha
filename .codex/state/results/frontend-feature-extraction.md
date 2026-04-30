# FRONTEND-FEATURE-EXTRACTION

## Summary

Extracted shared kubecrux auth/runtime logic into reusable modules in the new `web` app, using `old_web` only as a behavioral reference.

The work focused on reusable logic and adapters rather than page rebuilding:

- centralized auth/bootstrap API shapes and session helpers in `web/src/features/auth/auth-api.ts`
- centralized Pro runtime bootstrap/loading helpers in `web/src/features/auth/runtime.ts`
- rewired existing runtime and auth pages to consume the shared adapters instead of embedding request logic
- exposed the permission snapshot query key for reuse and added a cache-aware fallback in the auth guard

## Files Touched

- `web/src/features/auth/auth-api.ts`
- `web/src/features/auth/runtime.ts`
- `web/src/features/auth/permission-snapshot.ts`
- `web/src/features/auth/auth-guard.tsx`
- `web/src/features/auth/login-page.tsx`
- `web/src/features/auth/oidc-callback-page.tsx`
- `web/src/services/api-client.ts`
- `web/src/app.tsx`

## Functional Notes

- bootstrap parsing, branding normalization/application, auth provider loading, password login, OIDC code exchange, and refresh-token session renewal are now reusable business logic instead of being split across runtime and page code
- request interceptor token lookup now reuses store-backed helpers instead of direct `localStorage` parsing inside `app.tsx`
- the auth guard can now reuse cached permission snapshot data when the query result is temporarily absent after initial load

## Validation

Attempted:

- `npm test -- --runInBand src/routes/meta.test.ts`
- `npx vitest run src/features/access/access-authz.test.tsx src/routes/meta.test.ts src/routes/pro-route-manifest.test.ts`
- `npm run tsc`

Blocked by local dependency/tooling state:

- `jest: command not found`
- Vitest install is incomplete in this workspace: missing `@vitest/utils`
- TypeScript config references Jest types, but `@types/jest` is not installed: `TS2688 Cannot find type definition file for 'jest'`

## Risks

- `fetchBootstrapState` currently uses the store token directly and does not attempt refresh-on-bootstrap; if the Pro runtime boots with an expired access token and a valid refresh token, the current flow still redirects to login instead of retrying bootstrap after refresh
- auth logic is now centralized, so any backend contract drift in `/auth/bootstrap`, `/auth/login`, `/auth/refresh`, or `/auth/oidc/exchange` will affect both runtime startup and login flows

## Recommended Next Step

Add a targeted auth-runtime test surface around `auth-api.ts` and `runtime.ts` once the frontend test dependencies are repaired, especially for expired-token bootstrap, refresh success/failure, and branding normalization behavior.
