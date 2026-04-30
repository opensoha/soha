# FRONTEND-ACTIVE-WIDGET-WARNINGS

## Scope

- Owned only active widget/build-warning cleanup under `web/src/components/**`
- Did not modify unrelated `@umijs/max` route/runtime migration files

## Changes Made

- Updated `web/src/components/k8s-yaml-editor.tsx`
  - moved Monaco worker registration into the editor `beforeMount` path so workers exist before Monaco bootstraps
  - kept `monaco-yaml` as a single configured instance and reused it through `update(...)`
  - added widget-local cleanup to dispose the `monaco-yaml` handle on unmount
  - kept YAML schema behavior unchanged

## Warnings Removed

- Removed the active Monaco worker initialization warning path in the YAML editor
  - previous code configured `window.MonacoEnvironment.getWorker` in a React effect after `Editor` creation
  - new code configures workers before Monaco mount, which is the correct load order for `@monaco-editor/react`

## Validation

- `cd web && npm run build`
  - passes
  - remaining console notice is the standard `Browserslist: caniuse-lite is outdated` environment notice, not a widget code warning
- `cd web && npm run tsc`
  - still fails outside this worker scope
  - current blocker is the repo-wide `@umijs/max` migration state, beginning with:
    - `config/config.ts(4,10): error TS2305: Module '"@umijs/max"' has no exported member 'defineConfig'.`
  - this track did not widen into shell/router/runtime migration ownership

## Risks

- Monaco worker warning removal was validated through build success and code-path correction, but not through an interactive browser console capture in this worker
- `pod-log-viewer.tsx`, `pod-terminal.tsx`, and `web/src/typings.d.ts` did not require source edits for the active warning cleanup identified here

## Next Step

- Hand the remaining `npm run tsc` failures to the owner of the `@umijs/max` runtime/config migration so repo-level typecheck can become green again.
