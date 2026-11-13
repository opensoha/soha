# soha-skills

This repository owns OpenSoha's official skills, MCP presets, and agent profiles.

Soha's open-source core should consume these assets through published artifacts,
manifests, configured directories, or other explicit integration points. Do not
copy business logic between this repository and `github.com/opensoha/soha`.

## Layout

```text
soha-skills/
  agent-skills/    # Installable agent-facing meta skills, including $soha
  catalog/         # Gateway capability catalog snapshots used for validation
  skills/          # Official Soha skills
  schemas/         # JSON Schemas for repository assets
  tools/           # Validation and index generation scripts
  mcp-presets/     # MCP preset manifests and examples
  agent-profiles/  # Agent profile manifests and examples
```

The initial `skills/` content was copied from `soha/skills` as a low-risk split.

## Boundary

- This repository owns skill and preset source assets.
- `soha` remains the open-source core and runtime integration point.
- Cloud-only behavior belongs outside this repository unless exposed as a
  generic, open extension asset.
- Generated output should be published as versioned artifacts rather than
  committed here by default.

## Validation

Run the same asset gate used by CI before publishing changes:

```bash
python3 tools/validate_assets.py
```

The validator checks agent-facing meta skills, skill YAML front matter, duplicate skill ids, non-empty
capability references, required skill sections and guardrails, schema files,
`skills/index.json` freshness, MCP preset references, agent profile references,
allowed categories, examples, security content, and Gateway capability
references. It also validates `catalog/compatibility-matrix.json` against the
Gateway catalog and `skills/index.json`, then freshness-checks
`catalog/README.md`.

Validation first checks official assets against the public contracts schema
when it is installed, then applies the stricter local schema for official
OpenSoha assets. The preferred source is `node_modules/@opensoha/contracts`;
when public release artifacts are not available in local development, a sibling
`../soha-contracts` checkout is used as the fallback. The compatibility matrix
records the concrete contract schema paths for skill manifests, MCP presets,
and agent profiles:

- `node_modules/@opensoha/contracts/skills/skill-manifest.schema.json`
- `node_modules/@opensoha/contracts/presets/mcp-preset.schema.json`
- `node_modules/@opensoha/contracts/profiles/agent-profile.schema.json`
- `../soha-contracts/skills/skill-manifest.schema.json`
- `../soha-contracts/presets/mcp-preset.schema.json`
- `../soha-contracts/profiles/agent-profile.schema.json`

Local schemas may require extra official-asset fields or narrower values, but
they must not expose fields outside the matching public contract. This keeps the
repository-specific guardrails explicit without forking the cross-repository
asset contracts.

When this repository is checked out next to `soha`, validation also checks
`catalog/gateway-capabilities.json` against
`../soha/internal/application/aigateway/catalog.go`. The catalog is a release
snapshot of Gateway tools, not a replacement for the runtime manifest.

When skill front matter changes, regenerate the index with:

```bash
python3 tools/validate_assets.py --write-index
```

Check release packaging without writing artifacts:

```bash
python3 tools/validate_assets.py --package-dry-run
```

Write and independently verify a release package:

```bash
python3 tools/validate_assets.py --release-version 0.1.3 --package-output-dir dist
python3 tools/validate_assets.py --release-version 0.1.3 --verify-package dist/soha-skills-0.1.3.tar.gz
```

Write a CI/release validation report artifact:

```bash
python3 tools/validate_assets.py --package-dry-run --report-output dist/skills-validation-report.json
```

The report uses `schemas/skills-validation-report.schema.json` and records each
validation check, sibling checkout alignment status, asset counts, Gateway
catalog version, compatibility matrix summary, and package checksum,
manifest checksum, checksum-file checksum, file count, and member count when
package flags are used. CI uploads it as an independent artifact, and tagged
releases attach it next to the installable package.

## Asset Contract

- Skill categories are limited to `delivery`, `platform`, and `security`.
- Every skill must include `Examples`, `Permission Boundaries`, `Forbidden
  Actions`, and `Guardrails`.
- Examples must include an input example and expected Gateway tool calls.
- Security lint requires explicit sensitive-data handling and rejects
  secret-like assignments in skill content.
- `capabilityRefs` must exist in `catalog/gateway-capabilities.json`.
- MCP presets and agent profiles must declare `platformCapabilityRefs` from
  `catalog/platform-capabilities.json` so Direct/Agent support, risk, approval,
  and documentation constraints are explicit.
- `catalog/compatibility-matrix.json` must match the packaged skills version,
  Gateway catalog version, platform capability catalog version, and normalized
  Gateway `requiredScopes` union.
- `catalog/asset-governance.json` records release signing requirements,
  permission review coverage for every skill, MCP preset, and agent profile,
  and the required install audit event schema.

## Compatibility and Rollback

The packaged compatibility entry point is
[`catalog/compatibility-matrix.json`](./catalog/compatibility-matrix.json), with
human-readable policy in [`catalog/README.md`](./catalog/README.md).

The matrix records supported `soha-core`, `soha-cli`, and `soha-agent` version
ranges for the skills package. Install and upgrade flows should validate the
release manifest, checksum, compatibility matrix, and validation report before
activating a wrapper package or raw skills runtime. Rollback uses the previous
verified wrapper package and switches the active `~/.soha/skills` runtime
pointer back to the previous verified directory.

## Release Format

Tagged releases publish:

- `soha-skills-<version>.tar.gz`
- `soha-skills-<version>.tar.gz.sha256`
- `soha-skills-<version>.manifest.json`
- `soha-skills-<version>.validation-report.json`

The tarball contains `agent-profiles/`, `agent-skills/`, `catalog/`,
`mcp-presets/`, `schemas/`, `skills/`, `LICENSE`, `README.md`, and the release manifest under a
`soha-skills/` top-level directory. The manifest lists every packaged file with
its SHA-256 checksum and a stable GitHub release manifest URL.

`--verify-package` checks the sibling `.sha256` file, external manifest,
embedded manifest, package member list, per-file checksums, and embedded
`skills/index.json` version. Tagged releases also download the published GitHub
release assets and run this verification after upload.

Release workflows must also publish GitHub build provenance attestations for
the release tarball, checksum, manifest, and validation report. Installers
should write audit events that match
`schemas/skills-install-audit-event.schema.json` for verify, install, upgrade,
rollback, and activation decisions.

The standalone `soha` CLI is the cross-agent installer for these assets. Use
`soha setup --client <client>` (or the equivalent
`npx -y @opensoha/cli@latest setup ...` bootstrap once that package is
published), and manage the raw runtime
with `soha skill status|update|remove|rollback`. Platform-specific Codex or
Claude plugin packages, if added later, must remain thin distribution adapters
over this canonical release and must not copy the skill source.

## License

This repository is licensed under the Apache License 2.0. See
[LICENSE](./LICENSE) for the full license text.
