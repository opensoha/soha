---
id: k8s-operator
name: K8s Operator
version: 0.1.0
category: platform
description: Approval-bound Kubernetes changes against a reviewed UID and resourceVersion through Soha AI Gateway.
capabilityRefs:
  - k8s.resources.identity.read
  - k8s.resources.conditional.preflight
  - k8s.resources.conditional.update
  - k8s.resources.conditional.delete
  - k8s.resources.conditional.assess
  - k8s.workloads.conditional.preflight
  - k8s.workloads.conditional.action
  - k8s.workloads.scale
  - k8s.workloads.scale.assess
  - k8s.deployments.rollout_status
requiredScopes:
  - cluster
  - namespace
---

# K8s Operator

Use this separate change skill after the user requests a specific Kubernetes
change. Keep diagnosis in `k8s-sre` read-only.

## Operating Contract

- Discover every required tool and its input schema in the live Gateway manifest.
  These conditional tools require the Task115 runtime; a packaged catalog does
  not prove a deployed server or Agent supports them.
- Keep cluster, namespace, kind, name, original UID and opaque resourceVersion
  explicit. A resource with the same name and a new UID is a different target.
- For a cluster-scoped kind, pass the tool's explicit empty namespace and require
  cluster-wide authorization for that kind. Never invent a namespace to make a
  cluster-scoped target fit a namespace-only grant. Typed workload actions remain
  namespaced.
- Use the Gateway with the selected AI client and skill binding. A governed AI
  Key cannot fall back to native REST, a local shell, or a different client or skill.
- A ready preflight proves validation only. Follow the independent Gateway
  approval and current business permissions before executing the frozen input.
- Approval, an API update receipt and workload readiness are separate states.
  A CLI `--yes` flag cannot approve a Gateway request or expand server permissions.

## Workflow

1. Discover the live manifest with `soha capabilities --output inputs` or the
   equivalent MCP discovery. Stop if a required conditional tool is absent.
2. Confirm the requested change and scope. Read `k8s.resources.identity.read`
   for the target, and retain its UID and resourceVersion without coercing the
   version to a number. This identity read does not reveal Secret data.
3. For YAML changes, prepare and review the exact bounded content, then invoke
   `k8s.resources.conditional.preflight` with that content and the original
   preconditions. For restart, scale or rollback, use
   `k8s.workloads.conditional.preflight` with the exact kind and action instead.
   Scale requires a nonnegative replicas value; rollback requires a positive
   Deployment revision. Restart rejects both values. StatefulSet rollback and
   DaemonSet scale/rollback are unsupported.
4. Stop on any preflight conflict, denied permission, unavailable capability or
   changed identity. Do not replace the guard with the latest UID/version or
   force field ownership to make a rejected change succeed.
5. Present the exact target, original preconditions and intended change. Request
   the matching conditional update, deletion or workload action through the
   Gateway approval flow; execute only the approved original arguments.
6. Keep the approval ID and returned target/version. An unknown response does
   not authorize resubmission, another approval, automatic rollback or fallback
   to an older Agent path. Inspect the original approval/operation first.
7. Retain the original `mutationRequestId` and use
   `k8s.resources.conditional.assess` with that same target and request. For
   updates/actions, satisfied means the acknowledged UID and revision remain
   current. For deletion, `deletion_requested` is only acceptance: require a
   satisfied assessment backed by the versioned metadata collection before
   reporting that the original UID is absent. A same-named replacement remains
   untouched. A finalizer, missing API, old Agent, generic 404 or unknown original
   outcome is not success. Never remove finalizers to obtain a successful result.
8. For a Deployment change, observe `k8s.deployments.rollout_status` separately.
   Report readiness only from current observed conditions. For other kinds, use
   separately discovered read capabilities; missing observation is inconclusive.

For a Native scale goal that verifies desired replicas, discover the dedicated
`k8s.workloads.scale` / `k8s.workloads.scale.assess` pair. The write retains
independent approval, original UID/version and at-most-once request recovery.
The read requires the same actor/credential, a known applied original receipt,
the exact approved replica count/preconditions and the current original UID's
desired replicas. A later controller status revision is allowed. It does not
prove rollout readiness, health or uninterrupted state. Keep the generic action's
declared exact-revision checks intact; missing dedicated support is unsupported,
not permission to replace those checks or replay an unknown write.

## Examples

### Input Example

Scale Deployment `api` in namespace `demo`, cluster `lab`, from the reviewed
identity to two replicas.

### Expected Tool Calls

1. `k8s.resources.identity.read` with `clusterId=lab`, `namespace=demo`,
   `kind=Deployment`, `name=api`.
2. `k8s.workloads.conditional.preflight` with the same target, `action=scale`,
   `replicas=2`, and `preconditions.expectedUid` /
   `preconditions.expectedResourceVersion` from the reviewed response.
3. `k8s.workloads.scale` through approval with the same target, replicas and
   preconditions, without an `action` field.
4. `k8s.workloads.scale.assess` with the original `mutationRequestId` and the
   same target, replicas and preconditions. Unknown acknowledgments or different
   approved values remain inconclusive even if current state happens to match.
5. `k8s.deployments.rollout_status` to observe readiness, independently of the
   update receipt.

## Permission Boundaries

- Requires Gateway invocation permission and current cluster/namespace scope.
- Identity reads require the owning resource-kind view permission. YAML update
  and deletion require their exact kind/action permission; YAML update permission
  does not authorize scale, restart or rollback.
- Assessment requires current view permission and the original mutation's action
  permission, plus the same actor, credential and target. An old receipt grants
  no lasting access after permissions change.
- Typed workload preflight requires the same action permission as execution.
  Kubernetes RBAC, Agent action allowlists, external ownership protection and
  field-manager conflicts still apply.
- High-risk mutation tools require independent approval. Current actor, Key
  permission ceiling, fixed client/skill binding and scope are checked again
  during approved execution.
- Secret Store consumption and Kubernetes Secret data access are separate
  permissions. This skill does not request Secret values or bulk export them.

## Forbidden Actions

- Do not request bearer tokens, passwords, kubeconfigs, private keys or Secret
  payloads. Do not place credentials in YAML, chat, logs or approval summaries.
- Do not run local `kubectl`, direct provider commands, container exec,
  port-forward, node drain or arbitrary shell commands from this skill.
- Do not add mutations to `k8s-sre` or its read-only profile/preset.
- Do not force apply, change field managers, drop finalizers, bypass an approval,
  substitute a recreated target, or retry an uncertain mutation automatically.

## Guardrails

- Use immutable reviewed UID/resourceVersion guards and exact approved input.
- Treat unavailable versioned Agent support as unsupported; never downgrade.
- Keep sensitive content out of summaries and use opaque Secret references only
  in capabilities that explicitly support them.
- Keep accepted, applied, observed-ready, denied and unknown outcomes distinct.
