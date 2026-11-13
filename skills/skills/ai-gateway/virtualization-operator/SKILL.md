---
id: virtualization-operator
name: Virtualization Operator
version: 0.1.0
category: platform
description: Approval-bound virtual machine planning, creation, and typed lifecycle operations through Soha.
capabilityRefs:
  - virtualization.worker_pools.list
  - virtualization.worker_pools.get
  - virtualization.workers.create
  - virtualization.workers.assess
  - virtualization.capacity.check
  - virtualization.operations.get
  - virtualization.operations.cancel
  - virtualization.operations.retry
  - virtualization.vms.create.plan
  - virtualization.vms.create.trigger
  - virtualization.vms.action.trigger
requiredScopes:
  - virtualizationConnection
  - vm
  - cluster
  - workerPool
---

# Virtualization Operator

Use this skill to plan and operate virtual machines through Soha provider adapters.

Discover versioned capacity checks and VM creation operations from the live manifest. Use the Soha workbench or public HTTP API for other inventory and detail reads.

## Operating Contract

- Discover all VM tools from the live Gateway manifest before use.
- Confirm the selected connection is enabled and direct. Agent-connected KubeVirt virtualization is not supported.
- Use only actions returned by the VM's live `allowedActions`; do not assume provider parity. KubeVirt currently advertises CPU and memory resize only.
- Plan every VM create request before execution.
- Use typed lifecycle actions and stable idempotency keys only.
- When provider or bootstrap credentials are required, attach canonical references through `_sohaSecretRefs`; keep them outside cloud-init and business input.

## Workflow

1. Confirm the virtualization connection, image, flavor, VM name, and requested resources. For automated supply, call `virtualization.capacity.check`; unavailable or unknown capacity cannot be treated as available.
2. Select only secrets bound to the capability and target connection, then call `virtualization.vms.create.plan` and review warnings and redaction indicators.
3. Present the plan and obtain required approval.
4. Call `virtualization.vms.create.trigger` with the approved input, the same secret references, and a stable idempotency key.
5. Follow the returned task reference with `virtualization.operations.get`. Use `virtualization.operations.cancel/retry` on that same operation, preserving each mutation key after a lost reply. A retry rechecks current VM permissions.
6. Use `virtualization.vms.action.trigger` only for a named typed action on a known VM id.

## Examples

### Input Example

Create a small VM named `demo-api` from an approved image and flavor.

### Expected Tool Calls

1. `virtualization.vms.create.plan` for the selected connection.
2. `virtualization.vms.create.trigger` only after approval.

## Permission Boundaries

- Requires explicit virtualization connection and VM scope.
- Image, flavor, provider, policy, and approval checks remain server-side.
- PVE and KubeVirt availability depends on the selected live connection and its reported capabilities; this skill does not assert lab or provider readiness.
- Secret-backed operations additionally require `secret.use`; the server enforces secret scope, bindings, approval, and audit.

## Forbidden Actions

- Do not request provider passwords, API tokens, private keys, credentials, or console secrets.
- Do not run provider CLI, direct SSH, raw hypervisor commands, or untyped VM actions. Managed VM commands are permitted only through live Soha VM SSH capabilities with approval.

## Guardrails

