---
id: agent-runtime
name: Agent Runtime
version: 0.1.0
category: platform
description: Agent Runtime claim, callback, and failure artifact handoff through Soha AI Gateway.
capabilityRefs:
  - delivery.execution_tasks.list
  - delivery.execution_logs.list
  - diagnosis.release_failure.analyze
  - k8s.events.list
  - k8s.pods.logs
requiredScopes:
  - application
  - environment
  - executionTask
  - cluster
  - namespace
---

# Agent Runtime

Use this skill when an AI assistant is helping operators inspect Agent Runtime handoff state, callback evidence, and failed provider artifacts through Soha AI Gateway.

## Operating Contract

- Treat the control plane as the source of truth for execution tasks, callback status, and analysis artifacts.
- Use Gateway tools only; do not inspect runner workspaces, provider files, Kubernetes credentials, kubeconfig, or secret values directly.
- Keep application, environment, execution task, cluster, namespace, workload, pod, provider, and callback identifiers explicit.
- Separate pending, running, failed, callback_timeout, and completed states in the final answer.

## Workflow

1. Confirm the application, environment, execution task, cluster, namespace, and time range.
2. List execution tasks and logs to identify the runner or provider handoff point.
3. Read Kubernetes events and pod logs only when they are part of the declared runtime scope.
4. Invoke release-failure analysis for failed or timed-out runtime callbacks when the Gateway manifest exposes the analyzer.
5. Summarize artifacts, callback payload status, retry status, and missing evidence without exposing raw secret-looking data.
6. Produce the next operational action as inspect, wait, retry through approved workflow, or escalate to platform owner.

## Examples

### Input Example

Investigate why execution task `task-123` for application `billing-api` in environment `staging` did not receive an Agent Runtime callback.

### Expected Tool Calls

- `delivery.execution_tasks.list`
- `delivery.execution_logs.list`
- `diagnosis.release_failure.analyze`
- `k8s.events.list`
- `k8s.pods.logs`

## Permission Boundaries

- Requires application, environment, execution task, cluster, and namespace scope.
- Uses only read/analyze Gateway tools unless a separate approved delivery action is requested.
- Callback retry or workflow mutation must stay behind the owning delivery action and Gateway approval guardrail.

## Forbidden Actions

- Do not run provider binaries, kubectl, Docker, shell, or CI commands outside Gateway.
- Do not request or print tokens, kubeconfig, private keys, environment secrets, or raw credentials.
- Do not fabricate callback status when Gateway evidence is missing.

## Guardrails

- Redact secret-like strings from logs and callback summaries.
- Preserve every evidence identifier used in conclusions.
- Prefer concise failure artifacts over raw log dumps.
- Mark unsupported Agent Runtime states as unsupported instead of guessing.
