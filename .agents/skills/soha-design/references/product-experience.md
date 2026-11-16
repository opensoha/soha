# Soha Web Product Experience

This is the long-term product design reference for Soha Web. Read it with
[theme-system.md](../../soha-frontend/references/theme-system.md) for tokens and component styling and
[architecture-development.md](../../soha-frontend/references/architecture-development.md) for behavior and ownership.
Initiative plans and review records belong in workspace docs, not in this reference.
These are design requirements, not claims that every current page already complies.

## Product direction

Unify design language and interaction semantics without forcing every page into the same
skeleton. Reuse business capabilities and mature controls without inheriting the old page's
information organization. Keep React/Vite/TypeScript and Ant Design; removing Antd is not a
product goal. A component limitation needs a concrete reproduction before proposing a separately
authorized experiment. Do not initialize shadcn, restore unverified Tailwind setup, replace
Table/Form state engines, or add an Admin framework as part of visual redesign.

Soha supports product workspaces and professional management pages in one visual system.
Resource use and operational decisions emphasize identity, current state, exceptions and next
actions. Configuration, policy and audit work preserves precise values, comparison, filtering
and bulk efficiency. A resource may combine an overview with denser configuration and history.
Management pages also receive clear hierarchy and modern typography; product pages may use tables.

Use the repository's real Soha branding and existing branding configuration. Do not substitute a
reference image's generic cloud logo. Modern, restrained design can have meaningful color and
motion. Glass, gradients, large radii, monochrome styling or a decorative illustration do not
by themselves establish a product experience.

## Choose organization from the task

Before selecting components, describe the main user task, object, important state, primary
action and secondary information. Identify unsuitable old structures and behaviors to preserve.
Choose the composition, then reuse or adjust controls.

Resource browsing, resource detail, configuration forms, task execution and professional
workspaces are guides, not a fixed number of permitted templates. Table queries, table behavior
and page layout are separate concerns. Reuse AdminTable when its behavior fits; ManagementDataPage
is useful for an appropriate management list, not a mandatory parent for a scene. Do not create a
universal replacement page wrapper or a component-library adapter for speculative future use.

Keep a common brand, navigation language, typography hierarchy, semantic colors, surface levels,
control semantics, feedback and focus treatment. Choose density, default columns and layout for
the task. Standard reading and compact operations are different contexts; do not shrink the
whole interface or inflate every page into large cards to manage information volume.

Standard reading prioritizes object identity, important state and the next action; supporting
facts remain subordinate. Compact operations prioritize comparison, exact values, filtering and
bulk work. Reuse existing control sizes and table density controls; do not add a global density
preference without a concrete need. Keep the brand, sidebar width and navigation scale stable
when moving between scenes. A scene's reading density must not resize the shared shell.

## Titles and resource identity

When runtime navigation or breadcrumbs already identify the collection, do not repeat its menu
name as a large content heading with generic description or decorative icon. Retain semantic
headings and accessible names; a visually hidden heading can name a page without duplicating
its visible navigation. A route without sufficient visible identity still needs a concise title.

A detail identity region can combine the actual resource name, lifecycle state, Provider,
location and primary actions. Breadcrumbs do not replace that useful identity. Avoid stacking
breadcrumb, redundant return bar, page title, name card and repeated explanation for one purpose.
Every visible region should contribute information or an action relationship.

## Resource lists and summaries

Organize scope/overview, compact query, list/table and ongoing feedback as a continuous flow.
Do not give every statistic, button group and filter its own titled Card. Use grouping and
alignment before adding another container or divider. Prioritize one main action and organize
secondary actions by frequency and risk, preserving accessible discovery and confirmations.
Place collection actions in the query/list toolbar and selection actions beside the selected
scope. Do not add a separate large title panel just to host a right-aligned action button.

Summary items use recognizable category or status icons, token-managed color and numerical
hierarchy. A light surface, icon, value and an actual small trend may express emphasis; no fixed
bottom-rail or positional palette template is required. The same metric keeps its identity
across pages. Stopped resources are not errors merely because a summary uses a warm category hue.
Status always has text or an accessible label alongside color.

Counts must declare their resource/scope/filter coverage and use accurate totals. Current-page
row count is not a fleet total. An abnormal count requires a verified status definition; do not
silently combine statuses that the API cannot count or filter correctly. Missing aggregate or
trend capability is an explicit design/data gap, never permission to invent data.

For card-based overviews, give the main value priority and keep supporting charts small enough
that identity, value and explanation have separate space. Group task actions below the related
counts. Repetitive scope and aggregation details can move behind an accessible information
control; errors, stale data and actionable exceptions remain visible. Separate provider-level
summary from concrete instances, preserving actual observation times and nearby named actions.
This is an overview composition, not a template for configuration tables and forms.

