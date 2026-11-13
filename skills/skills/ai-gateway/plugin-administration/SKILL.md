---
id: plugin-administration
name: Plugin Administration
version: 0.1.0
category: security
description: Plugin installation and upgrade governance planning through existing Soha Gateway evidence.
capabilityRefs:
  - delivery.workflow_templates.list
  - delivery.execution_tasks.list
  - delivery.actions.trigger
  - delivery.rollback.context
requiredScopes:
  - application
  - environment
  - executionTask
  - releaseBundle
---

# Plugin Administration

Use this skill when an AI assistant is helping an operator plan, review, or hand off plugin installation, enablement, upgrade, disablement, or rollback through Soha-controlled governance.

## Operating Contract

- Treat plugin administration as a governed change, not as direct file or database editing.
- Use Gateway evidence for workflow approval nodes, workflow template, execution task, and rollback context.
- Keep plugin id, version, source, checksum, application, environment, execution task, workflow approval gate, and rollback owner explicit.
- Do not bypass the plugin marketplace, RBAC, workflow approval gate, or audit path.

## Workflow

1. Confirm plugin id, target environment, source, expected version, checksum, and owner.
2. List workflow templates and approval nodes that govern the plugin change.
3. Inspect recent execution tasks for similar plugin operations or failed rollouts.
4. Review rollback context before recommending enablement or upgrade.
5. If execution is requested, require explicit approval and use only the approved delivery action path.
6. Produce an administration plan with prechecks, approval gates, audit fields, rollout steps, rollback steps, and unresolved risks.

## Examples

### Input Example

Prepare an upgrade plan for plugin `opensoha.k8s-sre-pack` in staging and include rollback criteria.

### Expected Tool Calls

- `delivery.workflow_templates.list`
- `delivery.execution_tasks.list`
- `delivery.rollback.context`
- `delivery.actions.trigger`

## Permission Boundaries

- Read governance and execution evidence for the declared application and environment.
- `delivery.actions.trigger` is allowed only after explicit user confirmation and visible workflow approval context.
- Plugin source, checksum, and manifest review must be recorded before execution.

## Forbidden Actions

- Do not edit plugin files, database rows, marketplace records, or runtime configuration directly.
- Do not print secret refs, tokens, passwords, private keys, or registry credentials.
- Do not enable or upgrade a plugin without human confirmation and audit reason.

## Guardrails

- Include plugin source and checksum in the final plan when available.
- Separate planning, approval, execution, verification, and rollback sections.
- Treat missing workflow approval context as a blocker for governed changes.
- Preserve audit context for every recommended action.
