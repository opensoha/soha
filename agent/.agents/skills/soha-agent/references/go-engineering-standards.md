# Soha Agent Go Engineering Standards

Apply this reference to production Go changes in `soha-agent`. Repository contracts and CI are authoritative. Use [Effective Go](https://go.dev/doc/effective_go) for language idioms and load the task-relevant `cc-skills-golang` guidance, especially code style, project layout, design patterns, security, concurrency/context, testing, and continuous integration.

## Repository Boundary

- Keep `soha-agent` a standalone Go module and runtime. Never import `github.com/opensoha/soha/internal/**`.
- Exchange stable API shapes through released `github.com/opensoha/soha-contracts` types. A sibling replacement may be used for development and Docker build context, but changes must remain compatible with the intended released tag.
- Keep `cmd/agent/main.go` minimal and explicit. Construct configuration, logger, Kubernetes client, runner, and HTTP server through `internal/agent/bootstrap`.
- Do not add mutable global services, implicit `init` setup, a service locator, or a DI container. Define narrow consumer-owned interfaces and inject concrete implementations.

## Package Ownership

- `internal/agent/config` owns Viper loading, env mapping, defaults, secret-file reading, and production validation.
- `internal/agent/api` owns Gin middleware, authentication, action policy, route registration, HTTP/WebSocket boundaries, diagnostics, and public error shaping.
- `internal/agent/kubernetes` owns Kubernetes/Helm clients, provider objects, resource mutation, logs, terminal, port-forward, and cluster-specific mapping.
- `internal/agent/runner` owns claim, heartbeat, callback, workspace, Docker, Agent Runtime, retry, cancel, timeout, metrics, and terminal-state idempotency.
- `internal/agent/environment` owns environment leases, runtime launch/stop, snapshots, cleanup, and recovery.
- Keep `api/server.go` as assembly and lifecycle code. Register endpoint families from focused `routes_*.go` files. Splitting methods across files without reducing a broad dependency or responsibility surface is not an architecture improvement.
- Do not create generic helpers, reflection CRUD, or a universal runtime facade. Extract shared logic only when semantics and ownership are stable.
- Keep every new production function at cyclomatic complexity 20 or below. When changing an existing hotspot above 20, reduce or split it as part of the change; do not add another branch to a known oversized dispatcher.

## Idiomatic and Bounded Go

- Run `gofmt`; prefer early returns, explicit field names, clear identifiers, focused functions, and decision-oriented comments.
- Put `context.Context` first where operations can block. Propagate cancellation into Kubernetes, HTTP, Docker, process, and callback work.
- Bound concurrency, queues, response bodies, logs, WebSocket frames, retries, workspaces, command duration, and external calls.
- Arrange cleanup immediately after resource acquisition. Avoid goroutines without a visible owner, cancellation path, and shutdown wait.
- Wrap internal errors with `%w`, but return stable generic public errors. Redact tokens, kubeconfig paths, IP-sensitive causes, Docker output, provider stderr, and request payload secrets.
- Use `crypto/rand` for security values and constant-time comparison where secret comparison is exposed to timing observation.

## HTTP and Configuration Security

- Use `gin.New()` with explicit middleware. Preserve authentication, request IDs, recovery, audit, route limits, and safe logging order.
- Every mutation must pass `actionPolicy.Require` with a named allowlisted action. Production must reject wildcard or unknown mutation and Docker operation actions.
- Preserve strict WebSocket Origin validation, configured origin requirements for Docker terminal, read limits, idle/deadline handling, and generic stream errors.
- Keep diagnostics summary-only. Never return raw readiness failures or sensitive configuration.
- Production must reject placeholder/short bearer tokens. Prefer file-mounted control-plane tokens where supported; paths must be absolute, regular, non-symlink files with strict permissions and race-resistant open checks.
- Keep the default listener on loopback. Non-loopback exposure requires strong authentication and explicit origins for browser-facing streams.

## Runner Contracts

- Keep runner assembly independent of optional executor enablement: each executor contributes its own supported providers and claim identity. A shared claim may combine providers for one cluster identity, but a protected Manifest task must retain the admission receipt required by its contract from that specific claim response. Do not require that receipt for unrelated providers or empty claims. Preserve the action allowlist, attempt/callback, and cancellation isolation. A provider capability declaration must match the configuration that can actually claim and execute it; do not make every optional executor mandatory at startup.
- Validate required ports and typed-nil dependencies before startup. Two-phase assembly is acceptable when all components are built, bound, and validated before any are started. Keep interfaces consumer-owned, provider-specific technology isolated, and durable state/resource ownership explicit.
- For claim/executor composition changes, cover Manifest only, Helm only, both enabled, both disabled or missing identity, mismatched cluster identity, rejection without side effects, callback retry, and late callback. Select only the affected checks for a given change.

- Treat execution, Docker, Identity Outpost, environment runtime, and Agent Runtime flows as durable state machines with explicit ownership.
- Make terminal states idempotent and reject stale callbacks after cancel, timeout, or retry. Rotate attempt credentials where the control-plane contract requires it.
- Enforce `max_concurrency`, default timeouts, retry limits, allowed operation kinds, and workspace roots. Never allow payload paths or commands to escape configured boundaries.
- Normalize provider-native results into Soha contract DTOs. Keep secret material and raw provider state out of callbacks, metrics, logs, and diagnostics.
- Test claim misses, heartbeat, callback retry, cancellation, timeout, late callback, shutdown, metric accounting, and concurrent capacity changes when related behavior changes.

## Module and Build Rules

- Match the Go and toolchain versions declared by `go.mod`. Do not silently depend on a local `go.work`.
- Review every direct dependency and `go mod tidy` change. Prefer the standard library or existing dependencies when they clearly satisfy the requirement.
- Keep generic and Hermes images buildable from the same released contract boundary. Do not bake runtime tokens, kubeconfigs, provider credentials, or test files into images.
- Build release binaries with `GOWORK=off`, `CGO_ENABLED=0`, `-trimpath`, and the release workflow's version `ldflags`.

## Verification Gate

Use the [skill's Testing section](../SKILL.md#testing) to select checks by impact. Ordinary production Go fixes need the affected package's behavioral regression checks; they do not automatically require the full gate below. Documentation-only changes need content and link checks.

Before completing architecture, module/dependency, public-contract, security, concurrency/runner, image/packaging, or release changes, run the applicable full gate below and the affected artifact checks. CI requirements remain unchanged. Reuse successful results while the relevant code and environment are unchanged; expand or repeat checks only for new changes, failures, or a concrete unresolved risk.

Tool versions and exact CI commands are owned by [CI](../../../../.github/workflows/ci.yml) and [release](../../../../.github/workflows/release.yml). For local incremental lint, set `SOHA_AGENT_LINT_BASE_REF` to the verified PR target or configured integration upstream, then use the workspace verification wrapper for the selected checks.

```bash
GOWORK=off go mod tidy
git diff -- go.mod go.sum
GOWORK=off go mod verify
GOWORK=off go test ./...
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 20 internal/agent | awk '!/_test[.]go/'
: "${SOHA_AGENT_LINT_BASE_REF:?set the verified PR base or upstream ref}"
BASE_REV="$(git merge-base HEAD "$SOHA_AGENT_LINT_BASE_REF")"
golangci-lint run --new-from-rev="$BASE_REV" ./...
GOWORK=off go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
GOWORK=off CGO_ENABLED=0 go build -trimpath -o /tmp/soha-agent ./cmd/agent
git diff --check
```

- Review module diffs before retaining them; do not use tidy to normalize unrelated user changes. CI must run tidy on a clean checkout and fail if it produces a diff.
- Use the actual PR base revision for new-issue linting. Require zero new lint issues relative to that base. Do not hide a new finding behind the repository's historical baseline.
- Treat the gocyclo command as an incremental report until the repository-wide baseline reaches zero: changed production functions must not appear above 20, and no new hotspot may be introduced.
- Use named table-driven subtests for validation matrices. Test public behavior and boundary rejection rather than private implementation shape.
- Run the relevant real Docker build when Dockerfiles, contracts, release packaging, Helm, or runtime dependencies change.
- Validate both the generic agent and Hermes runner artifacts when shared contracts, runner behavior, or common image inputs change.
- Keep focused commands useful: `go test ./internal/agent/api`, `go test ./internal/agent/kubernetes`, and `go test ./internal/agent/runner`.
