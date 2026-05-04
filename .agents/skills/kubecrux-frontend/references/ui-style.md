# UI Style

## Design Intent

Use native Ant Design components but make them feel closer to shadcn's visual restraint:

- grayscale-first palette
- subtle borders and fills
- compact spacing
- low-noise surfaces
- actions that read as operational rather than promotional

Do not add a second design system or copy shadcn component APIs.

## Visual Rules

- Reuse tokens from `web/src/theme/semi-theme.ts`.
- Prefer neutral backgrounds, dark text, and restrained emphasis colors.
- Keep control shapes mostly square-edged instead of pill-shaped.
- Use shadow sparingly. Border and spacing should do most of the separation work.
- Avoid purple branding, saturated gradients, and oversized hero-like headers.

## Common Page Patterns

### List Pages

- Put scope filters, search, refresh, and batch actions in the panel toolbar.
- Keep tables flat and dense.
- Anchor the primary identifying column to the left when horizontal scrolling is needed.
- Keep mutation-heavy row actions fixed on the right when the table scrolls.

### Detail Pages

- Treat detail pages as operational workspaces, not read-only description sheets.
- Group tabs or sections around overview, metrics, YAML, and actions.
- Prefer incremental reveal of heavy panels such as logs, terminals, and editors.

### Forms and Drawers

- Keep forms short and sectioned only when the data model truly has multiple domains.
- Prefer inline help over long descriptive paragraphs.
- Use drawers for scoped edits that should preserve table context; use full pages when the workflow is multi-step or deeply operational.

## Status and Feedback

- Use tags, alerts, and result states consistently with backend semantics.
- Error and warning surfaces should be precise and actionable.
- Empty states should explain whether the page is unsupported, backend-pending, or simply empty in the current scope.
