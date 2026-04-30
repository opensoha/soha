# kubecrux Engineering Spec

## 1. Purpose

This file is the repository-level engineering memory for `kubecrux`.
It acts as the single working spec for:

- architecture design
- backend and frontend technical solutions
- functional design
- implementation boundaries
- performance expectations
- memory update rules after each change

The style follows a Harness-like engineering convention:

- one file as the stable execution baseline
- clear layering and ownership
- explicit change rules
- design and delivery documented together

## 2. Product Positioning

`kubecrux` is a multi-cluster Kubernetes platform console.
It is not only a resource viewer. It is intended to become a unified control plane for:

- platform management
- Kubernetes dashboard capability
- application delivery
- observability and alert collaboration
- access control
- system management
- AI-assisted analysis

Project summary:

- `kubecrux` is a multi-cluster Kubernetes platform console
- backend baseline: Go + Gin + PostgreSQL + Redis + `client-go`
- frontend baseline: React 18 + Vite 6 + TypeScript 5 + TanStack Query 5 + Zustand 5 + Ant Design 6
- docs baseline: Docusaurus

## 3. Engineering Baseline

### Backend

- language: Go
- HTTP framework: Gin
- persistence: PostgreSQL
- cache/session/runtime support: Redis
- Kubernetes access: `client-go`
- remote cluster mode: agent HTTP connector
- API style: aggregated platform view models, not raw Kubernetes objects

### Frontend

- runtime: React 18 + Vite 6 + TypeScript 5
- routing: React Router 6
- server state: TanStack Query 5
- client state: Zustand 5
- UI system: Ant Design 6
- utility styling: Tailwind CSS 4
- charts/visualization: Ant Design Charts, ECharts legacy transition allowed during migration
- test baseline: Vitest

### Docs

- in-repo docs site: Docusaurus
- architecture documents under `docs/architecture`
- this file is the top-level execution memory

## 4. System Architecture

The system is organized as a modular monolith with clear layers.

1. Presentation layer
   - `web` frontend console
   - `docs` documentation site
2. API layer
   - `internal/api`
   - routing, middleware, DTO parsing, response shaping
3. Application layer
   - `internal/application`
   - use-case orchestration and business coordination
4. Policy layer
   - `internal/policy`
   - RBAC, ABAC, scope filtering
5. Infrastructure layer
   - `internal/infrastructure`
   - config, logger, kubernetes, informer, database, redis, mcp, swagger
6. Repository layer
   - `internal/repository`
   - durable storage access
7. External systems
   - Kubernetes clusters
   - PostgreSQL
   - Redis
   - future CI/CD, alerting, MCP-capable integrations

## 5. Backend Technical Solution

### 5.1 Layering Rules

#### `internal/api`

- parse requests
- extract principal and request context
- delegate to application services
- map domain/application errors to HTTP responses
- must not implement Kubernetes traversal or policy decisions

#### `internal/application`

- orchestrate use cases
- apply access control and scope constraints
- decide between direct cluster mode and agent mode
- record audit and operation traces
- return frontend-facing aggregated view models

#### `internal/infrastructure`

- build and manage external clients
- hide vendor-specific details from the application layer
- own kubernetes manager, informer bootstrap, agent HTTP client, DB wiring

#### `internal/repository`

- persist business and runtime records
- keep SQL and GORM concerns out of handler/application orchestration

### 5.2 Cluster Access Model

Two runtime modes are supported:

- `direct_kubeconfig`
  - server talks to cluster using `client-go`
  - informer/cache is preferred where possible
  - short live-query fallback is allowed
- `agent`
  - server talks to remote agent over HTTP
  - agent performs cluster reads/actions
  - degraded agent state must surface as cluster health degradation

### 5.3 API Design Rules

- API returns platform view models instead of raw Kubernetes schema objects
- list endpoints must support cluster scope and namespace scope
- empty namespace means all namespaces unless the resource is cluster-scoped by nature
- transport should remain thin; semantics live in application services
- audit must be recorded for important read/write/action operations

### 5.4 Performance Rules

