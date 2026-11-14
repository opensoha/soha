---
id: knowledge-connector-operator
name: Knowledge Connector Operator
version: 0.1.0
category: platform
description: Operate governed external knowledge connectors and bounded synchronization jobs through public Soha APIs.
capabilityRefs:
  - gateway.manifest.read
  - knowledge.search
permissionKeys:
  - ai.knowledge.connectors.view
  - ai.knowledge.connectors.manage
  - ai.knowledge.ingestion.operate
requiredScopes:
  - aiClient
  - skill
metadata:
  httpCapabilityRefs:
    - knowledge.connectors.list
    - knowledge.connectors.create
    - knowledge.connectors.validate
    - knowledge.sync.start
    - knowledge.sync.status
    - knowledge.sync.cancel
    - knowledge.sync.retry
---

# Knowledge Connector Operator

## Operating Contract

- Use `soha knowledge connectors` and `soha knowledge sync` against protected public APIs.
- Treat every `configRef` and checkpoint cursor as opaque; never resolve or print a secret or credential.
- Apply an availability gate: baseline-approved HTTP capabilities must exist in the target release before use.

## Workflow

1. Confirm connector ID, kind, version, allowed hosts, path prefixes, base, source, and requested action.
2. List existing connector and synchronization state before mutation.
3. Validate SSRF boundaries and the opaque secret reference before creating or validating a connector.
4. Obtain explicit approval before create, validate, sync, cancel, or retry actions.
5. Follow the returned operation or job ID to a terminal state.
6. Run a bounded `knowledge.search` check and retain citation and trace IDs.

## Examples

### Input Example

Validate the `platform-handbook-git` connector, synchronize its source, and verify retrieval.

### Expected Tool Calls

- `gateway.manifest.read`
- Protected connector and sync HTTP capabilities listed in `metadata.httpCapabilityRefs`
- `knowledge.search`

## Permission Boundaries

- Connector reads require `ai.knowledge.connectors.view`; mutations require explicit manage or ingestion permission.
- Do not broaden tenant, workspace, Knowledge Base, source, or AI client scope.
- Keep every access token, password, private key, environment variable, and resolved secret value out of output.

## Forbidden Actions

- Do not submit inline credentials, arbitrary hosts, arbitrary local paths, or executable connector payloads.
- Do not retry a failed job without inspecting its bounded error code and current terminal state.
- Do not claim success from an accepted request; require terminal job and retrieval evidence.

## Guardrails

- Stop on missing runtime capability, authorization failure, invalid secret reference, or scope mismatch.
- Preserve connector, source, job, revision, trace, and audit IDs.
- Redact sensitive error text before reporting it.
