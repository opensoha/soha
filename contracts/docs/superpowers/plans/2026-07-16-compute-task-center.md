# Compute Task Center Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace duplicate Compute task menus with one actionable Task Center and expose shared task controls from relevant resource pages.

**Architecture:** Extend the public Compute contract first, then implement a core facade that delegates task control to existing virtualization and Docker application services. Build one Task Center UI with URL-owned filters and log selection; resource pages consume its public query/mutation surface for compact contextual controls.

**Tech Stack:** OpenAPI 3.1, Go 1.25, Gin, React 19, TypeScript, TanStack Query, React Router, Ant Design 6, Vitest.

## Global Constraints

- Task creation remains in resource context; the Task Center does not create arbitrary tasks.
- `/system/operations` remains separate from Compute execution tasks.
- Contextual integrations cover virtualization connections, Docker runtime hosts, and Docker projects only.
- Frontend code uses only unified `/compute/tasks` control endpoints for cancel, retry, detail, and logs.
- Server-provided `availableActions` controls action visibility; the client does not infer it from status.
- No bulk actions and no streaming logs in this iteration.

---

### Task 1: Extend the public Compute task contract

**Files:**
- Modify: `openapi/soha-api.yaml`
- Modify: `scripts/validate.mjs`
- Modify: `gen/go/sohaapi/client.go`
- Modify: `gen/go/sohaapi/client_test.go`
- Regenerate: `gen/go/sohaapi/types.go`
- Regenerate: `gen/ts/sohaapi/index.ts`

**Interfaces:**
- Produces: `ComputeTaskLog`, `ComputeTaskLogListEnvelope`, `Client.GetComputeTask`, `Client.ListComputeTaskLogs`, `Client.CancelComputeTask`, and `Client.RetryComputeTask`.
- Extends: `GET /compute/tasks` with `resourceKind` and `resourceId`.

- [ ] Add validation assertions that the four task-control operations and the two resource filters exist.
- [ ] Run `npm run validate` and confirm it fails because the log operation and filters are absent.
- [ ] Add `GET /compute/tasks/{domain}/{id}/logs`, log schemas, list filters, and `x-soha-implementation-status: implemented` on detail/cancel/retry/log operations.
- [ ] Add Go SDK methods using the existing `doJSON` helper and generated request/response DTOs.
- [ ] Add client tests asserting exact paths, domain escaping, request bodies, and decoded task/log envelopes.
- [ ] Run `npm run generate`, `go test ./gen/go/sohaapi`, `npm run validate`, `npm run lint:openapi`, `npm run check:generated`, and `npm run check:openapi-breaking`.
- [ ] Commit with `feat: extend compute task control contract`.

### Task 2: Implement the core Compute task facade

**Files:**
- Modify: `internal/application/compute/service.go`
- Modify: `internal/application/compute/service_test.go`
- Modify: `internal/bootstrap/wiring.go`

**Interfaces:**
- Add `VirtualizationTaskController` with `GetOperation`, `ListOperationLogs`, `CancelOperation`, and `RetryOperation` methods using virtualization domain types.
- Add `RuntimeTaskController` with the same four capabilities using Docker domain types.
- Add service methods `GetTask`, `ListTaskLogs`, `CancelTask`, and `RetryTask` returning contract DTOs.

- [ ] Extend test fakes with control calls and add failing table tests for both domains, permissions, missing tasks, invalid domains, normalized logs, cancel, retry, and resource filtering.
- [ ] Run `GOWORK=off go test ./internal/application/compute -count=1` and confirm failures identify the missing facade methods.
- [ ] Add the two narrow controller ports without expanding the existing aggregate reader ports.
- [ ] Implement domain validation and permission resolution before delegation.
- [ ] Map virtualization `TaskLog` and Docker `OperationLog` to `ComputeTaskLog`; reuse `virtualizationTaskView` and `runtimeTaskView` for task results.
- [ ] Filter normalized `ComputeTaskView.Resources` by exact `resourceKind` and `resourceId` matches before sorting/pagination.
- [ ] Wire existing virtualization and Docker application services as controllers while repositories remain aggregate readers.
- [ ] Run focused tests, `GOWORK=off go test ./internal/application/compute ./internal/bootstrap -count=1`, and `git diff --check`.
- [ ] Commit with `feat: add unified compute task controls`.

### Task 3: Expose HTTP routes and consolidate seeded menus

**Files:**
- Modify: `internal/api/handlers/compute.go`
- Modify: `internal/api/handlers/compute_test.go`
- Modify: `internal/api/routes/routes_runtime.go`
- Modify: `internal/api/routes/routes_test.go`
- Modify: `internal/bootstrap/database_menus.go`
- Modify: `internal/bootstrap/database_test.go`
- Modify: `internal/application/menu/visibility.go`
- Modify: `internal/application/menu/visibility_test.go`
- Modify: `internal/application/module/service.go`
- Modify: `internal/application/module/service_test.go`

**Interfaces:**
- Registers the four contract routes under `/compute/tasks/:domain/:id`.
- Keeps only `compute-workbench-tasks-operations` enabled and labels it `任务中心` / `Task Center`.