- prefer informer/cache for high-frequency namespace-scoped list reads
- use bounded live-query fallback when cache is not ready
- avoid frontend-driven N+1 aggregation when backend can aggregate once
- API output should be flattened for UI consumption
- timeouts must be explicit for live cluster reads

## 6. Frontend Technical Solution

### 6.1 Frontend Structure

The frontend is route-driven and domain-oriented.

- `web/src/routes`
  - route registry and navigation metadata
- `web/src/layouts`
  - shell layout
- `web/src/features`
  - route-level business pages
- `web/src/components`
  - real shared primitives only
- `web/src/services`
  - API client
- `web/src/stores`
  - auth, preference, platform scope
- `web/src/utils`
  - cross-page table/time helpers

Current structure summary:

- `web/src/routes` owns route registration and navigation metadata
- `web/src/features` owns domain-oriented route pages
- `web/src/components` owns real shared primitives only
- `internal/api`, `internal/application`, `internal/policy`, `internal/infrastructure`, and `internal/repository` keep strict backend layer responsibilities
- `docs/architecture` is the public architecture document set

### 6.2 UI and State Rules

- Ant Design is the primary component system
- `@/compat/semi-*` is a temporary migration aid only; new code must not depend on it
- Zustand stores lightweight local UI/runtime preferences
- TanStack Query owns server data lifecycle
- route metadata is the navigation source of truth
- platform pages must share persisted cluster/namespace scope

### 6.3 Frontend Performance Rules

- lazy-load route modules
- avoid page-level repeated query construction
- prefer shared scoped query helpers for platform pages
- do not issue one request per namespace from the browser when backend aggregation exists
- keep detail pages focused; expensive editors/log/terminal modules should remain lazy
- migration work must replace Semi semantics with native antd semantics rather than widening compatibility wrappers indefinitely

## 7. Functional Design

### 7.1 Platform Management / Kubernetes Dashboard

This is the current core execution area.

It includes:

- cluster inventory and connection management
- nodes and namespaces
- workload dashboard
  - overview
  - pods
    - runtime overview
    - container and condition detail
    - metrics
    - events
    - logs
      - recent 100 lines default
      - previous container logs
      - upward incremental history loading
    - terminal
    - exec
    - YAML
  - deployments
    - metrics
    - events
    - rollout status and rollout history
    - search, filter, batch restart, batch scale, and batch rollback
    - diagnostics export
  - statefulsets
  - daemonsets
  - jobs
  - cronjobs
- network dashboard
  - network topology
    - ingress host or entry domain to service to pod flow
    - gateway and HTTPRoute coverage must stay explicit even when service backend aggregation is still pending
    - demo fallback is allowed for visual review, but it must be clearly labeled as preview data
  - services
    - backend pod linkage
    - events
    - metrics
    - diagnostics export
  - ingresses
  - gateways
  - HTTP routes
- storage dashboard
  - PVC
  - PV
  - storage classes
- extensions dashboard
  - CRDs
  - Helm releases
  - Helm charts when backend capability exists

Design expectations:

- pages are list-first
- cluster and namespace scope must be obvious and persistent
- detail pages should provide actionable operations, not only display data
- workload/network/storage views should prefer aggregated APIs
- frontend behavior must match backend scope semantics
- cluster management should remain a registration and connection-management surface, not a separate cluster overview workspace
- the CRD workspace is now an operational surface: the catalog must expose each CRD's kind, plural, served versions, and scope, and the same workspace should list real CRD-backed resources plus YAML-based create/edit/delete actions for the selected kind
- CRD-backed resource listing follows CRD scope semantics: cluster-scoped CRDs ignore namespace selection, while namespaced CRDs must support both single-namespace views and all-namespaces aggregation when no namespace filter is active
- CRD-backed resource CRUD and YAML flows are currently direct-cluster capabilities only; agent-connected clusters must surface those paths as unsupported instead of implying parity

### 7.2 Delivery

- applications
- business lines
- delivery environments
- application environments
- workflow templates
- release board
- workflows
- releases
- registries

Design expectation:

- delivery remains platform-native
- business lines, delivery environments, and application-environment bindings are now treated as a standalone master-data domain in frontend navigation; they still serve delivery and access-control scope flows, but their ownership is no longer represented as delivery-only in the console IA
- environment binding and release orchestration must map to platform runtime context

