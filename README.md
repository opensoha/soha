# soha-contracts

Shared public contracts for OpenSoha.

This repository is the source of truth for schemas consumed across Soha core,
Soha CLI, Soha Agent, Soha Web, Soha App, official skills, connectors, and Soha
Cloud services. Product repositories should depend on generated SDKs, HTTP
APIs, JSON Schemas, or released artifacts from this repository instead of
copying business logic between repos.

## Layout

```text
openapi/soha-api.yaml                 Canonical public Soha HTTP API source
openapi/soha-api.json                 Generated canonical JSON distribution
openapi/spec_generated.go             Embedded Go accessors and version/hash metadata
compat/openapi-0.1.0.snapshot.json    OpenAPI compatibility baseline
examples                              Valid request/response and manifest examples
fixtures                              Valid and invalid contract payloads
capabilities/cluster-capability-matrix.schema.json  Direct/Agent runtime capability matrix
profiles/agent-profile.schema.json    Agent runtime profile manifest
presets/mcp-preset.schema.json        MCP preset manifest
governance/approval-request.schema.json  Approval request and trace contract
governance/audit-log.schema.json      Audit log contract
agent/agent-protocol.schema.json      Runner and agent control-plane protocol
agent/personal-guest-*.schema.json   Personal VM guest binding, enrollment and heartbeat
agent/guest                          Hand-maintained Go guest protocol validators
runner/operation-lifecycle.schema.json  Long-running operation lifecycle events
mcp/gateway-manifest.schema.json      AI Gateway / MCP capability manifest
skills/skill-manifest.schema.json     Official skill front matter contract
plugins/plugin-manifest.schema.json   Plugin and marketplace package manifest
events/event-envelope.schema.json     Shared event envelope contract
auth/token-claims.schema.json         Token/JWT claims contract
auth/permission-catalog.json          Canonical resource/action permission catalog
auth/permission-catalog.schema.json   Permission definition and migration metadata
connectors/connector-event-envelope.schema.json  Connector runtime event batch envelope
network/network-runtime-protocol.schema.json  Network control/runtime message contract
network/network-ingest-event.schema.json  Bounded heartbeat, Accounting and flow event batches
gen/go/sohaapi                        Go SDK types for stable cross-repo contracts
gen/ts/sohaapi                        TypeScript SDK types for stable Web contracts
```

## Boundary Rules

- `soha` remains the open-source core and must run standalone.
- Cloud-only tenant lifecycle, billing, quota, SaaS IAM, and operations
  contracts do not belong here unless they are explicitly public extension
  points.
- Open-source Soha may keep tenant-aware fields, but the default distribution
  uses `tenantId = default` and `workspaceId = default`.
- Public behavior changes should update this repository first, then update
  generated SDKs and consumers.

Personal VM guest messages are an independent protocol. Docker v1 and Network
Control credentials do not authorize it. Enrollment requires a guest-specific
mTLS identity and a short one-time challenge; Core must derive the frozen owner,
assistant, compute generation, data identity and policy. Heartbeats carry no task
execution or readiness grant. Certificate pinning, current authorization, atomic
challenge consumption and sequence persistence belong to the consuming service.
The contracts package alone does not implement guest enrollment or enable VM
personal environments.

## SDK Consumers

Current SDK entry points:

- Go: `github.com/opensoha/soha-contracts/gen/go/sohaapi`
- Go OpenAPI artifact: `github.com/opensoha/soha-contracts/openapi`
- TypeScript: `@opensoha/contracts/gen/ts/sohaapi`
- npm OpenAPI artifacts: `@opensoha/contracts/openapi/soha-api.yaml` and
  `@opensoha/contracts/openapi/soha-api.json`

The current SDK pass covers auth, AI Gateway capability/tool/result types,
runner-facing protocol DTOs, plugin marketplace/install DTOs, public Cloud
extension point DTOs, and AI Gateway token management, audit, approval, and
governance DTOs used by `soha-cli`, `soha-agent`, and `soha-web`.

SDK DTOs are generated from `openapi/soha-api.yaml` with
`openapi-typescript` and `oapi-codegen`:

