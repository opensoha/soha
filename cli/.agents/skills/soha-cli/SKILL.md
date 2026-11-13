---
name: soha-cli
description: Change or review the standalone soha CLI, authentication/profile handling, command metadata, or release packaging. Ordinary CLI use does not require this implementation skill.
---

# Soha CLI

## Overview

Use this skill for the `soha-cli` repository only. The CLI is a standalone Go
client that talks to Soha APIs through released `github.com/opensoha/soha-contracts`
types and local HTTP calls. It should remain testable without a real server.

## Workflow

1. Read `AGENTS.md` before implementation or substantive review. Use `internal/sohacli/command_metadata.go` for command-surface changes. Read CI workflows for build or release changes; command help, docs, completion, and dispatch flow from metadata.
2. Inspect the working tree and preserve unrelated edits. Add behavior in the smallest existing owner: profiles/context, MCP, skills, plugins, governance, AI platform/knowledge/evaluation, cloud diagnostics, tokens, service accounts, or Gateway calls.
3. Keep the `Run(ctx, args, Runtime)` boundary. Use `Runtime.In`, `Runtime.Out`, `Runtime.Err`, `Runtime.ConfigPath`, and injectable HTTP clients in tests; do not write directly to `os.Stdout` or `os.Stderr` outside `cmd/soha/main.go`.
4. Use stdlib `flag` plus `newRuntimeFlagSet`. Return errors from handlers and let `Run` map them to exit codes.
5. Use `APIClient`, focused clients, and contracts DTOs for HTTP work. Do not import `github.com/opensoha/soha/internal/**` or sibling checkout internals.
6. Keep the committed `go.mod` on a released `soha-contracts` tag. Use temporary local `go.work` only for unreleased multi-repo contract work.
7. For command-surface changes, update metadata, dispatch, completion words where relevant, tests, and regenerate `docs/commands.md` with `go run ./cmd/soha docs --format markdown > docs/commands.md`.
8. Keep `soha mcp` as the canonical direct stdio entry. Official SaaS is the endpoint default; self-hosted client configurations must carry an explicit `--base-url`. Keep `soha mcp start` compatible.
9. Source the agent-facing `$soha` skill from the verified `soha-skills/agent-skills/soha` release asset. Do not embed or generate a second copy in the CLI. Resolve normal installs and updates to the latest stable release; reserve explicit source pins for rollback and reproducibility rather than adding a routine `--skills-version` flow.

## Change Contract And Evidence

- Before editing, identify the command/client owner, callers, intended behavior,
  preserved flags/output/exit-code semantics and the applicable test entry.
  Read only relevant references; reuse unchanged context, and reload necessary
  rules after task switches or loss of critical context.
- Existing commands demonstrate current behavior, not automatic compliance with
  the effective public contract. Explain conflicts rather than copying legacy
  behavior or relaxing a baseline to make tests pass.
- A local command fix must not silently change shared HTTP defaults, profile
  handling or MCP transport. Shared changes require checks of their consumers;
  do not create a second client or duplicate Core business logic.
- Record CLI/Core/contracts and installed skill versions or resolved SHAs for
  integration evidence. Keep unit/httptest, real-server smoke, packaged-launcher
  and release-download evidence separate.
- Report pass, fail, skip and not-run explicitly. A successful build, mocked
  response or upload is not proof of authenticated runtime or anonymous release
  behavior. Existing release/security gates remain mandatory when applicable.
- Plans and skills describe work; they do not authorize production operations,
  installation, publishing or additional cross-repository changes.

## Security Rules

