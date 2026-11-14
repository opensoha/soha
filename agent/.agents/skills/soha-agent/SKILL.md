---
name: soha-agent
description: Change or review the standalone Soha agent APIs, runners, cluster adapters, security, or packaging. Preserve its independence from core internals.
---

# Soha Agent

## Overview

Use this skill for Root `agent/`, preserving its independent Go module and artifacts. The agent is a standalone
runtime that exposes local cluster and runtime APIs, claims work from the Soha
control plane, and calls back with task, Docker, or Agent Runtime results.

## Workflow

1. Consult the relevant sections of [Go engineering standards](references/go-engineering-standards.md) for package ownership, lifecycle, API/security, concurrency, dependency, or verification changes. Localized fixes can use the affected implementation and the Testing section below; do not reread unchanged guidance. Keep `cmd/agent/main.go` thin. Startup belongs in `internal/agent/bootstrap`, config in `internal/agent/config`, HTTP surfaces in `internal/agent/api`, Kubernetes access in `internal/agent/kubernetes`, and control-plane execution in `internal/agent/runner`.
2. Do not import core `github.com/opensoha/soha/internal/**` packages. Share API shapes through same-Root `contracts/` types in source mode, with declared external SDK verification kept separate.
3. Use `SOHA_AGENT_CONFIG_FILE`, `SOHA_AGENT_*` env overrides, and `configs/agent.config.yaml` as the config path. Keep viper env keys aligned with config struct tags.
4. When adding a runtime capability, decide whether it belongs to the local HTTP API, Kubernetes adapter, execution or Docker runner, Identity Outpost, environment runtime manager, Agent Runtime provider, or packaging. Put it in the existing owner.
5. Keep diagnostics safe. `/api/v1/diagnostics` may summarize readiness and counts; it must not expose bearer tokens, kubeconfig contents, command secrets, or raw internal failures.
6. For broad package or dependency changes, query `graphify-out/graph.json`. Refresh it only after source changes are stable and the worktree contents are understood; follow the Graphify workflow in the repository AGENTS.md and run diagnostics. Do not bypass shrink guards with `--force` by default.

## Architecture Rules

- Keep `internal/agent/api/server.go` focused on Gin construction, middleware, route registrar calls, and server lifecycle. Add endpoint families to focused `routes_*.go` files; do not rebuild a monolithic server or God Service across multiple files.
- Keep interfaces narrow and owned by the consuming package. Wire concrete Kubernetes and runner implementations in bootstrap; do not add globals, `init` service setup, a DI container, or imports from the core Soha internals.
- Keep provider-specific Kubernetes/Helm and runner execution details behind their owning packages. HTTP handlers parse, authorize, delegate, and map redacted responses only.
- Keep environment lease, launch, stop, snapshot, and cleanup behavior in `internal/agent/environment`; runners coordinate it through narrow interfaces.
- Preserve bounded concurrency, context cancellation, timeouts, stream limits, and graceful shutdown for every goroutine or external operation.

## Security Rules

- Production config must reject demo or short bearer tokens, wildcard mutation allowlists, unknown mutation actions, wildcard Docker operation kinds, and Docker terminal access without explicit `http.allowed_origins`.
- All mutating agent API routes must pass through `actionPolicy.Require(...)` with a named action constant.
- When adding a new mutation action, update `internal/agent/api/security.go`, `internal/agent/config/config.go` production validation, `configs/agent.config.yaml`, and focused deny/allow tests.
- Public error responses must stay generic. Keep details such as IPs, kubeconfig paths, tokens, Docker command output, and provider stderr out of HTTP error bodies.
- Action audit logs may record action, decision, route, request ID, client IP, and reason; never include secrets or raw request payloads.

## Runner Rules