The public DTO surface also includes Identity applications, providers,
policies, OIDC clients, SAML SP/IdP configuration, MFA challenges, runtime
capabilities, and versioned Outpost agent messages. Sensitive enrollment and
protocol inputs are write-only; response DTOs exclude private keys and raw
SAML assertions.

The `resource` package also provides `PodArgvToolInput` and
`PodExecutionLookupInput` helpers for the closed Gateway Pod execution schemas.
They validate targets, original references and bounded runtime input. They do
not grant execution permission or bypass Gateway approval; the consuming
Gateway must register and enforce the capability. The Go HTTP generator prunes
schemas that are used only by Gateway tools, so these semantic helpers remain
hand-maintained alongside `PodArgvInput`.

```sh
npm run generate
npm run check:generated
```

`npm run check:generated` generates SDK and OpenAPI distribution output in a
temporary directory and compares it to committed files. It must not rewrite
generated files during a freshness check. The Go `openapi` package exposes
copies of both formats plus `Version`, `YAMLSHA256`, and `JSONSHA256`.
The Go `auth` package embeds the canonical permission catalog and exposes its
catalog version, definition hash, and artifact hash. Refresh it with
`npm run generate:permissions`; consumers conformance-check their local keys
against this artifact instead of maintaining a second catalog.

The Go client in `gen/go/sohaapi/client.go` intentionally keeps a stable
hand-written method surface while DTOs are generated from OpenAPI. For
`0.1.x`, the supported Go client method surface covers:

- system health and readiness checks;
- public auth discovery, password login, refresh, OIDC code exchange, current
  principal/profile/bootstrap, logout, and stream tickets;
- delivery, Docker, and agent-run claim/status/callback runner APIs;
- Identity Outpost runtime claim, heartbeat, access-check, and event APIs;
- WebAuthn authentication/step-up and administrator MFA revoke/reset APIs;
- AI Gateway audit, approval, governance, token, service-account, and MCP
  capability APIs;
- plugin marketplace, install, enable/disable, upgrade, configuration, and
  manifest APIs.

Other Go consumers should use the generated DTOs directly and keep HTTP
calling code in the consumer until those APIs are promoted into the supported
client surface. Redirect-oriented flows such as OIDC provider redirects should
not use the JSON-only `doJSON` helper until a non-JSON client mode is added.

The Go generator and generated DTO checks run with Go 1.27.3 in CI.

## Validation

Run:

```sh
npm test
```

The validation gate checks generated SDK freshness, JSON schema syntax,
valid and invalid JSON Schema fixtures, valid and invalid OpenAPI DTO
fixtures, request/response and manifest examples, OpenAPI structure and
operation ID uniqueness, version synchronization, OpenAPI lint rules,
breaking-change compatibility against the checked-in baseline, TypeScript
type declarations, and Go compilation for the generated SDK package.

OpenAPI fixture directories are discovered automatically; registered required directories
must still exist. Put every JSON fixture in `valid/` or `invalid/`, with at least
one of each. The validation gate rejects misplaced or unrecognized entries.

Useful focused checks:

```sh
npm run check:version-sync
npm run validate
npm run lint:openapi
npm run check:openapi-breaking
npm run check:openapi-loose-boundaries
npm run check:json-schema-compat
npm run release:artifacts
npm run release:verify -- dist/release/opensoha-contracts-0.1.0.tgz
```

When intentionally establishing a new OpenAPI compatibility baseline, update
the snapshot with:

```sh
npm run check:openapi-breaking:update
```

Do not update the baseline just to silence an unexpected breaking-change
failure.

## Consumer Matrix

Network runtime schemas and network-access DTOs require `0.1.17` or later;
they are absent from npm `0.1.16` and Go `v0.1.15`. For unpublished changes,
use the local consumer matrix below: it packs this checkout for Web/App and
supplies an isolated Go workspace. After publishing, update consumer versions
and lockfiles and verify them without the local override.

The cross-repository consumer gate is:

```sh
npm run check:consumers
```