### 7.3 Observability

- monitoring summary
- alerts
- notifications
- on-call collaboration
- events
- AI-assisted root cause and performance analysis

Design expectation:

- observability is not isolated from platform data
- cluster/application/runtime context should be composable

### 7.4 Access / System / Settings

- user, role, team, policy management
- scope grants
- online users
- announcements
- menus
- audit logs
- operation logs
- identity settings
- monitoring settings
- AI settings

Design expectation:

- management surfaces must align with authorization model
- any permission or scope change must flow through policy design first

## 8. Current Design Convergence

The repository has already converged on these rules:

- one route registry drives the frontend shell
- one shared platform scope model drives platform resource pages
- platform collection pages should use shared scoped path construction
- backend all-namespaces aggregation is preferred over frontend namespace fan-out
- identity bootstrap baseline is a single `admin / kubecrux` seed from `auth.dev_principal`; legacy bootstrap migration and login fallback are removed
- built-in bootstrap defaults should be version-gated: first-time initialization and seed-version upgrades replay static roles/menus/policies/templates, while config-driven admin user and cluster sync stay as separate startup work
- pod detail is now expected to be an operational workspace, not only a static detail page
- workload list pages should support search/filter first, then batch action surfaces where backend capability already exists
- pod list pages should stay flat and list-first; inline row preview expansion, health summary columns, and multi-select batch actions should not compete with the dedicated pod detail workspace
- deployment list pages should stay flat and route related-pod inspection into the deployment detail workspace; multi-select header actions should operate on the current visible page and keep the right-side action icon rail fixed during horizontal scrolling
- pod list pages should avoid dashboard-style summary cards above the table when the same page already centers on scoped filtering and tabular operations
- pod list tables should keep the primary name column visually left-anchored and keep mutation actions fixed on the right when horizontal scrolling is enabled
- workload list pages should fold scope filters, search, refresh, and batch actions into the table panel toolbar instead of stacking a separate page header and external scope bar above the table
- pod metrics panels should present five equal-footprint charts; CPU and memory baselines stay inside the chart, while disk and network throughput use mirrored up/down axes to contrast read-or-in against write-or-out
- platform navigation should expose CRD as its own top-level entry, keep Helm adjacent to namespace-scoped operations, and place configuration resources between workloads and network
- platform navigation now treats Kubernetes RBAC as a standalone top-level workspace with child pages for service accounts and RBAC bindings, instead of nesting it under Helm
- namespace-scoped platform capability expansion may ship as complete navigation plus placeholder pages before backend aggregation APIs are ready, but those placeholders must say that the backend platform API is still pending
- Helm in platform management now focuses on releases and charts only; Kubernetes RBAC resources live under the standalone RBAC workspace
- network platform navigation no longer exposes HTTP Routes; unsupported resource families should be removed end-to-end from routes, menu seeds, and API handlers instead of staying as dead entries
- network now lands on a `网络拓扑` workspace before the raw resource lists; the topology view may combine live ingress, Gateway API, service, and backend relationships with explicitly pending route placeholders, but it must not present missing backend aggregation as if it were verified
- topology preview pages may fall back to clearly labeled demo traces when the current scope has no live entry path, so interaction and layout review do not depend on a pre-populated cluster
- network topology should use one layered left-to-right graph for entry, route, service, and backend relationships across both ingress-controller routes and Gateway API HTTPRoutes; gateways without visible HTTPRoute bindings may remain in the same graph as pending-route placeholders, while backend pods may stay collapsed into summary nodes until drill-down
- platform overview should expose cluster-aware pod runtime cards instead of only fleet and alert counters
- platform overview runtime cards must consume a backend workload overview aggregation endpoint and keep the active cluster/namespace scope visible; frontend should not fetch all Pod rows just to render dashboard summaries
- service pages should evolve from plain tables to operational workspaces when selector, metrics, and event context already exist
- cluster management remains the registration surface, but each cluster row should drill into a lightweight detail page for labels, version, health, and handoff into node operations
- node resources should expose a standalone node detail workspace with YAML, labels/taints editing, and scheduled pod context instead of limiting all actions to list-row modals
- page bundles may export multiple route-level pages until reuse pressure justifies further splitting
- frontend consumes aggregated platform views only
- access control pages must bind to backend's real role/user/group/policy schema instead of placeholder table columns or fake form fields
- user create/update must persist role bindings and user-group bindings in the same submission so RBAC/scope decisions take effect immediately
- user-facing terminology under access control is `用户组` while persistence and policy matcher internals may continue using `team/teams`
- authenticated frontend navigation must consume a backend permission snapshot instead of relying on static route visibility alone
- backend permission snapshots and console/API permission checks now resolve from persisted role `permissionKeys`; static built-in role maps remain bootstrap defaults only and custom roles must be able to drive backend authorization without `admin` special-casing
- menu visibility is now a conjunction of backend visible menu bindings and frontend route permission keys, while page buttons should progressively consume either permission keys or backend `allowedActions`
- access management and scope-grant CRUD must use explicit `access.*.(view|manage)` permission keys; scope-grant list/create/update/delete are principal-aware backend operations and are no longer safe to leave authenticated-only
- sidebar sibling ordering should honor backend visible-menu sort within each frontend group so menu-management sort changes affect the console without duplicating section headers
- monitoring and copilot APIs are no longer implicitly open to any authenticated user; user-facing reads and writes must check permission keys before hitting repository operations
- observability and AI pages should treat route visibility, button visibility, and backend API authorization as three separate gates that must stay aligned
- delivery catalog writes such as business lines, environments, application-environment bindings, workflow templates, and registry connections must enforce backend permission keys, not just frontend button hiding
- AI settings now split into `settings.ai.view` and `settings.ai.manage`; the provider form and copilot control-plane sections must stay consistent with those keys
- settings center should consistently use `settings.<domain>.view` for route visibility and `settings.<domain>.manage` for save/update actions instead of mixing permission keys with legacy admin-only checks
- system management should follow the same split: `system.<module>.view` for page access and `system.<module>.manage` for destructive or mutable actions such as session revocation, announcements, and menus
- access control should remain visible as a top-level console menu entry for admins, while its child pages can stay as nested routes beneath that entry
- settings center is a single top-level menu with in-page tabs for identity and AI; cluster-level monitoring configuration should not remain as a separate settings-center submenu
- settings center now includes a branding tab for console-level brand assets and title metadata; branding settings are distinct from cluster-level monitoring settings and should be applied globally in the web shell
- the console shell theme keeps a fixed Semi theme variant with brand overrides, while the header may expose a light/dark mode toggle as a user preference; theme-brand switching should still stay disabled unless it is intentionally restored end-to-end
- frontend theme customization now uses `web/src/theme/semi-theme.ts` as the single source for both antd `ThemeConfig` and shared `--kc-*` CSS variables; avoid duplicating theme tokens in `main.tsx` or standalone style files
- the console visual baseline has shifted from the older purple brand palette to a neutral shadcn-like grayscale palette, while still preserving light/dark mode support and shared CSS variable contracts for non-antd surfaces
- shared platform filters such as resource scope and workload search bars should use compact, square-edged controls rather than pill-shaped fields
- frontend migration baseline is now antd-first: new or migrated pages must import directly from `antd` and `@ant-design/icons`
- native antd migration should normalize shared foundation modules first: `resource-metrics-panel`, `resource-actions`, `page-header`, `status-tag`, and `platform-scope-toolbar` own the primary replacement of compat button/modal/tabs/descriptions/select/tag semantics for downstream pages
- remaining compat-removal execution order is fixed: shared foundation first, then domain feature waves (`platform`, `access`, `delivery`, `observability`, `copilot` plus remaining auth/system/settings/docs web modules), then compatibility-layer deletion, and only then docs-site Docusaurus cleanup
- compat-removal work must rewrite form/button/modal/tab/description/toast usage to native antd APIs in place; do not preserve Semi prop names behind new wrappers
- remaining `@/compat/semi-*` imports are migration debt; they should be removed module-by-module until the compatibility layer can be deleted
- antd-first is the stable frontend baseline; `platform`, `access`, `delivery`, `observability`, `copilot`, `system`, `settings`, docs-facing web modules, plus the remaining shared/auth/routes tail files have all converged on native `antd` and `@ant-design/icons`
- `rg -n "@/compat/semi-" web/src` and `rg -n "@/compat/semi-icons" web/src` are now expected to stay at zero; `web/src/compat/**` has been deleted after web-side compat usage fully cleared
- docs-side Docusaurus cleanup continues only after the web compat layer is fully cleared, and web cleanup is now complete
- docs migration baseline is Docusaurus-first; new docs-site work must target Docusaurus config, sidebars, and MDX component conventions instead of VitePress

