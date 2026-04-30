# Frontend Authz Tail Result

## Changed files
- `web/src/features/auth/permission-catalog.ts`
- `web/src/features/access/access-pages.tsx`
- `web/src/features/access/scope-grants-page.tsx`
- `web/src/routes/meta.ts`

## What changed
- Added missing access-center manage keys and scope-grant view/manage keys to the role permission catalog so role `permissionKeys` assignment matches the backend contract.
- Switched access-center mutation controls to the corresponding `*.manage` keys:
  - users: `access.users.manage`
  - roles: `access.roles.manage`
  - groups: `access.groups.manage`
  - policies: `access.policies.manage`
- Split scope-grant control gating away from user/group mutation gating:
  - embedded scope-grant actions now require `access.scope-grants.manage`
  - standalone scope-grant page visibility now requires `access.scope-grants.view`
  - standalone scope-grant mutations now require `access.scope-grants.manage`
- Updated the scope-grant route metadata permission key to `access.scope-grants.view`.

## Validation
- `npm run typecheck` in `web`: passed
- `npm run build` in `web`: passed

## Residual risks
- No focused frontend test coverage was added for the new permission-key splits, so regressions would currently be caught by manual QA or broader integration coverage.
- Production build still emits an existing Vite chunk-size warning for large bundles; this task did not change chunking strategy.
