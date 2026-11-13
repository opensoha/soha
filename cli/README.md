# OpenSoha Soha CLI

Root `cli/` contains the independently packaged command-line client for OpenSoha Soha.
The CLI command is `soha`, and the Go module is `github.com/opensoha/soha/cli`.

The CLI connects to a Soha HTTP API by base URL, so the same client can talk to
a self-hosted Soha server or a compatible Soha Cloud endpoint. Cloud-specific
lifecycle, billing, quota, and operations logic belongs outside this repository.

## Install

Run the official CLI without a global install through the thin npm launcher:

```sh
npx -y @opensoha/cli@latest mcp
npx -y @opensoha/cli@latest setup --client codex --mode both
```

The npm launcher is published by the tagged release workflow.

The npm package does not implement MCP in JavaScript. It downloads the native
binary from the matching GitHub release, verifies `checksums.txt`, caches it by
version, and rechecks the cached binary before execution.

Install the existing legacy release with Go (the new source module path is not yet publicly qualified):

```sh
go install github.com/opensoha/soha-cli/cmd/soha@v0.1.8
soha version
```

For release binaries, download the archive for your platform from the GitHub
release, then verify it against `checksums.txt` before placing `soha` on your
`PATH`:

```sh
sha256sum -c checksums.txt --ignore-missing
tar -xzf soha_0.1.8_linux_amd64.tar.gz
install -m 0755 soha /usr/local/bin/soha
```

Windows releases are published as `.zip` archives and use the same
`checksums.txt` file. Release builds inject `version`, `commit`, and `date`
through Go ldflags; `soha version --json` is the machine-readable verification
command for installed binaries.

## Build

Internal source builds use Go1.27.4 and the committed Root workspace containing
Core, Contracts, CLI and Agent. From Root:

```sh
make -C cli test
make -C cli build
npm test --workspace @opensoha/cli
npm pack --workspace @opensoha/cli --dry-run
make -C cli verify-image
```

The image uses the complete Root context and the same `contracts/` source.
`make build` injects the real Root commit and UTC build date into the existing
ldflags symbols. Set `VERSION=0.1.8` when intentionally producing that CLI version;
without it the native development version remains unchanged. Root and CLI do not
share a product version.

The module/import/ldflags identity is `github.com/opensoha/soha/cli`.
The launcher package, checksum verification and existing release download URLs
remain unchanged. Root Node22 installs its dependency-free workspace; it adds no install hook. Nested `.github/workflows` preserve the old
release contract, while Root workflows own active source gates. Publisher, tag
and public download changes remain separate B5 work.

For external declared-dependency qualification, run `make -C cli test-declared`
or explicitly use `GOWORK=off` from `cli/`. That result and internal source
compatibility are separate evidence. Missing same-Root source must fail; do not
create another sibling workspace or silently fall back to a published SDK.

## Contracts Compatibility

`soha-cli` treats `github.com/opensoha/soha/contracts` as its public API source.
B4 pins the actual SDK checkpoint in `go.mod` and uses Root `contracts/` for
internal source validation. This does not assert that the declared SDK is publicly
available or that a new module location has been published. External consumers
and `GOWORK=off` distribution checks retain their own qualification. Contract upgrades should be reviewed with this minimum matrix:

| soha-cli | soha-contracts | Compatible Soha API | Notes |
| --- | --- | --- | --- |
| Published legacy `v0.1.x` | Legacy `v0.1.x` SDK | `v0.1.x` beta API | Existing release line; the new Go namespace requires a separately qualified release. |
| Root source candidate | same-commit `contracts/` | same-commit API | Complete each module gate and distinguish source results from declared external consumer qualification. |

When a contracts change is not backward compatible, release a new contracts tag
first, update this module in a dedicated commit, and run the consumer matrix for
`soha`, `soha-web`, `soha-cli`, and `soha-agent` before tagging the CLI.

## Configuration

The v2 authentication features below are a **pending release combination**:
they require matching contracts, Core and Web versions, with CLI authentication
enabled by the administrator. Existing releases retain their v1 login behavior.

Local profiles are stored under `~/.soha/config.json` by default with `0600`
permissions (a protected current-user ACL on Windows). New v2 profiles contain
metadata and a credential reference. `auto` uses the system keyring and fails
explicitly if unavailable; it never silently falls back. Explicit `file` storage
puts plaintext secrets in private files under the adjacent `credentials/`
directory. Keep both configurations and credentials out of source control and logs.

