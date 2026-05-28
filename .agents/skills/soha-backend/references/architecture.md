# Backend Architecture

## Primary Structure

- `cmd/server`: main API entrypoint.
- `cmd/agent`: remote agent runtime entrypoint.
- `internal/api`: routes, handlers, DTOs, middleware, HTTP error and response shaping.
- `internal/application`: business orchestration and view-model assembly.
- `internal/domain`: shared contracts and domain-level types.
- `internal/infrastructure`: config, DB, Redis, Kubernetes, informer, agent, Swagger, MCP, and logger wiring.
- `internal/policy`: RBAC, ABAC, and scope evaluation.
- `internal/repository`: durable persistence adapters.
- `internal/bootstrap`: dependency graph, migrations, seed data, and runtime startup.

## Where Changes Usually Belong

- New endpoint path or request schema: `internal/api/routes`, `internal/api/handlers`, and `internal/api/dto`.
- New behavior behind an existing endpoint: `internal/application/<module>`.
- New persistence or query shape: `internal/repository/<module>`.
- New external integration or client: `internal/infrastructure/<module>`.
- Startup-time dependency wiring: `internal/bootstrap/app.go`.
- Permission model changes: `internal/policy/**` and the relevant application authorization flow.
- New workbench or module visibility: `internal/application/module/service.go`, bootstrap seed defaults, route metadata/menu IDs, and permission keys.
- New long-running runtime work: a durable task or operation model plus runner claim/status/callback/cancel/retry APIs; never direct shell execution in handlers.

## Current Workbench Boundaries

- Delivery owns applications, application services and containers, build templates, workflow templates, release bundles, execution tasks, execution logs, approval policies, releases, registries, and catalog master data.
- Monitoring owns alert rules, alert events, notification policy, healing policy, on-call routes, schedules, rotations, escalation policies, and operational event views.
- AI owns copilot sessions, messages, analysis runs, analysis artifacts, inspection tasks, automation policies, toolsets, data sources, and MCP-backed evidence collection.
- Virtualization owns KubeVirt and PVE connections, VM inventory and lifecycle, images, flavors, console URLs, metrics, tasks, task logs, retries, cancel, and sync.
- Docker owns hosts, Compose projects, services, port mappings, templates, operation records, operation logs, and runner callback state. It may request host provisioning through the virtualization adapter only through an explicit host-provisioner interface.

## Execution and Runner Contracts

- Delivery claim and callback APIs are token-protected runner endpoints. Keep task claim, heartbeat, timeout, callback, cancel, retry, artifact extraction, and business-record backfill in `internal/application/execution`.
- `ci_agent_runner` executes workspace-aware local commands from payload workspace settings and must report logs, result payload, artifacts, and terminal state through callbacks.
- `k8s_job_runner` dispatches Kubernetes Jobs only when execution-cluster config is present; otherwise the service must fall back or report unsupported behavior.
- Docker operations are also runner-backed. The API records desired state and operation rows; the agent claims Docker operations, materializes Compose workspaces, runs whitelisted actions, and callbacks runtime service/port/log state.
- Callback handlers must reject stale terminal-state updates and retry attempts must rotate callback tokens before re-queueing work.

## Existing Runtime Notes

- Config is file-first through `configs/config.yaml`.
- `internal/api/routes/router.go` already serves embedded SPA and docs assets when `web/dist` and `docs/build` exist at build time.
- `internal/bootstrap/app.go` is the canonical dependency graph. Add new repositories, services, or handlers there instead of creating hidden singletons.
- `modules.*.enabled` controls module availability reporting, not authorization. Keep it aligned with module descriptors and frontend workbench defaults.
- The local agent config can claim delivery execution tasks and Docker operations. Keep `control_plane.runtime_endpoint`, runner status polling, and operation callback behavior compatible when editing runner APIs.

## Verification

- Prefer targeted `go test ./internal/<module>/...`.
- When route wiring or bootstrap changes, also run a broader build or test pass that crosses the edited package boundary.
- When platform APIs change, record scope, aggregation direction, and performance impact in repo memory during the same task.
- When runner or callback flows change, test both the application service and agent runner behavior around claim, heartbeat, cancel, retry, timeout, and stale callback rejection.
