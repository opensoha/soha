# Soha Web Theme System

This reference owns token application, shared component styling and CSS boundaries. Use
[product-experience.md](product-experience.md) for page organization, identity, metrics/console
truthfulness and visual review; [architecture-development.md](architecture-development.md) owns
route/data/state/runtime behavior. Documentation changes do not apply new runtime styles.

## Core Standard

- Use Ant Design 6 as the base component system.
- Keep exactly one app-level `ConfigProvider` in `src/main.tsx`.
- Treat `src/theme/app-theme.ts` as the source of truth for antd tokens and Soha CSS variables.
- The light content canvas is `#f6f8fb`; content, inputs and the brand/navigation base stay white.
  Dark mode retains its existing semantic palette. Theme values belong in `app-theme.ts`, not
  page-local global overrides.
- Use Soha semantic variables instead of page-local hardcoded colors and shadows.
- Keep shape tokens split by purpose: `--soha-radius-control` for inputs/buttons/list items, `--soha-radius-panel` for cards/query panels/detail headers/table shells, and `--soha-radius-lg` for larger scene containers.
- Reuse management search width tokens and responsive query behavior where suitable. A page
  owns its task layout; adjust shared variants or documented scoped composition when needed,
  without repeatedly overriding global input geometry or unmounting hidden query fields.

## Style ownership and density

- `app-theme.ts` owns semantic/category colors, typography, control sizing, spacing and elevation
  tokens. Shared components own behavior and reusable variants; pages own task composition.
- Use public APIs, component tokens and semantic styling supported by the installed Antd version.
  Deep `.ant-*` selectors, internal DOM assumptions and `!important` require a documented owner,
  scope and reason; they are exceptions, not the normal design method. Inventory existing
  overrides before removing them in a separately authorized implementation change.
- Keep standard body, supporting text, controls and table typography as distinct semantic levels.
  Determine numbers in a real sample; never shrink the whole UI to fit more data. Standard reading
  and compact operation density can coexist. Pilot values are not new global defaults.
- Surface depth can combine hierarchy, spacing, grouping, contrast and restrained material. Do
  not limit it to radius/shadow or use decoration to duplicate information. Ordinary specifications
  and addresses use text; category and state color carry meaning.

### Current foundation ownership

`getAntdTheme` and `applyAppTheme` share the typography, control height and radius values in
`app-theme.ts`. The latter applies Soha variables before React mounts. Do not redefine those
values in `globals.css`; a CSS fallback is not a separate design setting.

They also own `--soha-motion-duration-fast/mid/slow`, `--soha-motion-ease[-out]` and
`--soha-motion-activity`, derived from Antd. System reduced motion updates the single
ConfigProvider and CSS together. Scene CSS consumes these tokens while retaining its own
task layout and transition shape; avoid separate equivalent VM/Compute/Delivery token sets.

Keep the provider subtree stable when motion preferences change at runtime. Verify that drafts,
selection and professional sessions survive the first transition in both directions; testing
only a page that starts with reduced motion does not cover this lifecycle boundary.

- The shared reading baseline is 14 px / 22 px. Supporting text is 12 px; regular input, number,
  date, select and button text is 13 px with 32 px default controls. Section and modal headings
  are 16 px / 24 px. Existing explicit compact/large variants remain available.
- Antd controls own their geometry, explicit sizes, multiline input and Select internals.
  Management query CSS owns field widths, label alignment, wrapping and collapse, using
  `--soha-control-height` for its default alignment rather than forcing child control heights.
- The compact app sidebar uses `--soha-sidebar-item-height` (31 px); other menus retain the
  standard Menu token (38 px). This shell-specific rule lives in `.soha-nav-menu`, is independent
  of the route, and must preserve collapsed navigation. Remaining width/indent overrides belong
  to that shell; table fixed-column background/scroll corrections remain behavioral exceptions.
- Small tables keep 12 px / 20 px and 8 px vertical padding; middle tables use 14 px and 12 px
  vertical padding. Keep the existing density toggle; no new global density preference. Menu and
  Breadcrumb stay 12 px independently of the body scale. Shared calibration is not page acceptance.

### Shared visual hierarchy

| Layer | Current setting | Owner / purpose |
| --- | --- | --- |
| Body / supporting text | 14px/22px; supporting 12px/18px | Theme and `--soha-font-size[-supporting]` |
| Controls | 13px, default height 32px; compact button/input 12px | Antd component tokens; preserve size behavior |
| Section / dialog title | 16px/24px, semibold | Card/Modal tokens and `--soha-font-size-section` |
| Resource Tabs | 14px; active weight 600; 2px indicator | Shared resource Tabs CSS and Antd Tabs tokens |
| Query region | White surface, quiet border, 12px/16px padding | Shared query CSS; label/control rhythm, no title panel |
| Shared panel body | 16px padding | Existing management panel; no new container required |
| Main action | Primary color, subtle 1px/2px shadow | Button token; secondary neutral, dangerous action explicit |
| Navigation | Existing brand/width, 12px labels and 31px sidebar items | Scoped shell geometry, independent of content density |

