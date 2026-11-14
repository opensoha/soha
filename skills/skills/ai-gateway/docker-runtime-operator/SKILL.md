---
id: docker-runtime-operator
name: Docker Runtime Operator
version: 0.1.0
category: platform
description: Approval-bound Docker host, Compose project, and service operations through Soha.
capabilityRefs:
  - docker.hosts.quick_create.plan
  - docker.hosts.quick_create.trigger
  - docker.projects.create
  - docker.projects.get
  - docker.projects.deploy.plan
  - docker.projects.deploy.trigger
  - docker.services.action.trigger
  - docker.operations.get
  - docker.projects.runtime.assess
  - docker.operations.cancel
requiredScopes:
  - virtualizationConnection
  - dockerHost
  - dockerProject
  - dockerService
  - dockerOperation
---

# Docker Runtime Operator

Use this skill to provision Docker hosts, configure projects, and operate Docker/Compose services through Soha.

## Operating Contract

- Discover every tool from the live Gateway manifest before use.
- Plan host creation and project deployment before requesting execution.
- Use only typed actions and stable idempotency keys.
- When credentials are required, attach canonical references through `_sohaSecretRefs`; keep them outside Compose content and business input.

## Workflow

1. Confirm the virtualization connection, Docker host, project, and service scope.
2. Select only secrets bound to the capability and target connection or project, then call the matching `.plan` tool.
3. Present changes, warnings, and approval requirements.
4. Call the matching `.trigger` tool only after approval, with the same secret references used by the plan.
5. Preserve the returned capability version and `task` reference. When present in the live manifest, invoke `task.statusCall` (`docker.operations.get` with `operationId`) to read the same durable record after submission or reconnect. Each read still requires current permissions.
6. When available, use `docker.projects.runtime.assess` with the project, completed deployment operation, and expected services. Fresh post-deployment inventory can prove runtime state; stale, missing, or earlier inventory is inconclusive. This assessment does not prove application health or URL reachability.
7. Use the returned cancellation call only within authorized intent, follow any cancellation approval, and preserve the child task until its domain confirms the outcome.
8. Bound polling by an interval and deadline. A caller timeout does not cancel the domain operation; a terminal operation does not prove workload health or URL reachability. Report missing verification separately.

## Continuous Delivery

- Use `docker.projects.create` to save inline Compose or single-container configuration with a stable creation key. The approved call creates configuration and service records only; its fixed receipt does not deploy a workload. Use `docker.projects.get` for current identity and configuration state.
- Use the Delivery Developer skill and live `delivery.drafts.create/confirm` capabilities to create the application, services, and Docker environment targets, binding the created project and host IDs through typed plan outputs.
- Create a delivery workflow, then submit a delivery batch with a verified release bundle. The batch freezes configuration and image digests, runs preflight, follows environment approval, deploys, and evaluates fresh runtime inventory.
- Frozen Compose must be self-contained. Only the bundled `.env` file is supported; build directives, external files, variable interpolation, host-environment inheritance, and unpinned images are rejected. Use governed SecretRefs when the live capability supports them.
- Preserve the batch, plan, and Docker operation IDs. Configuration receipts, preflight success, and container readiness each prove their own stage. Return application health and reachable URLs only with separate evidence.

## Examples

### Input Example

Deploy project `demo-api` on an existing Docker host.

### Expected Tool Calls

1. `docker.projects.deploy.plan` with the scoped project id.
2. `docker.projects.deploy.trigger` with the approved input and stable idempotency key.
3. `docker.operations.get` with the returned operation id until terminal or the caller's deadline.

## Permission Boundaries

- Host provisioning requires both Docker host and virtualization VM permissions.
- Project and service actions remain bound to the selected Docker host and object ids.
- Secret-backed operations additionally require `secret.use`; the server enforces secret scope, bindings, approval, and audit.

## Forbidden Actions

- Do not request SSH keys, passwords, tokens, secret material, credentials, or raw daemon access.
- Do not run shell commands or send untyped Docker, Compose, or system commands.

## Guardrails

- Keep secrets out of Compose content, environment values, plans, logs, and chat output.
- Pass references only through `_sohaSecretRefs`; never pass, resolve, log, or return secret values.
- Stop when a required capability is absent or an operation is pending approval.
- Reuse the operation id for status tracking and the idempotency key only for identical retries.
- When provisioning a backing VM as automated capacity, use `requireCapacity: true` if declared by the live quick-create schema. The VM uses a stable key tied to the Docker host; insufficient or unknown provider inventory must not be bypassed with a second request or by removing the capacity requirement.
- Treat a backing VM in `canceling` as unresolved. Preserve the Docker operation, VM task and any returned VM identity until the domain confirms its outcome; cancellation does not imply resource rollback.
- Parent and VM retries must succeed together. A capacity admission rejection leaves the parent unqueued. A completed backing VM is reused while waiting for Agent registration. Once an Agent owns the operation, only that runner's cancellation acknowledgment can confirm it stopped; VM completion alone cannot do so.
