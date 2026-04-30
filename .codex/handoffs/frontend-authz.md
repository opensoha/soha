# Frontend Authz Handoff

## Ownership
- You own frontend implementation only.
- Allowed write scope:
  - `web/src/features/access/**`
  - `web/src/features/auth/**`
  - `web/src/features/settings/**` where permission tabs depend on auth changes
  - `web/src/features/system/**` where permission-gated actions depend on auth changes
  - `web/src/layouts/app-layout.tsx`
  - `web/src/routes/meta.ts`
  - `web/src/types/**`
  - `.codex/state/results/frontend-authz.md`
- Do not edit `internal/**`, `migrations/**`, docs, or `AGENTS.md`.

## Current Findings To Address
- `web/src/features/auth/permission-snapshot.ts` still defines a permissive static role-to-permission fallback and merges it with backend data.
- That fallback can widen access and is incompatible with formal permission delegation.
- Role management UI in `web/src/features/access/access-pages.tsx` is still centered on generic action capabilities and does not yet expose the console permission assignment surface needed to control menus/APIs formally.
- Route, menu, and button visibility need to align with backend-authoritative permission keys and visible-menu decisions.

## Required Outcomes
- Remove permissive frontend widening of permissions. The backend snapshot must become authoritative for authenticated flows.
- Adapt the role-management UI to the backend contract for persisted console permissions.
- Ensure access pages, route guards, menu rendering, and key action surfaces read the new permission model consistently.
- Keep the UI usable for formal permission assignment by admins or delegated managers.
- Run frontend validation appropriate to changed files and record it in `.codex/state/results/frontend-authz.md`.

## Assumed Backend Contract
- Roles will expose persisted console permission keys in addition to any resource-action capability model the backend keeps.
- Permission snapshot remains the frontend source of truth.
- Access-center permissions will likely split view/manage instead of relying on `admin`.

## Constraints
- You are not alone in the codebase. Do not revert unrelated edits.
- If backend contract differs from the assumptions above, adapt and record the delta in your result note.