- [ ] Add failing handler tests for parameter binding, optional reason, log limit, and 200/202 response envelopes.
- [ ] Add failing route/menu tests requiring one enabled Task Center menu and all control routes.
- [ ] Run focused handler, route, menu, and bootstrap tests and confirm expected failures.
- [ ] Extend `ComputeService`, implement handlers, and register routes.
- [ ] Pass `resourceKind` and `resourceId` from list handler to the application filter.
- [ ] Disable legacy Sync/Build seed records, rename the canonical seed, and remove legacy IDs from module management lists while retaining routes for URL compatibility.
- [ ] Run focused packages, `make complexity-check` for changed production functions, and `git diff --check`.
- [ ] Commit with `feat: expose compute task center API`.

### Task 4: Build the frontend Compute task data layer

**Files:**
- Modify: `src/features/compute/api.ts`
- Modify: `src/features/compute/keys.ts`
- Modify: `src/features/compute/queries.ts`
- Create: `src/features/compute/mutations.ts`
- Modify: `src/features/compute/index.ts`
- Create: `src/features/compute/api.test.ts`
- Create: `src/features/compute/keys.test.ts`
- Create: `src/features/compute/mutations.test.ts`

**Interfaces:**
- Produces hierarchical list/detail/log keys and `computeTaskMutations.cancel` / `computeTaskMutations.retry` mutation option factories.
- Produces `computeTaskLogPath(domain, id)` for Task Center deep links.

- [ ] Add failing API tests for resource filters, detail, logs, cancel, and retry wire calls.
- [ ] Add failing key and mutation tests for normalized keys and exact list/detail/log/overview invalidation.
- [ ] Run the three Vitest files and confirm missing APIs/factories fail.
- [ ] Implement the minimal API, key, query, mutation, and public exports using contract-generated types.
- [ ] Run the focused tests, TypeScript typecheck, and frontend boundary check.
- [ ] Commit with `feat: add compute task client controls`.

### Task 5: Consolidate routes and build the Task Center UI

**Files:**
- Modify: `src/features/compute/routes.ts`
- Modify: `src/features/compute/navigation.ts`
- Modify: `src/features/compute/navigation.test.ts`
- Modify: `src/features/compute/tasks/page.tsx`
- Modify: `src/features/compute/tasks/page.test.ts`
- Create: `src/features/compute/tasks/task-actions.tsx`
- Create: `src/features/compute/tasks/task-log-drawer.tsx`
- Create: `src/features/compute/tasks/task-center.test.tsx`
- Remove: `src/features/compute/tasks/sync-page.tsx`
- Remove: `src/features/compute/tasks/build-page.tsx`
- Rename: `src/features/compute/tasks/operations-page.tsx` to `src/features/compute/tasks/task-center-page.tsx`

**Interfaces:**
- `TaskActions` renders log/cancel/retry icons from `availableActions`.
- `TaskLogDrawer` reads URL selection and renders task metadata plus normalized logs.
- Compatibility route adapters preserve query parameters and force their category.

- [ ] Add failing navigation/route tests requiring one visible `任务中心` entry and redirect-only legacy routes.
- [ ] Add failing page tests for category URL round trips, action visibility, confirmations, retry selection, and Drawer loading/empty/error/success states.
- [ ] Run focused Vitest tests and confirm failures reflect the old three-menu/read-only UI.
- [ ] Implement the canonical Task Center route, compatibility redirects, category filter, resource URL state, fixed action column, mutations, and Drawer.
- [ ] Ensure cancel/retry use `Popconfirm`, logs use an icon button, and the Drawer uses Ant Design 6 `destroyOnHidden` rather than deprecated APIs.
- [ ] Run focused tests, `npm run check:routes`, `npm run check:frontend-boundaries`, `npm run typecheck`, and `antd lint` on changed TSX files.
- [ ] Run `graphify update . --force` and inspect graph diagnostics because duplicate route modules are deleted.
- [ ] Commit with `feat: consolidate compute task center`.

### Task 6: Add contextual task controls to resource pages

**Files:**
- Modify: `src/features/virtualization/clusters/list-page.tsx`
- Modify: `src/features/docker/hosts/page.tsx`
- Modify: `src/features/docker/projects/list-page.tsx`
- Modify or create focused page tests beside each capability.

**Interfaces:**
- Consumes: Compute task resource queries, `TaskActions`, and Task Center deep-link helpers from the Compute public surface.
- Behavior: in-place cancel/retry; log navigation opens the canonical Task Center Drawer; all-task links use exact resource filters.

- [ ] Add failing tests for connection, runtime-host, and project task controls and deep links.
- [ ] Run focused tests and confirm the resource pages lack the shared controls.
- [ ] Add compact latest-task treatments without embedding the full task table or duplicating state rules.
- [ ] Invalidate both resource-local queries and Compute task prefixes after cancel/retry.
- [ ] Run focused tests, typecheck, frontend boundaries, and `antd lint` on changed files.
- [ ] Commit with `feat: add contextual compute task controls`.

### Task 7: Cross-repository verification

- [ ] Run `npm test` in `soha-contracts`.
- [ ] Run `GOWORK=off go mod verify`, `GOWORK=off go test ./...`, handler/application race tests, `GOWORK=off go vet ./...`, backend build, and `git diff --check` in `soha`.
- [ ] Run frontend format, typecheck, lint, tests, route checks, boundary checks, bundle budget, build, Ant Design lint, and `git diff --check` in `soha-web`.
- [ ] Start or reuse the Vite server and verify `/compute/tasks/operations`, legacy redirects, task actions, log Drawer, contextual resource links, desktop/mobile layout, and console errors in the browser.
- [ ] Inspect `git status` and commits in all three repositories and report any pre-existing gate failures separately.
