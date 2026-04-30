# Frontend Authz Tests Handoff

## Ownership
- You own focused frontend test work only.
- Allowed write scope:
  - frontend test files for auth/access route and page gating
  - minimal directly-related helper files if required by the tests
  - `.codex/state/results/frontend-authz-tests.md`
- Avoid touching bundling config or broad page implementation unless a tiny testability hook is unavoidable.

## Goal
- Add focused tests for:
  - access-center read vs manage permission splits
  - scope-grant page `view` vs `manage` splits
  - route visibility or guard behavior where the new permission keys matter

## Constraints
- Do not edit backend files.
- Do not widen into UI redesign.
- Run targeted frontend validation/tests and record the exact commands and results.
