---
name: soha-connectors
description: Change or review the connector SDK, provider runtimes, webhooks, manifests, and releases in Root connectors/.
---

# Soha Connectors

## Purpose

Use Root `connectors/` with [Root rules](../../../AGENTS.md) and
[component rules](../../../connectors/AGENTS.md). Paths below are relative to this component.
Keep its independently packaged runtimes contract-driven and safe at webhook
and outbound provider boundaries.

## Workflow

1. Inspect the affected connector and its SDK callers. Read its manifest and capability
   matrix for capability changes, and the operations runbook for operational behavior.
2. Put reusable lifecycle, HTTP, reliability, logging, and redaction behavior
   in `src/sdk`; keep provider behavior in `src/connectors/<provider>`.
3. Keep `connectors/<provider>/connector.manifest.json`,
   `connectors/index.json`, and `connectors/capability-matrix.json` aligned
   with implemented code and evidence.
4. Change the canonical public event or manifest contract in
   `../contracts` first, then update the local compatibility copy.
5. Add boundary tests for signatures, replay/idempotency, retries, rate limits,
   redaction, event normalization, and action errors as applicable.

## Rules

- Preserve the lifecycle `created -> configured -> started -> stopped`.
- Validate configuration and secret references without leaking secret values.
- Verify inbound signatures before processing; bound bodies, timeouts, retry
  queues, dead-letter storage, and outbound concurrency.
- Use built-in Node capabilities and existing dependencies before adding a
  runtime package.
- Do not import `soha/internal/**`. Integrate through contracts, HTTP, events,
  MCP, manifests, or released artifacts.
- Hosted fleet operations, billing, quotas, and managed connector lifecycle
  belong in `soha-cloud`.

## Verification

From Root `soha/`, after Root `npm ci`:

```bash
npm run check --workspace connectors
```

Run package and release artifact checks only when exports, manifests, or
release contents change.

Use [active Connectors CI](../../../.github/workflows/connectors-ci.yml) and
[Connectors release](../../../.github/workflows/connectors-release.yml). Historical component workflows remain in Git history; active CI and release workflows are in Root; package exports, provider lifecycle and release
installation remain independent from Core implementation and source checks.
