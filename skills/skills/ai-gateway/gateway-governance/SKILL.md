---
id: gateway-governance
name: Gateway Governance
version: 0.1.0
category: security
description: Read-only review of Soha AI Gateway clients, policies, tools, approvals, and audit evidence.
capabilityRefs:
  - gateway.manifest.read
  - gateway.governance.status
  - gateway.audit_logs.list
requiredScopes:
  - aiClient
  - skill
  - policy
  - tool
  - audit
---

# Gateway Governance

Use this skill to inspect Soha AI Gateway exposure, policy state, and audit evidence without changing governance configuration.

## Operating Contract

- Treat the live Gateway manifest and governance status as the source of truth.
- Start with read-only inventory and preserve client, policy, tool, approval, and audit identifiers in findings.
- Keep secrets, token values, credentials, and private configuration out of requests and summaries.
- Require a separate approved workflow for every governance mutation or approval decision.

## Workflow

1. Confirm the AI client, policy, tool, and time range in scope.
2. Read the Gateway manifest to establish the exposed capability surface.
3. Read governance status and compare enabled clients, policies, and tools with the requested scope.
4. Query bounded audit evidence for discrepancies or recent changes.
5. Report missing permissions, unbound tools, policy drift, and unsupported capabilities without guessing.

## Examples

### Input Example

Review whether AI client `operations-console` can invoke only its approved tools and show the recent governance evidence.

### Expected Tool Calls

- `gateway.manifest.read`
- `gateway.governance.status`
- `gateway.audit_logs.list`

## Permission Boundaries

- Requires scoped access to the AI client, skill, policy, tool, and audit evidence being reviewed.
- Uses only read-only Gateway tools; this skill does not grant approval or mutation authority.
- Stops when the Gateway returns an approval requirement or access denial.

## Forbidden Actions

- Do not create, rotate, reveal, or print token values, credentials, private keys, passwords, or secrets.
- Do not modify clients, grants, policies, bindings, approvals, relay upstreams, or model routes.
- Do not infer effective access when manifest, policy, or audit evidence is unavailable.

## Guardrails

- Redact secret-like values from audit summaries.
- Keep every conclusion tied to live Gateway evidence and explicit identifiers.
- Mark unavailable or unimplemented capabilities as unsupported.
