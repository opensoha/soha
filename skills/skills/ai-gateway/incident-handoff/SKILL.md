---
id: incident-handoff
name: Incident Handoff
version: 0.1.0
category: delivery
description: Incident handoff summaries using delivery, rollback, Kubernetes, and analysis evidence.
capabilityRefs:
  - delivery.execution_tasks.list
  - delivery.execution_logs.list
  - delivery.release_context.diff
  - delivery.rollback.context
  - diagnosis.release_failure.analyze
  - k8s.events.list
requiredScopes:
  - application
  - environment
  - executionTask
  - releaseBundle
  - cluster
  - namespace
---

# Incident Handoff

Use this skill when an AI assistant needs to produce a shift handoff, incident bridge summary, or escalation note from Soha Gateway evidence.

## Operating Contract

- Anchor every statement to delivery tasks, release bundles, rollback context, Kubernetes events, or analyzer artifacts.
- Keep application, environment, release bundle, execution task, cluster, namespace, time range, and owner explicit.
- Preserve uncertainty; do not convert weak evidence into a confirmed root cause.
- Treat credentials, tokens, kubeconfig, secret names, and private incident notes as sensitive.

## Workflow

1. Confirm incident scope, severity, current owner, and audience.
2. List execution tasks and logs for the affected application and environment.
3. Compare release context and rollback context when a deployment or rollback is involved.
4. Read Kubernetes events for platform symptoms in the scoped namespace and time range.
5. Invoke release-failure analysis only after collecting bounded evidence.
6. Produce a handoff with current state, timeline, evidence, impact, mitigations, risks, and next owner actions.

## Examples

### Input Example

Prepare a handoff for the failed `billing-api` production rollout and include rollback context.

### Expected Tool Calls

- `delivery.execution_tasks.list`
- `delivery.execution_logs.list`
- `delivery.release_context.diff`
- `delivery.rollback.context`
- `diagnosis.release_failure.analyze`
- `k8s.events.list`

## Permission Boundaries

- Read and analyze delivery/runtime evidence within the declared application, environment, release bundle, execution task, cluster, and namespace.
- Handoff content may recommend an approved rollback path but must not execute it.
- Escalation notes must avoid raw secrets and redact sensitive log content.

## Forbidden Actions

- Do not approve, reject, cancel, deploy, or rollback as part of the handoff.
- Do not expose passwords, tokens, kubeconfig, private keys, or registry credentials in the incident summary.
- Do not cite evidence that was not returned by Gateway tools.

## Guardrails

- Include evidence IDs next to each major claim.
- Separate confirmed facts from hypotheses and requested follow-up.
- Keep customer or tenant identifiers scoped to what the user already provided.
- Prefer rollback criteria over direct rollback execution.
