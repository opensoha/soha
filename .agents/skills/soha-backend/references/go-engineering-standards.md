# Soha Go Engineering Standards

Apply this reference to production Go changes in the `soha` repository. Repository contracts and CI are authoritative. Use [Effective Go](https://go.dev/doc/effective_go) for language idioms and load the relevant `cc-skills-golang` guidance for the task, especially `golang-code-style`, `golang-project-layout`, `golang-design-patterns`, `golang-dependency-injection`, `golang-security`, `golang-testing`, and `golang-continuous-integration`.

## Dependency Direction

- Keep the modular monolith and one Go module. Do not split modules or add a generic framework to improve a directory metric.
- Organize functional packages by business ownership, not workbench menus. A feature need not create an empty directory in every layer. Existing ingest/control/gateway runtimes retain their explicit process and privilege boundaries.
- Keep `cmd/server` thin: parse startup commands, invoke bootstrap, and own process exit only.
- Keep Gin transport in `internal/api`, orchestration and policy in `internal/application`, business types in `internal/domain`, external technology in `internal/infrastructure`, persistence adapters in `internal/repository`, and dependency/lifecycle wiring in `internal/bootstrap`.
- Keep domain packages free of Gin, HTTP, TLS, GORM, Kubernetes, Helm, database, and provider SDK types.
- Keep application production packages free of concrete repository and infrastructure imports. Define ports in the consuming application package and bind adapters in bootstrap.
- Infrastructure owns HTTP/WebSocket/provider protocol details and egress validation. Application owns route policy, authorization, quotas, and audit; consumer-specific transport ports are appropriate, and standard-library types are not categorically forbidden.
- Repository adapters may implement consumer-owned ports and lock, read, and write within one transaction, but do not own cross-domain policy. Cross-domain SQL may use an explicitly owned transaction adapter or read projection. Preserve transaction-local checks, lock order, version checks, and constraints; moving checks outside the transaction can introduce TOCTOU races.
- Keep HTTP/TLS console transport contracts in application-owned transport ports such as `virtualization/consoleport`, not in domain models.
- Keep provider-specific syntax checks behind an explicit runtime validation port. For Worker Pool, Kubernetes names and label syntax belong to the Kubernetes adapter; capacity, ownership and reserved-label policy stay in domain. Registration, persistence and durable-task replay must apply the same business and technical checks before side effects. Missing optional runtime validation disables that capability; it must not silently bypass checks or remove scope coordinates from historical reads.
- Keep Kubernetes/Helm direct access, cache/live fallback, SPDY, terminal, events, port-forward, and object mapping in `internal/infrastructure/resourcebackend`. Application resource code owns authorization, audit, capability routing, and stable Soha DTOs.
- Keep domain rules distinct from platform technical utilities. Business owners extract actual tool calls from their typed plans/checkpoints; a generic JSON walker must not infer calls from arbitrary business fields. Give each model, tool, approval, Native, and personal-environment capability one clear runtime owner. Return DTOs with the provider/route receipt for the operation that actually succeeded; preserve approval and durable receipt flows, and never choose duplicate execution branches by registration order.

## Packages, Files, and Interfaces

- Split a package only when ownership, dependency direction, or independent change cadence becomes clearer. Moving a God Service across files does not fix its dependency surface.
- Split large files by stable behavior or capability. Keep one significant primary type and its closely related methods together where practical.
- Do not create `utils`, universal repositories, reflection-based CRUD, or empty wrapper packages. Extract duplication only after the semantic pattern is stable.
- Define interfaces where consumed and return concrete implementations from constructors. Prefer capability interfaces over a complete service facade.
- Keep a handler's directly consumed capability interfaces at seven methods or fewer. A larger cohesive state-machine interface requires an explicit reason and focused contract tests.
- Preserve manual constructor injection. Required dependencies must fail construction or an explicit assembly validation when absent or typed nil. Bootstrap constructs, binds, validates, and manages lifecycle. Business approvals, heartbeats, recovery, and lease decisions belong to application services; SQL lock mechanics belong to repositories. Two-phase assembly is acceptable only when all required ports are bound and validated before `Start`; never discover required dependencies through runtime assertions or hidden global/`init` setup. Declare optional capabilities and their unsupported/degraded behavior explicitly; optional absence does not require the whole process to fail.
- Keep graph metrics diagnostic. Investigate high concrete dependency degree, but do not wrap stable protocol leaf helpers such as error writers or principal extraction only to lower inferred degree.

## Idiomatic Construction

- Run `gofmt`; use clear package and identifier names, early returns, field-named composite literals, and comments that explain decisions rather than syntax.
- Put `context.Context` first and propagate request cancellation. Bound external calls, retries, queues, reads, response sizes, and goroutine lifetimes.
- Wrap errors with operation context and `%w`; inspect with `errors.Is` or `errors.As`. Return stable public error codes/messages and log only redacted internal causes.
- Acquire resources and immediately arrange cleanup. Report close/flush errors when durability matters.
- Prefer explicit typed routing and small functions over reflection. Do not add an abstraction solely to satisfy duplication or line-count tools.
- Use `crypto/rand` for newly generated runtime keys and tokens. Do not use that rule to replace Soha's documented zero-configuration system-key defaults; deployment config may override those values. Never log secrets, bearer tokens, kubeconfigs, private keys, raw credential payloads, or unredacted provider errors.

## Gin and Stream Boundaries

- Keep handlers limited to parsing, boundary validation, principal extraction, application invocation, and response mapping. Do not coordinate multi-step business writes such as saving a connection and credentials; application services own that flow. Business policy and provider traversal do not belong in handlers.
- Preserve fail-closed authentication and authorization. Reject ambiguous duplicate credential headers and do not accept generic query access tokens.
- Keep trusted proxies disabled by default; only explicit proxy IPs or CIDRs may influence client identity.
- Use single-use, short-lived, path-bound stream tickets. Strictly validate WebSocket Origin and enforce route-appropriate read, idle, first-byte, and total limits.
- Add handler tests for new transport behavior and semantic tests for permission, scope, audit, error, and stream lifecycle changes.
- For model/tool/approval/Native/personal-environment behavior, test declared capabilities, provider/route receipts, approval preservation, and unique execution ownership where affected.

## Complexity and Duplication

- Production cyclomatic complexity must remain at or below 20. Split complex functions into named business steps, not anonymous indirection.
- Run `make complexity-check` for every production Go change. Use `make complexity-report` to inspect `cyclop`, `dupl`, `funlen`, and `maintidx` findings.
- Treat duplicated direct/agent branches as a design defect. Route once through typed capabilities and keep connection-specific adapters behind the port.
- Review each remaining duplication finding semantically. Typed wrappers, metric families, and adapter cache patterns may be clearer when explicit.

## Test and Build Gate

Run focused package tests during development. Documentation-only edits need metadata, link, and consistency checks rather than Go builds. Before completing an architecture, dependency, security, concurrency, configuration, module, or release code change, run the applicable full gate from the repository root. Versions and exact CI commands are owned by [CI](../../../../.github/workflows/ci.yml) and [release](../../../../.github/workflows/release.yml); local execution follows the workspace verification wrapper and output isolation rules.

For incremental lint, set `SOHA_LINT_BASE_REF` to the verified PR target or actual configured upstream; an arbitrary feature-branch tracking ref is not a substitute for the integration target.

```bash
GOWORK=off go mod tidy
git diff -- go.mod go.sum
GOWORK=off go mod verify
GOWORK=off go test ./...
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
make complexity-check
: "${SOHA_LINT_BASE_REF:?set the verified PR base or upstream ref}"
BASE_REV="$(git merge-base HEAD "$SOHA_LINT_BASE_REF")"
golangci-lint run --new-from-rev="$BASE_REV" ./...
GOWORK=off go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
GOWORK=off CGO_ENABLED=0 go build -tags embedassets -o /tmp/soha ./cmd/server
git diff --check
```

- Do not accept a `go mod tidy` diff accidentally. Review dependency and checksum changes before keeping them. CI must run tidy on a clean checkout and fail if it produces a diff.
- Keep the existing incremental lint baseline: changed code must introduce zero new issues. Resolve the baseline from the actual PR base or configured upstream; do not assume `origin/main` exists or is current.
- Use table-driven named subtests for behavior matrices. Test observable contracts, failure paths, boundary rejection, cancellation, terminal states, retry, stale callbacks, and typed-nil construction where relevant.
- Existing Go test discovery runs recursive import checks in `internal/application/dependency_boundary_test.go` and `internal/domain/dependency_boundary_test.go`. The application check freezes pre-existing SDK imports and selected connector HTTP symbols at exact file/dependency pairs; each has an owner and must disappear when its source dependency does. These are migration debt, not permission for new imports or directory-wide exceptions.
- Gateway/console transport and VM Handler checks cover their named production families. Handler capability checks follow the actual consumer fields and embedded interfaces. These static checks do not prove authorization, transaction, replay, startup or protocol behavior; run the affected owner's semantic tests and report uncovered paths rather than claiming a universal architecture gate.
- Run `go test -race` for all concurrency, cache, stream, runner, lease, and lifecycle changes even when the focused package test passes.
- Stage the built `soha-web/dist` artifact before the `embedassets` build. For release or image changes, use the real multi-stage Docker build rather than treating an unstaged backend-only build as equivalent.
- For broad structural or dependency changes, query the existing `graphify-out` first. Refresh only after source changes are stable and worktree contents are understood; follow the repository Graphify runbook and shrink guards without defaulting to `--force`. Compare concrete ownership paths rather than only God Node degree.
- For image or deployment changes, also follow `$soha-deploy`; a successful Go build alone is not deployment verification.
