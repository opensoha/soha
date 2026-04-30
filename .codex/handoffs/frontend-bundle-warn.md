# Frontend Bundle Warning Handoff

## Ownership
- You own only the bounded `web` bundling-warning reduction task.
- Allowed write scope:
  - `web` build config
  - route/module loading files directly related to chunking
  - small supporting frontend files needed for chunk splitting
  - `.codex/state/results/frontend-bundle-warn.md`
- Do not touch backend files or auth-page logic unless truly required for chunk splitting.

## Goal
- Investigate the current Vite chunk-size warning and apply the smallest reasonable fix.
- Prefer explicit code-splitting or manual chunking over broad rewrites.
- If the warning cannot be removed cleanly in scope, document the concrete blocker and remaining warning.

## Constraints
- Keep changes bounded and reversible.
- Run frontend build validation and record whether the warning was removed, reduced, or remains.
