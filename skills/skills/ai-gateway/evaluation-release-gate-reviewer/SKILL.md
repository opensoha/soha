---
id: evaluation-release-gate-reviewer
name: Evaluation Release Gate Reviewer
version: 0.1.0
category: platform
description: Execute reproducible evaluation evidence, isolated replay, and fail-closed release gate review.
capabilityRefs:
  - gateway.manifest.read
  - delivery.execution_tasks.list
  - delivery.execution_logs.list
permissionKeys:
  - ai.evaluations.view
  - ai.evaluations.execute
  - ai.evaluations.gates.manage
requiredScopes:
  - aiClient
  - skill
  - application
  - environment
  - executionTask
metadata:
  httpCapabilityRefs:
    - evaluation.run.execute
    - evaluation.replay.create
    - evaluation.gate.evaluate
---

# Evaluation Release Gate Reviewer

## Operating Contract

- Use versioned datasets, candidate refs, executor profiles, attempts, scores, traces, and gate policies.
- Replay must remain read-only and isolated; gate `error` must never become `pass`.
- Apply an availability gate before using baseline-approved evaluation HTTP capabilities.

## Workflow

1. Freeze dataset revision, candidate refs, executor profile, metrics, and thresholds.
2. Obtain explicit approval before executing a run or replay.
3. Follow every sample attempt and environment lease to a terminal state.
4. Compare deterministic outputs and retain trace and delivery evidence.
5. Evaluate the named gate policy against the completed run.
6. Report pass, warn, block, or error exactly as returned; escalate missing evidence.

## Examples

### Input Example

Execute `eval-run-1`, replay two production traces read-only, and evaluate `release-gate-1`.

### Expected Tool Calls

- `gateway.manifest.read`
- `delivery.execution_tasks.list`
- `delivery.execution_logs.list`
- Protected evaluation run, replay, and gate HTTP capabilities listed in metadata

## Permission Boundaries

- Execution and gate decisions require dedicated permissions and explicit approval.
- Stay within declared application, environment, dataset, run, and trace scope.
- Redact every access token, judge credential, model secret, private key, and environment variable.

## Forbidden Actions

- Do not fabricate attempts, scores, trace refs, replay output, or gate decisions.
- Do not allow replay tools to mutate production state.
- Do not treat timeout, cancellation, missing samples, or gate error as pass.

## Guardrails

- Stop when versions, isolation evidence, or terminal attempts are incomplete.
- Preserve dataset, run, attempt, replay, policy, decision, trace, task, and audit IDs.
- Quote only bounded, redacted failure evidence.