Profile `expiresAt` is the access token expiration time. Commands automatically
refresh an expired access token with the stored refresh token and persist the
rotated token set. If a profile has an expired access token and no refresh
token, run `soha login` again. Legacy plaintext v1 profiles migrate to a private
file envelope after a successful refresh; sign in again to choose keyring storage.
`SOHA_TOKEN` overrides authentication without refreshing or changing the profile.
With `SOHA_SERVER`, it works without a configuration file. Server identity includes
scheme, host, port and deployment path; changing only the server cannot send old
credentials to another instance. Long-running MCP processes coordinate credential
refresh with short CLI processes using the same profile.

| Name | Purpose |
| --- | --- |
| `SOHA_CONFIG` | Use another config file path. |
| `SOHA_SERVER` | Select an instance for login or environment authentication; stored credentials require the same canonical server. The login default is `https://mcp.opensoha.com`. |
| `SOHA_TOKEN` | Supply a bearer credential without persistence or refresh; prefer a scoped PAT/service-account Key for CI. Logout cannot unset a shell variable. |
| `SOHA_LOGIN` | Provide the default login name for `login`. |
| `SOHA_PASSWORD` | Provide the login password non-interactively. Prefer short-lived shell scope or a secret manager. |
| `SOHA_HTTP_TIMEOUT` | HTTP request timeout, such as `10s`, `1m`, or bare seconds like `15`. Defaults to `30s`. |
| `SOHA_AI_CLIENT_ID` | Default AI client ID stored by `login`. |
| `SOHA_AI_CLIENT` | Default AI client display name stored by `login`. |

All HTTP commands also accept a global `--timeout` flag. It can be placed before
or after the command:

```sh
soha --timeout 10s capabilities
soha capabilities --timeout 10s
```

`soha login` defaults to a browser link and a six-digit code, retaining leading
zeroes. Open the complete request-bound link, sign in through the existing Web
password/third-party/MFA flow, enter the code and explicitly approve the terminal.
Remote/non-interactive terminals use `--device-code --no-browser`. Interrupted
pending logins resume with the same request; `--restart` cancels before replacing it.

Use `--with-token` to import a PAT or service-account Key from stdin. The CLI
validates `/auth/context`, including service identities; it does not require
`/auth/me`. Explicit password login uses `--method password --login NAME` and
`--password-stdin`; interactive password entry hides terminal echo. Older Core
requires an explicit `--method legacy-password`; there is no automatic downgrade.

`--credential-store ephemeral` only validates credentials. Before normal exit it
revokes a newly created session, preserving existing profiles and shared Keys.
JSON reports `verified: true`, `authenticated: false`, `persisted: false`, and
`temporarySessionRevoked: true` for sessions. Unconfirmed cleanup fails with
`ephemeral_cleanup_unconfirmed`; review the session in Web. Use environment Key
authentication for subsequent stateless commands.

`auth status` checks the server; `--offline` reports unverified local metadata.
`whoami` supports user and service-account identities. Session logout revokes the
session before clearing local credentials; Key logout clears locally unless
`--revoke` is explicit. `auth sessions` and `auth revoke-session --id ID` manage
your CLI sessions. `profile remove NAME` only removes that local profile.

New Key creation/rotation defaults to a future 90-day expiry. Key rotation immediately
revokes the old Key and uses a durable request ID. Supply a separate same-instance
`--auth-profile` for recovery; `token rotation-status --request-id ID` returns the
replacement ID without replaying plaintext. Keep the one-time new value safely.

## Core Workflows

```sh
soha login --server http://localhost:8080 --profile local
soha login --server https://cloud.soha.run --profile cloud --device-code --no-browser
soha auth status --profile local
soha whoami --profile local --json
soha profile list
soha context set --ai-client-id codex-local --ai-client Codex --source codex
soha capabilities --output names
soha diagnose --tool k8s.pods.logs
soha mcp
soha mcp --base-url https://soha.internal.example
soha mcp install --profile default --command /usr/local/bin/soha
soha setup --client codex --profile default --mode both --command /usr/local/bin/soha
```

### Capability Versions And Tasks

Versioned Gateway tools expose `version` and `execution` in the manifest. Tool
calls, project plan/apply, and the MCP adapter send the discovered version back
as `capabilityVersion`; stale versions fail instead of silently selecting a new
contract. A saved call can require its version explicitly with
`soha tool call <name> --capability-version <version>`. Legacy tools remain
callable without claiming these guarantees.