## Management query and form composition

Keep management-list queries in a distinct area above table actions and results. Single-keyword
and multi-condition queries should share that layout and spacing; do not merge a keyword into
the action toolbar merely because it is the only current filter. Reuse `ManagementQueryPanel`
and its field/action controls, preserving query behavior, reset, pagination and table settings.

Preserve the established form layout, field order and spacing during maintenance. Any future
regrouping must improve the real task without compressing the form to save space. Keep values,
validation, initialization and mutation ownership in the existing Antd Form; submission stays
in the existing footer. Use `StepForm`/`StepFormModal` only for tasks that actually have steps.

Step-form dialogs show their title in a 32px minimum-height header. Both step-form dialogs and
the Access create/edit dialogs sit 32px below the viewport top and remain horizontally centered.
Let steps, form content and actions use the available dialog width; retain each dialog's outer
width and standalone forms' content-width settings. Keep the header borderless, preserve field
spacing and allow the body to scroll when the viewport is short.

## Details, metrics and console

Order detail regions by the user's decision: identify the object and important state, expose
the primary action, then group supporting facts and history. Console and metrics may lead when
they serve the actual task; configuration and audit details may prioritize other evidence.
Keep secondary sections and original capabilities discoverable. Business-specific sample order
belongs in the selected initiative, not in the shared rule.

A console area or preview card should be visually identifiable and paired with accurate runtime
state. Separate VM power state from console session state: running does not mean connected.
Without a real preview source, show an honest unconnected/open-console presentation. Never use a
static desktop screenshot as a live preview, or open an extra session merely to obtain a thumbnail.
Preserve authorization, connection tickets, lazy loading, disconnect handling, fullscreen and
cleanup. Theme and shell changes must not gratuitously remount a live terminal or VNC session.

Metric cards combine category color, value and a small chart when the data supports one. State
source, unit, time range and freshness. Distinguish loading, not collected, unauthorized, failed,
empty and genuine zero. Allocated disk capacity is not disk utilization. A rising trend is not
always an improvement. Use the actual Provider's supported series; do not invent disk utilization
or history to fill a four-card composition. Supported receive/transmit metrics or an explicit
missing state are legitimate choices.

Align ordinary specifications and addresses with text, responsive grids or Descriptions where
useful. Do not turn every fact into a colored Tag, force the whole detail into one Descriptions
Card, or split every field into its own Card. Express depth with information hierarchy, spacing,
grouping, contrast and restrained material together.

## Continuity and professional capability

Differentiate first load, background refresh, submission and asynchronous execution. Keep usable
content during refresh and label stale/error state. Preserve applicable filters, sort, page,
return position and edits. Scope/resource changes must isolate data and selection correctly;
background refill must not overwrite a user's edits without an explicit policy.

Motion should explain expansion, switching and feedback and honor reduced-motion preferences.
Keep long values available, keyboard paths usable and focus restoration predictable. Permission,
confirmation, approval, cancellation and task completion follow real business semantics; closing
a prompt does not cancel a server operation. Specialized engines retain their lifecycle.

## Reference and review discipline

For each accessible reference, record its real file, referenced regions, adopted/excluded points
and unresolved capability questions. A supplied or generated image is a reference until the user
approves a specific implemented version; it is not an API specification or proof of functionality.
Sample-only menu reduction and side-by-side comparison canvases do not authorize removing
production navigation or forcing a permanent split view.

Do not copy poster headers, slogans, mountain backgrounds, large decorative gradients, oversized
3D objects, a saturated blue top bar added merely to distinguish libraries, incorrect branding,
or unverified regions/billing/snapshot/password/IP actions. No fake metrics, uptime or console
connection state. Real page captures and code versions establish the eventual review baseline.

Review actual light/dark pages, long Chinese/English names and IDs, narrow/wide views, 200% zoom,
keyboard/IME, loading/empty/error/no-permission and operation states. Check specifically:

- redundant collection titles/copy, and immediate recognition of object/state/primary action;
- useful, consistently colored summaries and truthful metrics/charts;
- a clear console focus with correct session state and access boundaries;
- excessive Cards, Tags, dividers, buttons or template-forced layout;
- real branding, accessible values/controls and preserved business behavior.

Build/type success is engineering evidence, not design acceptance. Report unavailable checks and
unresolved differences. Never self-declare user approval. After a real sample is approved, extract
only demonstrated reuse and roll out complete business chains rather than replacing every Button
or Input across the repository. Each batch verifies behavior, visual outcome and engineering.
