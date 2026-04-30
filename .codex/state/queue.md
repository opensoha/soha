# Queue

## Active Tracks

| ID | Owner | Scope | Status | Depends On | Goal |
| --- | --- | --- | --- | --- | --- |
| BACKEND-MENU-DERIVATION | worker | backend menu/domain/repository/service/bootstrap/tests for derived visibility | pending | none | Implement common-path menu visibility derivation from role `permissionKeys` |
| FRONTEND-MENU-MGMT | worker | system menu management UI and directly-related auth/menu frontend files | pending | none | Adapt frontend menu management and visibility assumptions to the derived-menu model |
| AUTHZ-DOCS-UPDATE | worker | docs/operations and docs/architecture auth/menu guidance | pending | none | Update runbook and architecture docs for the new menu derivation workflow |
| MAIN-ORCHESTRATION | main | `.codex/state/**`, `.codex/handoffs/**`, worker coordination, result synthesis | in_progress | all tracks | Coordinate the optimization without editing product code |

## Ownership Rules
- Workers are not alone in the codebase and must not revert unrelated edits.
- Backend, frontend, and docs should stay on disjoint files where practical.
- Main thread owns orchestration artifacts under `.codex/` only.
