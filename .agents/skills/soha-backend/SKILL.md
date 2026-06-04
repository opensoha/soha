---
name: soha-backend
description: >-
  Implement and refactor soha backend capabilities in `cmd/**`,
  `internal/**`, and `configs/**` for Go 1.23, Gin, PostgreSQL, Kubernetes
  `client-go`, and agent-connected clusters. Use when adding or changing HTTP
  routes, handlers, application services, repositories, policy checks,
  bootstrap wiring, cluster or resource aggregation, or delivery,
  observability, AI workbench, virtualization, Docker workbench, and
  control-plane APIs. This skill enforces the modular-monolith layers,
  platform view-model APIs instead of raw provider objects, explicit cluster
  and namespace scope semantics, permission-key aligned authorization, audit
  and operation logging for important actions, direct-versus-agent cluster
  behavior rules, and durable runner-backed operation flows.
---

# Soha Backend

## Overview

Implement backend changes through the repository's layered Go architecture. Keep handlers thin, put behavior in application services, and expose aggregated platform-facing contracts instead of leaking raw infrastructure details.

## Workflow

1. Identify the change boundary first: transport, orchestration, policy, infrastructure, repository, or bootstrap.
2. Read the existing handler, service, repository, and route wiring before editing. Follow the current module rather than creating a parallel path.
3. Keep authorization, scope semantics, audit, and operation logging aligned with the behavior change.
4. For Kubernetes-facing work, decide whether the capability should use informer/cache, live query fallback, or agent mode, and make unsupported agent paths explicit.
5. For delivery, Docker, virtualization, and AI work, identify the durable control-plane object first: release bundle, execution task, Docker operation, virtualization task, AI session, analysis run, or inspection task.
6. Update tests, config defaults, deployment-facing manifests, menu seeds, permissions, and memory or docs in the same task when contracts or semantics change.
7. Validate with focused `go test` runs, or at minimum with the affected package tests and a repo build path.

## Non-Negotiables

- `internal/api` parses requests, maps errors, and returns HTTP responses. It must not own Kubernetes traversal or policy decisions.
- `internal/application` owns orchestration, scope handling, authorization checks, audit recording, and view-model shaping.
- `internal/repository` owns durable persistence details. Keep SQL and GORM concerns out of handlers and orchestration code.
- `internal/infrastructure` owns external clients and vendor-specific wiring such as Kubernetes managers, informer startup, agent HTTP clients, config loading, DB, Swagger, and MCP registries.
- `internal/bootstrap` wires dependencies and startup lifecycle. Do not hide new cross-module dependencies in ad hoc globals.
- Prefer domain or platform view models for API output. Do not return raw Kubernetes schema objects unless the route is explicitly a YAML or passthrough surface.
- Runtime shell work does not belong in handlers. Build, release, Docker Compose, Docker Engine, and VM-control execution must go through application services plus durable task/operation records and runner callbacks.

## Modularity Ground Rules

- Keep the backend a single-repository, single-`go.mod`, multi-`cmd` modular monolith. `cmd/server` is the management control-plane server, and `cmd/agent` is the remote cluster agent/runner. Future specialized runtimes, such as security device ingest or security workers, should be added as same-repo `cmd/**` entries that reuse internal packages.
- Keep `internal/api/routes/router.go` thin. It should assemble the Gin engine, global middleware, compatibility paths, static assets, and top-level groups only. Add or change business routes in same-package route files such as `routes_platform.go`, `routes_delivery.go`, `routes_monitoring.go`, `routes_runtime.go`, or `routes_governance.go`.
- Public, runner, and callback routes belong in `routes_public.go` unless they require user-session authentication. Authenticated routes should be connected from `registerProtectedRoutes`; module-gated domains should keep their `cfg.Modules.*.Enabled` checks inside their domain registration function.
- Keep `internal/bootstrap/app.go` focused on dependency graph assembly. Put lifecycle methods in `lifecycle.go`, narrow cross-module adapters in dedicated files, and seed concerns in focused files such as `database_menus.go` instead of growing `database.go`.
- When adding menus or permissions, update the domain seed file, role permission keys, visible-menu behavior, frontend route metadata, and docs together. Menu seed filtering by disabled modules must remain backend-owned.
- Do not implement future internal-security business behavior as part of groundwork refactors. Reserve boundaries without implying runtime parity:
  - `/api/v1/security/**` for Soha web-admin security management APIs.
  - `/api/client/v1/**` for future Wails desktop and Flutter mobile client APIs.
  - `/api/ingest/v1/**` for future device reporting, heartbeats, audit evidence, and telemetry ingest, ideally owned by a future `cmd/security-ingest` entrypoint.
