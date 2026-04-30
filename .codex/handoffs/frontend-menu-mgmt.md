# Frontend Menu Management Handoff

## Ownership
- You own frontend implementation only.
- Allowed write scope:
  - `web/src/features/system/**` where menu management UI lives
  - directly-related menu/auth frontend helpers if required
  - `.codex/state/results/frontend-menu-mgmt.md`
- Do not edit backend files or docs.

## Goal
- Adapt the frontend menu management surface to the new derived-menu visibility model.
- Operators should understand when menu visibility is automatic from `permissionKeys` and when an explicit override still applies.

## Constraints
- Keep the UX bounded and consistent with existing system-management patterns.
- Run targeted frontend validation and summarize residual risks.
