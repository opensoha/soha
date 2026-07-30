---
name: soha-backend
description: >-
  Implement or review open-source Soha backend capabilities in `cmd/**`,
  `internal/**`, and `configs/**` for Go 1.26.5, Gin, PostgreSQL, Kubernetes
  `client-go`, and agent-connected clusters. Use when changing HTTP routes,
  handlers, application services, repositories, policy, bootstrap wiring,
  platform aggregation, durable operations, AI Gateway, Identity, knowledge,
  evaluation, memory, or other control-plane modules. This skill enforces the
  modular-monolith dependency direction, contracts-first public behavior,
  explicit scope and authorization, audit-safe operations, direct-versus-agent
  capability handling, and the open-source versus Cloud boundary.
---

# Soha Backend

## Overview

Implement backend changes through the repository's layered Go architecture. Keep handlers thin, put behavior in application services, and expose aggregated platform-facing contracts instead of leaking raw infrastructure details.

## Workflow

1. Identify the change boundary first: transport, orchestration, policy, infrastructure, repository, or bootstrap.
2. Read `references/go-engineering-standards.md` for every production Go change, then inspect the existing handler, service, port, adapter, and route wiring before editing. Follow the current module rather than creating a parallel path.
3. Keep authorization, scope semantics, audit, and operation logging aligned with the behavior change.
4. For Kubernetes-facing work, decide whether the capability should use informer/cache, live query fallback, or agent mode, and make unsupported agent paths explicit.
5. For external or long-running work, identify the existing durable task, operation, session, run, lease, or callback state machine before adding another execution path.
6. When public behavior changes, update `../soha-contracts` first, then the affected consumer, docs, permissions, menus, config, and tests.
7. Use `graphify-out/graph.json` for broad ownership or dependency questions. Refresh it only after structural changes are stable and the worktree contents are understood; use `--force` after deletions and run diagnostics.
8. Run focused tests while iterating, then apply the verification tier from `references/go-engineering-standards.md`; architecture, dependency, security, module, or release changes require the full gate.

## Non-Negotiables

- `internal/api` parses requests, maps errors, and returns HTTP responses. It must not own Kubernetes traversal or policy decisions.
- `internal/application` owns orchestration, scope handling, authorization checks, audit recording, and view-model shaping.
- `internal/repository` owns durable persistence details. Keep SQL and GORM concerns out of handlers and orchestration code.
- `internal/infrastructure` owns external clients and vendor-specific wiring such as Kubernetes managers, informer startup, agent HTTP clients, config loading, DB, Swagger, and MCP registries.
- `internal/bootstrap` wires dependencies and startup lifecycle. Do not hide new cross-module dependencies in ad hoc globals.
- Keep manual constructor injection and `internal/bootstrap` as the composition root. Define narrow interfaces in the consuming package, reject missing or typed-nil required dependencies, and do not introduce a DI container or service locator without an explicit architecture decision.
- Prefer domain or platform view models for API output. Do not return raw Kubernetes schema objects unless the route is explicitly a YAML or passthrough surface.
- Runtime shell work does not belong in handlers. Build, release, Docker Compose, Docker Engine, and VM-control execution must go through application services plus durable task/operation records and runner callbacks.

## Modularity Ground Rules

- Keep the backend a single-repository, single-`go.mod` modular monolith. `cmd/server` is the management control-plane server. Remote agent and frontend runtime work belongs in sibling repos unless the task explicitly changes this repository boundary.
- Keep `internal/api/routes/router.go` thin. It should assemble the Gin engine, global middleware, compatibility paths, static assets, and top-level groups only. Add or change business routes in same-package route files such as `routes_platform.go`, `routes_delivery.go`, `routes_monitoring.go`, `routes_runtime.go`, or `routes_governance.go`.
- Public, runner, and callback routes belong in `routes_public.go` unless they require user-session authentication. Authenticated routes should be connected from `registerProtectedRoutes`; module-gated domains should keep their `cfg.Modules.*.Enabled` checks inside their domain registration function.
- Keep `internal/bootstrap/app.go` focused on dependency graph assembly. Put lifecycle methods in `lifecycle.go`, narrow cross-module adapters in dedicated files, and seed concerns in focused files such as `database_menus.go` instead of growing `database.go`.
- When adding menus or permissions, update the domain seed file, role permission keys, visible-menu behavior, and public docs together. Frontend route metadata lives in `soha-web`.
- Do not create routes, entrypoints, integrations, or abstractions for planned products that do not have a current executable owner.

