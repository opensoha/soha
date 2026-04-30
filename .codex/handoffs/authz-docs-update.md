# Authz Docs Update Handoff

## Ownership
- You own docs/process only.
- Allowed write scope:
  - `docs/operations/**`
  - `docs/architecture/**`
  - optional docs navigation/config files if needed
  - `.codex/state/results/authz-docs-update.md`
- Do not edit product code.

## Goal
- Update the runbook and architecture docs for the optimized model:
  - default menu visibility derives from `permissionKeys`
  - explicit menu-role bindings are only for override/exception cases if the backend keeps them
- Clarify the new operator workflow and validation steps.

## Constraints
- Keep docs practical and concise.
- Note any unresolved exception paths explicitly.
