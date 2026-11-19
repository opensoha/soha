# Test and review evidence

Keep the fixture, generation, compatibility and full consumer matrix entry points. Fixed consumer
SHAs come from compat/consumer-revisions.json; missing pins fail. Latest integration is separate. Current source CI is Root `.github/workflows/contracts-ci.yml`;
historical component workflow definitions remain in Git history; Root workflows are active. All six
pinned consumers retain declared checks. Candidate evidence uses migrated Root Core/CLI/Agent
(`internal-source`) and pinned external Web/App/Skills (`candidate-source`); the separate five-consumer
same-source job does not prove fixed legacy Go source accepts the new module identity.
UI/HTTP acceptance is held in Web and database/runtime acceptance in each owner; their location
does not change contract ownership. This quality workflow adds private evidence only, no wire schema.
OCR rules must include fixtures, tests and workflows; generated artifacts may be excluded with reasons.
Preview and schema checks are offline; model review requires separate egress authorization and trusted
rules. Use [Root soha-security](../../soha-security/SKILL.md) when that
review is in scope; cloning Root provides the component rules without old repository checkouts.
