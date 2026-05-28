---
id: delivery-developer
name: Delivery Developer
category: delivery
capabilityRefs:
  - delivery.applications.list
  - delivery.applications.create
  - delivery.application_environments.list
  - delivery.actions.trigger
requiredScopes:
  - businessLine
  - application
  - environment
---

# Delivery Developer

Use this skill when an AI coding tool is helping a developer onboard an application or trigger self-service build, deploy, build_deploy, workflow, or verify actions through soha AI Gateway.

## Operating Contract

- Treat soha as the source of truth for applications, environments, release bundles, execution tasks, approvals, and audit.
- Use Gateway tools only through the MCP tool list returned by the current identity.
- Never call Kubernetes, PostgreSQL, Docker, CI runners, or deployment targets directly.
- Keep build and deploy actions inside `delivery.actions.trigger`; do not synthesize runner callbacks.
- Preserve application, business line, environment, branch, commit, release bundle, and execution task identifiers in the final answer.

## Workflow

1. Read capabilities and confirm the required delivery tools are visible.
2. List matching applications before creating a new one.
3. If creating an application, ask for or infer only non-secret metadata: name, key, business line, owner, description, and tags.
4. Query application environment bindings before triggering any action.
5. For build or deploy actions, include the target application environment, build source, branch or commit, and a short reason.
6. After triggering, read release bundle and execution task status, logs, and artifacts when those tools are available.
7. Return a compact handoff with status, IDs, links if present, and the next safe manual step.

## Guardrails

- Do not include access tokens, refresh tokens, kubeconfig, registry credentials, environment variables, or runner secrets in tool arguments or output.
- Do not trigger production deploys unless the user explicitly names the production environment and confirms intent.
- If Gateway reports approval is required, stop at the approval handoff and do not retry as a different action.
- If a capability is missing, explain the missing permission or scope instead of inventing a workaround.
