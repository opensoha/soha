# Compatibility and Versioning

This repository owns the public OpenSoha contract surface. Consumers should
pin released versions and treat this repository as the first place where public
API, schema, and SDK behavior changes are proposed.

## Version Streams

Current stream: `0.1.x`.

- `0.x` releases may still evolve quickly, but compatibility gates must run for
  every change.
- `0.1.x` patch releases may add optional fields, optional query parameters,
  new enum values, new response status codes, new operations, and new schemas.
- `0.1.x` patch releases must not remove existing operations, rename
  `operationId` values, remove response status codes, remove schema properties,
  add required request properties, remove enum values, or narrow existing
  schema types.
- Minor `0.x` releases may include intentional breaking changes only when the
  migration is documented in the release notes and consumers have a clear
  adoption path.
- `1.0.0` starts the stable contract line. After `1.0.0`, breaking changes
  require a major version bump, a migration note, and consumer coordination.

## Proxy Configuration Inputs

The proxy configuration endpoint accepts partial updates through
`NetworkProxyConfigurationPatchInput`. Its optional `content` distinguishes omission
from an explicit empty value. Existing Go consumers retain
`NetworkProxyConfigurationInput.Content` as a string and the full-input
`UpdateNetworkProxyConfigurationJSONRequestBody` alias. New Go consumers that omit
encrypted content must explicitly use the Patch input. TypeScript operation types
follow the partial HTTP schema. Both full and partial requests use the same endpoint.

## Version Synchronization

Each release must keep these values aligned:

- `package.json` `version`
- `package-lock.json` root package version
- `openapi/soha-api.yaml` `info.version`
- `gen/go/sohaapi/client.go` default user agent version
- Git tag `v<version>`
- Go module tag for consumers importing
  `github.com/opensoha/soha-contracts/gen/go/sohaapi`

The version sync gate runs with:

```sh
npm run check:version-sync
```

The gate fails when any of the synchronized versions diverge. In GitHub tag
release workflows it also verifies that the pushed tag is exactly
`v<package.json version>`.

Release order for `0.1.x`:

1. Update `package.json`, `package-lock.json`, and OpenAPI `info.version` to
   the same patch version.
2. Regenerate SDKs when OpenAPI changed.
3. Run `npm test`, `npm pack --dry-run`, `npm run release:artifacts`, and
   the consumer matrix.
4. Create tag `v<version>`.
5. Let the release workflow stage the npm package with `NPM_TOKEN`. After a
   maintainer approves the stage with 2FA, dispatch recovery for the same tag.
   The workflow verifies the public npm package and Go tag before attaching
   the tarball, checksums, and release manifest to the GitHub release.

## Compatibility Baseline

`compat/openapi-0.1.0.snapshot.json` is the current OpenAPI compatibility
baseline. It is generated from `openapi/soha-api.yaml` by:

```sh
npm run check:openapi-breaking:update
```

Do not update the baseline just to make a failing check pass. Update it only
when starting a new compatibility baseline after an intentional versioned break
or when establishing a baseline for a newly stabilized stream.

The breaking-change check currently blocks these high-risk changes:

- Removing a path or HTTP operation.
- Changing an existing `operationId`.
- Removing an existing response status or media type.
- Adding a required request body or required parameter.
- Removing a component schema.
- Removing an existing schema property.
- Adding a required schema property.
- Removing an enum value.
- Changing an existing schema type, format, `$ref`, array item schema, or
  object `additionalProperties` contract in a narrowing direction.

This is a baseline guard, not a full API review substitute. Contract changes
that pass this script can still require consumer tests when semantics change.

## OpenAPI Loose Boundary Baseline

`compat/openapi-loose-boundaries-0.1.0.json` records every current OpenAPI use
of `AnyValue`, `GenericObject`, and bare `additionalProperties: true`. The
check runs with:

```sh
npm run check:openapi-loose-boundaries
```

New loose boundaries must not be added accidentally. If a future contract truly
requires dynamic JSON, add it to the baseline with an intentional review note.
If a loose boundary is tightened, remove it from the baseline in the same
change so the contract surface keeps moving toward named schemas.

`CapabilityCall.input` is intentionally dynamic: a continuation references a
registered capability whose version and input schema are checked by the Gateway.
The call envelope and task reference are closed schemas; this input map does not
permit arbitrary HTTP endpoints, scripts, or bypasses of current authorization.

