# Authz Runbook Result

## Changed Files

- `docs/operations/role-authorization-assignment.md`
- `docs/en/operations/role-authorization-assignment.md`
- `docs/architecture/authorization.md`
- `docs/en/architecture/authorization.md`
- `docs/.vitepress/config.ts`

## Summary

- Published a formal operator runbook for role authorization assignment in Chinese and English.
- The runbook now standardizes prerequisites, the end-to-end assignment checklist, session refresh expectations, validation flow, and common failure modes.
- The checklist explicitly documents the current dual gate:
  - role `permissionKeys`
  - menu-role visibility bindings
- The runbook also calls out the third gate for scoped platform data:
  - ABAC policy and `scope grants`
- Added architecture-page cross-links so operators can move from the authorization model to the operational checklist directly.
- Added docs navigation entries so the runbook is discoverable under `operations`.

## Long-Term Optimization Guidance Captured

- Derive menu visibility from `permissionKeys` for the common path.
- Build a single authorization matrix for `route/menu/api -> permissionKeys -> scope requirement`.
- Separate page/API gates from platform resource scope gates more explicitly.

## Notes

- This track stayed within docs/process scope only.
- No product code was edited.
