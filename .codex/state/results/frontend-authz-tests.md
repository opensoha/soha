# Frontend Authz Tests Result

## Changed Files
- `web/src/routes/meta.test.ts`
- `web/src/features/access/access-authz.test.tsx`

## Implemented Coverage
- Added route-permission tests for the access center parent route using `permissionStrategy: any-child`.
- Added route-permission coverage for the dedicated `access.scope-grants.view` path.
- Added jsdom page tests for access-center tab visibility under split `view` permissions.
- Added jsdom page tests for scope-grant `view` vs `manage` behavior, including create-action visibility.

## Commands Run
1. `npm test -- --run src/routes/meta.test.ts src/features/access/access-authz.test.tsx`
2. `npm run typecheck`

## Results
- `npm test -- --run src/routes/meta.test.ts src/features/access/access-authz.test.tsx`
  - passed
  - `2` test files, `8` tests passed
- `npm run typecheck`
  - passed

## Residual Risks
- The targeted page test uses lightweight module mocks for `AdminTable`, `api`, and `usePermissionSnapshot`, so it verifies authz gating behavior without exercising full table rendering or real network integration.
- The test run still emits Ant Design deprecation warnings from existing product code (`Alert.message`, `Modal.destroyOnClose`, `Modal.maskClosable`). They do not fail the suite and were not changed here because this worker owns frontend authz tests only.
