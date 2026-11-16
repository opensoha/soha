---
name: soha-skills
description: Change or review runtime skills, agent-facing assets, presets, and catalogs in Root skills/. Development collaboration skills use skill-creator.
---

# Soha Skills Catalog

## Purpose

Maintain the Root `skills/` component's runtime catalog, security and compatibility
evidence. Read [Root rules](../../../../AGENTS.md) and [component rules](../../../AGENTS.md).
Paths below are relative to this component: `skills/**` are governed runtime assets,
`agent-skills/**` are installable user-agent skills, and `.agents/skills` holds
development instructions. Do not mix component development skills into product assets.

## Workflow

1. Read the affected asset, its local schema, `catalog/README.md`,
   `catalog/compatibility-matrix.json`, and `tools/validate_assets.py`.
2. If a public manifest field changes, update `../contracts` first.
   Local schemas may be stricter but may not fork the public contract.
3. Keep capability references backed by the Gateway, platform, or AI platform
   catalog and by real runtime evidence.
4. When published runtime skill front matter changes, regenerate `skills/index.json` with
   `python3 tools/validate_assets.py --write-index`; do not hand-maintain it.
5. Run validation before packaging. Verify release artifacts independently
   when release behavior changes.
6. For `agent-skills/**`, follow the shared skill format, keep only canonical source content here, and run the skill creator `quick_validate.py` in addition to asset validation.

## Asset Rules

- Official skills keep required examples, permission boundaries, forbidden
  actions, guardrails, and sensitive-data handling.
- Agent-facing meta skills may route into runtime skill references, but must discover live Gateway capabilities instead of claiming unavailable tools.
- MCP presets and agent profiles declare only capabilities their target runtime
  actually supports.
- Keep Cloud-only policy out of open assets unless expressed as a generic
  public extension.
- Update compatibility and governance catalogs from evidence, not version
  guesses.
- Do not copy core business logic or treat a catalog snapshot as the live
  runtime source of truth.

## Verification

From Root `soha/` (or use the corresponding `tools/` command from `skills/`):

```bash
python3 skills/tools/validate_assets.py
python3 skills/tools/validate_assets.py --package-dry-run
```

Use the package dry run, `--release-version`, and `--verify-package` for package/release-facing
changes. Component `.agents/skills` prose does not update the runtime index; validate
its metadata and references instead.

Source validation requires same-Root `contracts/` and Core inputs. Use [active Skills CI](../../../../.github/workflows/skills-ci.yml)
and [Skills release](../../../../.github/workflows/skills-release.yml); component-local workflows
are inactive history. Keep the public `soha-skills` archive name/prefix and stable release
installation distinct from source checks.