Main actions stay in the existing list/flow toolbar, selection actions beside selection context,
and dialog actions at the form's existing footer. Foundation styling does not insert headings,
move business actions, or normalize every page into one skeleton.

### Overview summaries

- `OverviewChip` is a supporting summary, below the main metric hierarchy: 13px label, 22px
  value and 28px icon, without an extra inner-card shadow. Preserve helper text and status tone.
- Its shared grid fits the available container width and visible item count, using auto-fit with
  a 120px minimum track that can shrink to the container. Do not reserve five columns inside a
  half-width panel. At viewport widths up to 480px, use one column for readable long labels.
- Let labels and values wrap without clipping. Keep the existing link focus indicator and
  suppress overview hover movement/transitions when reduced motion is requested.



### Overview token and composition ownership

Keep the reading hierarchy in `app-theme.ts`: `--soha-font-size-metric` (34px) for primary
resource counts, `--soha-font-size-summary` (22px) for supporting counts, and the supporting /
section line heights (18px / 24px). Existing 30px Statistic and 32px task treatments are separate
variants; do not silently normalize every number. `StatGrid`, `OverviewChip` and the compute
composition consume the corresponding tokens without changing their existing density.

`--soha-card-padding` and Antd Card header/body padding share one 20px source. A management panel
may keep its existing 16px body; compact variants are not forced to adopt overview spacing.
`OVERVIEW_COMPACT_CHART_SIZE` supplies both the small overview chart canvas and its CSS slot
(`--soha-overview-chart-size`, 64px). It does not set the size of full metrics, topology or console
views. Theme switches must retain matching canvas/slot geometry.

The compute overview owns its four/two/one-column grid, 52px header, 4px body-top inset, task
number treatment and information order in capability CSS. These are deliberate composition
choices, not route-dependent global defaults. Continue using Card semantic `classNames` for
header/body styling. Keep the inherited palette, surface, border and radii in the shared theme;
keep category selection and real state mapping with the feature. Do not promote this layout into
a universal page component, add another ConfigProvider or change navigation/controls to match it.


## Shared Components

- Choose `ManagementDataPage` for management lists that fit its composition; it is a reusable
  example, not the default shape of every new page. Queries, tables and page layout are separable.
- `ManagementQueryPanel`, `ManagementQueryField`, `ManagementQueryScope`, `ManagementKeywordField`, `ManagementQueryActions`, `ManagementToolbarSearch`, `ManagementSearchableListPane`, `ManagementTableToolbar`, and `AdminTable` are the primary shared building blocks.
- `ManagementQueryPanel` / `ManagementQueryGrid` own responsive query collapse. Query fields must stay mounted; the shared grid decides whether fields fit in one row and only shows expand/collapse when the container actually wraps.
- `ManagementQueryActions` is the default reset/search button group. Only the submit action should be primary; reset and expand/collapse remain neutral.
- `ManagementKeywordField` is the default main query keyword field.
- `ManagementToolbarSearch` is the default compact headerExtra filter search.
- `ManagementSearchableListPane` is the default searchable left list.
- Use the shared `soha-form-segmented` treatment for a fixed set of two to six mutually exclusive
  form modes. Keep it block-width when the modes define the form below, and never add page-local
  selected colors; query filters still belong in `ManagementQueryScope`.
- Step-form dialogs and Access create/edit dialogs show a concise title in a 32px minimum-height
  header and sit 32px below the viewport top, horizontally centered. Step-form dialog content
  fills the available body width; standalone step forms retain their configured content width.
  Deliberately chrome-light nested dialogs may still hide a repeated title with
  `visuallyHiddenModalTitleStyle`, while keeping the semantic `title` for assistive technology.

## Surface Rules

- Components at the same surface level share the corresponding radius token; the page need not
  wrap each section in Card, QueryPanel, DetailHeader or a separate table shell.
- Table header top corners must match the table shell radius.
- Query inputs and toolbar searches read as white surfaces in light mode. A query region uses
  a quiet border against the canvas; the data panel supplies the stronger boundary. Avoid nesting
  extra borders around ordinary groups.
- Ordinary management buttons in query areas should stay neutral; only the core submit action should use the theme color.

## Navigation Icons

- Runtime sidebar icons come from the server menu `iconKey` and resolve through
  `src/features/system/menu-icons.tsx`; change built-in defaults in the backend menu seed and
  idempotent upgrader, not in route-local icon metadata.
