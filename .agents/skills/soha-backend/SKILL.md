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
- `internal/infrastructure` owns external clients and vendor-specific wiring such as Kubernetes managers, informer startup, agent HTTP clients, config loading, DB, Redis, Swagger, and MCP registries.
- `internal/bootstrap` wires dependencies and startup lifecycle. Do not hide new cross-module dependencies in ad hoc globals.
- Prefer domain or platform view models for API output. Do not return raw Kubernetes schema objects unless the route is explicitly a YAML or passthrough surface.
- Runtime shell work does not belong in handlers. Build, release, Docker Compose, Docker Engine, and VM-control execution must go through application services plus durable task/operation records and runner callbacks.

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
