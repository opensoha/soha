---
id: observability
name: Observability
version: 0.1.0
category: platform
description: Cross-domain observability triage through Soha AI Gateway evidence.
capabilityRefs:
  - observability.metrics.assess
  - k8s.pods.logs
  - k8s.events.list
  - k8s.nodes.detail
  - delivery.execution_logs.list
  - diagnosis.release_failure.analyze
requiredScopes:
  - cluster
  - namespace
  - application
  - environment
---

# Observability

Use this skill when an AI assistant is helping correlate Soha delivery logs, Kubernetes events, pod logs, node context, and AI analysis artifacts.

## Operating Contract

- Use Soha Gateway as the evidence boundary for the delivery logs, Kubernetes events, pod logs, node detail, and release analysis declared in `capabilityRefs`.
- Discover `observability.metrics.assess` in the live manifest before evaluating registered metric conditions. Other telemetry remains unavailable unless an installed skill and preset expose the corresponding runtime capability and scope.
- Keep cluster, namespace, application, environment, pod, node, execution task, and time range explicit.
- Prefer aggregated summaries and evidence IDs over raw logs that may contain passwords, tokens, or credentials.
- Distinguish symptoms, correlated signals, likely cause, and missing telemetry.
- Fix the data source, metric scope, unit, threshold and observation window. For a release, bind `notBefore` to `delivery.batches.assess` output `/deployedAt` and allow the metric's lookback window to finish after that time. Every query step needs a finite value and fresh underlying sample timestamps. Missing coverage, no traffic, stale samples or partial responses remain inconclusive. The built-in HTTP error ratio uses `http_requests_total` with a three-digit `status` label and counts 5xx responses; do not assume another instrumentation convention matches it.

## Workflow

1. Confirm scope, incident time window, and the affected workload or release.
2. Read recent delivery execution logs when an application or execution task is in scope.
3. Read Kubernetes events, pod logs, and node detail through Gateway tools.
4. Use release-failure analysis only after collecting the minimum evidence needed for a useful analysis request.
5. Summarize correlated signals by timestamp and ownership boundary.
6. Return an action-oriented triage note with confidence, gaps, and next checks.

## Examples

### Input Example

Correlate pod restarts in namespace `payments` with the failed deployment task for `billing-api`.

### Expected Tool Calls

- `delivery.execution_logs.list`
- `k8s.events.list`
- `k8s.pods.logs`
- `k8s.nodes.detail`
- `diagnosis.release_failure.analyze`

## Permission Boundaries

- Read-only observability tools are allowed within the declared application, environment, cluster, and namespace.
- Analysis tools may be used only with redacted summaries and scoped evidence identifiers.
- Any remediation remains a handoff unless a separate approved change skill is active.

## Forbidden Actions

- Do not tail logs outside Gateway or bypass scope with direct Kubernetes access.
- Do not disclose secrets, Authorization headers, registry credentials, or private keys found in logs.
- Do not turn an observability finding into an unapproved mutation.

## Guardrails

- Quote only short non-sensitive log excerpts when necessary.
- Use event IDs, pod names, node names, and execution task IDs for traceability.
- Call out missing metrics or logs as gaps.
- Keep provider payloads and raw secret-looking output out of the final answer.