- FreeRADIUS, Fleet, mihomo, and similar systems are managed or integrated execution-side systems. Soha should own software catalog, device inventory/reporting, policy, audit, and control-plane records, but those external tools should not become runtime cores inside `cmd/server`.

## Go Hotspot Refactor Rules

- Split oversized files by stable behavior domains before changing logic. Prefer same-package file moves first so method receivers, private helpers, tests, and API contracts stay intact.
- Platform handler REST methods are split by resource domain: `platform_inventory.go`, `platform_workloads.go`, `platform_configuration.go`, `platform_network.go`, `platform_storage.go`, `platform_rbac.go`, `platform_crd_helm.go`, `platform_generic.go`, and `platform_observability.go`. WebSocket stream behavior belongs in `platform_streams.go`; keep the shared `websocketStreamSession` lifecycle helper there.
- Platform resource application methods are split by resource family: `pods.go`/`pods_helpers.go`, `workloads.go`, `configuration.go`, `rbac.go`, `network.go`, `storage.go`, `crd.go`, `events.go`, and `resource_yaml.go`. Shared authorization/audit helpers belong in `common.go`; shared direct Kubernetes bundle and timeout helpers belong in `direct_query.go`.
- When changing resource-service behavior, keep the existing family file boundaries and run at least `go test ./internal/application/resource`. Avoid changing agent/direct behavior in the same patch as a mechanical move.
- AI Gateway is split by behavior domain: `manifest.go`, `tools.go`, `policies.go`, `rate_limit_budget.go`, `redaction.go`, `approval.go`, `tokens.go`, `audit.go`, and `governance.go`; keep `service.go` for wiring, interfaces, and constructor/setter methods.
- Execution-plane changes must include focused tests around status transitions, callback tokens, late callbacks, retry, cancel, timeout, artifact persistence, and build/release backfill. The execution service started with explicit state-machine coverage; do not let it regress to untested callback behavior.
- Handler coverage is currently low, so new transport behavior should add handler tests. Pure file moves may rely on package compile plus route-registration comparison, but stream behavior changes need websocket or writer lifecycle tests.

## Platform and Authorization Rules

- List endpoints must respect cluster scope and namespace scope. Empty namespace means all namespaces for namespaced resources.
- Cluster-scoped resources must ignore namespace filters instead of pretending to support them.
- Agent-mode gaps must surface as unsupported or degraded behavior, never as silent parity.
- Important reads, writes, and operational actions should record audit logs. Mutations should also record operation logs where the existing module already does so.
- Backend permission checks, route visibility, and menu visibility are related but separate. Keep permission keys aligned with frontend expectations.
- Module status from `modules.*.enabled`, visible menus, and permission keys are separate gates. Disabling a module is not a substitute for service-level authorization.
- Prefer backend aggregation over frontend joins and namespace fan-out, especially for platform pages.

## Workbench-Specific Rules