## Public Cloud Extension Points

`CloudExtensionPoint` OpenAPI DTOs describe only public extension points that
Cloud can consume through shared contracts, HTTP APIs, SDKs, webhooks, or event
sinks. Billing, quota, and SaaS IAM remain Cloud-only domains and must not be
promoted into this public contracts repository.

For `0.1.x`, patch releases may add optional public extension point fields,
new extension point kinds, new public boundary kinds, and new supported
consumer names. They must not remove or rename `id`, `name`, `kind`,
`boundary`, `contractRefs`, or `excludedCloudOnlyDomains`; must not add new
required fields; and must not remove `billing`, `quota`, or `saas-iam` from
the Cloud-only exclusion enum.

## Fixture Policy

### Outpost management compatibility

Outpost management retains the deployed `online/offline/degraded` status values,
node metadata, endpoint, version and last-contact fields. The shared
`IdentityResourceStatus` type is preserved for SDK source compatibility; these
legacy runtime values apply to Outposts, not to other resources' management
transitions. Management `status` and `version` inputs remain accepted for old
clients but cannot manufacture runtime observations. New clients use
`runtimeStatus`, `lastHeartbeatAt`, applied configuration information and the
explicit `forwardAuthUrl`. A desired configuration version of zero represents
an unregistered node and must not be displayed as a deployed version.

The two Outpost metadata references in the loose-boundary inventory document an
existing deployed JSON object surface. They preserve legacy clients and do not
authorize runtime health, configuration versions or lease expiry; those are
separately typed and recorded by authenticated runtime operations.

Every JSON Schema owned by this repository must have at least one valid and one
invalid fixture under `fixtures/json-schema/<contract>/`.

High-value OpenAPI DTOs must have valid and invalid fixtures under
`fixtures/openapi/<SchemaName>/`. Add fixtures when a schema becomes important
to a cross-repository workflow, especially auth, runner callbacks, AI Gateway,
MCP, plugins, tokens, approvals, and governance.

Fixture validation runs with:

```sh
npm run validate
```

Request/response and manifest examples under `examples/` are also validated
by `npm run validate`. Examples are release documentation assets, so they
should be valid positive payloads rather than negative test cases.

## Ecosystem JSON Schema Baseline

`compat/json-schema-ecosystem-0.1.0.json` protects the public plugin, skill,
and connector JSON Schemas from accidental breaking changes. The check runs
with:

```sh
npm run check:json-schema-compat
```

The baseline blocks removing existing root properties, adding new root required
properties, and removing existing enum values. Optional fields and enum values
may be added in `0.1.x` patch releases when fixtures and compatibility notes
are updated.

## Cluster Capability Matrix

`capabilities/cluster-capability-matrix.schema.json` owns the public
Direct/Agent runtime capability matrix consumed by Core, Web, CLI, Agent,
Skills, Connectors, and Cloud extension points. It describes Kubernetes, Helm,
YAML, logs, exec, port-forward, Docker, and runtime support with explicit
available, partial, and unsupported states.

For `0.1.x`, patch releases may add optional fields, new capability entries,
new categories, new risk levels, and new support diagnostics. They must not
remove or rename `items`, entry `key`, `label`, `category`, `riskLevel`,
`requiresApproval`, `direct`, or `agent`; must not add new required fields; and
must not narrow the existing `available`, `partial`, or `unsupported` status
values. Any `partial` or `unsupported` support state must continue to provide a
human-readable reason so downstream UI and diagnostics can explain degraded
runtime behavior.

## Plugin Manifest

`plugins/plugin-manifest.schema.json` owns the public package manifest for
skills, skill packs, MCP presets, connectors, AI provider adapters, agent
profiles, and gateway policy packs. Marketplace, CLI, Web, Skills, Connectors,
and Cloud consumers should use this schema as the source of truth for package
identity, compatibility declarations, asset references, permissions, secrets,
and integrity metadata.

For `0.1.x`, patch releases may add optional plugin types, optional asset
groups, optional capability groups, optional permission fields, optional secret
metadata, and optional integrity metadata. They must not remove or rename `id`,
`name`, `version`, `publisher`, or `type`; must not add new required fields;
must not narrow the plugin ID pattern; must not remove existing plugin `type`
enum values; and must not allow unknown top-level fields outside explicitly
versioned metadata.

