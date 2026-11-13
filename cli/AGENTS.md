# Soha CLI Repository Rules

## Scope

These rules apply to the entire `soha-cli` repository. In a confirmed OpenSoha workspace,
read `../AGENTS.md` once; do not assume it is loaded across the Git root.
For a standalone clone, these local rules are sufficient to begin work.
Before implementation or substantive review, read [.agents/skills/soha-cli/SKILL.md](.agents/skills/soha-cli/SKILL.md)
and its references relevant to the task. Skill symlinks provide discovery, not proof of loading.
Read README for unfamiliar command behavior and the relevant workflow under
`.github/workflows/` for build, dependency, or release work.

The executable sources of truth are:

- `.github/workflows/ci.yml` for pull-request and `main` gates.
- `.github/workflows/release.yml` for tagged artifacts, GitHub Releases, npm,
  and GHCR publishing.
- `internal/sohacli/command_metadata.go` for command discovery, help, generated
  docs, and completion metadata.
- Released `github.com/opensoha/soha-contracts` types for public API contracts.

When these rules disagree with an executable workflow, first determine whether
the workflow or documentation is stale, then update both in the same change.

## Task Contract And Evidence

- Identify the command, owning client/runtime, affected consumers and behavior to preserve
  before editing. Include stdout/stderr, exit status, non-interactive behavior and public
  request/response semantics where relevant; a small textual change is not permission to
  change global defaults or the command engine.
- Reuse unchanged guidance within a task; after a handoff or changed source, reload only the
  missing relevant constraints. Read-only questions need only the evidence they use.
- Existing code establishes current behavior, not automatic compliance with the target
  contract. Resolve substantive conflicts using source and tests, record the decision, and
  do not weaken assertions or compatibility baselines to obtain a pass.
- Separate local fixtures, actual Core integration, packaged launcher execution and release
  acceptance. Record exact CLI/Core/Contracts revisions or artifact versions; branch names
  and a successful build alone do not establish a tested combination.
- Report passed, failed, skipped and not-run checks distinctly. Reuse a successful result only
  while the relevant source, dependencies and environment remain unchanged.
- Collaboration-document-only changes require content, reference and diff checks, not a
  runtime build or release. Existing security and release gates below remain mandatory for
  changes to which they apply. Skills and historical plans do not authorize installation,
  remote writes, publishing or merging a pull request.

## Ownership And Boundaries

- Keep `soha-cli` a standalone Go client. Do not import `soha/internal/**` or
  rely on a sibling core checkout at build or release time.
- Keep `go.mod` on a released `soha-contracts` version. Use an uncommitted local
  `go.work` only while coordinating unreleased contract changes.
- Keep Cloud-only lifecycle, billing, quota, and operations logic outside this
  repository. The CLI may call public Cloud APIs without owning their logic.
- Keep the npm package a checksum-verifying launcher for the native Go binary.
  Do not implement a second CLI or MCP server in JavaScript.
- Official agent-facing skills belong to `opensoha/soha-skills`. The CLI must
  install verified release assets rather than embed another copy.

## Implementation Invariants

- Preserve `Run(ctx, args, Runtime)` and injectable stdin, stdout, stderr,
  config path, clock, and HTTP clients. Only `cmd/soha/main.go` may bind process
  I/O directly.
- Use stdlib `flag` through the existing runtime flag helpers. Return errors
  from handlers and let `Run` map them to exit codes.
- Route HTTP work through existing focused clients and contracts DTOs. Test
  methods, paths, headers, payloads, refresh behavior, and error redaction.
- For every command-surface change, update command metadata, dispatch,
  completion hints where applicable, tests, and generated `docs/commands.md`.
- Keep `soha mcp` as the canonical stdio entry and `soha mcp start` compatible.
  Default to the official SaaS endpoint; persist an explicit `--base-url` for
  self-hosted client setup.
- Resolve the normal skill install/update path to the latest stable GitHub
  Release. Do not add a routine `--skills-version` flow. Explicit source pins
  remain available only for rollback and reproducible installs.

## Security Invariants

- Preserve `0700` profile directories and `0600` profile files.
- Never print full access tokens, refresh tokens, passwords, service-account
  tokens, plugin secrets, or authorization headers. Redact diagnostic output
  and test that failure messages do not leak secrets.
- Treat `SOHA_TOKEN` as an invocation-only override and never persist it.
- Refresh expired access tokens with the stored refresh token, persist rotated
  tokens atomically, and never send an Authorization header on refresh calls.
- Keep interactive passwords off stdout and disable terminal echo.
- Keep setup, skill, and plugin installation idempotent, path-safe, auditable,
  dry-run/check capable, and fail-closed on integrity errors.
