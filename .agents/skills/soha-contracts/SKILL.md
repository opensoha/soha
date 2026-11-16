---
name: soha-contracts
description: Change or review public API schemas, generated SDKs, compatibility, and releases in Root contracts/. Consumer-only UI changes do not require contract edits.
---

# Soha Contracts

## Purpose

Use Root `contracts/` as the versioned source of truth shared by Root components
and external consumers. Read [Root rules](../../../AGENTS.md) and
[component rules](../../../contracts/AGENTS.md). Code paths below are relative to `contracts/`;
commands explicitly identify Root or component cwd. Preserve the independent
`github.com/opensoha/soha/contracts` Go module, generated SDKs and public package identity.

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
4. Run Root `npm run generate --workspace contracts` when OpenAPI source changes affect generated artifacts. Review generated diffs;
   never patch generated DTOs to bypass the source schema.
5. Stabilize the source contract and generated artifacts and pass the applicable contract checks
   before updating consumers. Independent consumers may then proceed in parallel with explicit
   file and test-resource ownership; dependent steps remain ordered. Verify the final candidate
   combination with the complete consumer matrix; parallel implementation does not waive it.
6. For public contract changes, run focused compatibility checks, Root `npm test --workspace contracts`, and
   the complete consumer matrix defined by [active Contracts CI](../../../.github/workflows/contracts-ci.yml)
   and the consumer checker. Collaboration prose changes do not regenerate SDKs or run consumers.

## Ownership Rules

- `gen/ts/sohaapi/index.ts` and generated Go DTOs come from OpenAPI.
- `gen/go/sohaapi/client.go` is the deliberate hand-maintained exception:
  keep its supported method surface stable and cover additions with client tests.
- Keep optional additions backward compatible during `0.1.x` and `0.2.x`. Do not remove
  operations, statuses, properties, or enum values, and do not add required
  request fields without an explicit version decision.
- Update `compat/**` only for an intentional reviewed baseline change, never
  to silence a failure. A proposed revision pin is not evidence of a passing matrix.
- Cloud-only tenancy, billing, quota, or SaaS operations stay in
  `soha-cloud` unless they are genuine public extension points.
- Root components use the same-commit SDK through Root workspaces; external consumers
  use their declared SDK, HTTP, schemas or released artifacts. Neither mode permits copying
  contract definitions or importing another component's private internals.

## Consumer Revision Evidence

- `compat/consumer-revisions.json` owns fixed consumer source revisions. Keep
  existing reviewed pins unless an explicit revision update is part of the task.
- `scripts/select-consumer-revision.mjs` requires a full commit for each selected
  consumer; missing entries never fall back to `main`. Test selector changes with
  `node --test scripts/select-consumer-revision.checks.mjs`.
- Root CI uses pinned revisions fetched from the canonical Root repository; App
  remains an external repository. Historical consumer IDs in the matrix are stable
  compatibility labels. A latest-branch qualification needs explicit checkout/layout
  selection and its own evidence; do not pass unsupported options to the current
  workflow or rewrite the fixed manifest. Record actual source SHAs, not only ref names.
- Adding a consumer requires the real checker support, workflow matrix entry and
  reviewed pin together. Do not reduce the matrix or hide missing consumers to pass.
- Mock, compile, runtime, package and release evidence remain distinct; skipped or
  unavailable checks must be reported. Compatibility verification is not deployment.

## Verification

From Root `soha/`:

```bash
npm test --workspace contracts
(cd contracts && GOWORK=off go vet ./...)
(cd contracts && GOWORK=off go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...)
npm run check:internal-consumers
git diff --check
```

The external matrix additionally uses the isolated checkouts and both declared/candidate
modes from active Contracts CI. Go declared mode uses `--declared --archived-declared-sdk`
for the preserved legacy SDK; original module paths, checksums and pins stay unchanged.
Never substitute same-source success for that matrix.

Use the current `scripts/check-consumers.mjs` and CI matrix for supported consumer
names and options; a selected-consumer run does not replace the full required matrix.
The Contracts `npm test` includes generation, schema, fixture, compatibility, package, and Go checks;
CI also runs the existing lint/vet/vulnerability gates. Tool versions are owned by
package scripts and workflows. Run release artifact checks when package or release
behavior changes; do not report unavailable external tools as passed.