## Skill Manifest

`skills/skill-manifest.schema.json` owns the public structured front matter
contract for official and ecosystem skill assets. Skill repositories may add
repository-local metadata, but cross-repository routing, permission review, and
capability binding should use this schema first.

For `0.1.x`, patch releases may add optional fields, optional provider skill
reference metadata, optional capability reference conventions, and optional
permission or scope annotations. They must not remove or rename `id`, `name`,
or `category`; must not add new required fields; must not narrow the skill ID
pattern; and must not allow unknown top-level fields outside `metadata`.

## Agent Profile

`profiles/agent-profile.schema.json` owns the public agent runtime profile
manifest used by official skills, CLI installers, Web previews, Agent runtime
selection, and Cloud managed fleet configuration. It binds skills, MCP presets,
capabilities, scopes, enabled/disabled tools, execution budgets, and guardrails.

For `0.1.x`, patch releases may add optional modes, optional budget fields,
optional guardrail conventions, and optional metadata. They must not remove or
rename `schemaVersion`, `id`, `name`, `version`, `mode`, `providerKind`,
`skillRefs`, `mcpPresetRefs`, `capabilityRefs`, `platformCapabilityRefs`,
`requiredScopes`, `enabledToolRefs`, or `guardrails`; must not add new required
fields; must not narrow the ID pattern or semantic version pattern; and must
not allow unknown top-level fields outside `metadata`.

## MCP Preset

`presets/mcp-preset.schema.json` owns the public MCP preset manifest used by
official skills, CLI installers, Gateway capability binding, Web previews, and
Cloud/local preset packaging. It declares a bounded set of tools, resources,
prompts, scopes, capabilities, risk level, and guardrails.

For `0.1.x`, patch releases may add optional risk levels, optional capability
metadata, optional top-level metadata, and optional tool/resource/prompt fields.
They must not remove or rename `schemaVersion`, `id`, `name`, `version`,
`riskLevel`, `skillRefs`, `platformCapabilityRefs`, `requiredScopes`, `tools`,
or `guardrails`; must not add new required fields; must not narrow the ID
pattern or semantic version pattern; and must not allow unknown top-level
fields outside `metadata`.

## Governance Approval Request

`governance/approval-request.schema.json` owns the public approval request,
trace, decision, stage, and timeline event contract for high-risk Gateway and
platform operations. OpenAPI approval DTOs should stay compatible with this
schema when the same fields are exposed over HTTP.

For `0.1.x`, patch releases may add optional approval fields, optional trace
fields, optional decision metadata, optional stage metadata, and optional
timeline event metadata. They must not remove or rename `id`, `status`,
`strategy`, `actorType`, `actorId`, `toolName`, `riskLevel`,
`requiresApproval`, `summary`, `createdAt`, or `updatedAt`; must not add new
required fields; must not remove existing risk level values; and must not allow
unknown top-level fields outside explicitly modeled metadata objects.

## Governance Audit Log

`governance/audit-log.schema.json` owns the public audit log contract for
Gateway, platform, connector, plugin, skill, and Cloud extension point actions.
OpenAPI audit DTOs and event pipeline audit payloads should stay compatible
with this schema when the same fields are exposed outside a repository.

For `0.1.x`, patch releases may add optional audit fields, optional actor
display metadata, optional resource scope fields, optional request correlation
fields, and optional metadata conventions. They must not remove or rename `id`,
`actorType`, `actorId`, `action`, `result`, `summary`, or `createdAt`; must not
add new required fields; must not remove existing risk level values; and must
not allow unknown top-level fields outside `metadata` or `resourceScope`.

## Connector Event Envelope

`connectors/connector-event-envelope.schema.json` owns the public connector
runtime event sink batch envelope. Connector repositories may mirror this file
for package compatibility, but the canonical `$id` remains
`https://contracts.opensoha.dev/connectors/connector-event-envelope.schema.json`.

For `0.1.x`, patch releases may add optional fields to the wrapper or event
items and optional provider payload conventions, but must not remove or rename
top-level `connectorId` or `events`; must not remove or rename event `id`,
`type`, `source`, `occurredAt`, `subject`, or `payload`; must not add new
required fields, allow unknown wrapper/event fields, narrow the connector ID
pattern, or move provider-specific data out of `payload`.