## Go Hotspot Refactor Rules

- Split oversized files by stable behavior domains before changing logic. Prefer same-package file moves first so method receivers, private helpers, tests, and API contracts stay intact.
- Platform handler REST methods are split by resource domain: `platform_inventory.go`, `platform_workloads.go`, `platform_configuration.go`, `platform_network.go`, `platform_storage.go`, `platform_rbac.go`, `platform_crd_helm.go`, `platform_generic.go`, and `platform_observability.go`. WebSocket stream behavior belongs in `platform_streams.go`; keep the shared `websocketStreamSession` lifecycle helper there.
- Platform resource application methods are split by resource family: `pods.go`/`pods_helpers.go`, `workloads.go`, `configuration.go`, `rbac.go`, `network.go`, `storage.go`, `crd.go`, `events.go`, and `resource_yaml.go`. Keep authorization, audit, capability orchestration, typed connection routing, and platform DTO contracts in application code. Keep direct Kubernetes/Helm clients, cache/live fallback, SPDY transport, and provider-object mapping in `internal/infrastructure/resourcebackend`.
- Application production code must not import Kubernetes/Helm SDKs or `internal/infrastructure`. Preserve the zero-tolerance dependency boundary tests; do not recreate `DirectClients`, an application `ResourceCache`, or connection-mode branch clones.
- When changing resource behavior, keep the existing family boundaries and run at least `go test ./internal/application/resource ./internal/infrastructure/resourcebackend`. Avoid mixing semantic changes with mechanical file moves unless tests prove the contract is unchanged.
- AI Gateway is split by behavior domain: `manifest.go`, `tools.go`, `policies.go`, `rate_limit_budget.go`, `redaction.go`, `approval.go`, `tokens.go`, `audit.go`, and `governance.go`; keep `service.go` for wiring, interfaces, and constructor/setter methods.
- Execution-plane changes must include focused tests around status transitions, callback tokens, late callbacks, retry, cancel, timeout, artifact persistence, and build/release backfill. The execution service started with explicit state-machine coverage; do not let it regress to untested callback behavior.
- New transport behavior requires handler tests. Pure file moves may rely on package compile plus route-registration comparison, but stream behavior changes need websocket or writer lifecycle tests.

## Platform and Authorization Rules

- List endpoints must respect cluster scope and namespace scope. Empty namespace means all namespaces for namespaced resources.
- Cluster-scoped resources must ignore namespace filters instead of pretending to support them.
- Agent-mode gaps must surface as unsupported or degraded behavior, never as silent parity.
- Important reads, writes, and operational actions should record audit logs. Mutations should also record operation logs where the existing module already does so.
- Backend permission checks, route visibility, and menu visibility are related but separate. Keep permission keys aligned with frontend expectations.
- Module status from `modules.*.enabled`, visible menus, and permission keys are separate gates. Disabling a module is not a substitute for service-level authorization.
- Prefer backend aggregation over frontend joins and namespace fan-out, especially for platform pages.

## Execution And Runtime Rules

- Delivery, Docker, virtualization, and Agent Runtime work must use their existing durable claim, heartbeat, callback, cancel, retry, timeout, and backfill flows.
- Preserve workspace roots, operation allowlists, callback token rotation, terminal-state idempotency, and stale-callback rejection.
- Keep provider and runtime adapters in infrastructure packages. Application services own authorization, orchestration, audit, normalized Soha DTOs, and business-record state.
- Treat optional runners and agent capabilities as supported, degraded, or unsupported from real configuration and capability evidence; never imply parity.
- Keep Agent Runtime provider-agnostic at the application boundary and normalize provider-native output before it reaches public contracts.
- High-risk AI or automation writes still pass through the owning module's approval, permission, audit, and durable-operation flow.
- Resolve shared operational context such as on-call routing and cross-namespace aggregation in the backend rather than duplicating joins in the frontend.

