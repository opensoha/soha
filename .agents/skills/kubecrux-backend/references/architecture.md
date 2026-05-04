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

## Existing Runtime Notes

- Config is file-first through `configs/config.yaml`.
- `internal/api/routes/router.go` already serves embedded SPA and docs assets when `web/dist` and `docs/build` exist at build time.
- `internal/bootstrap/app.go` is the canonical dependency graph. Add new repositories, services, or handlers there instead of creating hidden singletons.

## Verification

- Prefer targeted `go test ./internal/<module>/...`.
- When route wiring or bootstrap changes, also run a broader build or test pass that crosses the edited package boundary.
- When platform APIs change, record scope, aggregation direction, and performance impact in repo memory during the same task.