- Keep cloud-init credentials and secret material out of plans, logs, and chat output.
- Pass provider/bootstrap references through `_sohaSecretRefs`; managed guest initialization uses `guestAccess.publicKeyRefs` with explicit `vm_cloud_init` permission. Never pass, resolve, log, or return secret values.
- Stop if the plan changes before execution and request a new approval.
- Reuse an idempotency key only for an identical approved request.
- Capacity suggestions expire after 30 seconds and never reserve resources. Bind node/storage suggestions only within the same connection; creation performs fresh atomic admission.
- For automated capacity supply, require the live create schema to support `requireCapacity: true`. Keep it in the approved input; do not retry without it after insufficient or incomplete inventory. Admission reserves CPU cores, memory in MiB and root disk in GiB with the existing VM task.
- PVE admission requires whole-provider audit visibility and counts configured guest commitments and storage volume sizes. KubeVirt requires direct clients, eligible nodes, Pod requests, synchronized quotas and CSI storage capacity; the current root-disk path is DataSource cloning. Provider admission and final VM/Agent readiness remain separate checks.
- KubeVirt capacity mode requires `startAfterCreate: true`. Quotas must use supported CPU, memory and storage request dimensions; scoped quotas, limits and object-count quotas currently block admission because the reservation cannot account for them completely.
- Connection configuration can set `capacitySourceId` to identify aliases of the same physical provider pool, and `capacityMemoryOverheadMiB` to adjust the default 512 MiB allowance. Preserve these operator-owned settings. Missing capacity is not permission to change limits or overcommit.
- Preserve the original task after timeout or `canceling`. The provider VM identity is frozen before dispatch; unknown effects retain their reservation. A cancellation confirmation may include an already-created VM and does not mean the VM was rolled back. Do not submit a fresh creation key to work around an unknown result.
- A pre-dispatch retry must pass fresh capacity admission and reacquire its original reservation atomically. A dispatched retry keeps the original provider identity and requires the retained reservation; it cannot bypass a released or accounted reservation. Cancellation observes the original VM after the caller has stopped; absence or a provider lock is inconclusive.
- A durable PVE creation retains partially created resources for recovery. Use the returned VM reference for authorized cleanup or follow-up; do not assume a failed or canceled creation deleted it.

## Kubernetes Worker Supply

1. Discover the live `virtualization.worker_pools.list/get`, `virtualization.workers.create/assess` schemas. Read the operator-registered pool and its current revision. Do not invent a provider, cluster, image, version, supply owner or node limit.
2. The initial supported pool uses an independent PVE provider, an approved Ubuntu 24.04 amd64 image with containerd and matching kubeadm/kubelet, and the registered kubeadm cluster. Registration alone is not evidence of current capacity or node readiness. Other distributions and supply owners require their own advertised adapter.
3. Request one worker with the frozen pool revision and a stable idempotency key through the governed creation capability. Soha performs atomic pool and provider admission. Node creation requires `platform.nodes.create` in addition to VM and image permissions. Do not switch to unrestricted VM creation after a pool rejection.
4. Preserve the returned original operation and use its status/cancel/retry capabilities. If the VM already exists, continue that operation; never create a replacement key to hide an unknown result. Missing bootstrap keys must be configured by the operator, never supplied through chat.
5. Call `virtualization.workers.assess` for fresh original Node identity, ownership, heartbeat, schedulability and required daemon evidence. A VM receipt or terminal task alone does not meet the deployment goal. Repeat delivery scheduling preflight before continuing deployment and obtain fresh deployment/access/metrics evidence.
6. Cancellation revokes the original short-lived bootstrap token where possible. It does not delete a partial VM, a registered Node, or issued kubelet certificates. Keep partial-resource references and scope them explicitly for any authorized follow-up.

Pool edits use revision checks. Unused pool configurations may be deleted; pools with operation history are disabled and retained for recovery and audit. Saving a pool is a configuration action and never creates a worker automatically.

## Candidate VM SSH workflow

Use this workflow only when the live manifest exposes `virtualization.vm_ssh.preflight`, `execute`, `get`, `cancel`, and `assess`. It requires the matching candidate Core/Agent protocol; this package does not assert released or real PVE readiness.

1. Read preflight for the named VM and current binding revision. The binding fixes guest account, credential version, provider identity, trusted host key and execution Agent. Missing executor capability, stale identity or missing permission stops execution.
2. Review the exact command and timeout, then obtain the tool's required approval. Use an identical approved request and stable idempotency key with `execute`; never select arbitrary hosts or resolve credentials into environment variables.
3. Follow `get` for the original command. `cancel` cancels that same task and revokes its authorization. A dispatched command without a reliable exit status stays unknown; do not replay it or claim that remote effects were rolled back.
4. `assess` proves only VM SSH capability and current binding. It does not mark a complete Personal Computer environment ready.

For candidate PVE admission, a configured policy applies even when a caller omits `requireCapacity`. CPU and memory ratios are separate bounded configuration budgets; actual capacity and storage remain enforced. Administrators confirm physical source identity and activate old policies explicitly. Do not change ratios, VM configurations or source identity to bypass a rejection. Credential rotation never automatically updates guest authorized_keys or login bindings.
