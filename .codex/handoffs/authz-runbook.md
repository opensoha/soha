# Authz Runbook Handoff

## Ownership
- You own documentation and process guidance only.
- Allowed write scope:
  - `docs/operations/**`
  - `docs/architecture/authorization.md` only if a cross-link or short clarification is needed
  - `AGENTS.md` only if the repo memory needs an operational rule note
  - `.codex/state/results/authz-runbook.md`
- Do not edit product code.

## Goal
- Publish a formal operator checklist for role authorization assignment.
- The checklist must explain that menu/page/API access depends on both:
  - role `permissionKeys`
  - menu-role bindings
- Include:
  - prerequisites
  - step-by-step assignment flow
  - validation flow
  - common failure modes
  - at least one optimization proposal for simplifying this dual-gate model long-term

## Constraints
- Keep it practical and directly usable by operators/admins.
- Prefer a docs/operations runbook over abstract architecture prose.
