---
id: security-change
name: Security Change
category: security
capabilityRefs:
  - delivery.actions.trigger
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
- High-risk actions must remain behind Gateway risk policy, approval policy, and the owning domain service.
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

## Guardrails

- Do not request or reveal tokens, kubeconfig, private keys, passwords, environment secrets, or registry credentials.
- Do not downgrade approval, scope, or risk controls.
- Do not split one high-risk action into smaller tool calls to avoid approval.
- If the user asks for an unsafe bypass, refuse the bypass and provide the closest auditable soha workflow.
