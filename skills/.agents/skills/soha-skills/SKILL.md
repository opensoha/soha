---
name: soha-skills
description: Change or review published Soha runtime skills, agent-facing assets, presets, and catalogs in soha-skills. Repository-local collaboration skills use skill-creator.
---

# Soha Skills Catalog

## Purpose

Maintain the official runtime asset catalog and its security and compatibility
evidence. This repository's `skills/**` are governed runtime workflow assets,
`agent-skills/**` are installable skills for user agent tools, and
`.agents/skills` contains repository collaboration instructions.

## Workflow

1. Read the affected asset, its local schema, `catalog/README.md`,
   `catalog/compatibility-matrix.json`, and `tools/validate_assets.py`.
2. If a public manifest field changes, update `../soha-contracts` first.
   Local schemas may be stricter but may not fork the public contract.
3. Keep capability references backed by the Gateway, platform, or AI platform
   catalog and by real runtime evidence.
4. When published runtime skill front matter changes, regenerate `skills/index.json` with
   `python3 tools/validate_assets.py --write-index`; do not hand-maintain it.
5. Run validation before packaging. Verify release artifacts independently
   when release behavior changes.
6. For `agent-skills/**`, follow the shared skill format, keep only canonical source content here, and run the skill creator `quick_validate.py` in addition to repository validation.

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

```bash
python3 tools/validate_assets.py
python3 tools/validate_assets.py --package-dry-run
```

Use the package dry run, `--release-version`, and `--verify-package` for package/release-facing
changes. Repository-local `.agents/skills` prose does not update the runtime index; validate
its metadata and references instead.