An invocation with `result: success` may have only submitted asynchronous work.
When the response contains `task`, retain its domain `kind`, `id`, and
`statusCall`, then use that registered status tool to continue under current
authorization. `terminal` describes the domain operation and does not establish
application health or URL reachability. MCP returns the same Gateway envelope
in `structuredContent` and text; the underlying domain output schema is metadata
because approvals and redaction can change the visible output. Idempotency
annotations use explicit execution contracts. Restart the MCP process after a
capability version change to reload its tool catalog.

### MCP Endpoint Selection

`soha mcp` is the direct stdio server entry point. `soha mcp start` remains as
a compatibility form. Without an endpoint flag, MCP uses the official SaaS
address `https://mcp.opensoha.com`. Use `--base-url` for a self-hosted Soha
deployment; `--server` and `SOHA_SERVER` are compatibility overrides.

For safety, a self-hosted profile is not silently redirected to the official
endpoint. Run `soha mcp --profile <name> --base-url <profile-server>`, or use
`soha setup`, which writes the self-hosted URL into the generated MCP client
configuration. Authentication still comes from the selected profile or the
one-command `SOHA_TOKEN` override.

### Kubernetes Workbench Through MCP

Kubernetes support uses the same manifest-driven MCP server; there is no
separate kubeconfig or `soha k8s` execution path. Install the verified
`k8s-sre` runtime skill and bind the generated MCP client configuration to it:

```sh
soha setup --client codex \
  --mode both \
  --skills k8s-sre \
  --skill-id k8s-sre
soha capabilities --output inputs
soha diagnose --tool k8s.pods.logs --resource soha://k8s/runtime
```

The live Gateway manifest remains authoritative for available tools and input
schemas. Direct CLI automation can invoke the same read-only surface with
`soha tool call k8s.pods.list --input-json '{"clusterId":"local"}'`.

### Agent And IDE Setup

`soha setup --client codex` writes a `[mcp_servers.soha]` entry to Codex
`config.toml`, installs a Codex-compatible `soha` skill package under
`~/.agents/skills/soha`, and installs the raw Soha runtime skills under
`~/.soha/skills`:

```sh
soha setup --client codex \
  --mode both \
  --profile local \
  --base-url http://localhost:8080 \
  --command "$(command -v soha)"
```

Use `--mode mcp`, `--mode skill`, or `--mode both` to control what is
installed. `--dry-run` prints planned changes and `--check` verifies the
profile, client MCP entry, canonical `$soha` skill, references, and runtime
skills without writing files:

```sh
soha setup --client codex --check
soha setup --client claude --mode mcp --base-url https://soha.internal.example
```

The default `--scope user` writes user-level client configuration and skills.
Use `--scope project` to write repository-local client configuration and put
raw runtime skills under `.soha/skills`. Explicit `--config`, `--dest`, and
`--runtime-skill-dest` paths take precedence over the selected scope:

```sh
soha setup --client codex --scope project
soha skill status --scope project
```

Run `soha setup` without a target to choose one or more clients interactively.
The selection accepts numbers, names, or `all`. Supported targets are `codex`,
`claude`, `cursor`, `kiro`, `gemini`, `antigravity`, `antigravity-ide`, and
`trae`. `soha add` remains a compatibility alias for the older integration
flow.

By default, skills come from the latest stable GitHub Release in
`opensoha/soha-skills`. The CLI resolves that release to its concrete semantic
version before downloading release assets; it never installs directly from a branch.
Use `--source github:opensoha/soha-skills@v0.1.0` only to pin a rollback or
reproducible install. Local checkouts, local release tarballs, and HTTPS release
asset URLs remain available through `--source` or `SOHA_SKILLS_SOURCE`.

The canonical agent-facing `$soha` skill is owned by
`soha-skills/agent-skills/soha`; the CLI copies it from the verified release
instead of embedding a second prompt. Release sources are cached only after the checksum, external and embedded
manifests, validation report, every packaged file hash, the compatibility
matrix, and archive paths pass validation. A latest release missing any required
asset fails closed. Downloading and installing skills does not require a Soha
login; invoking Soha MCP tools still requires a configured profile and the
backend permissions for each capability.

Raw runtime installs are managed as verified generations. Install and update
stage a complete directory before activation, retain the previous generation
for rollback, and append activation records to a local JSONL audit log. The
normal update path always resolves the latest stable release:

```sh
soha skill status
soha skill update
soha skill remove k8s-sre
soha skill rollback
```