- Choose icons for the primary operator task. Within one sibling group, avoid reusing the same
  rendered glyph for unrelated actions when a clearer registered icon exists; reuse the shared
  registry and installed icon set before adding assets or dependencies.
- Preserve administrator customization: a default migration may replace the prior default icon
  key, but must not overwrite an arbitrary persisted icon key.

## Workbench Overviews

- Choose patterns from the user task. Existing management summaries, AI chip grids and portal
  launchers are reusable cases, not the full set of permitted overview structures.
- Reuse `OverviewMetricCard`, `OverviewChip` and grids when their behavior and layout fit. Shared
  grids own their own responsive implementation; the page can compose different regions and
  proportions instead of forcing all scenes into that geometry.
- Give meaningful regions identifiable names and action relationships. A native Card `title`,
  custom semantic heading or accessible grouping without a visible title can each be appropriate.
  Do not add or repeat a title bar merely to satisfy a component template.
- Map metric/category color by stable meaning through shared tokens, not array position. Allow
  colored icons, light backgrounds and genuine trend lines; a fixed color sequence and 2 px
  bottom rail are not mandatory. Keep the same metric recognizable across pages and themes.
- Keep category identity distinct from lifecycle/severity. A stopped resource is not automatically
  a warning. Status labels accompany color; actual warning/danger draws attention intentionally.
- Use theme variables and appropriate `color-mix()`; feature CSS must not invent its own palettes.
- Independently queried summaries retain their own loading/error/permission/missing state and
  freshness. Unknown does not become zero; retain retry for failed sections. Data rules are in
  product-experience.md and must be checked before drawing a trend or aggregate number.
- Navigable summaries use semantic links/buttons and visible keyboard focus. A lightweight
  group may have no visible heading when navigation already establishes identity.

### AI And Delivery Overviews

- AI overview cards summarize permission-scoped interaction, knowledge, provider, and gateway
  data on shared surfaces. Chat, graph, and streaming runtime views remain token-driven scene
  exceptions and must expose loading, empty, error, retry, and streaming state explicitly.
- Delivery overview cards summarize applications, environments, release flow, and execution
  evidence. Use success, warning, and danger only for lifecycle meaning; release, rollback, retry,
  and approval commands remain explicit and confirmation-gated.

## Management Tables

- `AdminTable` retains shared table behavior over Ant Design `Table`; it does not require a
  `ManagementDataPage` outer shell. Choose query/page composition for the task and preserve
  the appropriate table controls. Do not create a parallel wrapper that copies the Antd API.
- Embedded tables use `AdminTable` directly, with `pagination={false}` and
  `enableColumnSelection={false}` when those controls are unnecessary. Preserve expansion,
  row keys, fixed columns, and callbacks; do not add a page query shell to an embedded list.
- Top-level lists retain the pagination footer and total/range summary. Cursor APIs must retain
  their real navigation semantics: do not invent totals or replace the footer with toolbar arrows.
  K8s full-collection resource lists use the domain-owned `KubernetesResourceTable` composition
  over AdminTable: an unpaginated virtual body, truthful scope/filter counts and per-resource
  layout preferences. Server-paginated catalogs and other workbenches retain their real pagination;
  do not apply K8s defaults globally.
- Table tokens in `src/theme/app-theme.ts` own header text (`colorText`), header background
  (`colorBgMuted`), and header split (`colorBorder`). Shared table CSS sets header weight to 600.
  Field separation means vertical short separators between column headers, not stronger
  horizontal row borders or a full bordered grid. Do not add page-local divider overrides.
- Current reusable compact (`small`, 8 px vertical padding) and comfortable (`middle`, 12 px)
  variants both use 12 px horizontal padding. These are implementation defaults, not immutable
  product targets. Validate changes with the sample and keep shared geometry token-owned.
- Reuse `src/utils/table-columns.ts` presets for status, time, and actions. Names stay text/links;
  metadata is secondary and actions stay predictably placed. Long identifiers remain accessible
  through wrapping, copying, or a tooltip rather than irreversible clipping.
- Fill available content width; use deliberate horizontal scrolling for wide datasets instead of
  page-local fixed maximum widths. List/Card remains appropriate for browse-and-open tasks;
  unification does not mean every list must become a table.

## Tag Semantics

- `src/components/status-tag.tsx` owns the compact `filled` treatment: `StatusTag` for
  lifecycle/health/result/severity, `MetadataTag` for categories, `BooleanTag` for boolean labels.
  Do not add page-local palettes, outlined variants, or global `.ant-tag` color overrides.
- Resolve status color from canonical values, not translated text: success/healthy green,
  failure/denial red, warning/approval orange, processing blue, unknown/inactive neutral. Check
  the shared map before adding values. A healthy running workload is not a running task;
  do not recolor every `running` value as processing.
