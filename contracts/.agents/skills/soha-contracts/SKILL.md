---
name: soha-contracts
description: Change or review shared OpenSoha API schemas, protocol contracts, generated SDKs, compatibility, and releases. Consumer-only UI changes do not require contract edits.
---

# Soha Contracts

## Purpose

Keep `soha-contracts` the versioned source of truth for behavior shared by
Soha repositories. Change contracts before consumers and preserve stable SDK
import paths.

## Workflow

1. Read `AGENTS.md`, `README.md`, `COMPATIBILITY.md`, the affected schema, its
   fixtures and the generator or compatibility check before implementation.
   Documentation-only work reads the relevant text and source; reuse unchanged context.
2. Decide whether the source is `openapi/soha-api.yaml`, a focused JSON
   Schema, or a small hand-maintained Go helper package. Do not model private
   implementation details as public contracts.
3. Add or update valid and invalid fixtures for boundary behavior. Keep examples
   representative and free of secrets. Identify permission/scope, lifecycle,
   side effects, errors, pagination/count semantics and affected consumers;
   structural compatibility alone does not establish behavioral compatibility.
4. Run `npm run generate` when OpenAPI source changes affect generated artifacts. Review generated diffs;
   never patch generated DTOs to bypass the source schema.
5. Stabilize the source contract and generated artifacts and pass the applicable contract checks
   before updating consumers. Independent consumers may then proceed in parallel with explicit
   file and test-resource ownership; dependent steps remain ordered. Verify the final candidate
   combination with the complete consumer matrix; parallel implementation does not waive it.
6. For public contract changes, run focused compatibility checks, `npm test`, and
   the complete consumer matrix defined by `.github/workflows/ci.yml` and the
   consumer checker. Collaboration prose changes do not regenerate SDKs or run consumers.

## Ownership Rules

- `gen/ts/sohaapi/index.ts` and generated Go DTOs come from OpenAPI.
- `gen/go/sohaapi/client.go` is the deliberate hand-maintained exception:
  keep its supported method surface stable and cover additions with client tests.
- Keep optional additions backward compatible during `0.1.x`. Do not remove
  operations, statuses, properties, or enum values, and do not add required
  request fields without an explicit version decision.
- Update `compat/**` only for an intentional reviewed baseline change, never
  to silence a failure. A proposed revision pin is not evidence of a passing matrix.
- Cloud-only tenancy, billing, quota, or SaaS operations stay in
  `soha-cloud` unless they are genuine public extension points.
- Consumers use released artifacts, generated SDKs, HTTP, or schemas. They must
  not copy contract definitions or import sibling internals.

## Consumer Revision Evidence

- `compat/consumer-revisions.json` owns fixed consumer source revisions. Keep
  existing reviewed pins unless an explicit revision update is part of the task.
- `scripts/select-consumer-revision.mjs` requires a full commit for each selected
  consumer; missing entries never fall back to `main`. Test selector changes with
  `node --test scripts/select-consumer-revision.checks.mjs`.
- Normal CI uses pinned revisions. Manual `consumer_mode=latest` exercises current
  branches separately; it never rewrites the fixed manifest. Record actual contracts
  and consumer SHAs from the run summary, not only a requested ref or package version.
- Adding a consumer requires the real checker support, workflow matrix entry and
  reviewed pin together. Do not reduce the matrix or hide missing consumers to pass.
- Mock, compile, runtime, package and release evidence remain distinct; skipped or
  unavailable checks must be reported. Cross-repository verification is not deployment.

## Verification

```bash
npm test
GOWORK=off go vet ./...
GOWORK=off go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
npm run check:consumers -- --require-all
git diff --check
```

Use the current `scripts/check-consumers.mjs` and CI matrix for supported consumer
names and options; a selected-consumer run does not replace the full required matrix.
`npm test` includes generation, schema, fixture, compatibility, package, and Go checks;
CI also runs the existing lint/vet/vulnerability gates. Tool versions are owned by
package scripts and workflows. Run release artifact checks when package or release
behavior changes; do not report unavailable external tools as passed.
