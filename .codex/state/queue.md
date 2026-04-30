# Queue

## Active Tracks

| ID | Owner | Scope | Status | Depends On | Goal |
| --- | --- | --- | --- | --- | --- |
| FRONTEND-PRO-REBUILD | main | new `web/**` shell decisions, integration decisions, `.codex/state/**`, validation orchestration | in_progress | none | Drive the current page-migration wave and keep the rebuilt Pro baseline coherent |
| FRONTEND-PLATFORM-DETAIL-OWNERSHIP | worker | active `web/src/pages/platform/**`, selected `web/src/features/platform/**` detail and placeholder routes | pending | FRONTEND-PRO-REBUILD | Decide and implement which platform detail/placeholder pages stay standalone versus fold into Pro shells |
| FRONTEND-ACCESS-NORMALIZATION | worker | active `web/src/features/access/**`, relevant access entry pages under `web/src/pages/**`, shared access query helpers | pending | FRONTEND-PRO-REBUILD | Finish normalizing the Access workspace onto shared query/invalidation patterns |
| FRONTEND-NONPLATFORM-PAGE-DEEPENING | worker | active non-platform entry pages under `web/src/pages/{delivery,observability,ai-observe,system,settings}.tsx` and close helper files | pending | FRONTEND-PRO-REBUILD | Deepen non-platform entry pages from shell wrappers into fuller Pro-native pages |
| BACKEND-GIN-ADAPTATION | worker | `internal/api/**`, `internal/bootstrap/**`, related backend tests | pending | none | Keep Gin bootstrap/auth/menu/API behavior aligned while frontend deepens |
| MEMORY-DOCS | main | `AGENTS.md`, docs, result synthesis | pending | all tracks | Keep engineering memory aligned with the full Pro rebuild strategy |

## Ownership Rules
- Workers are not alone in the codebase and must not revert unrelated edits.
- Main thread owns final frontend architecture decisions inside the new `web`.
- `FRONTEND-PLATFORM-DETAIL-OWNERSHIP` owns only active platform detail/placeholder page ownership and shell integration decisions.
- `FRONTEND-ACCESS-NORMALIZATION` owns only Access-domain query/invalidation normalization.
- `FRONTEND-NONPLATFORM-PAGE-DEEPENING` owns only non-platform active page deepening beyond shell wrappers.
- `BACKEND-GIN-ADAPTATION` owns only Gin/backend compatibility work and tests.
