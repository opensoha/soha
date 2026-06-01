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

- Reuse tokens from `web/src/theme/app-theme.ts`.
- Prefer neutral backgrounds, dark text, and restrained emphasis colors.
- Keep control shapes mostly square-edged instead of pill-shaped.
- Use shadow sparingly. Border and spacing should do most of the separation work.
- Avoid purple branding, saturated gradients, and oversized hero-like headers.

## Common Page Patterns

### List Pages

- Put scope filters, search, refresh, and batch actions in the panel toolbar.
- Keep tables flat and dense.
- For ordinary business tables, mirror the clusters-page style: compact `AdminTable`, quiet border-only shell, small pagination/footer, toolbar actions on the right, optional column settings, and no left-top table title. Leave `AdminTable.title` empty unless multiple adjacent tables need labels to disambiguate their purpose.
- Anchor the primary identifying column to the left when horizontal scrolling is needed.
- Keep mutation-heavy row actions fixed on the right when the table scrolls.
- Prefer backend paging or aggregate DTOs for heavy operational lists. Avoid table-side joins that require one request per row or namespace.

### Management Query + Table Pages

- Use `web/src/components/management-list.tsx` and the clusters-page pattern for management surfaces that need a richer query form: compact query card first, then a separate table panel with create, refresh, density, column settings, selection, and batch actions.
- Keep the query card visually short. Use tight card body padding, 28px controls, restrained gaps, and no decorative copy.
- Make horizontal antd forms line up precisely: label, filled `Input` placeholder text, and `Select` placeholder text should share the same vertical center. For `Input allowClear`, remember that antd renders an `ant-input-affix-wrapper`; set the wrapper height and alignment separately from the inner `input` so the placeholder does not drift.
- Do not show generic table titles such as `查询表格` when the table context is already clear. Put operational controls in the table header instead.
- Do not add left-header titles to standard CRUD/resource tables just to fill the table header. The clusters page is the baseline: the table surface starts with toolbar controls and the data grid, not a decorative title.
- Avoid default sort affordances on list columns. Add sorting only when it is part of the expected workflow and the backing data semantics are clear.
- Prefer icon-only row actions with tooltips for dense operational tables. Use familiar `@ant-design/icons` such as view, edit, delete, refresh, density, and settings icons.
- Use antd `Popconfirm` for row-level and lightweight batch destructive confirmations. Reserve `Modal.confirm` for heavier, multi-step, or high-context decisions.
- Compress pagination/footer spacing so the table panel ends tightly after the data, while preserving enough hit area for page and size controls.

### Detail Pages

- Treat detail pages as operational workspaces, not read-only description sheets.
- Group tabs or sections around overview, metrics, YAML, and actions.
- Prefer incremental reveal of heavy panels such as logs, terminals, and editors.
- Keep consoles, log streams, YAML editors, and analysis artifacts visually subordinate to the active task. They should not resize the whole layout unpredictably when content streams in.

### Forms and Drawers

- Keep forms short and sectioned only when the data model truly has multiple domains.
- Prefer inline help over long descriptive paragraphs.
- Use drawers for scoped edits that should preserve table context; use full pages when the workflow is multi-step or deeply operational.

## Status and Feedback

- Use tags, alerts, and result states consistently with backend semantics.
- Error and warning surfaces should be precise and actionable.
- Empty states should explain whether the page is unsupported, backend-pending, or simply empty in the current scope.
- Operation-driven pages should expose queued, dispatching, running, callback timeout, canceled, failed, and completed states distinctly when the backend provides them.
- AI analysis pages should distinguish draft/session context, running analysis, completed artifacts, missing permission, missing datasource, and budget or timeout exhaustion.
- Docker and virtualization pages should label lab-only, emulated, degraded, or unsupported states instead of presenting them as normal production readiness.

## Workbench Chrome

- AI-specific mode switching, conversation history, toolset, and skill controls belong inside the AI workbench page, not in the global sidebar.
- Docker and virtualization workbench navigation should stay compact and operational. Avoid dashboard hero sections or marketing-style cards.
- Monitoring and on-call pages should lead with active operational tasks or alert context before configuration tables.
- System and settings pages can use dense forms and tables; avoid nesting cards inside larger page cards.
