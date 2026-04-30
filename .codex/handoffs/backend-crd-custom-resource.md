# Backend CRD Custom Resource Handoff

## Task
Implement backend support for CRD-backed custom-resource operations.

## Files To Read First
- `/Users/shanchui/Downloads/kubecrux/.codex/state/current_task.md`
- `/Users/shanchui/Downloads/kubecrux/internal/domain/resource/models.go`
- `/Users/shanchui/Downloads/kubecrux/internal/application/resource/service.go`
- `/Users/shanchui/Downloads/kubecrux/internal/api/handlers/platform.go`
- `/Users/shanchui/Downloads/kubecrux/internal/api/routes/router.go`
- `/Users/shanchui/Downloads/kubecrux/internal/infrastructure/agent/client.go`
- `/Users/shanchui/Downloads/kubecrux/internal/agent/api/server.go`
- `/Users/shanchui/Downloads/kubecrux/internal/agent/kubernetes/client.go`

## Current Findings
- `ListCRDs` already exists and `CRDView` already carries `kind`, `plural`, `versions`, `scope`.
- Generic fixed-kind YAML/delete/apply support exists in `service.go`, but it relies on `resourceGVRForKind` and does not support arbitrary CRDs.
- Existing direct-mode generic resource helpers around `GetResourceYAML`, `DeleteResourceByKind`, `CreateResourceFromYAML`, and `applyResourceYAML` are good structural references.
- Agent mode currently supports `ListCRDs`, but write/YAML flows for generic resources are generally unsupported.

## Ownership
- You own backend files only.
- Do not edit frontend files.
- You are not alone in the codebase. Do not revert unrelated edits.

## Expected Outcome
- Design and implement a CRD-backed custom-resource API surface with exact routes and payloads.
- Prefer cluster-ID + CRD-name based routes so the frontend does not have to guess group/plural/version mappings itself.
- Reuse the CRD definition to resolve served/preferred version, plural resource name, kind, and scope.
- Support list/create/get-yaml/apply-yaml/delete for CRD-backed instances in direct mode.
- Keep agent-mode unsupported paths explicit and accurate if full support is not practical.

## Report Back With
1. Sources consulted and exact files read
2. Exact routes/handlers/service methods added or changed
3. Files changed
4. Validation run and results
5. Known gaps or follow-up notes for frontend integration
