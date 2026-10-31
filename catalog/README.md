# Skill Catalog

This catalog is a packaged release asset. It records the Gateway capability
snapshot consumed by official OpenSoha skills and the compatibility matrix used
by local validation.

## Compatibility Matrix

- Skills package version: `0.1.3`
- Gateway capability catalog version: `0.1.1`
- Platform capability catalog version: `0.1.0`
- Supported `soha-core`: `>=0.1.0 <0.2.0`
- Supported `soha-cli`: `>=0.1.0 <0.2.0`
- Supported `soha-agent`: `>=0.1.0 <0.2.0`

The normalized Gateway required scope union is:

- `application`
- `applicationEnvironment`
- `aiClient`
- `approval`
- `audit`
- `businessLine`
- `cluster`
- `computeDomain`
- `computeResource`
- `computeTask`
- `dataSource`
- `deployment`
- `device`
- `dockerHost`
- `dockerOperation`
- `dockerProject`
- `dockerService`
- `enrollment`
- `environment`
- `executionTask`
- `gateway`
- `grant`
- `namespace`
- `node`
- `pod`
- `policy`
- `producer`
- `relayCache`
- `relayCall`
- `relayRoute`
- `relayUpstream`
- `releaseBundle`
- `repository`
- `resource`
- `runtime`
- `service`
- `serviceAccount`
- `session`
- `site`
- `skill`
- `storage`
- `subject`
- `timeRange`
- `token`
- `tool`
- `virtualizationConnection`
- `vm`
- `workerPool`

Validation prefers the published `node_modules/@opensoha/contracts` package
when it is available. A sibling `../soha-contracts` checkout is a local
development fallback before public release artifacts are published.

The packaged contract schema locations are:

- `node_modules/@opensoha/contracts/skills/skill-manifest.schema.json`
- `node_modules/@opensoha/contracts/presets/mcp-preset.schema.json`
- `node_modules/@opensoha/contracts/profiles/agent-profile.schema.json`
- `../soha-contracts/skills/skill-manifest.schema.json`
- `../soha-contracts/presets/mcp-preset.schema.json`
- `../soha-contracts/profiles/agent-profile.schema.json`

[`asset-governance.json`](./asset-governance.json) records release signing
requirements, permission review coverage for every packaged skill, MCP preset,
and agent profile, and the install audit event schema required for verify,
install, upgrade, rollback, and activate decisions.

Install the versioned wrapper package and expand the matching raw skills
runtime into a staging directory under `~/.soha/skills`. Upgrade only after the
release manifest, checksum, compatibility matrix, validation report, and target
CLI load check pass. Roll back by restoring the previous verified wrapper
package and switching the active `~/.soha/skills` runtime pointer back to the
previous verified directory.

Worker supply adds the `workerPool` scope alongside the virtualization connection and target cluster. Discover registered pools and their revisions before creation; follow the original VM operation and fresh worker assessment for recovery. The catalog lists source capabilities and is not proof that a running server has upgraded or a provider is ready.

The four CRD/custom-resource and workload metric entries reflect the reviewed Core 0.1.10 tool definitions. Discover capabilities from the running server before invoking them; older supported versions may not expose these tools.

The Task115 candidate adds conditional resource/workload tools and the
separate `k8s-operator` change skill. Their Gateway registration, permission
checks and approval behavior come from `conditional_resource_provider.go`;
they require live capability discovery and an upgraded Core/Agent. The existing
`k8s-sre`, read-only preset and read-only agent profile remain unchanged. Native
Pod argv/TTY support is not implied by this Gateway skill. No candidate package
or runtime has been published by this change.

The initial 2026-10-25 permission review added `k8s-operator` after scoped source
and independent agent review. Earlier asset approvals are retained; this record
is not a human release approval, proof of deployment, or authorization to perform
a Kubernetes change. Cluster-scoped targets use an explicit empty namespace and
cluster-wide resource permissions; a namespace-only grant is insufficient.

The Task115 candidate also adds `k8s.pods.exec`, `k8s.pods.exec.receipt` and
`k8s.pods.exec.assess`
through `pod_execution_provider.go`, with the separate `k8s-pod-executor` skill.
Execution requires independent approval, credential-bound delegation, current
exec permissions and target scope. Receipt lookup never submits a process.
Gateway output contains metadata and a separately redacted text projection;
binary output is withheld. The original native receipt remains encrypted and
unchanged. Discover all tools and schemas live; older runtimes may lack them.
Protected argv/stdin require explicitly enabled encrypted durable storage for
capability plans. Native must use an owned governed task and the live provider's
original-call recovery and receipt-bound verification checks. Missing support
remains unsupported. Direct encrypted approval and original receipt lookup remain
available; neither path authorizes automatic replay of an unknown process.
The 2026-10-25 scoped permission review covers this separate asset after source
and independent agent review, plus focused Core tests for approval, credentials,
scope and output protection. It is a source permission review, not a release or
deployment approval; full runtime and final integration evidence remain separate.
Existing read-only assets and the resource-change skill do not gain exec.

The dedicated `k8s.workloads.scale` / `k8s.workloads.scale.assess` pair retains
independent approval, original-call recovery and original UID/version guards.
Its assessment requires a known original acknowledgment and the exact approved
replicas/preconditions before observing current desired replicas on that UID.
Later status revisions are allowed; rollout readiness and uninterrupted state
are not claimed. The generic action and its exact-revision assessment remain
unchanged. Isolated Direct/Agent, CLI/MCP and wrong-value checks passed for the
candidate; the packaged catalog still does not prove deployed availability.

The 2026-10-29 scoped source review adds the read-only original assessments to
the two change skills. `k8s.resources.conditional.assess` binds the original
actor/credential/request and target, current view/action permissions and the
connection. For deletion, only a complete versioned metadata collection can prove
the original UID absent; finalizers, unknown outcomes and generic 404 are not
success, and a replacement is left untouched. Pod assess proves known zero-exit
process completion only. Runtime fixtures, source review and package validation
are separate evidence; this catalog is not a publication or deployment claim.
