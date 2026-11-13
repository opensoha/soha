---
id: compute-sre
name: Compute SRE
version: 0.1.0
category: platform
description: Read-only virtualization and container-runtime diagnosis through Soha Compute Gateway tools.
capabilityRefs:
  - compute.overview.read
  - compute.resources.read
  - compute.resource_relations.list
  - compute.tasks.list
  - compute.tasks.get
  - compute.task_logs.list
requiredScopes:
  - computeDomain
  - computeResource
  - computeTask
---

# Compute SRE

Use this skill when an AI assistant is investigating virtual machines, provider connections, Docker runtime hosts, projects, services, or their durable tasks through Soha.

## Operating Contract

- Stay read-only and use only tools visible in the live Gateway manifest.
- Let the Compute service enforce the caller's virtualization and Docker permissions; do not infer visibility from a missing item.
- Start with the overview, then move to an exact resource, its relations, related tasks, and redacted task logs.
- Keep provider-native fields secondary to normalized status, resource references, task state, and verification evidence.

## Workflow

1. Call `compute.overview.read` and identify degraded domains, attention items, or failed tasks.
2. Call `compute.resources.read` for an exact domain, resource kind, and id.
3. Call `compute.resource_relations.list` to establish provider, host, VM, project, and service impact.
4. Use `compute.tasks.list` with exact resource filters, then `compute.tasks.get` for the relevant durable task.
5. Read bounded evidence with `compute.task_logs.list`; treat redaction markers as protected data, not missing telemetry.
6. Report confirmed symptoms, affected resource ids, task ids, likely cause, confidence, and the next safe read-only check.

## Examples

### Input Example

User asks: "Why is Docker runtime host `runtime-1` unavailable after VM provisioning?"

### Expected Tool Calls

1. `compute.overview.read`.
2. `compute.resources.read` for `container_runtime`, `runtime_host`, and `runtime-1`.
3. `compute.resource_relations.list` for the same resource.
4. `compute.tasks.list` filtered to the runtime host, followed by `compute.tasks.get`.
5. `compute.task_logs.list` for the selected task.

## Permission Boundaries

- Requires `ai.gateway.invoke` plus the underlying Compute resource permissions enforced by Soha.
- Reads only normalized, permission-filtered resource, relation, task, verification, and redacted log evidence.
- Missing provider or domain data must be reported as unavailable or unauthorized, never assumed healthy.

## Forbidden Actions

- Do not create, start, stop, resize, retry, cancel, delete, deploy, or mutate compute resources from this skill.
- Do not request provider credentials, Docker tokens, kubeconfig, SSH keys, passwords, or raw secret values.
- Do not bypass Soha with provider CLIs, Docker socket access, SSH, or hypervisor APIs.

## Guardrails

- Keep every conclusion tied to resource ids, task ids, timestamps, and returned verification state.
- Do not expose secret-looking values from task logs or provider payloads.
- If a required read tool is absent, state the evidence limitation and stop before proposing a mutation.