- Execution, Docker, and Agent Runtime work are claim/heartbeat/callback loops. Preserve idempotency around terminal states, cancel, timeout, retry, and stale callbacks.
- Respect `control_plane.max_concurrency`, `default_timeout`, `callback_retry`, and workspace roots.
- Keep workspace path handling bounded under configured roots. Do not let task payloads write arbitrary host paths.
- For Agent Runtime providers, normalize provider output into control-plane callback contracts. Do not leak provider-native state directly into external APIs.
- Metrics and active-task views should reflect claims, misses, heartbeats, callbacks, failures, cancellations, and timeouts.
- Identity Outpost claim, heartbeat, access checks, signed configuration, and event delivery must preserve trust-key validation, configuration-lease replay protection, and redaction.

## Packaging Rules

- `deploy/Dockerfile` builds the generic `soha-agent` image.
- Its `buildpacks-runtime` target adds pinned pack and Docker CLI tooling for a dedicated CNB runner; never share the business-node Docker socket. `buildpacks-daemonless-runtime` adds pinned Podman for direct local lifecycle containers and requires its own Linux rootful runner and exclusive storage, with no daemon socket.
- `deploy/Dockerfile.hermes-agent-runner` builds the Hermes-derived runner image.
- `deploy/kubernetes/**` is a starter manifest; Helm charts live in `opensoha/soha-helm`.
- Dockerfiles consume the complete Root context, all four module manifests and Root `contracts/` source. No sibling context or registry fallback is used for source builds. Use `github.com/opensoha/soha/agent` module/import/ldflags identity and `github.com/opensoha/soha/contracts` SDK; preserve existing public download routes.
- DSH keeps its own Node>=24.9 lock and Node24 image; it is not part of the Root Node22 npm workspace. Runtime skills/MCP assets remain supplied by their explicit Core-owned definitions or same-Root `skills/` packages, never an implicit sibling checkout.

## Testing

- Run focused package tests for affected behavior; broaden to the full gate for architecture, dependency, security, runner/concurrency, packaging, or release changes. Documentation-only collaboration edits need metadata and link checks, not runtime builds.
- Use the committed Root `go.work` for internal source verification, executing this module explicitly. Use `GOWORK=off` only for separate declared external dependency/distribution checks; source success does not prove the SDK is published.
- Run `go test ./internal/agent/api` for auth, action allowlist, route, and stream changes.
- Run `go test ./internal/agent/runner` for claim/callback, Docker runner, Agent Runtime, cancellation, timeout, and metrics changes.
- Run `python3 deploy/hermes-tools/test_plugin.py` for the Hermes controlled-tool adapter; CI also runs this standard-library check.
- Run `go test ./internal/agent/environment` for environment lease, process/container runtime, snapshot, cleanup, and recovery changes.
- Run `go test ./internal/agent/kubernetes` for Kubernetes proxy behavior, YAML, logs, terminal, Helm, port-forward, and CRD changes.
- Run `go vet ./...` and `go test -race ./...` for concurrency or runner changes. For provider claim or executor composition changes, consult [runner composition standards](references/go-engineering-standards.md#runner-contracts).

## CI Gate

Use the toolchain and tool versions declared by [go.mod](../../../go.mod), [Root active CI](../../../../.github/workflows/), and the [release workflow](../../../.github/workflows/release.yml). Run the full gate for dependency, packaging, Dockerfile, workflow, or release changes; local execution uses the workspace verification wrapper and its temporary output paths:

```bash
export GOWORK="$(git rev-parse --show-toplevel)/go.work"
python3 ../scripts/source-workspace.py check-go
go mod verify
go test ./...
python3 deploy/hermes-tools/test_plugin.py
go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
make build
kubectl kustomize deploy/kubernetes/outpost | go run github.com/yannh/kubeconform/cmd/kubeconform@v0.8.0 -kubernetes-version 1.34.1 -strict -summary -
make verify-images # all six Agent runtime images, using the complete Root context
git diff --check
```

CI also runs `golangci-lint v2.14.0` with only-new-issues semantics. A missing local Docker or Kubernetes CLI prerequisite must be covered by the corresponding successful GitHub Actions job.

## References

- `references/go-engineering-standards.md`: mandatory package boundaries, Gin construction, security, runner, module, build, and CI verification rules.
