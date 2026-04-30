# Backend Authz Harden Handoff

## Ownership
- You own backend hardening only.
- Allowed write scope:
  - backend services/handlers/helpers involved in resolver fallback behavior
  - directly-related backend tests
  - docs/memory only if the hardening changes guidance materially
  - `.codex/state/results/backend-authz-harden.md`
- Do not edit frontend files.

## Goal
- Reduce or eliminate nil-resolver fallback exposure in runtime-facing console/API authorization.
- Normal runtime authorization should not depend on a warmed in-process static matrix for custom-role console/API permission correctness.
- If any fallback remains, document where and why.

## Constraints
- Keep RBAC action capabilities and ABAC/resource authorization unchanged.
- Keep changes bounded to console/API permission enforcement, not a broad auth redesign.
