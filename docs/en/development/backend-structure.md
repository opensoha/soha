# Backend Structure

```text
.
  cmd/server
  cmd/agent
  configs/config.yaml
  internal/api
  internal/application
  internal/bootstrap
  internal/domain
  internal/infrastructure
  internal/policy
  internal/platform
  internal/repository
  migrations
```

## Module Responsibilities

- `api`: HTTP transport, middleware, request parsing, and response shaping.
- `application`: use-case orchestration, authorization, scope semantics, audit, operation logs, and frontend-facing view models.
- `bootstrap`: dependency graph assembly, database migration, built-in seeds, and startup lifecycle.
- `domain`: domain contracts and platform view models.
- `infrastructure`: config, logger, PostgreSQL, Kubernetes, informer, agent, Swagger, MCP, and external clients.
- `policy`: RBAC, ABAC, and scope calculation.
- `repository`: durable access for audit, business, and runtime records.

## Modular Monolith Convention

The backend stays a single repository, single `go.mod`, multi-`cmd` modular monolith. `cmd/server` is the current management control-plane entrypoint. `cmd/agent` is the remote cluster agent and runner entrypoint. If the internal security workbench or device reporting capability is added later, prefer a same-repository entrypoint such as `cmd/security-ingest` or `cmd/security-worker` instead of pushing high-frequency device ingest directly into `cmd/server` Gin handlers.

New entrypoints should reuse stable capabilities from `internal/bootstrap`, `internal/infrastructure`, `internal/repository`, and `internal/application`. Keep only entrypoint lifecycle, listen address, auth mode, queue or worker loops, and other runtime-shape differences in `cmd/**` or dedicated bootstrap files.

## Route Registration Convention

`internal/api/routes/router.go` owns only Gin engine setup, global middleware, baseline compatibility paths, static assets, and top-level route group assembly. Business routes belong in same-package domain files, for example:

- `routes_public.go`: health, login, public webhooks, runner claim/callback, and other routes that do not use a user session but may still require a token or public protocol.
- `routes_protected.go`: top-level orchestration for authenticated routes.
- `routes_platform.go`: Kubernetes platform resources.
- `routes_delivery.go`: application delivery, execution tasks, releases, and artifacts.
- `routes_monitoring.go`: monitoring, alerts, notifications, and on-call.
- `routes_runtime.go`: virtualization, Docker, AI workbench, and runtime workbenches.
- `routes_governance.go`: system, access, settings, AI Gateway, audit, and operation surfaces.

Do not grow `router.go` for new domains. Add `register<Domain>Routes`, then connect it from `registerProtectedRoutes` or `registerPublicRoutes`. Keep module switches, public callback paths, and authenticated user paths explicit.

## Go Hotspot File Split Convention

Optimize oversized files with same-package mechanical splits before changing behavior. Keep receivers, private helpers, DTOs, error semantics, audit records, and operation logs stable during the split.

- Platform handler REST methods are split by resource domain: `platform_inventory.go`, `platform_workloads.go`, `platform_configuration.go`, `platform_network.go`, `platform_storage.go`, `platform_rbac.go`, `platform_crd_helm.go`, `platform_generic.go`, and `platform_observability.go`. WebSocket stream behavior belongs in `platform_streams.go`, including the shared `websocketStreamSession` lifecycle helper.
- Platform resource application services are split by resource family: `pods.go`/`pods_helpers.go`, `workloads.go`, `configuration.go`, `rbac.go`, `network.go`, `storage.go`, `crd.go`, `events.go`, and `resource_yaml.go`. Shared authorization and audit helpers belong in `common.go`; shared direct Kubernetes bundle and timeout helpers belong in `direct_query.go`.
- When changing resource-service behavior, keep the existing family boundaries and run at least `go test ./internal/application/resource`. Do not mix mechanical moves with agent/direct behavior changes in one patch.
- Execution-plane state transitions must have tests. Claim, callback, cancel, retry, timeout, callback token rotation, late callbacks for terminal states, artifact persistence, and build/release backfill all belong in execution service coverage.
- AI Gateway is split by behavior domain: `manifest.go`, `tools.go`, `policies.go`, `rate_limit_budget.go`, `redaction.go`, `approval.go`, `tokens.go`, `audit.go`, and `governance.go`. `service.go` keeps wiring, interfaces, constructor, and setters only.
- Handler tests are required when transport behavior changes. Pure file moves can initially rely on package compile, full Go tests, and route-registration comparison.

## Bootstrap and Seed Convention

`internal/bootstrap/app.go` stays focused on top-level dependency assembly. Lifecycle methods belong in `lifecycle.go`; narrow cross-module adapters belong in dedicated files, such as Docker quick-host provisioning through `docker_provisioner.go`.

Database bootstrap should also stay split by concern. `database.go` keeps seed orchestration and common role, policy, user, and cluster persistence helpers. Menu seeds, disabled-module filtering, menu role bindings, and built-in menu upgrade logic belong in `database_menus.go`. When adding domain menus or permissions, update the matching seed file, permission keys, menu visibility, frontend route metadata, and docs together.

## Future Security Workbench Boundaries

The repository does not implement internal security workbench business behavior yet. The reserved boundaries are:

- `/api/v1/security/**`: management control-plane APIs for the Soha web admin.
- `/api/client/v1/**`: client APIs for the future Wails desktop app and Flutter mobile app.
- `/api/ingest/v1/**`: device reports, heartbeats, audit evidence, and security telemetry ingest APIs, suitable for a future `cmd/security-ingest` entrypoint.

Soha owns software library, device inventory and reporting, policy, audit, and control-plane data. FreeRADIUS, Fleet, mihomo, and similar systems should remain managed or integrated execution-side systems, not runtime cores inside the main Soha server process.
