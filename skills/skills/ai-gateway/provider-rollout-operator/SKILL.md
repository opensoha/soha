---
id: provider-rollout-operator
name: Provider Rollout Operator
version: 0.1.0
category: platform
description: Review and operate bounded Agent Provider fleet rollouts with conformance and rollback evidence.
capabilityRefs:
  - gateway.manifest.read
  - delivery.execution_tasks.list
  - delivery.execution_logs.list
permissionKeys:
  - ai.agent-fleet.view
  - ai.agent-fleet.manage
requiredScopes:
  - aiClient
  - skill
  - application
  - environment
  - executionTask
metadata:
  httpCapabilityRefs:
    - agent.providers.rollout
---

# Provider Rollout Operator

## Operating Contract

- Use provider catalog revision, fleet target, conformance result, ACK/NACK, LKG, and active-run evidence.
- Operate rollout transitions only through protected action endpoints, never arbitrary status updates.
- Apply an availability gate before using the baseline-approved rollout HTTP capability.

## Workflow

1. Confirm provider/plugin version, target environment/platform/architecture/labels, and rollout policy.
2. Inspect conformance and delivery evidence without exposing provider credentials.
3. Verify active runs can drain and identify the previous last-known-good revision.
4. Obtain explicit approval for rollout, pause, resume, or rollback.
5. Monitor targeted runner ACK/NACK and convergence state.
6. Roll back on conformance failure, unsafe NACK patterns, or failed acceptance thresholds.

## Examples

### Input Example

Review the canary evidence for a Codex provider upgrade and roll back if the fleet cannot converge.

### Expected Tool Calls

- `gateway.manifest.read`
- `delivery.execution_tasks.list`
- `delivery.execution_logs.list`
- Protected provider rollout HTTP capability listed in metadata

## Permission Boundaries

- Fleet reads and mutations require their dedicated permissions and declared environment scope.
- Installation, runtime availability, and permission grant remain separate states.
- Redact every access token, provider credential, private key, environment variable, and raw stderr.

## Forbidden Actions

- Do not replace a provider version while active runs are pinned to it.
- Do not bypass conformance, fleet targeting, approval, or LKG rollback controls.
- Do not infer convergence from a single runner or accepted HTTP response.

## Guardrails

- Stop if the target, previous revision, conformance evidence, or rollback path is missing.
- Preserve catalog, rollout, runner, provider, task, and audit IDs.
- Keep logs bounded and redact sensitive output.
