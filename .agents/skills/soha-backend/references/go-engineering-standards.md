# Soha Go Engineering Standards

Apply this reference to production Go changes in the `soha` repository. Repository contracts and CI are authoritative. Use [Effective Go](https://go.dev/doc/effective_go) for language idioms and load the relevant `cc-skills-golang` guidance for the task, especially `golang-code-style`, `golang-project-layout`, `golang-design-patterns`, `golang-dependency-injection`, `golang-security`, `golang-testing`, and `golang-continuous-integration`.

## Dependency Direction

- Keep the modular monolith and one Go module. Do not split modules or add a generic framework to improve a directory metric.
- Keep `cmd/server` thin: parse startup commands, invoke bootstrap, and own process exit only.
- Keep Gin transport in `internal/api`, orchestration and policy in `internal/application`, business types in `internal/domain`, external technology in `internal/infrastructure`, persistence adapters in `internal/repository`, and dependency/lifecycle wiring in `internal/bootstrap`.
- Keep domain packages free of Gin, HTTP, TLS, GORM, Kubernetes, Helm, database, and provider SDK types.
- Keep application production packages free of concrete repository and infrastructure imports. Define ports in the consuming application package and bind adapters in bootstrap.
- Keep HTTP/TLS console transport contracts in application-owned transport ports such as `virtualization/consoleport`, not in domain models.
- Keep Kubernetes/Helm direct access, cache/live fallback, SPDY, terminal, events, port-forward, and object mapping in `internal/infrastructure/resourcebackend`. Application resource code owns authorization, audit, capability routing, and stable Soha DTOs.

## Packages, Files, and Interfaces

- Split a package only when ownership, dependency direction, or independent change cadence becomes clearer. Moving a God Service across files does not fix its dependency surface.
- Split large files by stable behavior or capability. Keep one significant primary type and its closely related methods together where practical.
- Do not create `utils`, universal repositories, reflection-based CRUD, or empty wrapper packages. Extract duplication only after the semantic pattern is stable.
- Define interfaces where consumed and return concrete implementations from constructors. Prefer capability interfaces over a complete service facade.
- Keep a handler's directly consumed capability interfaces at seven methods or fewer. A larger cohesive state-machine interface requires an explicit reason and focused contract tests.
- Preserve manual constructor injection. Required dependencies must fail construction when absent or typed nil; do not hide optional behavior behind globals, `init`, or setters.
- Keep graph metrics diagnostic. Investigate high concrete dependency degree, but do not wrap stable protocol leaf helpers such as error writers or principal extraction only to lower inferred degree.

## Idiomatic Construction

- Run `gofmt`; use clear package and identifier names, early returns, field-named composite literals, and comments that explain decisions rather than syntax.
- Put `context.Context` first and propagate request cancellation. Bound external calls, retries, queues, reads, response sizes, and goroutine lifetimes.
- Wrap errors with operation context and `%w`; inspect with `errors.Is` or `errors.As`. Return stable public error codes/messages and log only redacted internal causes.
- Acquire resources and immediately arrange cleanup. Report close/flush errors when durability matters.
- Prefer explicit typed routing and small functions over reflection. Do not add an abstraction solely to satisfy duplication or line-count tools.
- Use `crypto/rand` for newly generated runtime keys and tokens. Do not use that rule to replace Soha's documented zero-configuration system-key defaults; deployment config may override those values. Never log secrets, bearer tokens, kubeconfigs, private keys, raw credential payloads, or unredacted provider errors.

## Gin and Stream Boundaries

- Keep handlers limited to parsing, boundary validation, principal extraction, application invocation, and response mapping. Business policy and provider traversal do not belong in handlers.
- Preserve fail-closed authentication and authorization. Reject ambiguous duplicate credential headers and do not accept generic query access tokens.
- Keep trusted proxies disabled by default; only explicit proxy IPs or CIDRs may influence client identity.
- Use single-use, short-lived, path-bound stream tickets. Strictly validate WebSocket Origin and enforce route-appropriate read, idle, first-byte, and total limits.
- Add handler tests for new transport behavior and semantic tests for permission, scope, audit, error, and stream lifecycle changes.

## Complexity and Duplication

- Production cyclomatic complexity must remain at or below 20. Split complex functions into named business steps, not anonymous indirection.
- Run `make complexity-check` for every production Go change. Use `make complexity-report` to inspect `cyclop`, `dupl`, `funlen`, and `maintidx` findings.
- Treat duplicated direct/agent branches as a design defect. Route once through typed capabilities and keep connection-specific adapters behind the port.
- Review each remaining duplication finding semantically. Typed wrappers, metric families, and adapter cache patterns may be clearer when explicit.

## Test and Build Gate

Run focused package tests during development. Before completing an architecture, dependency, security, concurrency, configuration, module, or release change, run the full gate from the repository root:

```bash
GOWORK=off go mod tidy
git diff -- go.mod go.sum
GOWORK=off go mod verify
GOWORK=off go test ./...
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
make complexity-check
BASE_REV="$(git merge-base HEAD origin/main)"
golangci-lint run --new-from-rev="$BASE_REV" ./...
GOWORK=off go run golang.org/x/vuln/cmd/govulncheck@v1.3.0 ./...
GOWORK=off CGO_ENABLED=0 go build -tags embedassets -o /tmp/soha ./cmd/server
git diff --check
```

- Do not accept a `go mod tidy` diff accidentally. Review dependency and checksum changes before keeping them. CI must run tidy on a clean checkout and fail if it produces a diff.
- Keep the existing incremental lint baseline: changed code must introduce zero new issues. Set `BASE_REV` to the actual PR merge base when `origin/main` is unavailable or stale; do not suppress new findings because historical findings exist elsewhere.
- Use table-driven named subtests for behavior matrices. Test observable contracts, failure paths, boundary rejection, cancellation, terminal states, retry, stale callbacks, and typed-nil construction where relevant.
- Run `go test -race` for all concurrency, cache, stream, runner, lease, and lifecycle changes even when the focused package test passes.
- Stage the built `soha-web/dist` artifact before the `embedassets` build. For release or image changes, use the real multi-stage Docker build rather than treating an unstaged backend-only build as equivalent.
- For structural changes, rebuild or update `graphify-out`, confirm there are no import cycles, and compare concrete ownership/dependency paths rather than only God Node degree.
- For image or deployment changes, also follow `$soha-deploy`; a successful Go build alone is not deployment verification.
