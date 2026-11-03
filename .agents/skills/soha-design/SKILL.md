---
name: soha-design
description: Design or review Soha Web page organization, hierarchy, density and interaction semantics. Use for UI design changes; implementation also follows soha-frontend. Copy-only maintenance does not start a redesign.
---

# Soha Design

Read [product experience](references/product-experience.md) for the affected scene and
[theme ownership](../soha-frontend/references/theme-system.md) for tokens. Actual values live
in `src/theme/app-theme.ts`; existing pages and screenshots are evidence, not approved samples.

For state changes, operations, metrics/session continuity or relationship exploration, also read
[interaction feedback](references/interaction-feedback.md); keep those semantics independent of
the scene's layout.

Identify the task, object, important state, main action and supporting facts before composing
controls. Keep Ant Design and reuse mature capabilities while preserving API, permissions,
query and professional engine lifecycle. The current task determines scope; no fixed pilot pages.

Validate the actual themes, viewports, long values, states and interaction paths affected by the
change. New or redesigned pages cover applicable light/dark, narrow views,
loading/empty/error/permission states, and mouse/keyboard paths separately; local copy or style
adjustments do not automatically rerun the entire matrix. Use soha-web-testing when executable
browser acceptance is needed, not as an automatic full-E2E step for every visual adjustment. Produce
candidate screenshots when approval is missing; never update accepted baselines or sign user
approval to make a check green. Report design gaps separately from compilation and test results.
