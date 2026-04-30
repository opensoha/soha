# Frontend Bundle Warning Result

## Changed Files
- `web/vite.config.ts`

## What Changed
- Kept the existing manual chunking approach and added a narrow VisActor split only.
- Split `@visactor/react-vchart`, `@visactor/vchart` / `@visactor/vchart-extension`, `@visactor/vrender-*`, and supporting VisActor runtime packages into separate lazy vendor chunks.
- Left route structure and feature code untouched.

## Build Validation
- Command: `pnpm --dir web build`
- Result: passed
- Outcome: the previous Vite chunk-size warning was removed

## Observed Chunk Impact
- Before:
  - shared async chunk `download-*.js` was about `1494 kB` and triggered the warning
- After first bounded split:
  - `download-*.js` dropped to about `50 kB`
  - warning moved to a single `vchart-*.js` lazy vendor chunk at about `1444 kB`
- Final:
  - `vchart-react-*.js` about `10 kB`
  - `vchart-runtime-*.js` about `94 kB`
  - `vchart-core-*.js` about `553 kB`
  - `vchart-render-*.js` about `791 kB`
  - all emitted chunks are now under the configured `800 kB` warning limit

## Residual Risks
- The fix is config-only and reversible, but it increases the number of lazy vendor requests for metrics-heavy pages.
- The VisActor graph is still large overall; this change reduces warning pressure without reducing total chart runtime cost.
- If metrics/chart usage expands further, route-level dynamic imports around chart-bearing panels may still be worth considering later.