## Runner Operation Lifecycle

`runner/operation-lifecycle.schema.json` owns the public event contract for
long-running operation lifecycle breadcrumbs across delivery, Docker, VM, AI
provider, agent run, and connector action runners.

For `0.1.x`, patch releases may add optional operation kinds, optional states,
optional evidence kinds, and optional metadata fields. They must not remove or
rename `operationId`, `operationKind`, `runnerId`, `state`, `occurredAt`,
`evidence`, `finalState`, or `callback`; must not add new required fields; and
must not narrow existing lifecycle state names or final status values.

## Network Runtime Protocol

`network/network-runtime-protocol.schema.json` owns messages exchanged by
network-control, endpoint services and network gateways. The contract carries
runtime identity, configuration and policy versions, bounded lease grants and
apply/revoke acknowledgements. It must never carry WireGuard private keys,
RADIUS shared secrets, passwords, EAP private material or mihomo subscription
credentials.

For `0.1.x`, patch releases may add optional fields and new message types with
strict payload definitions. They must not remove or rename `schemaVersion`,
`messageId`, `messageType`, `producerId`, `runtimeId`, `runtimeKind`,
`occurredAt`, `expiresAt` or `payload`; must not remove existing message,
runtime-kind, access-profile or status enum values; and must not loosen unknown
field rejection. Existing lease identity, subject/device binding, policy
version and expiry fields must remain required.

NAS authorization and session control use the same versioned envelope. Their
payloads contain only Soha identity references, bounded standard RADIUS
attributes and command acknowledgements. Public management bindings map a NAS
to one registered runtime/site and an access profile to site enforcement
attributes; neither contract may add RADIUS shared secrets or authentication
credential material.

WireGuard enrollment carries only a public key. VPN connection authorization
creates a session-scoped NetworkLease; desired configuration may add a strict
WireGuard block with overlay addresses, peers, split routes and default-deny
firewall rules. Private keys never cross this contract. Existing consumers
must ignore no fields because runtime messages remain closed; therefore a
consumer advertises `wireguard` only after it supports the full block and its
configuration version/readback semantics.

## Network Ingest Event

`network/network-ingest-event.schema.json` owns bounded, replayable telemetry
batches sent to `/api/ingest/v1/**`. Heartbeat, RADIUS Accounting, flow
aggregate and connection summary payloads remain typed and cannot be treated
as authorization state.

For `0.1.x`, patch releases may add optional fields, producer kinds and event
types with strict payload definitions. They must not remove or rename
`schemaVersion`, `batchId`, `producerId`, `producerKind`, `sentAt`, `events`,
event `id`, `type`, `sequence`, `occurredAt` or `payload`; must not remove
existing enum values; must not raise the 1,000-event batch maximum without a
resource-limit review; and must not introduce an open provider payload object.

## Permission Catalog

`auth/permission-catalog.json` owns grantable OpenSoha resource/action keys,
risk and scope metadata, approval posture, and temporary legacy aliases. Its
`contentHash` covers the ordered permission definitions; the Go `auth` package
also publishes the full artifact hash.

For `0.1.x`, patch releases may add active permission definitions and legacy
aliases. They must not remove active definitions, change an active key's
resource/action meaning, or make a legacy alias authorize a broader action.
Coarse keys become non-assignable before removal, and are removed only after
role migration and consumer conformance prove no active dependency.

## Consumer Adoption

Session access tokens may add the optional positive `authzVersion` claim. Core
binds versioned tokens to the current user authorization version; refreshing a
session cannot revive a token issued before a membership change. During migration,
tokens without the claim must resolve current roles, teams, projects and tags from
the authoritative identity store rather than trusting their embedded membership.
Refresh tokens and existing client login/refresh response fields are unchanged.
This does not replace session revocation, account-state or per-resource authorization.