By default, the script discovers sibling checkouts under the parent directory
and skips any missing consumer. CI uses the strict form:

```sh
npm run check:consumers -- --consumer soha --require-all
npm run check:consumers -- --consumer soha-web --require-all
npm run check:consumers -- --consumer soha-app --require-all
npm run check:consumers -- --consumer soha-cli --require-all
npm run check:consumers -- --consumer soha-agent --require-all
npm run check:consumers -- --consumer soha-skills --require-all
```

Go consumers run in a temporary `go.work` that replaces the contracts module
with this checkout. `soha-web` and `soha-app/frontend` run `npm ci`, install a packed copy of this
checkout without changing their dependency manifests or lockfiles, and build
against that package. The App pin represents its recorded committed source;
local uncommitted Native adaptations need separate worktree evidence and a new
reviewed pin after they are committed.

Use `--dry-run` to inspect the discovered commands without executing consumer
builds.

## Execution task callback credentials

`ExecutionTask.callbackToken` is a callback capability, not ordinary task-view data.
Normal task list/get and application, environment, runtime and release-board read
views return the fixed non-secret string `[redacted]` when a callback is configured.
This preserves the existing SDK string shape and configured-callback presence
signal; the marker cannot be submitted as a callback credential. Historical empty
values remain empty. Only authenticated runner claim/status paths return the real
credential needed by that runner. Internal execution and callback validation keep
the original value; task-view or configuration-secret read permission does not
grant it.

This documents the security projection within the existing v0.1.26 field shape.
It adds no schema fields or SDK types and does not change the runner protocol.
Clients must obtain runner credentials through the authorized runner flow rather
than copying them from a user-facing task read.

## Compatibility Policy

See [COMPATIBILITY.md](./COMPATIBILITY.md) for the versioning, compatibility,
fixture, baseline, and consumer adoption rules. In short, `0.1.x` changes may
add optional contract surface but must not remove existing public operations,
rename operation IDs, remove existing response statuses, remove schema
properties, add required request properties, or remove enum values.

## Release Shape

The stable TypeScript package name is `@opensoha/contracts`, and the stable Go
module path is `github.com/opensoha/soha-contracts/gen/go/sohaapi`.

Release tags must be `v<package.json version>`. `npm test` runs
`npm run check:version-sync`, which verifies `package.json`,
`package-lock.json`, OpenAPI `info.version`, the Go module path, the Go client
user agent version, and the GitHub release tag when the workflow runs from a
tag.

Release artifacts are prepared and verified with:

```sh
npm run release:artifacts
npm run release:verify -- dist/release/opensoha-contracts-0.1.0.tgz
```

The command writes `dist/release/<npm-tarball>.tgz`, a matching
`.sha256` checksum file, `dist/release/release-manifest.json`, a CycloneDX SBOM
with its checksum, a provenance policy manifest with its checksum, and, when
provided by CI, `dist/release/consumer-matrix.json`. `release:verify` checks
the tarball checksum, manifest metadata, packed `package.json`, every exported
package entrypoint, both OpenAPI artifact hashes, the SBOM identity and
checksum, the provenance policy and checksum, and the consumer matrix summary.

The tag workflow runs `npm test`, `npm pack --dry-run`, and the strict consumer
matrix, then uses `NPM_TOKEN` to stage the npm tarball with provenance. A
maintainer approves the stage on npmjs.com with 2FA. Dispatch the release
workflow with the same tag and `publish_mode=recover` after approval. Recovery
verifies the public npm package and Go module tag, attaches the release
artifacts to GitHub, and verifies the downloaded tarball.

The local release manifest records the provenance policy expected from the tag
workflow. Verify the registry attestation's artifact digest and source revision
after approval makes the package public.

During private staging, consumers intentionally use sibling repository
checkouts, `go.work` / `replace` entries, and `file:../soha-contracts` package
dependencies instead of resolving this repository through anonymous GitHub
module fetches or npm. After the repository is publicly readable and the npm
package is published, released consumer modules can switch to a real
`soha-contracts` version tag and the published npm package without changing SDK
import paths.

