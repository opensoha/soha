# Current Task

## Title
Rebuild the `web` frontend fully on top of `ant-design-pro` conventions, using `old_web` only as a functional and API reference, and adapt the Gin backend accordingly.

## Objective
The migration target is no longer a hybrid shell or a direct carry-over of old page structure.

The new `web` must be rebuilt according to `ant-design-pro` design and project conventions:
- Pro layout
- Pro routing and access model
- Pro page organization
- Pro-style list / form / workspace composition

`old_web` is now reference material only:
- feature scope reference
- interaction reference
- API usage reference
- permission and route semantics reference

It must not remain the basis of the new page structure or shell implementation.

## Desired Direction
- Keep `web` as the only active frontend target.
- Keep `old_web` as a read-only migration reference.
- Rebuild primary pages in Pro-native structure instead of wrapping old pages wholesale.
- Preserve product scope and backend integration, but not old page layouts or old shell composition.

## Required Outcomes
1. Frontend rebuild
   - use `ant-design-pro` runtime, route config, layout, and page patterns as the baseline
   - remove scaffold demos, mocks, and example pages that are irrelevant to kubecrux
   - rebuild kubecrux entry pages and navigation in Pro style
2. Functional continuity
   - preserve current product scope across platform, delivery, observability, access, system, settings, and auth
   - preserve current permission/menu semantics and scope behavior
   - migrate page logic from `old_web` selectively, not page structure wholesale
3. Backend adaptation
   - adapt Gin backend bootstrap/auth/menu contracts where needed for the Pro runtime
   - keep same-origin `/api/v1` integration
4. Validation and memory
   - run targeted frontend build/type validation
   - run targeted backend tests where contracts changed
   - update `AGENTS.md` and related frontend docs

## Phase 2 Focus

The scaffold replacement baseline is already in place.

Current execution focus is now:
- clear `web` TypeScript errors until `npm run tsc` passes
- keep `npm run build` passing
- move the current route entry pages beyond "thin wrappers" toward fuller Pro-native pages
- prioritize:
  - platform entry pages first
  - then delivery, observability, AI observe, access, system, and settings entry pages

`old_web` remains reference only.

## Frontend Rebuild Specification
1. Structure baseline
   - `web/config/**`, `web/src/app.tsx`, `web/src/access.ts`, `web/src/pages/**`, `web/src/components/**` follow Pro conventions first
   - kubecrux-specific shared code may live under `web/src/features/**`, `web/src/services/**`, `web/src/stores/**`, `web/src/utils/**`, but Pro page entry points should own route composition
2. Design rule
   - no preservation of old shell visuals
   - no preservation of old route wrapper structure unless technically necessary
   - new pages should read like a Pro-based enterprise console, not a transplanted Vite app
3. Migration rule
   - use `old_web` to extract:
     - API shapes
     - feature logic
     - permission semantics
     - scope behavior
   - do not port old shell/layout code unless a small internal utility is still useful
4. Backend rule
   - Gin backend may add bootstrap or aggregation endpoints to support Pro runtime startup and reduce frontend round trips

## Hard Requirements
- Main thread must orchestrate and keep `.codex/state/**` current before spawning child threads.
- Workers are not alone in the codebase and must not revert unrelated edits.
- Frontend and backend tasks must stay clearly separated.
- `old_web` must remain available as migration reference until the new `web` is stable.

## Definition Of Done
- `web` is a Pro-native kubecrux frontend, not a wrapped copy of `old_web`.
- `old_web` remains only as migration reference.
- Demo scaffold content is removed or replaced.
- Gin backend supports the rebuilt Pro frontend startup and access flow.
- Memory/docs are updated in the same task.
