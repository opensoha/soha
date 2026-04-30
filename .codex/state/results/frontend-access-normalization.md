# FRONTEND-ACCESS-NORMALIZATION

## Scope
- Finished Access-domain TanStack Query normalization in the active Access feature workspace.
- Limited ownership to `web/src/features/access/**` query/list/invalidation cleanup and related helper alignment.
- Kept access routes, permission keys, and backend contracts unchanged.

## What Changed
- Expanded `web/src/features/access/queries.ts` with `accessPoliciesQueryKey` and `accessPoliciesQueryOptions()` so the policies workspace now shares the same helper/export pattern as users, roles, teams, and scope grants.
- Reworked `useCRUD` inside `web/src/features/access/access-pages.tsx` to accept explicit list-query option builders plus targeted invalidation keys instead of deriving list ownership from resource strings.
- Switched `AccessRolesPage`, `AccessTeamsPage`, and `AccessPoliciesPage` onto that helper-driven CRUD path.
- Replaced the remaining local string-key scope-grant list query and invalidations in `ScopeGrantManager` with:
  - `accessScopeGrantsQueryOptions`
  - `accessScopeGrantsQueryKey`
- Replaced the supporting delivery master-data lookups inside `ScopeGrantManager` with shared delivery helpers:
  - `deliveryBusinessLinesQueryOptions`
  - `deliveryEnvironmentsQueryOptions`
  - `deliveryApplicationsQueryOptions`
- Kept `AccessUsersPage` on the already shared access helper pattern and aligned its invalidations with `accessUsersQueryKey`.
- Kept `web/src/pages/access.tsx` unchanged because it was already consuming shared Access list query helpers for workspace stats.

## Normalized Query Areas
- Access users list and user CRUD invalidation
- Access roles list and role CRUD invalidation
- Access teams list and team CRUD invalidation
- Access policies list and policy CRUD ownership
- Access scope grants list and scope-grant CRUD invalidation inside the shared modal manager
- Access scope-grant supporting business-line, environment, and application lookups

## Behavior Notes
- No route paths changed.
- No permission keys changed.
- No backend API paths or payload contracts changed.
- This task only normalizes cache-key/query-helper ownership; it does not redesign Access page UX or backend authorization semantics.

## Validation
- Ran `cd web && npm run tsc`
- Result: passed

## Risks
- `useCRUD` still posts/puts/deletes by path string; list ownership is now normalized, but write endpoints are not yet wrapped in a domain mutation helper layer.
- `ScopeGrantManager` still filters the full scope-grant list client-side for the active user or team. Cache ownership is shared now, but the data shape still assumes a broad list endpoint.
- Access and delivery helper modules still expose lightweight inline result shapes rather than a stricter exported contract package.

## Recommended Next Step
- Add shared Access mutation helpers or invalidation utilities so user/role/team/policy/scope-grant writes stop repeating per-page `useMutation` success handlers while preserving the same explicit query-key ownership.