Connection tests and Compute provider health checks return HTTP 200 with
`ConnectionCheckResultEnvelope` after each fresh probe. Consumers read `healthy`
and the diagnostic fields directly; no task or task log is created. The Compute
endpoint still accepts `Idempotency-Key` for request compatibility, but it no
longer replays stored checks. Operation/audit logs and existing task history are
preserved. HTTP 202 task envelopes remain documented for older servers. Core,
Web and CLI must adopt the new HTTP 200 result together. Discovery and asset
synchronization remain asynchronous. The Go SDK
`CheckComputeProviderInstanceHealth` method now returns `ConnectionCheckResult`
instead of `ComputeTaskView`; callers consuming its fields must update with Core
to preserve the immediate health result and diagnostic details. Upgrade Core
before these clients. The SDK rejects legacy task responses with a server-upgrade
error instead of reporting an empty health result; the 202 schema documents older
server wire behavior and does not promise task polling in the new check method.

Docker port mapping creation reserves control-plane metadata synchronously; it
does not enqueue `port_reserve` or bind a host port. Existing operation kinds and
historical records remain compatible. Service log readers use the existing
project runtime logs API. The service-action API accepts start, stop and restart;
`logs` returns an invalid-argument error directing callers to the direct project
log query instead of creating a task. Compute resource actions expose only these
mutations. Historical `connection_test`, `port_reserve`, and `service_action`
with action `logs` remain readable, but their operation state is not retryable
and retry requests are rejected without requeueing or appending task logs.

Recommended adoption flow:

1. Change contracts and fixtures here first.
2. Run `npm test` in this repository.
3. Regenerate SDKs with `npm run generate` when OpenAPI changes.
4. Update consumers to the same contract version.
5. Run consumer build/test gates before release.

Private staging may use sibling checkouts, `go.work`, Go `replace`, or
`file:../soha-contracts`. Public release builds should pin tagged versions or
published npm packages.

The consumer matrix gate is:

```sh
npm run check:consumers
```

Local runs skip missing sibling checkouts by default. CI and release candidate
validation should use `--require-all` or a specific `--consumer <name>` so a
missing checkout is a failure.

## Capability lifecycle metadata

`execution.checks` adds optional version-pinned references to read-only availability,
precondition, and assessment capabilities. Discovery filters references using the
caller-visible catalog before pagination. They are suggestions for explicit plan
steps, not automatic execution or a replacement for domain admission. No declared
check means unknown support, not a successful check.

`execution.recoveryMode=original_call` declares read-only receipt lookup by the
managed goal executor with the exact persisted invocation and current authorization.
It does not promise rollback or authorize a new write. Consumers without these
optional fields keep the existing version, idempotency, status, and cancellation
behavior.

## Contracts-first consumer revisions

`compat/consumer-revisions.json` pins a reviewed full commit SHA for every consumer
in the executable matrix. CI and release use `scripts/select-consumer-revision.mjs`;
missing or invalid revisions fail instead of falling back to `main`. Update a pin
only with an explicit compatibility reason and successful matrix evidence.

Manual CI may select `consumer_mode=latest` for separate integration evidence.
This does not rewrite the fixed manifest or replace fixed-combination acceptance.
Record the actual contracts and consumer commits, not only the selected mode.

The matrix includes the independent App frontend. Its initial pin
`0c3c76c373246f7d2218b48f68b89bf3097f777b` was built from isolated committed source
against the packed Contracts 0.1.23 working tree to establish backward compatibility.
That commit predates the uncommitted Native client adaptation; current-worktree
checks are separate, and release of that adaptation requires a reviewed new App
pin and verification with the published SDK rather than the local package overlay.

## External knowledge source import ceilings

`knowledge/source-import-policy.schema.json` defines optional trusted
`config.importPolicy` for external HTTP/Git/Object connectors. Core intersects
fetched document grants with this ceiling before persisting documents and chunks.
Omission defaults to the stable knowledge-base owner; an explicitly empty private
scope grants nobody. Public returned ACLs cannot exceed a finite ceiling. Finite
grants intersect within the same identity category, without inferring membership
across users, roles, teams or projects. Inline source behavior is unchanged.

Older external configurations that relied on returned public/shared ACLs must
be reviewed and given an explicit trusted policy before reingestion. Existing
indexed documents are not retroactively rewritten by this change; source-policy
editing, revocation of existing indexed data and a migration/backfill remain
separate acceptance work. Upgrade every Core replica before relying on the new
ceiling. No independent SDK release or deployment is established by a sibling
consumer matrix.

