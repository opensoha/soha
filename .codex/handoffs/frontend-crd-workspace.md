# Frontend CRD Workspace Handoff

## Task
Turn the CRD page into an operational CRD/custom-resource workspace.

## Files To Read First
- `/Users/shanchui/Downloads/kubecrux/.codex/state/current_task.md`
- `/Users/shanchui/Downloads/kubecrux/web/src/features/platform/extensions-pages.tsx`
- `/Users/shanchui/Downloads/kubecrux/web/src/features/platform/platform-management-pages.tsx`
- `/Users/shanchui/Downloads/kubecrux/web/src/features/platform/configuration-detail-pages.tsx`
- `/Users/shanchui/Downloads/kubecrux/web/src/components/resource-actions.tsx`
- `/Users/shanchui/Downloads/kubecrux/web/src/components/k8s-yaml-editor.tsx`

## Current Findings
- The current CRD page only shows CRD metadata.
- Existing platform pages already provide reusable patterns for list-first tables, delete actions, and YAML-based create flows.
- The frontend should stay generic and must not hardcode custom schemas for arbitrary CRDs.

## Ownership
- You own frontend files only.
- Do not edit backend files.
- You are not alone in the codebase. Do not revert unrelated edits.

## Expected Outcome
- Expand the CRD page so operators can see `kind` clearly and select a CRD.
- Add a second pane/section/table that lists resource instances for the selected CRD.
- Support create, edit, and delete flows for selected CRD-backed resources using the existing YAML editor pattern.
- Keep cluster/namespace scope visible and respect CRD scope:
  - namespaced CRDs should honor namespace selection
  - cluster-scoped CRDs should make namespace irrelevant/disabled in behavior
- Assume the backend will expose CRD-name keyed resource endpoints; if exact route names differ slightly, keep the implementation easy to align.

## Report Back With
1. Sources consulted and exact files read
2. Files changed
3. Key UX decisions and assumptions about backend contract
4. Validation run and results
5. Any integration dependency on backend API shape
