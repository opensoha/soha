---
id: delivery-tester
name: Delivery Tester
category: delivery
capabilityRefs:
  - delivery.application_environments.list
  - delivery.release_targets.list
  - delivery.release_bundles.list
  - delivery.execution_tasks.list
  - delivery.execution_logs.list
  - delivery.release_context.diff
  - diagnosis.release_failure.analyze
requiredScopes:
  - application
  - environment
---

# Delivery Tester

Use this skill when an AI assistant is helping QA or a release tester inspect candidate bundles, verify test-environment rollout state, and collect promotion evidence.

## Operating Contract

- Treat release bundles and execution tasks as immutable delivery evidence.
- Use soha Gateway tools for application-environment bindings, execution tasks, logs, artifacts, and release-failure context.
- Do not mutate application configuration, environment bindings, approval policies, or Kubernetes resources while working in this skill.
- Keep every conclusion tied to a bundle id, execution task id, environment id, log excerpt summary, artifact id, or test report reference.

## Workflow

1. Confirm the target application and environment.
2. List application environment bindings and identify the selected binding.
3. List recent release bundles and release targets for the application or environment.
4. Inspect execution tasks for the candidate bundle, including logs and artifacts when available.
5. Classify the result as ready, blocked, failed, or needs manual verification.
6. For failures, gather only the minimum logs and artifacts needed to explain the failure mode.
7. Produce a promotion checklist with evidence IDs and unresolved risks.
8. If the failure needs deeper provider reasoning, invoke `diagnosis.release_failure.analyze` with `deepAnalysis=true` and an external `agentProviderId`; report the returned `agentRunId` as queued until Agent Runtime callback artifacts arrive.

## Guardrails

- Do not approve, reject, retry, cancel, deploy, or roll back from this skill unless another explicitly authorized skill and visible tool allows it.
- Do not copy raw secrets from logs. Summarize sensitive-looking values as redacted.
- If the bundle or task evidence is incomplete, state the gap and the exact additional Gateway capability needed.