## 9. Change Rules

Every non-trivial change must be evaluated against these dimensions:

1. architecture impact
2. backend contract impact
3. frontend route/page impact
4. functional behavior impact
5. performance impact
6. testing impact
7. memory/documentation impact

## 10. Memory Update Rules

After each change, memory must be updated.
For this repository, that means:

### Mandatory

- update `agents.md` when architecture, technical方案, module boundary, delivery rule, performance rule, or feature scope changes
- update `docs/architecture/*` when the public architecture description changes materially
- update route metadata when navigation or page ownership changes
- update tests when semantics or contracts change
- when frontend migration tasks land, record whether a module still depends on `@/compat/semi-*` or has been fully moved to native antd imports

### For Platform Dashboard Changes

Whenever `platform` pages or APIs are changed, record:

- which module changed
- whether scope semantics changed
- whether aggregation moved frontend-to-backend or backend-to-frontend
- whether a new page is placeholder, partial, or production-usable
- what performance risk was removed or introduced

### Memory Priority

When both code and docs change, update memory in the same task, not later.

## 11. Definition Of Done

A task is not complete unless all applicable items are true:

- code compiles
- affected backend tests pass
- affected frontend tests pass
- if no test exists, at least build/typecheck validation passes
- memory is updated
- behavior matches scope and authorization rules
- unnecessary query fan-out or repeated logic was avoided
- design remains consistent with current platform console model

