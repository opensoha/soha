---
id: memory-privacy-curator
name: Memory Privacy Curator
version: 0.1.0
category: security
description: Inspect governed AI memory provenance and execute auditable privacy deletion.
capabilityRefs:
  - gateway.manifest.read
permissionKeys:
  - ai.memory.view
  - ai.memory.manage
requiredScopes:
  - aiClient
  - skill
metadata:
  httpCapabilityRefs:
    - memory.inspect
    - memory.delete
---

# Memory Privacy Curator

## Operating Contract

- Inspect memory through protected filtered APIs and delete only by immutable record ID.
- Treat consent, purpose, provenance, confidence, TTL, ACL, and deletion propagation as required evidence.
- Apply an availability gate before using baseline-approved memory HTTP capabilities.

## Workflow

1. Confirm the subject, request authority, purpose, record IDs, and expected deletion scope.
2. Inspect bounded memory metadata and provenance without echoing sensitive content unnecessarily.
3. Identify derived Context, cache, graph, and evaluation references that require propagation.
4. Obtain explicit approval before deletion.
5. Delete the explicit record and retain the returned audit or operation evidence.
6. Verify the record and affected derived views fail closed after propagation.

## Examples

### Input Example

Inspect memory records for `user-1` and delete `memory-1` under an approved privacy request.

### Expected Tool Calls

- `gateway.manifest.read`
- Protected memory inspect and delete HTTP capabilities listed in metadata

## Permission Boundaries

- Inspection requires `ai.memory.view`; deletion requires `ai.memory.manage` and explicit approval.
- Do not broaden the declared subject, tenant, workspace, or AI client scope.
- Redact every access token, password, private key, credential, environment variable, and unnecessary memory content.

## Forbidden Actions

- Do not delete by broad query, inferred identity, or unverified subject relationship.
- Do not convert model inference into durable memory without consent and provenance.
- Do not claim deletion complete before propagation evidence reaches terminal state.

## Guardrails

- Stop on ambiguous identity, absent consent/provenance, scope mismatch, or unavailable delete capability.
- Preserve record, subject, source, operation, propagation, and audit IDs.
- Minimize quoted content and redact sensitive values.
