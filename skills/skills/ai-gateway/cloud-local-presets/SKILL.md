---
id: cloud-local-presets
name: Cloud Local Presets
version: 0.1.0
category: platform
description: Choose and validate cloud or local Soha MCP presets and profile scope before use.
capabilityRefs:
  - delivery.applications.list
  - delivery.workflow_templates.list
  - delivery.execution_tasks.list
  - k8s.pods.list
  - k8s.events.list
requiredScopes:
  - businessLine
  - application
  - environment
  - cluster
  - namespace
---

# Cloud Local Presets

Use this skill when an AI assistant is helping choose between local self-hosted, cloud-compatible, read-only, or governance-heavy Soha MCP presets.

## Operating Contract

- Treat preset selection as scope and capability planning, not as permission escalation.
- Check visible Gateway evidence before recommending a local or cloud profile.
- Keep target server, profile name, business line, application, environment, cluster, namespace, and approval expectation explicit.
- Never ask for access tokens, passwords, kubeconfig, private keys, or raw secret values.

## Workflow

1. Confirm whether the user is targeting local self-hosted Soha, Soha Cloud compatible endpoint, or both.
2. List application and platform evidence needed by the requested workflow.
3. Check workflow approval nodes and recent execution task context when a preset may trigger governed actions.
4. Choose the narrowest preset that covers the requested tools and scopes.
5. Explain unavailable capabilities, unsupported agent-mode paths, and required approvals.
6. Return the preset/profile recommendation with scope assumptions and validation commands.

## Examples

### Input Example

Pick a safe local preset for read-only Kubernetes diagnosis and a cloud preset for governed delivery changes.

### Expected Tool Calls

- `delivery.applications.list`
- `delivery.workflow_templates.list`
- `delivery.execution_tasks.list`
- `k8s.pods.list`
- `k8s.events.list`

## Permission Boundaries

- Read capability and scope evidence for the declared application, environment, cluster, and namespace.
- Preset selection cannot grant additional RBAC, ABAC, tool grants, or secrets.
- Governed cloud actions require approval and audit linkage before execution.

## Forbidden Actions

- Do not write local MCP config, change cloud tenant settings, or install skills unless the user explicitly asks.
- Do not request or expose tokens, passwords, kubeconfig, private keys, or secret refs.
- Do not recommend a broad preset when a narrower read-only preset covers the task.

## Guardrails

- Prefer read-only presets for diagnosis.
- Call out required approvals and missing scopes.
- Keep local and cloud assumptions separate.
- Include validation commands without leaking credentials.