## 12. Preferred Optimization Direction

Long-term optimization priorities:

1. remove repeated scoped query code from frontend platform pages
2. move aggregation to backend where cluster-wide reads are required
3. standardize detail-page capability patterns
   - overview
   - metrics
   - YAML
   - action surface
4. expand tests around platform API semantics
5. keep route/page structure explicit until real shared abstractions emerge

## 13. Immediate Working Focus

Current focus remains:

- continue improving the Platform Management menu as the main k8s dashboard
- keep frontend and backend behavior aligned
- keep memory synchronized after each change
- complete testing for both sides
- continuously optimize performance and code design

## 14. Reference Documents

Primary supporting documents:

- `README.md`
- `docs/architecture/index.md`
- `docs/architecture/application-delivery.md`
- `docs/architecture/authorization.md`
- `docs/architecture/monitoring-and-alerting.md`

## 15. Codex Collaboration Baseline

This repository may use repo-local collaboration files under `.codex/` for isolated Codex threads or agents.
`AGENTS.md` remains the engineering baseline; `.codex/` carries task execution context.

- `.codex/state/current_task.md` is the canonical task snapshot for any spawned thread
- `.codex/state/queue.md` tracks subtask ownership, priority, status, and dependencies
- `.codex/state/results/` stores concise role outputs instead of full transcripts or raw long logs
- `.codex/handoffs/` stores explicit handoff notes between `main`, `coder`, `tester`, and `reviewer`
- `.codex/prompts/` stores reusable role prompt templates for child threads
- multi-track migrations should assign disjoint write ownership by directory or module family in `queue.md`; shared foundation ownership must be resolved before compat-file deletion or docs migration begins
- child threads must not assume access to the full parent-thread conversation; they should rely on `current_task`, relevant handoff files, result files, and the referenced code files
- handoffs should pass only the minimum necessary context: current task summary, exact files to read, verification status, open risks, and one recommended next step
- when command output is long, keep only a concise summary plus the minimal failing or trailing excerpt needed for the next role
- threads should keep changes scoped to the assigned task and avoid widening work without updating the queue or handoff
- When the user provides a new feature request or bug report in natural language, the main orchestrator must first convert it into a concrete current_task snapshot before spawning coder/tester/reviewer subagents.