- Delivery execution must route task claim, callback, cancel, retry, heartbeat, timeout, artifact extraction, and build/deploy record backfill through `internal/application/execution`; delivery handlers must not bypass that orchestration.
- `ci_agent_runner` tasks are workspace-aware. Preserve `workspace.path`, `workspace.commandDir`, `workspace.checkout`, and `workspace.artifactFiles` semantics when changing build or release payloads.
- `k8s_job_runner` is only real when `runtime.execution_job_cluster_id` and related execution-job settings are configured. Fall back or surface unsupported behavior instead of implying parity.
- Docker APIs persist desired state and enqueue operations. Docker Engine and Compose work is claimed through `/api/v1/docker/operations/claim`, observed through `/api/v1/docker/operations/:id/runner-status`, and completed through `/api/v1/docker/operation-callbacks`.
- Docker quick host provisioning may call virtualization only through the narrow host-provisioner adapter. Keep Docker independent from virtualization data models except for explicit link fields such as `virtualizationConnectionId`.
- Virtualization operations should stay task-based. KubeVirt and PVE adapter behavior belongs in `internal/infrastructure/virtualization`, while lifecycle orchestration, permissions, logs, and sync state belong in `internal/application/virtualization`.
- AI workbench data should stay session-first. Global provider and datasource settings remain settings-controlled; session toolsets, evidence budgets, analysis artifacts, and inspection flows belong in the copilot application service.
- AI Agent Runtime must stay provider-agnostic at the application boundary. Pages, handlers, and business flows should depend on soha `AgentProvider`, `AgentRun`, `AgentCapability`, `AgentToolBinding`, `AgentSkillBinding`, `AnalysisArtifact`, toolset, and analysis profile contracts rather than Hermes, OpenClaw, or any provider SDK/CLI.
- Hermes is only the first external provider behind the runner claim/callback path. New providers should extend provider catalogs, tool bindings, skill bindings, and runner executors without rewriting AI workbench flows or automation policy semantics.
- Agent Runtime capabilities should expose logs, metrics, traces, platform events, delivery context, on-call context, Docker context, and virtualization context as soha capability and MCP/tool entries. Skills are platform methodology definitions and may map to Hermes skills, MCP capabilities, prompt templates, or future provider skill systems.
- Agent Runtime outputs must be normalized into soha `AnalysisArtifact` with evidence, hypotheses, recommendations, graph, tool execution records, and data-source snapshots. Provider-native output should not leak directly into frontend contracts.
- Continuous AI analysis is scheduled and audited by soha automation policy. Hermes cron or other provider-native schedulers remain optional experiments and must not become the source of truth for dedup, cooldown, budget, permission, or audit behavior.
- soha owns permissions, menus, audit, budget, data redaction, and operation boundaries for Agent Runtime. Agents are pluggable executors only, and high-risk write actions must still route through the owning module's durable operation or approval flow.
- On-call and notification flows should resolve active assignments in the backend from alert context and route rules. Do not reimplement route matching only in the frontend.

## Common Pitfalls

- Adding a route without a permission key, seed menu, and permission catalog update creates a visible-but-forbidden or hidden-but-callable feature.
- Returning raw Kubernetes, KubeVirt, Docker, or PVE objects leaks vendor schemas into the console contract; map them to soha DTOs.
- Treating `admin` as a hard-coded backend bypass breaks custom role `permissionKeys`. Use the permission resolver.
- Leaving related records in `queued`, `running`, or provider-native statuses after callbacks causes split-brain status. Callback paths must backfill bundle, build, deploy, Docker, or virtualization records as appropriate.
- Accepting late callbacks after cancel, timeout, or retry can overwrite a newer attempt. Retry paths must rotate callback tokens and terminal tasks must reject stale updates.
- Running unbounded live cluster reads or namespace loops in application services will regress large-cluster behavior.
- Storing secrets in logs, operation payloads, AI artifacts, or audit details is unsafe. Persist references or redacted summaries instead.

## Read These References When Needed

- `references/architecture.md`: module responsibilities, bootstrap wiring, and where common backend changes should land.
- `references/platform-rules.md`: cluster-access behavior, scope semantics, performance expectations, and backend verification prompts.

## Repo-specific reminders

- When changing identity or login flows, update the matching docs in `docs/architecture/**`, `docs/en/api/**`, and `docs/operations/**` in the same task.
- Treat legacy `auth.oidc.*` config as a compatibility layer when multi-provider login settings exist; do not silently break old OIDC runtime paths.
- If a provider type is only configuration-visible and not runtime-complete, make that explicit in API behavior and docs rather than implying parity.
- When adding a module or workbench, update `internal/application/module/service.go`, route metadata/menu seeds, permission keys, bootstrap defaults, and frontend visibility tests together.
- When adding a migration after the consolidated baseline, add an incremental file under `migrations/postgres/` and keep bootstrap tests aligned; do not recreate removed root-level legacy migration mirrors.
- Keep generated `docs/build` and `.docusaurus` artifacts out of hand-written source changes unless the task explicitly asks to publish built docs.

## Done Criteria

- Layer boundaries remain intact.
- Scope semantics and authorization behavior are explicit.
- New platform reads avoid unnecessary live-query or frontend fan-out regressions.
- Long-running or external execution is task/operation-backed and callback-safe.
- Menus, module status, route visibility, and permission keys are aligned when API surface changes affect navigation.
- Affected packages are tested, and memory or docs are updated when contracts changed.
