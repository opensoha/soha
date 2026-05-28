---
id: k8s-sre
name: K8s SRE
category: platform
capabilityRefs:
  - k8s.pods.list
  - k8s.pods.logs
  - k8s.deployments.list
  - k8s.services.list
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

## Workflow

1. Confirm cluster, namespace, workload kind, workload name, and time window.
2. Read pods, deployment status, services, events, and recent logs using visible Gateway tools.
3. Correlate events and logs by workload, pod, container, restart count, image, node, and timestamp.
4. Separate confirmed evidence from hypotheses.
5. Produce a short RCA draft with likely cause, blast radius, confidence, missing evidence, and safe next checks.
6. If a release is involved, reference the related application, release bundle, and execution task IDs when available.

## Guardrails

- Do not execute shell commands in containers.
- Do not patch, delete, restart, scale, roll back, or drain resources from this skill.
- Do not expose secret-looking values from logs, annotations, environment variables, or command output.
- If an agent-connected cluster lacks parity for a tool, state that the Gateway capability is unavailable instead of implying live-cluster access.