- Keep GitHub skill downloads independent from Soha login. Never send a Soha
  bearer token to GitHub release endpoints.

## Change-To-Validation Matrix

| Change | Required focused validation |
| --- | --- |
| Command, flag, help, or completion | `Run` tests, metadata/docs regeneration, completion assertions |
| Profile, login, token refresh, or config | permission, atomic-write, refresh, redaction, and non-interactive tests |
| HTTP or contracts usage | `httptest.Server` request/response tests and `GOWORK=off` compatibility checks |
| MCP, setup, skills, or plugins | stdio/config fixtures, idempotency, `--dry-run`, `--check`, path safety, and integrity tests |
| npm launcher or release download | `npm test`, `npm pack --dry-run`, checksum/cache/tamper tests, and real tagged `npx` smoke |
| Dockerfile, module, CI, or release workflow | the complete CI gate plus a real Docker or corresponding successful Actions job |

## Complete CI Gate

Use the change-to-validation matrix during iteration. This full gate applies
to the changes that require it and to CI/release verification, not every edit.

Use Go `1.27.2`, Node.js `20.x`, `govulncheck v1.8.0`, and
`golangci-lint v2.14.0`. Run release-sensitive checks with the parent workspace
disabled:

```bash
GOWORK=off go mod tidy
git diff --exit-code -- go.mod go.sum
GOWORK=off go mod verify
GOWORK=off go test ./...
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
golangci-lint run --timeout=5m
GOWORK=off CGO_ENABLED=0 go build -o /tmp/soha ./cmd/soha
(cd npm/cli && npm test && npm pack --dry-run)
docker build -f Dockerfile -t ghcr.io/opensoha/soha-cli:test .
git diff --check
```

Do not report a missing local tool or Docker daemon as a pass. For release work,
use the successful matching GitHub Actions job to complete verification. The
current `ci.yml` enforces the commands through the Docker build;
`git diff --check` is an additional mandatory local pre-submit check.

## Release Contract

- Treat a published semantic-version tag as immutable. Ship subsequent fixes
  under a new patch version rather than moving a public tag.
- Tag only a commit already merged to `main` with a successful matching
  `cli-ci` run. The packaging-focused Release workflow does not replace the
  complete race, vet, vulnerability, lint, npm, and Docker gate.
- Build six targets: Linux, macOS, and Windows on `amd64` and `arm64`.
- Publish exactly 13 custom GitHub assets: `checksums.txt`, six bare binaries,
  and six platform archives. GitHub's two source archives are additional.
- Inject version, commit, and UTC build date into every native binary. Verify
  the identity with `soha version --json`.
- Publish a non-draft, non-prerelease GitHub Release and verify the remote asset
  name set exactly. A green upload command without remote verification is not
  sufficient.
- Publish `@opensoha/cli` only after the GitHub Release exists. The npm version
  must equal the tag without the `v` prefix, use public provenance, and allow
  an idempotent retry when that exact version already exists.
- The launcher must download the matching bare binary and `checksums.txt`,
  enforce download bounds and HTTPS, verify SHA-256 before execution, and
  revalidate cached binaries.

Before declaring a release complete, require all of the following:

1. The tagged Release workflow and every dependent job succeed.
2. Anonymous requests can download `checksums.txt` and at least one native
   binary from the public GitHub Release.
3. `npm view @opensoha/cli@<version>` returns the expected version and
   provenance metadata.
4. A clean-cache `npx -y @opensoha/cli@<version> version --json` downloads,
   verifies, and executes the matching native binary.
5. For login, profile, MCP, setup, or delivery-tool changes, the released CLI
   logs in to a local Soha instance, preserves config permissions, and performs
   an authenticated `capabilities` call without exposing credentials.

## Working Tree Discipline

- Preserve unrelated local modifications and generated artifacts owned by the
  user. Stage only task-owned hunks.
- Do not hand-edit generated command docs, release archives, binaries, caches,
  or coverage output. Regenerate them through their owning command or workflow.
- Report validation precisely, including checks delegated to GitHub Actions and
  any integration path that was not exercised.

## Graphify

- 跨模块依赖、共享能力影响、模块迁移或跨仓任务，先查询已有图谱并核对源码；在已确认的多仓工作区按 [Graphify 开发手册](../docs/guides/graphify-development/runbook.md)检查新鲜度、查询和刷新。
- 结构变更稳定后显式刷新受影响仓库及跨仓图；普通小修改、文案和局部样式不自动建图。刷新失败先诊断，不默认使用 `--force`。
- 独立克隆时使用本仓 `graphify-out/graph.json` 与原生 Graphify 命令；缺少工具或图谱时直接查源码并说明缺口，不要求补齐相邻仓库。