The command is idempotent for the MCP section and generated skill package. Pass
`--server` or `--base-url` to set the Soha API base URL for the generated
profile. For new profiles, the default API base URL is
`https://mcp.opensoha.com`. Pass `--codex-config`, `--dest`, or
`--runtime-skill-dest` to target non-default locations. Restart Codex or open a
new Codex session after running it so MCP servers and skills are reloaded.

## Delivery workflows and batches

Delivery documents use `delivery.soha.io/v1alpha1` and identify `BuildTemplate`,
`DeploymentTemplate`, `WorkflowTemplate`, or `Workflow`. YAML and JSON describe the
same object. Validate locally, then preview server permissions, references and
revision conflicts before importing:

```bash
soha delivery documents validate --file build.yaml
soha delivery documents preview --file build.yaml --file workflow.json --profile local
soha delivery documents import --preview-id <id> --candidate-digest <candidateDigest> --idempotency-key import-1 --yes --profile local
soha delivery documents export BuildTemplate <id> --version 1 --format yaml --out build-v1.yaml --profile local
```

Import uses the reviewed preview; after an uncertain response, retry with the same
preview ID, digest and idempotency key. It saves drafts/configuration without
publishing or executing. A preview expires after 30 minutes. `--file -` reads one
document from stdin. Files are bounded to 1 MiB each, 2 MiB combined, and 100 files.
Export writes the raw document to stdout or creates `--out` without overwriting.
Template exports require a published version; Workflow exports its saved definition.

Git sources use stored repository connections and create drafts through the same
reviewed import path. `sync` resolves one commit and saves its preview; `apply`
consumes that exact preview. Neither publishes templates nor starts delivery:

```bash
soha delivery template-sources create --input source.json --yes --profile local
soha delivery template-sources sync <source-id> --input sync.json --yes --profile local
soha delivery template-sources run <source-id> --run-id <run-id> --profile local
soha delivery template-sources apply <source-id> --run-id <run-id> --input apply.json --yes --profile local
soha delivery template-sources objects <source-id> --offset 0 --limit 50 --profile local
soha delivery documents source BuildTemplate <id> --version 1 --profile local
```

`source.json` contains `name`, `repositoryId`, `refType` (`branch`, `tag`, or
`commit`), `refValue`, repository-relative `path` (`.` for root), `kinds`, `enabled`,
and `expectedGeneration: 0`. Updates require the loaded generation. Optional
`includePatterns`/`excludePatterns` select files; credentials stay in the stored
connection. `sync.json` requires `expectedGeneration` and an 8–128 byte
`idempotencyKey`; `apply.json` also requires the preview's exact `candidateDigest`.
Retry an uncertain response with the same run, generation, digest and key.

Review all `objects` pages before `detach` or `remove`. Both require an input with
`expectedGeneration` and explicit `disposition: "keep"` or `"deprecate"`; detach
also takes `--kind` and `--object-id`. Workflow supports `keep` only. These actions
preserve objects, bindings, executions and immutable version provenance. Active
Git definitions remain read-only in Soha until detached; copying creates a new
Soha-managed object. Source reads that fail validation return a nonzero exit code
and the persisted run with diagnostics.

Names never imply overwrite. For updates, use `preview --input import-request.json`
with explicit per-file targets and revisions (the latter is a Workflow version):

```json
{"files":[{"path":"build.yaml","content":"<YAML or JSON document>","targetId":"template-1","expectedRevision":3}]}
```

Save a workflow with `soha delivery workflows create --input workflow.json --yes
--profile local`. The JSON contains `definition`, including its name, targets,
execution mode, and `stopOnFailure` (defaults to true). Updating a workflow also
requires the last observed `expectedVersion`; saving never starts delivery.

Start a saved revision with a batch input such as:

```json
{"idempotencyKey":"release-20260912-01","workflowId":"workflow-1","workflowVersion":1}
```

```bash
soha delivery batches create --input batch.json --yes --profile local --timeout 3m
soha delivery batches get batch-1 --profile local
soha delivery plans get plan-1 --profile local
soha delivery plans approve plan-1 --comment reviewed --yes --profile local
soha delivery batches cancel batch-1 --reason paused --yes --profile local
```