Connector credential references use the existing Gateway `secretRefs` channel
with alias `SOURCE`, instead of ordinary fetch input. Executable references use
`soha://secrets/<id>` with an optional `/versions/<n>`, matching the existing
secret service. Legacy `secret:` references remain valid stored configuration,
but fetching rejects them until an authorized source edit selects a canonical
reference; names and paths are not automatically mapped to a secret identity.
A runtime without a
versioned per-call credential delivery protocol must reject a nonempty resolved
secret context before sending an action. Static runtime credentials do not
substitute for the caller-selected source credential.

`connectors/connector-action.schema.json` adds credential-bearing action v1.
Only actions explicitly declaring `credentialProtocol=opensoha.dev/connector-action/v1`
and bounded `credentialAliases` may receive it. The caller sends the same action
in the path and envelope, invocation-local values with exact pinned references,
and a bounded expiry. The runtime authenticates Core, validates declarations,
rejects unknown/expired frames, signals cooperative cancellation and rejects late
responses on expiry or disconnect. This does not prove an external effect stopped. Core remains
the authority for identity, reference versions and scope; wire metadata does not
authorize a caller. Credentials require authenticated HTTPS or loopback transport,
and must not follow redirects, enter persistent retry queues, logs, events or results.
Unversioned requests without credentials remain compatible; old runtimes continue
to be rejected before credential-bearing actions are sent. New runtimes must not
silently substitute globally configured credentials. Connectors SDK and Core
receive dedicated HTTP boundary tests in addition to the existing consumer matrix.

The v1 runtime also bounds canonical UTF-8 JSON frames to 1 MiB, each value to
64 KiB, and structured material to depth 16 and 64 distinct nonempty strings,
including keys and the opaque value. The declaration advertises support; calls
without credentials retain the legacy envelope. An action requiring credentials
must explicitly reject their absence. Known raw and structured string material
is masked in SDK results/errors and Core outputs; conservative masking may also
mask matching business keys. A trusted connector must honor cancellation and
never persist credentials; masking is not a universal encoded-exfiltration guard.


## 0.1.24 adoption inputs

The 0.1.24 package publishes the personal-workspace and managed-VM contracts
already committed after the 0.1.23 tag. The release does not replace the bytes
of the previously published 0.1.23 package.

The fixed source-consumer matrix intentionally advances two revisions:

- Core: `e022021d9f8da8917c135671f92248ef19f2b5b4` to
  `c29cc6294dd4b7cb69db1187d728c227475fdf7c`, which implements the newly
  documented VM access, SSH and capacity routes. The earlier revision fails
  its live-route conformance test against the new operations.
- Web: `816ec0fb82e6e804f0f66d47d3446a7b5baeabc1` to
  `447cbe5358802ef40d86ac67b7b59c5afe9c846a`, which handles the additional
  `vm` and `vm_create` secret-scope enum members. The earlier exhaustive
  TypeScript record does not compile against those additive enum members.

The other four fixed revisions and all six checks remain in place. These are
reviewed adoption inputs, not a claim that the earlier source revisions pass
against the expanded SDK. The OpenAPI compatibility snapshot and all JSON
Schema and loose-boundary expectations remain unchanged; the latter two files
only synchronize their release-version metadata to satisfy their existing
guards. Candidate matrices use the packed/local contracts source. Consumers
must still pin the published version and validate the registry/proxy package
before claiming released-version acceptance.


## 0.1.26 delivery workbench contracts

This release candidate adds optional stable Kubernetes resource identities,
generic root-CR relationships and publication control, immutable template
versions/adoption previews, migration declarations, and create-only first-release
initialization. Existing request fields, operation IDs, action enums and client
methods remain compatible. Published version snapshots and fixed consumer
revisions are preserved.

Template adoption is configuration, not proof of deployment. Runtime baselines
require matching original resource UIDs and healthy execution/observation
receipts. Unknown existing deployments remain blocked; initialization cannot
adopt existing resources or replay uncertain writes. Migration guides are
review information and are not executed as scripts.

Core and Agent must implement the matching capability/provider protocols before
using root-CR or initialization tasks. Old runtimes must reject unsupported
protocols rather than interpret them as ordinary SSA or Helm updates. Publishing
this SDK alone does not enable the feature or validate a production deployment.
Consumers must pin the actually published npm/Go artifacts and validate the
released combination; local tarballs and provenance policy files do not prove
registry publication or an npm provenance attestation.
