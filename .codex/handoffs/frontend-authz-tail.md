# Frontend Authz Tail Handoff

## Ownership
- You own frontend implementation only.
- Allowed write scope:
  - `web/src/features/auth/permission-catalog.ts`
  - `web/src/features/access/access-pages.tsx`
  - `web/src/features/access/scope-grants-page.tsx`
  - `web/src/routes/meta.ts`
  - directly-related frontend type/test files only if needed
  - `.codex/state/results/frontend-authz-tail.md`
- Do not edit `internal/**`, `migrations/**`, docs, or `AGENTS.md`.

## Review Findings To Fix
- Permission catalog does not expose:
  - `access.users.manage`
  - `access.roles.manage`
  - `access.groups.manage`
  - `access.policies.manage`
  - `access.scope-grants.view`
  - `access.scope-grants.manage`
- Access-center pages still derive `canManage*` from the `*.view` keys.
- Scope-grant route/page is still tied to `access.users.view` instead of `access.scope-grants.view`.

## Required Outcomes
- Role UI can assign the full access-center and scope-grant permission-key surface.
- Mutation buttons and scope-grant controls are shown only with the corresponding `*.manage` permission.
- Scope-grant visibility becomes independently assignable.
- Run frontend validation and record changed files, validation, and residual risks in `.codex/state/results/frontend-authz-tail.md`.

## Constraints
- You are not alone in the codebase. Do not revert unrelated edits.
- Backend contract for these keys already exists; consume it rather than inventing a new one.