- Local config defaults to `~/.soha/config.json` or `SOHA_CONFIG`; preserve `0700` parent dirs and `0600` config file permissions.
- Never print full access tokens, refresh tokens, passwords, service account tokens, or plugin secrets. Use existing redaction helpers and test for leaks.
- `SOHA_TOKEN` is a one-command override and must not be persisted back to the profile.
- Expired profile access tokens should refresh with the stored refresh token and persist rotated token state; refresh requests must not send an Authorization header.
- Interactive passwords go to stderr prompts and must not echo into stdout, logs, or test failures.
- `soha setup`, `soha add`, and skill/plugin install paths must stay idempotent, path-safe, checkable, and dry-run friendly.
- The npm package must remain a checksum-verifying launcher for the native Go binary. Do not reimplement CLI commands or MCP transport in JavaScript.
- The default remote skill source must resolve the latest stable GitHub Release to a concrete semantic-version tag before downloading assets; never install from a branch. Keep explicit version pins for rollback and reproducibility. Preserve checksum, external and embedded manifest, validation-report, per-file hash, compatibility, extraction-bound, and cache-hit integrity checks.
- Managed raw skills must stage a complete runtime directory before activation, retain a verified previous generation, expose status/update/remove/rollback, and append schema-valid install audit events. User scope is the default; project scope writes repository-local client and skill paths, while explicit destination flags win.
- Keep skill download/install independent from Soha login. MCP tool invocation still uses profile authentication and backend authorization; do not put Soha bearer tokens on GitHub release requests.

## Testing

- For command logic, write tests through `Run` with injected buffers, temp config paths, and `httptest.Server`.
- For HTTP clients, cover request path, method, auth headers, refresh behavior, and redacted output.
- For command-surface changes, assert metadata, dispatch, help, completion, and generated command docs stay aligned.
- For login and profile changes, cover `0700`/`0600` permissions, atomic writes, non-interactive credentials, token rotation, and secret-free errors.
- For skill releases, cover a real tar.gz fixture, latest GitHub Release resolution, explicit version pins, cache reuse and tampering, checksum mismatch, unsafe archive members, generation activation/rollback, scope precedence, audit output, and the packaged release smoke path.
- Run `npm test` and `npm pack --dry-run` in `npm/cli` when launcher or release assets change.
- Run affected package tests for local behavior changes; documentation-only collaboration edits need metadata and link checks, not runtime builds.
- Run `GOWORK=off go test ./...` before changes that touch contracts, module state, Dockerfile, or release behavior.
- Run `go vet ./...` and `go test -race ./...` for concurrency, token refresh, MCP stdio, or release-sensitive work.

## Release Integrity

- Keep public tags immutable. Build Linux, macOS, and Windows for `amd64` and `arm64` and inject version, commit, and UTC build date.
- Tag only a `main` commit whose matching `cli-ci` run passed. The Release workflow packages and publishes artifacts but does not replace the complete race, vet, vulnerability, lint, npm, and Docker gate.
- Publish the current 13-asset contract: `checksums.txt`, six bare binaries used by the npm launcher, and six platform archives. Verify the exact remote asset name set after upload.
- Force release recovery paths to end with a published, non-prerelease Release. Do not treat an authenticated `gh release view` or a green upload process as proof that anonymous downloads work.
- Keep the npm version equal to the tag without `v`. Publish `@opensoha/cli` after GitHub assets with provenance; make retries idempotent only when the exact version already exists.
- Before release completion, verify anonymous binary and checksum downloads, registry metadata, and a clean-cache `npx -y @opensoha/cli@<version> version --json` invocation.
- For auth, MCP, setup, or delivery-tool releases, also log in against a local Soha instance, inspect redacted profile state and permissions, and make an authenticated `capabilities` call.

## CI Gate

Use Go `1.27.2` and run the release-sensitive gate with the root workspace disabled:

```bash
GOWORK=off go mod tidy
git diff --exit-code -- go.mod go.sum
GOWORK=off go mod verify
GOWORK=off go test ./...
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
golangci-lint run --timeout=5m
GOWORK=off CGO_ENABLED=0 go build -o /tmp/soha-cli ./cmd/soha
(cd npm/cli && npm test && npm pack --dry-run)
docker build -f Dockerfile -t ghcr.io/opensoha/soha-cli:test .
git diff --check
```

Use `golangci-lint v2.14.0`; the workflow action is the executable source of truth for that version. `git diff --check` is an additional mandatory local pre-submit check rather than a current `ci.yml` step. Dockerfile, npm launcher, workflow, or release changes require their corresponding checks. A missing local Docker daemon is not a pass and must be covered by a successful GitHub Actions Docker job.