For an ad hoc batch, supply `definition` instead of the workflow ID and version.
Reuse the same idempotency key after an uncertain response. An intentional retry
uses a new key and `retryOfBatchId`, preserving the previous attempt. Review the
final plan after its image digest and manifest are known; `--yes` only skips the
CLI prompt. Environment authorization and approval remain enforced by the server.
Cancellation may remain `canceling` until running tasks acknowledge it; completed
deployments remain recorded. JSON and YAML output retain the API envelope and
permission-filtered batch status, counts, and `partialView` marker.
The server resumes batch-owned plans after approval. Standalone plan creation and
confirmation use the public DeliveryPlan API or the Gateway tools
`delivery.plans.create` and `delivery.plans.confirm` via `soha tool call`.
The `delivery plans` command group supports `get`, `approve`, and `reject`.

## Command Matrix

For the generated command reference, run `soha docs --format markdown` or read
[`docs/commands.md`](./docs/commands.md).

| Command | Purpose | Common examples |
| --- | --- | --- |
| `version` | Print build version information. | `soha version`, `soha version --json` |
| `login` | Authenticate through browser/device code, Key or explicit password. | `soha login --server http://localhost:8080 --profile local` |
| `auth status`, `whoami`, `logout` | Check authentication, inspect the confirmed identity or revoke/clear credentials. | `soha auth status --profile local`, `soha whoami --profile local --json`, `soha logout --profile local` |
| `auth sessions`, `auth revoke-session` | List or revoke your CLI sessions. | `soha auth sessions --profile local`, `soha auth revoke-session --id SESSION_ID --profile local` |
| `capabilities` | Print the AI Gateway manifest. | `soha capabilities --output names`, `soha capabilities --output inputs` |
| `logs query`, `logs tail` | Query or follow cluster, Docker project, and delivery environment logs. | `soha logs query --source cluster --cluster-id local --namespace default`, `soha logs tail --source docker --project-id project-1` |
| `operation get`, `operation wait`, `operation cancel` | Inspect and control asynchronous compute operations. | `soha operation wait virtualization task-1`, `soha operation cancel container_runtime task-2 --yes` |
| `compute` | Inspect compute capabilities, providers, resources, relations, and tasks through the unified Compute API. | `soha compute overview`, `soha compute resources relations virtualization vm vm-1`, `soha compute tasks list --status failed` |
| `delivery` | Save release workflows, run or cancel batches, and inspect or decide final deployment plans. | `soha delivery batches list --profile local`, `soha delivery plans get plan-1 --profile local` |
| `tool call` | Invoke an AI Gateway tool with protected-call confirmation and redacted preview. | `soha tool call k8s.pods.list --input-json '{"clusterId":"local"}'`, `soha tool call delivery.actions.trigger --preview` |
| `project plan`, `project apply` | Plan and apply dependency-ordered `.soha/project.yaml` environments through live Gateway capabilities. | `soha project plan`, `soha project apply --yes` |
| `resource read` | Read an AI Gateway MCP resource. | `soha resource read soha://k8s/runtime --context-json '{"clusterId":"local"}'` |
| `prompt get` | Get an AI Gateway MCP prompt. | `soha prompt get soha.k8s.diagnose_workload --arguments-json '{}'` |
| `secret list`, `secret create`, `secret rotate` | Manage write-only Secret Store metadata and immutable local or Vault KV v2 versions. | `soha secret list --scope-type project --scope-id demo`, `soha secret create --name registry-token --vault-mount secret --vault-path demo/app --vault-key token --vault-version 3` |
| `token list` | List personal access tokens. | `soha token list` |
| `token create` | Create a personal access token. | `soha token create --name local --permission-keys ai.gateway.view` |
| `token revoke` | Revoke a personal access token. | `soha token revoke pat_123` |
| `token rotate`, `token rotation-status` | Replace a PAT and recover its recorded outcome using independent authentication. | `soha token rotate --id TOKEN_ID --auth-profile manager`, `soha token rotation-status --request-id REQUEST_ID --auth-profile manager` |
| `service-account list` | List AI Gateway service accounts. | `soha service-account list` |
| `service-account create` | Create a service account. | `soha service-account create --name runner --role-ids operator` |
| `service-account token-list` | List service account tokens. | `soha service-account token-list` |
| `service-account token-create` | Create a service account token. | `soha service-account token-create --service-account-id sa_123 --name ci` |
| `service-account token-revoke` | Revoke a service account token. | `soha service-account token-revoke sat_123` |
| `service-account token-rotate`, `service-account rotation-status` | Replace a service Key or query its outcome. | `soha service-account token-rotate --id TOKEN_ID --auth-profile manager` |
| `audit list` | Query AI Gateway audit logs. | `soha audit list --tool-name k8s.pods.logs --limit 20` |
| `approval list` | List approval requests. | `soha approval list --status pending` |
| `approval timeline` | Show an approval timeline. | `soha approval timeline approval_123` |
| `approval approve` | Approve an approval request. | `soha approval approve approval_123 --comment "ship"` |
| `approval reject` | Reject an approval request. | `soha approval reject approval_123 --comment "needs review"` |
| `approval cancel` | Cancel an approval request. | `soha approval cancel approval_123` |
| `governance status` | Show AI Gateway governance health and metrics. | `soha governance status --window-hours 48`, `soha governance status --json` |
| `cloud fleet diagnostics` | Show Cloud managed agent fleet capability diagnostics. | `soha cloud fleet diagnostics --tenant-id tenant_123 --fleet-id agent-fleet_123` |
| `profile list` | List local profiles. | `soha profile list` |
| `profile show` | Show one local profile with tokens redacted. | `soha profile show default` |
| `profile use` | Switch the current profile. | `soha profile use cloud` |
| `profile remove` | Remove one local profile and its private stored credential. | `soha profile remove cloud` |
| `context show` | Show AI client context headers. | `soha context show` |
| `context set` | Update AI client context headers. | `soha context set --skill-id k8s-sre --source codex` |
| `mcp` | Run the Soha MCP stdio server against official SaaS by default. | `soha mcp`, `soha mcp --skill-id k8s-sre` |
| `mcp start` | Compatibility form of the MCP stdio server. | `soha mcp start --profile default` |
| `mcp install` | Print MCP client configuration. | `soha mcp install --command /usr/local/bin/soha` |
| `skill list` | List Soha skills from a local or verified release source. | `soha skill list` |
| `skill install` | Install Soha skills from a local or verified release source. | `soha skill install --dest ~/.soha/skills k8s-sre` |
| `skill status` | Show managed skill generation and integrity status. | `soha skill status`, `soha skill status --scope project --json` |
| `skill update` | Update managed skills from the latest stable release. | `soha skill update`, `soha skill update --all` |
| `skill remove` | Remove managed skills while retaining rollback state. | `soha skill remove k8s-sre` |
| `skill rollback` | Restore the previous verified generation. | `soha skill rollback` |
| `setup` | Configure MCP, skills, or both for an AI agent or IDE. | `soha setup --client codex --mode both --skills k8s-sre --skill-id k8s-sre`, `soha setup --client codex --check` |
| `add` | Compatibility alias for the previous setup flow. | `soha add codex --profile local --base-url http://localhost:8080` |
| `plugin search` | Search plugin marketplace entries. | `soha plugin search --query k8s` |
| `plugin show` | Show marketplace, installed, or manifest details. | `soha plugin show opensoha.k8s-sre-pack --installed` |
| `plugin install` | Install a marketplace or manifest plugin. | `soha plugin install opensoha.k8s-sre-pack --enable` |
| `plugin list` | List installed plugins. | `soha plugin list` |
| `plugin enable` | Enable an installed plugin. | `soha plugin enable opensoha.k8s-sre-pack` |
| `plugin disable` | Disable an installed plugin. | `soha plugin disable opensoha.k8s-sre-pack` |
| `plugin upgrade` | Upgrade an installed plugin. | `soha plugin upgrade opensoha.k8s-sre-pack` |
| `plugin config` | Configure metadata and secret refs. | `soha plugin config opensoha.k8s-sre-pack --secret-ref token=secret://k8s/token` |
| `plugin remove` | Remove an installed plugin. | `soha plugin remove opensoha.k8s-sre-pack` |
| `diagnose` | Check profile, AI client setup, and Gateway visibility. | `soha diagnose --client codex --output json` |
| `completion` | Print shell completion script. | `soha completion bash`, `soha completion zsh`, `soha completion fish`, `soha completion powershell` |
| `docs` | Generate CLI command reference documentation. | `soha docs --format markdown` |

Structured command results use stdout; diagnostics and prompts use stderr. Exit
codes are `0` for success/help, `1` for validation, execution, API, or terminal
operation failures, `2` for a missing/unknown top-level command or malformed
flag, and `130` for interrupted execution.

## License

This repository is licensed under the Apache License 2.0. See
[LICENSE](./LICENSE) for the full license text.

For AI chat recovery, use `soha ai chat request SESSION --request-id ID` to inspect the original request. `native-request` remains a compatible alias. If a stream outcome is unknown, retain the original identifiers and inspect the request, run events, and session messages; do not resend automatically. Recovery decisions and historical Native reconciliation remain server-authoritative.
