# FRONTEND-PRO-ROUTES Result

## Changed files
- `web/src/routes/pro-route-manifest.ts`
- `web/src/routes/pro-route-manifest.test.ts`

## What changed
- Added a Pro-facing route manifest adapter derived from `web/src/routes/meta.ts` so the existing route registry remains the source of truth for menu structure, redirects, and inherited permission/menu semantics.
- Added helpers to produce both a filtered Pro menu tree and a `route:<id>` access map for Pro runtime `access` integration.
- Added tests covering parent-child manifest nesting, access-filtered children, and generated access flags.

## Risks
- The current adapter only emits menu-visible route nodes. Detail and hidden routes still need a separate runtime route registration strategy from the scaffold owner.
- Icon names are passed through as existing string ids from `routeMeta`; the scaffold thread still needs to decide whether to translate them to Pro-compatible icon components or ignore them for now.

## Recommended next step
- Wire `getProRouteManifest()` and `buildProRouteAccessMap()` into the new Pro scaffold runtime once `web/src/app.tsx` and `web/src/access.ts` are stable, then add one integration test proving the backend permission snapshot drives Pro menu visibility end to end.
