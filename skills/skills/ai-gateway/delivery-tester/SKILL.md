---
id: delivery-tester
name: Delivery Tester
version: 0.1.0
category: delivery
description: Release testing and promotion evidence through Soha AI Gateway.
capabilityRefs:
  - delivery.application_environments.list
  - delivery.release_targets.list
  - delivery.release_bundles.list
  - delivery.execution_tasks.list
  - delivery.execution_logs.list
  - delivery.release.plan
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
- Do not mutate application configuration, environment bindings, workflow approval gates, or Kubernetes resources while working in this skill.
- Keep every conclusion tied to a bundle id, execution task id, environment id, log excerpt summary, artifact id, or test report reference.
- Separate saved definitions, imported drafts, published template versions, accepted triggers, running batches, and verified results. A successful synchronization or external CI trigger does not prove a build or deployment completed.
- For GitLab, inspect the frozen pipeline/source commits, parent/child result, artifact report, and verified registry digest. For Buildpacks, report the runner architecture and whether execution was native or emulated.
- For GitOps and Operator delivery, require the current generation and matching child inventory. For blue-green/canary, inspect actual Service revisions or traffic weights, the full metric window, and confirmed stable traffic after failure/cancellation. If Gateway lacks this evidence, state the gap; do not query a controller directly or promote/abort a rollout from this read-only skill.

## Workflow

1. Confirm the target application and environment.
2. List application environment bindings and identify the selected binding.
3. List recent release bundles and release targets for the application or environment.
4. Inspect execution tasks for the candidate bundle, including logs and artifacts when available.
5. Classify the result as ready, blocked, failed, or needs manual verification.
6. When asked for a promotion or deploy recommendation, use `delivery.release.plan` to produce a DeliveryPlan preview only; do not trigger execution.
7. For failures, gather only the minimum logs and artifacts needed to explain the failure mode.
8. Produce a promotion checklist with evidence IDs and unresolved risks.
9. If the failure needs deeper provider reasoning, invoke `diagnosis.release_failure.analyze` with `deepAnalysis=true` and an external `agentProviderId`; report the returned `agentRunId` as queued until Agent Runtime callback artifacts arrive.

## Examples

### Input Example

User asks: "Check whether release bundle `rb-20260609-17` for checkout service is ready to promote from test to staging."

### Expected Tool Calls

1. `delivery.application_environments.list` for the checkout application and test environment.
2. `delivery.release_targets.list` to confirm the target binding.
3. `delivery.release_bundles.list` to locate `rb-20260609-17`.
4. `delivery.execution_tasks.list` for execution evidence tied to the bundle.
5. `delivery.execution_logs.list` for bounded, redacted failure or verification logs.
6. `delivery.release_context.diff` to compare candidate context before producing the promotion checklist.
7. `delivery.release.plan` only if the user asks for a deploy/promotion plan preview; confirmation and execution stay outside this skill.

## Permission Boundaries

- Requires Gateway-visible delivery read and analysis capabilities for `application` and `environment` scopes.
- Reads release evidence and summarizes sensitive-looking log values as redacted.
- Uses `delivery.release.plan` only for previewing a DeliveryPlan; it must not confirm or execute the plan.
- Uses `diagnosis.release_failure.analyze` only when bounded evidence is insufficient and the current manifest exposes that capability.

## Forbidden Actions

- Do not approve, reject, retry, cancel, deploy, roll back, or mutate release state from this skill.
- Do not include access token, refresh token, password, kubeconfig, private key, credential, or raw secret-looking log values in output.
- Do not invent artifact, task, or test-report identifiers when Gateway evidence is missing.

## Guardrails

- Do not approve, reject, retry, cancel, deploy, or roll back from this skill unless another explicitly authorized skill and visible tool allows it.
- Do not copy raw secrets from logs. Summarize sensitive-looking values as redacted.
- If the bundle or task evidence is incomplete, state the gap and the exact additional Gateway capability needed.