## Common Pitfalls

- Adding a route without a permission key, seed menu, and permission catalog update creates a visible-but-forbidden or hidden-but-callable feature.
- Returning raw Kubernetes, KubeVirt, Docker, or PVE objects leaks vendor schemas into the console contract; map them to soha DTOs.
- Treating `admin` as a hard-coded backend bypass breaks custom role `permissionKeys`. Use the permission resolver.
- Leaving related records in `queued`, `running`, or provider-native statuses after callbacks causes split-brain status. Callback paths must backfill bundle, build, deploy, Docker, or virtualization records as appropriate.
- Accepting late callbacks after cancel, timeout, or retry can overwrite a newer attempt. Retry paths must rotate callback tokens and terminal tasks must reject stale updates.
- Running unbounded live cluster reads or namespace loops in application services will regress large-cluster behavior.
- Storing secrets in logs, operation payloads, AI artifacts, or audit details is unsafe. Persist references or redacted summaries instead.

## Read These References When Needed

- `references/go-engineering-standards.md`: mandatory Go construction, package-boundary, complexity, security, testing, and CI gates for production backend changes.
- `references/architecture.md`: module responsibilities, bootstrap wiring, and where common backend changes should land.
- `references/platform-rules.md`: cluster-access behavior, scope semantics, performance expectations, and backend verification prompts.

## Repo-specific reminders

- When changing identity or login flows, update the matching public docs under `../soha-docs/content/{en,zh}/**` when that repository is in scope.
- Treat legacy `auth.oidc.*` config as a compatibility layer when multi-provider login settings exist; do not silently break old OIDC runtime paths.
- If a provider type is only configuration-visible and not runtime-complete, make that explicit in API behavior and docs rather than implying parity.
- When adding a module or workbench, update `internal/application/module/service.go`, route metadata/menu seeds, permission keys, bootstrap defaults, and frontend visibility tests together.
- When adding a migration after the consolidated baseline, add an incremental file under `migrations/postgres/` and keep bootstrap tests aligned; do not recreate removed root-level legacy migration mirrors.
- Keep generated frontend artifacts out of hand-written source changes unless the task explicitly asks to publish built output.

## CI Gate

Use Go `1.26.5` and run the release-sensitive gate with the root workspace disabled:

```bash
GOWORK=off go mod tidy
git diff --exit-code -- go.mod go.sum
GOWORK=off go mod verify
GOWORK=off go test ./...
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
make complexity-check
GOWORK=off go run golang.org/x/vuln/cmd/govulncheck@v1.3.0 ./...
GOWORK=off CGO_ENABLED=0 go build -tags embedassets -o /tmp/soha ./cmd/server
docker build --build-context contracts=../soha-contracts -f deploy/Dockerfile -t ghcr.io/opensoha/soha:test .
git diff --check
```

CI also runs `golangci-lint v2.9.0` with only-new-issues semantics on pull requests and builds the sibling `soha-web` artifact before the embed and image checks. Dependency, Dockerfile, workflow, or release changes require this full gate; a missing local Docker daemon must be covered by a successful GitHub Actions Docker job.

## Done Criteria

- Layer boundaries remain intact.
- Scope semantics and authorization behavior are explicit.
- New platform reads avoid unnecessary live-query or frontend fan-out regressions.
- Long-running or external execution is task/operation-backed and callback-safe.
- Menus, module status, route visibility, and permission keys are aligned when API surface changes affect navigation.
- Production complexity stays at or below 20, consumer capability interfaces stay small, and dependency boundary tests remain green.
- Affected packages are tested, the applicable full Go gate passes, and contracts or public docs are updated when behavior changed.
