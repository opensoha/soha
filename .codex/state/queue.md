# Queue

## Active Tracks

| ID | Owner | Scope | Status | Depends On | Goal |
| --- | --- | --- | --- | --- | --- |
| FRONTEND-PRO-SCAFFOLD | main | `web/package.json`, `web/config/**`, `web/src/app.tsx`, `web/src/access.ts`, `web/src/requestErrorConfig.ts`, `web/src/pages/**`, `web/src/components/**` needed for Pro shell bootstrap | in_progress | none | Land the ant-design-pro runtime baseline inside `web` |
| FRONTEND-PRO-ROUTES | worker | `web/src/routes/**`, Pro route-manifest mapping files, route wrappers, menu/access mapping glue | pending | FRONTEND-PRO-SCAFFOLD | Map current route/permission/menu semantics into Pro navigation |
| FRONTEND-PRO-FEATURES | worker | `web/src/features/**`, feature-level wrappers/adapters, login/auth presentation migration, scoped page container conversion | pending | FRONTEND-PRO-SCAFFOLD | Re-host existing feature pages inside Pro page patterns while preserving behavior |
| BACKEND-PRO-ADAPTATION | worker | `internal/api/**`, `internal/application/**`, backend tests directly related to auth/menu/bootstrap adaptation | pending | none | Reconfirm and adapt backend contracts needed by the Pro runtime bootstrap |
| MEMORY-DOCS | main | `AGENTS.md`, `docs/development/frontend-structure.md`, handoffs/results synthesis | pending | all tracks | Keep engineering memory aligned with the scaffold migration |

## Ownership Rules
- Workers are not alone in the codebase and must not revert unrelated edits.
- Main thread owns `.codex/state/**` and integration decisions.
- `FRONTEND-PRO-SCAFFOLD` owns the initial Pro runtime bootstrap and should avoid broad edits in business feature modules.
- `FRONTEND-PRO-ROUTES` owns route manifest translation and permission/menu glue, not backend contracts.
- `FRONTEND-PRO-FEATURES` owns feature re-hosting and page container adaptation, not core runtime bootstrap.
- `BACKEND-PRO-ADAPTATION` owns any API/bootstrap compatibility work and must report exact contract changes.
