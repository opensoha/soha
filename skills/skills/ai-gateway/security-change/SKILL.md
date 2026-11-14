---
id: security-change
name: Security Change
version: 0.1.0
category: security
description: Security-sensitive operational change planning through Soha AI Gateway.
capabilityRefs:
  - delivery.actions.trigger
  - delivery.workflow_templates.list
  - delivery.rollback.context
  - delivery.execution_tasks.list
  - k8s.events.list
requiredScopes:
  - application
  - environment
  - cluster
  - namespace
---

# Security Change

Use this skill when an AI assistant is helping plan, review, or hand off a security-sensitive operational change in soha.

## Operating Contract

- This skill is a control checklist, not a permission grant.
- High-risk actions must remain behind Gateway risk policy, Gateway approval guardrail, and the owning domain service.
- Prefer change plans, rollback criteria, evidence collection, and approval handoffs over direct execution.
- Treat credentials, security policy, network exposure, registry references, and production deploys as sensitive.

## Workflow

1. Identify the asset, owner, environment, cluster, namespace, and business impact.
2. Classify the change as read-only, mutate, execute, or high risk.
3. Verify that the Gateway manifest exposes only the tools required for the stated change.
4. Draft the change plan with expected result, rollback signal, rollback owner, and audit reason.
5. Collect pre-change evidence through read-only tools.
6. If execution is required, stop for explicit human confirmation and use only the approved Gateway action.
7. Collect post-change evidence and compare it with the pre-change baseline.

## Examples

### Input Example

User asks: "Plan a production rollback for payments API after a suspected security regression, but do not execute it yet."

### Expected Tool Calls

1. `delivery.workflow_templates.list` to identify permitted workflow paths and approval nodes.
2. `delivery.rollback.context` to gather rollback candidates and signals.
3. `delivery.execution_tasks.list` to collect recent execution evidence.
4. `k8s.events.list` for bounded pre-change runtime evidence when cluster and namespace are in scope.
5. `delivery.actions.trigger` only after explicit human confirmation and only when the Gateway approval path allows execution.

## Permission Boundaries

- Requires Gateway-visible delivery governance and scoped runtime evidence for `application`, `environment`, `cluster`, and `namespace` scopes.
- Keeps execution behind Soha Gateway risk policy, approval guardrail, audit, and durable task handling.
- Uses read-only evidence first and treats approval-required responses as a stop point.

## Forbidden Actions

- Do not request or reveal access token, refresh token, kubeconfig, password, private key, environment secret, or registry credential values.
- Do not downgrade approval, scope, risk, or audit controls.
- Do not split high-risk production changes into smaller calls to bypass approval or confirmation.

## Guardrails

- Do not request or reveal tokens, kubeconfig, private keys, passwords, environment secrets, or registry credentials.
- Do not downgrade approval, scope, or risk controls.
- Do not split one high-risk action into smaller tool calls to avoid approval.
- If the user asks for an unsafe bypass, refuse the bypass and provide the closest auditable soha workflow.
