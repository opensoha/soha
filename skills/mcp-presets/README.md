# MCP Presets

This directory is reserved for official OpenSoha MCP preset manifests and
examples.

Presets should be portable assets that can be consumed by `soha`, `soha-agent`,
or Soha Cloud through explicit configuration or published artifacts.

Current presets:

- `k8s-readonly.yaml`: read-only Kubernetes diagnosis tools aligned with the
  `k8s-sre` skill, the OpenSoha default marketplace reference, and the
  platform capability matrix entries that are safe for read-only diagnosis in
  Direct and Agent modes.
- `local-runtime-diagnosis.yaml`: local self-hosted runtime diagnosis preset
  for `k8s-sre`, `agent-runtime`, `observability`, and `cloud-local-presets`
  skills.
- `cloud-governance.yaml`: cloud-compatible governance preset for onboarding,
  incident handoff, plugin administration, security-change planning, and
  approved delivery actions.
- `local-environment-provisioning.yaml`: approval-bound VM, Docker Compose, and
  Kubernetes demo provisioning. Credential-bearing calls pass only canonical
  references through the reserved `_sohaSecretRefs` MCP argument; the adapter
  promotes them to top-level invocation `secretRefs`, and remote agents receive
  only a short-lived one-time lease.