## License

This repository is licensed under the Apache License 2.0. See
[LICENSE](./LICENSE) for the full license text.

### Workbench model preferences

`WorkbenchSendMessageStreamRequest.modelPreferences` selects a public model and
an optional `auto`, `low`, `medium`, or `high` reasoning effort for internal general
chat. Preferences persist in session metadata when a message is sent; an empty
public model follows the global default. External agents and analysis modes manage
their own models. The workbench catalog exposes only permitted public model names.

Gateway routes may declare `metadata.reasoningEfforts: ["low", "medium", "high"]`
(or a supported subset). Only OpenAI-wire routes without protocol conversion expose
these levels; every eligible fallback must support a level for it to be offered.
Unknown capabilities offer automatic reasoning only. Explicit supported efforts map
to `reasoning_effort` for chat completions and `reasoning.effort` for Responses.

### Shared native Helm task adapter

`helmrelease/runtime` applies the versioned Helm execution contract through Helm 4.
Both core Direct and the standalone Agent supply their own authorized action configuration;
queueing, approvals, secret resolution and chart downloads remain outside this package.
SDK-only consumers need not import this optional package. Its native revision and recovery
checks are tested once for both execution modes.

Agent-local authenticated POST endpoints `/api/v1/platform/helm/delivery/prepare`,
`/api/v1/platform/helm/delivery/preflight` and `/api/v1/platform/helm/delivery/observe` accept `HelmExecutionTaskPayload` and return a
`data` envelope with the prepared payload or `HelmExecutionTaskResult`, respectively.
They enforce the configured cluster identity and are read-only; `apply` is accepted
only by the claimed-task runner. Confidential requests are bounded to 8 MiB,
rendered manifests plus hooks to 2 MiB and values to 1 MiB, and are never logged.

### Controller-aware Agent execution

Manifest tasks emitted by the controller-aware Core use `manifest_agent_v3.<clusterId>` and require the Agent summary capability `manifest.execution.v3`. New Agents also accept legacy `manifest_agent.<clusterId>` and `manifest_agent_v2.<clusterId>` tasks using the protected executor. Old Agents cannot claim the new provider kind; capability checks alone are not a substitute for this claim boundary during rolling upgrades.

Plan-bound Manifest tasks additionally use `manifest_delivery_v1.<existingProvider>` and require `manifest.delivery-admission.v1`. Core validates the durable private operation and current Plan authority on every claim, then supplies `X-Soha-Manifest-Admission-Version: 1` in that claim response. Agent runners must reject a protected task without that exact response confirmation, including when polling an older Core with general runner credentials. A payload or a later task read is not admission proof. Direct providers remain local-only; protected Agent providers require the cluster Agent token. No fallback to an older provider is permitted. Known old application Plan tasks with `fieldManager` beginning `opensoha-manifest/` or a nonempty `initializationPlanId` are rejected on legacy providers, even with a header. These markers only deny execution; they never reconstruct or authorize Plan identity. Existing initialization and root-CR identity rules still apply to the underlying provider. These wire additions use existing SDK task and HTTP client APIs and remain source-compatible with SDK v0.1.26.


Native YAML/CR writes and workload restart, scale, rollback and image changes use the existing request/response bodies under `/api/v1/platform/ownership-v2`, with the original route suffix. This endpoint family guarantees live external-owner checks for Operator, Argo CD, and Argo Rollouts resources and optimistic identity fencing; ownership conflicts return HTTP 409. Core must not fall back to older mutations after 404/405. Reads retain their existing routes.

`resource/runtime` implements bounded WorkloadCronJob, Argo CD, and Argo Rollouts adapters shared by Direct and Agent. The frozen inventory, operation identity, current generation, resource UID/version, and child-resource evidence determine completion. Blue-green and Traefik canary delivery observe native Service revisions, weights, and complete AnalysisRun metric windows. Promotion only releases manual pauses; analysis failure and cancellation require confirmed stable traffic. Restoring desired configuration uses a new governed operation. Other CRs and traffic plugins are not inferred as supported from a generic Ready condition.
