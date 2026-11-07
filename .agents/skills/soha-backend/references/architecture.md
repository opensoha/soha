# Backend Architecture

## Primary Structure

- `cmd/server`: main API entrypoint.
- `internal/api`: routes, handlers, DTOs, middleware, HTTP error and response shaping.
- `internal/application`: business orchestration and view-model assembly.
- `internal/domain`: shared contracts and domain-level types.
- `internal/infrastructure`: config, DB, Kubernetes, informer, agent, Swagger, MCP, and logger wiring.
- `internal/policy`: RBAC, ABAC, and scope evaluation.
- `internal/repository`: durable persistence adapters.
- `internal/bootstrap`: dependency graph, migrations, seed data, and runtime startup.

These are ownership boundaries, not a requirement for every feature to create every directory. Workbenches aggregate capabilities. Existing ingest, network control, gateway, proxy, and RADIUS process packages keep their explicit runtime boundaries.

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

- The delivery workbench aggregates several owners: `application/application` manages application/service records; `catalog` manages definitions and environment bindings; `delivery` orchestrates delivery plans and targets; `workflow` owns run/node progression; `execution` owns executor task leases and callbacks; `manifest` owns declarative source/binding/revision behavior. Build, release, registry, and approval capabilities retain their established owners. Cross-domain orchestration uses explicit application ports and an owned transaction boundary where atomicity is required.
- Monitoring owns alert rules, alert events, notification policy, healing policy, on-call routes, schedules, rotations, escalation policies, and operational event views.
- AI owns copilot sessions, messages, analysis runs, analysis artifacts, inspection tasks, automation policies, toolsets, data sources, and MCP-backed evidence collection.
- Virtualization owns KubeVirt and PVE connections, VM inventory and lifecycle, images, flavors, console URLs, metrics, tasks, task logs, retries, cancel, and sync.
- Worker Pool keeps business bounds and supply ownership in `domain/virtualization`. Kubernetes namespace/name and label syntax are a pure infrastructure validator, injected into application runtime capabilities and persistence validation. Provider inspection remains separate from syntax validation, so disabling supply does not require a reachable cluster. Frozen task scope remains readable independently of whether the optional worker runtime is enabled.
- Docker owns hosts, Compose projects, services, port mappings, templates, operation records, operation logs, and runner callback state. It may request host provisioning through the virtualization adapter only through an explicit host-provisioner interface.

## Execution and Runner Contracts

- Delivery claim and callback APIs are token-protected runner endpoints. Keep task claim, heartbeat, timeout, callback, cancel, retry, artifact extraction, and business-record backfill in `internal/application/execution`.
- `ci_agent_runner` executes workspace-aware local commands from payload workspace settings and must report logs, result payload, artifacts, and terminal state through callbacks.
- `k8s_job_runner` dispatches Kubernetes Jobs only when execution-cluster config is present; otherwise the service must fall back or report unsupported behavior.
- Docker operations are also runner-backed. The API records desired state and operation rows; the agent claims Docker operations, materializes Compose workspaces, runs whitelisted actions, and callbacks runtime service/port/log state.
- Callback handlers must reject stale terminal-state updates and retry attempts must rotate callback tokens before re-queueing work.

## Existing Runtime Notes

- Config is file-first through `configs/config.yaml`.
- `internal/api/routes/router.go` serves the embedded SPA from `internal/staticassets/web/dist` when the artifact is staged for embed builds.
- `internal/bootstrap` is the composition root, entered through `app.go` with constructors/adapters in focused files. Complete construction, binding, and required-port validation before starting workers or accepting requests; do not place runtime business decisions in wiring callbacks.
- `modules.*.enabled` controls module availability reporting, not authorization. Keep it aligned with module descriptors and frontend workbench defaults.
- Agent runners live in sibling repos and call back into this control plane. Keep runner claim/status/callback behavior compatible when editing runner APIs.

## Verification

- Prefer targeted `go test ./internal/<module>/...`.
- When route wiring or bootstrap changes, also run a broader build or test pass that crosses the edited package boundary.
- When platform APIs change, document scope, aggregation direction, and performance impact in the task's designated engineering or verification record when applicable.
- When runner or callback flows change, test both the application service and agent runner behavior around claim, heartbeat, cancel, retry, timeout, and stale callback rejection.


## Continuing Boundary Checks

Application and domain root tests recursively inspect production imports, so new functional subdirectories remain covered. Application SDK debt is limited to the exact source/dependency entries in its boundary test; remove obsolete entries instead of broadening them. The resource package retains its local check for focused package runs. Handler tests inspect the actual NetworkAccess and Virtualization consumer fields, while their transport tests preserve browser-facing upgrade behavior and reject upstream implementation coupling.

These checks complement the owning module's authorization, transaction, construction and replay tests. They do not infer that a changed dependency graph proves runtime correctness or that a fixed local source combination has passed release CI.
