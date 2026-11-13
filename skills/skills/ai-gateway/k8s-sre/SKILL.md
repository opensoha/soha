---
id: k8s-sre
name: K8s SRE
version: 0.1.0
category: platform
description: Read-only Kubernetes diagnosis through Soha AI Gateway.
capabilityRefs:
  - k8s.namespaces.list
  - k8s.workloads.overview
  - k8s.configmaps.list
  - k8s.secrets.metadata
  - k8s.helm.releases.list
  - k8s.pods.list
  - k8s.pods.logs
  - k8s.pods.describe
  - k8s.deployments.list
  - k8s.deployments.rollout_status
  - k8s.deployments.events
  - k8s.services.list
  - k8s.services.backends
  - k8s.routes.context
  - k8s.storage.context
  - k8s.nodes.detail
  - k8s.events.list
requiredScopes:
  - cluster
  - namespace
---

# K8s SRE

Use this skill when an AI assistant is helping SREs perform read-only Kubernetes diagnosis through soha AI Gateway.

## Operating Contract

- Stay read-only unless the user switches to a separate approved change skill and the Gateway manifest exposes a mutation tool.
- Use soha platform view-model tools; do not ask for kubeconfig or run `kubectl` locally.
- Keep cluster and namespace scope explicit in every tool call and every conclusion.
- Prefer backend aggregated evidence over repeated namespace fan-out.
- Treat custom-resource access as API-group-specific RBAC and Prometheus access as control-plane reachability; Agent mode does not remove either boundary.

## Workflow

1. Discover the live Gateway manifest and select `k8s-sre`; with the CLI, use `soha capabilities --output inputs` and `soha diagnose --tool <name> --resource soha://k8s/runtime` before relying on an unfamiliar tool.
2. Confirm cluster, namespace, workload kind, workload name, and time window.
3. Start with namespace and workload overviews, then read rollout status, deployment events, pod describe context, service backends, route context, storage context, node detail, and recent logs using visible Gateway tools.
4. Correlate events and logs by workload, pod, container, restart count, image, service selector, route backend, PVC binding, node condition, and timestamp.
5. Treat `capabilityWarnings` as explicit evidence of an unavailable optional API family, not as a successful empty result.
6. Separate confirmed evidence from hypotheses.
7. Produce a short RCA draft with likely cause, blast radius, confidence, missing evidence, and safe next checks.
8. If a release is involved, reference the related application, release bundle, and execution task IDs when available.
9. For deeper release-failure reasoning, call `diagnosis.release_failure.analyze` with `deepAnalysis=true` and an external `agentProviderId` only after collecting the bounded context; treat the returned `agentRunId` as queued Agent Runtime work until a runner callback writes artifacts.

## Examples

### Input Example

User asks: "In cluster `prod-cn`, namespace `payments`, why is deployment `api` failing rollout?"

### Expected Tool Calls

1. `k8s.deployments.rollout_status` for `prod-cn`, `payments`, and deployment `api`.
2. `k8s.workloads.overview` and `k8s.deployments.events` for health and recent rollout evidence.
3. `k8s.pods.list` and `k8s.pods.describe` for affected pods.
4. `k8s.pods.logs` with bounded `tailLines` or `sinceSeconds`.
5. `k8s.configmaps.list`, `k8s.secrets.metadata`, or `k8s.helm.releases.list` when configuration or release metadata is relevant; never request their content or values.
6. `k8s.services.backends`, `k8s.routes.context`, and `k8s.storage.context` when network or storage symptoms appear.
7. `k8s.nodes.detail` and `k8s.events.list` when scheduling, pressure, or node condition evidence is needed.

## Permission Boundaries

- Requires Gateway-visible read-only Kubernetes capabilities for `cluster` and `namespace` scopes.
- Reads only scoped status, metadata, event, route, storage, node, and log evidence exposed by the Gateway manifest.
- Treats missing capabilities, agent parity gaps, and `capabilityWarnings` as explicit evidence limitations.

## Forbidden Actions

- Do not request or use access token, refresh token, kubeconfig, password, private key, or credential material.
- Do not run shell commands, `kubectl`, exec, port-forward, patch, delete, restart, scale, roll back, or drain resources.
- Do not reveal secret-looking values from logs, annotations, environment variables, or command output.

## Guardrails

- Do not execute shell commands in containers.
- Do not patch, delete, restart, scale, roll back, or drain resources from this skill.
- Do not expose secret-looking values from logs, annotations, environment variables, or command output.
- If an agent-connected cluster lacks parity for a tool, state that the Gateway capability is unavailable instead of implying live-cluster access.