- Metadata colors are stable by field meaning, never random by value or index:

  | Meaning | Tone |
  | --- | --- |
  | Role | `purple` |
  | Organization | `cyan` |
  | Login/mapping/session source; permission/action category | `blue` |
  | Counts, overflow `+N`, ordinary IDs, absent metadata | `default` |

- A source/provider is not a success result. Reuse `src/utils/login-provider.ts` for login
  labels/tones. Permission names are metadata; actual allow/deny decisions are statuses.
  Unknown status values remain neutral with their text preserved.
- Keep primary identities as text/links. Routine addresses, versions, and specifications do not
  automatically need colored tags; category color must help scanning.
- Do not repeat information with decorative row icons, provider-colored side borders, or accent
  rails when the same provider or resource type is already visible as text or a tag.
- Keep each tag single-line. Small groups may wrap at the container; large table collections
  need a compact summary and keyboard-accessible expansion. Access tables reuse
  `src/features/access/shared/compact-mapped-tags.tsx`: default two visible values, neutral `+N`,
  and the same tone in summary and expansion. Do not deep-import this feature-private helper
  elsewhere; promote it only when cross-feature reuse is actually needed.

## Management Table Toolbars

- Default toolbar controls use the global control height; explicit compact variants may retain
  their size inside vertically centered toolbar slots. Use `ManagementTableToolbar`; do not set
  page-local button heights.
- Keep the toolbar and table header as one continuous panel surface. Do not add a divider between
  them unless the toolbar contains a distinct titled section rather than ordinary list actions.

## Detail Overviews

- Follow the task-led composition in product-experience.md. Identity/actions, console/runtime
  state, metrics and grouped facts can form purposeful regions; one overview panel is not a limit.
- Use Descriptions or a responsive grid where label-value alignment helps. Ordinary facts use
  selectable/wrap-safe text, states use StatusTag and meaningful categories may use MetadataTag.
- Reuse surface and metric-category tokens across useful console, metric and task regions. Do
  not force every fact into one Card or wrap every field separately. Identity icons may aid
  recognition; oversized decorative icons and duplicate provider ornament add no information.
- Keep unavailable readings distinct from real zero. Pair missing values with the reason where
  known and show actionable collection/permission/error limits near the affected section.
- Do not restyle or remount a console engine to create depth. Its real connection state, ticket,
  fullscreen and lifecycle behavior remain with the existing runtime component.

## Resource Tabs

- Use the shared `soha-resource-tabs` class for peer resource categories and resource-detail
  content views. Keep the shared 14px label, 600 active weight, 2px centered indicator, and
  token-driven divider treatment consistent across workbenches.
- Add `is-header-only` when Tabs only selects the dataset and the page owns the content below it;
  omit it when each tab item owns a content panel.
- Do not use resource Tabs for shortcuts that filter one dataset. Those belong in
  `ManagementQueryScope` inside the query panel.

## Breadcrumbs

- Detail pages must preserve the navigable list-route ancestor and use the decoded resource
  identifier from the final dynamic path segment as the terminal breadcrumb. Do not expose a
  generic `Detail` label when a stable route parameter is available.
- Keep workbench, menu-group, and menu labels sourced from the runtime navigation. Route-only
  ancestors use their route titles so a shared menu ID does not collapse distinct list levels.
- Do not repeat an already identified collection/menu name as a large content header, generic
  description and decorative icon. Keep an accessible page heading where needed. A resource
  identity region combining name, status, Provider/location and actions has a distinct purpose
  and is allowed even when the breadcrumb contains the resource name. Avoid redundant return bars.
- Place mutation commands in the context where their target and risk are clear, whether list
  row/batch or useful resource actions. Preserve existing permissions, confirmations and scope;
  moving presentation does not authorize changing operation behavior.

## Shared Query Behavior

- Keep all query fields mounted in the DOM.
- Use `ManagementQueryScope` for a stable set of two to six single-select shortcuts that filter
  one dataset. It must remain one intrinsic-width layout unit: never shrink, wrap, split, or
  truncate its Segmented options. Use a Select for longer, dynamic, or larger option sets.
- Label quick filters by their meaning, such as `业务域`, `日志范围`, or `快捷范围`. Reserve
  `视图` for changes in presentation such as list, tree, timeline, or saved query layouts.
- Resetting a query should return the grid to the collapsed baseline when collapse is available.

## Exceptions

- Keep login, terminal, noVNC, DAG, topology, AI graph, and AI workbench scene-specific, but still token-driven.
- Keep Access Users and other true multi-pane pages on shared query/search components whenever ordinary keyword input is involved.
- Do not add page-local `ConfigProvider` instances or ad hoc global theme overrides. Scene tokens
  belong in `app-theme.ts`; use existing component sizes and scoped semantic variables for an
  authorized scene. Keep the app-level provider and authentication, query and Antd App context;
  do not switch global shell sizing based on a page route.
