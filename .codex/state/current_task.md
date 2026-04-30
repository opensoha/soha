# Current Task

## Title
Optimize authorization by deriving menu visibility from `permissionKeys` for the common path.

## Objective
The current authorization model is operational but still carries dual-maintenance overhead:
- role `permissionKeys` gate page/API access
- `menu_role_bindings` gate sidebar/menu visibility

This task should reduce configuration drift by introducing a common-path model where menu visibility can be derived from route/menu `permissionKeys`, while still preserving explicit overrides for exceptional cases if needed.

## Desired Direction
- Menu visibility should not require a second manual role-binding step for the normal case when a menu maps directly to a known permission key.
- Roles with the right `permissionKeys` should automatically surface the matching menus unless the menu is explicitly configured otherwise.
- The final model must remain auditable and compatible with existing route metadata and permission snapshot behavior.

## Required Outcomes
1. Backend/menu model
   - define and implement a derivation rule for visible menus from `permissionKeys`
   - preserve compatibility for menus that still need explicit role bindings or override behavior
   - keep permission snapshot and visible menu output coherent
2. Frontend/system menu management
   - adapt the menu management surface to the new derived/explicit visibility model
   - avoid confusing operators with a stale “always bind roles manually” workflow
3. Documentation/runbook
   - update the authz runbook and architecture guidance to explain the optimized flow
   - document which cases are automatic and which still require explicit menu overrides
4. Validation
   - add/update focused tests for derived menu visibility behavior
   - run targeted backend/frontend validation for changed files

## Hard Requirements
- Main thread remains orchestration-only.
- Frontend and backend execution stay split across subagents with disjoint ownership.
- Do not revert unrelated user changes.
- Preserve existing access/ABAC behavior outside this menu-visibility optimization.

## Definition Of Done
- A role with the necessary `permissionKeys` can see the corresponding menu in the common case without a separate manual menu-role binding step.
- Explicit menu override behavior, if retained, is documented and test-covered.
- Operator documentation clearly states the new default workflow.
