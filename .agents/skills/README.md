# soha Repo Skills

This directory stores repo-local Codex skills that define repeatable execution rules for soha work.

Current skill families:

- `soha-backend`
- `soha-frontend`
- `soha-deploy`

## Scope

Repo-local skills exist to carry soha-specific engineering rules that should not live only in the global Codex environment.

Use them for:

- folder ownership
- architecture boundaries
- runtime invariants
- validation expectations
- repo-specific workflow rules

Do not use them for:

- duplicated copies of `AGENTS.md`
- user-facing product docs
- long changelog-style history
- generic framework tutorials that Codex already knows

## Required Structure

Each skill directory must contain:

- `SKILL.md`

`SKILL.md` must contain:

- YAML frontmatter with `name` and `description`
- a concise workflow section
- non-negotiable repo rules
- validation expectations

## Writing Rules

When adding or updating repo-local skills:

1. Keep the trigger description narrow and concrete.
2. Put only soha-specific guidance in the body.
3. Prefer short workflow bullets over long prose.
4. Point to real repo paths instead of abstract layers whenever possible.
5. Keep validation commands current with the active repo toolchain.
6. Update skill text in the same task when architecture or workflow rules change.

## Relationship To AGENTS.md

`AGENTS.md` remains the top-level engineering memory.

Repo-local skills should refine that memory for a specific execution area, not compete with it.

A good rule:

- global engineering baseline -> `AGENTS.md`
- bounded implementation guidance -> `.agents/skills/*/SKILL.md`

## Update Requirement

If a change alters:

- module ownership
- route ownership
- validation commands
- deployment entrypoints
- required runtime caveats

then the relevant repo-local skill must be reviewed and updated in the same task.
