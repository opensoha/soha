# Agent Profiles

This directory is reserved for official OpenSoha agent profile manifests and
examples.

Profiles should describe reusable runtime behavior and should not depend on
private Soha Cloud implementation details.

Current profiles:

- `k8s-sre-readonly.yaml`: read-only Kubernetes SRE profile using the
  `k8s-readonly` MCP preset and `k8s-sre` skill, with explicit platform
  capability refs for Direct/Agent support diagnostics.
- `local-agent-runtime.yaml`: local self-hosted Agent Runtime and observability
  profile using the `local-runtime-diagnosis` preset.
- `cloud-governance-admin.yaml`: cloud-compatible governance profile using the
  `cloud-governance` preset for onboarding, incident handoff, plugin
  administration, and approved delivery actions.
