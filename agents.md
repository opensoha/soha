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
- frontend baseline: React 18 + TypeScript 5 + Ant Design 6 + Vite + React Router + TanStack Query 5 + Zustand 5
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

- runtime: React 18 + TypeScript 5 + Vite
- layout and scaffold: custom antd shell with shared theme tokens
- routing: React Router
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

### Deployment

- root `Dockerfile` owns the single-project image build path
- root `docker-compose.yaml` owns local app-plus-PostgreSQL startup
- root `deployment.yaml` owns the raw Kubernetes manifest baseline
- root `chart/` owns repeatable Helm installation assets

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

The frontend active baseline has been reset to the Vite application.

- active target: `web`
- backup directory: `web_pro_backup`
- migration reference: `old_web`
- `web/src/App.tsx`
  - app bootstrap and theme application
- `web/src/layouts`
  - shared shell layout and navigation chrome
- `web/src/routes`
  - React Router route registration and route metadata
- `web/src/features`
  - kubecrux business logic and route-level feature implementations
- `web/src/components`
  - shared antd primitives and complex reusable widgets
- `web/src/services`
  - API clients and auth/runtime helpers
- `web/src/stores`
  - auth, preference, platform scope state
- `web/src/utils`
  - cross-page helpers and theme/branding helpers

Current structure summary:

- root-level `Dockerfile`, `docker-compose.yaml`, `deployment.yaml`, and `chart/` are the canonical repo-local deployment assets
- `web` is the only active frontend target
- `web_pro_backup` preserves the previous frontend backup and must not be treated as the active shell
- `old_web` remains as migration/reference material only after the reset
- `web/src/routes/**` defines the active route surface for the Vite app
- `web/src/features/**` provides kubecrux business logic behind that route surface
- `internal/api`, `internal/application`, `internal/policy`, `internal/infrastructure`, and `internal/repository` keep strict backend layer responsibilities
- `docs/architecture` is the public architecture document set

### 6.2 UI and State Rules

- Ant Design is the primary component system
- custom theme tokens under `web/src/theme/app-theme.ts` remain the single source for light/dark/system mode and shared CSS variables
- Zustand stores lightweight local UI/runtime preferences
- TanStack Query owns server data lifecycle
- route metadata in `web/src/routes/meta.ts` drives navigation, breadcrumb, and permission-aware route behavior
- platform pages must share persisted cluster/namespace scope

### 6.3 Frontend Performance Rules

- lazy-load route modules where practical inside the Vite shell
- avoid page-level repeated query construction
- prefer shared scoped query helpers for platform pages
- do not issue one request per namespace from the browser when backend aggregation exists
- keep detail pages focused; expensive editors/log/terminal modules should remain lazy
- frontend rebuild work must preserve the custom theme system and shared antd shell instead of reintroducing Umi-only shell assumptions

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
- the CRD workspace is now a two-level operational surface: the outer catalog should expose available CRD API groups first, and each API-group detail workspace should expose its served kinds as left-side tags/cards plus the real CRD-backed resources and YAML-based create/edit/delete actions for the selected kind
- CRD-backed resource listing follows CRD scope semantics: cluster-scoped CRDs ignore namespace selection, while namespaced CRDs must support both single-namespace views and all-namespaces aggregation when no namespace filter is active
- CRD-backed resource CRUD and YAML flows are currently direct-cluster capabilities only; agent-connected clusters must surface those paths as unsupported instead of implying parity

### 7.2 Delivery

- applications
- build templates
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
- application delivery now centers on four stable objects: applications, build templates, application-environment bindings, and execution records
- the next enterprise execution-plane baseline is now started in code: `release_bundles`, `execution_tasks`, `execution_logs`, `execution_callbacks`, and `approval_policies` exist as first-class delivery control-plane objects
- applications may own multiple build sources (`repo_dockerfile`, `platform_build_template`, `external_pipeline`), while each application-environment binding selects one concrete build source plus one workflow template and one or more explicit release targets
- application-environment bindings now also carry strategy/promotion/approval/artifact policy references, and release targets now carry `targetKind`, `executorKind`, `groupKey`, `waveKey`, `regionKey`, and `configRef` as the stable enterprise target contract
- legacy top-level application build fields may remain as backend compatibility/migration inputs, but the active application-center UI should no longer expose them as the primary editing surface once `buildSources` is available
- application-environment binding `buildPolicy` and `releasePolicy` are now structured contracts rather than free-form JSON blobs; the frontend should edit them with typed controls instead of raw JSON textareas wherever possible
- platform-managed build templates are now a first-class delivery object with arbitrary build-command payloads, but those commands are only allowed inside the dedicated build worker path and must never execute inline in API handlers
- release-board and application/application-environment detail pages now prefer backend aggregate endpoints instead of frontend fan-out joins across builds, workflows, and releases
- workflow `manual_approval` now pauses runs with status `waiting_approval`, and approve/reject decisions persist to `workflow_approvals`
- build and release entrypoints now begin dual-writing `releaseBundleId` and `executionTaskId`, so the execution plane can evolve independently from the current synchronous delivery path
- the active delivery web surface now exposes minimal enterprise control-plane pages for release bundles, execution tasks, execution logs, and approval policies so the new execution-plane objects are inspectable from the console instead of remaining backend-only
- the first runnable `ci_agent_runner` chain now exists: control plane task claim + callback APIs, agent-side polling, local shell execution, and callback-driven task/bundle state advancement are all in place for command-driven build/release tasks
- execution-task claim and callback handling must stay routed through `internal/application/execution`; delivery HTTP handlers must not bypass that orchestration path because heartbeat persistence, release-bundle updates, and build/deploy record backfill now depend on it
- execution tasks now persist `last_heartbeat_at`, and callback-driven task updates must keep `build_records` and `deploy_records` synchronized with execution-plane status instead of leaving business records stranded at `queued`
- `ci_agent_runner` tasks are now workspace-aware: build and release payloads may carry `workspace.path`, `workspace.commandDir`, `workspace.checkout`, and `workspace.artifactFiles`, and the agent runner is responsible for preparing that workspace before executing shell commands
- asynchronous build callbacks must normalize release-bundle state back to `ready` and update `artifact_ref` or `artifact_digest` from callback payloads; they must not leave build bundles stuck at raw task states like `completed`
- the execution plane now owns a server-side timeout sweep: `dispatching` and `running` tasks that exceed `timeout_seconds` without heartbeat must transition to `callback_timeout`, emit an execution log entry, and backfill bundle, build-record, and deploy-record failure state from the execution service
- execution tasks now expose explicit control-plane operations for `cancel` and `retry`; retry must rotate the callback token before re-queueing so stale agent callbacks cannot overwrite the new attempt
- `ci_agent_runner` must inspect execution-callback responses during heartbeat and stop its local process when the control plane has already transitioned the task to a terminal state such as `canceled` or `callback_timeout`; cancel is no longer allowed to be database-only
- the runner-facing delivery surface now also exposes a token-protected task-status read path so `ci_agent_runner` can poll current task state during long commands instead of waiting for the next heartbeat callback to discover cancellation
- the agent process now owns an in-memory active-task registry plus agent-local runtime APIs for active-task list/get/cancel; these APIs are the stable local control surface for future control-plane initiated stop flows and manual runner diagnostics
- execution-task claims now include an agent runtime endpoint, and the control plane will attempt runtime cancellation directly before relying on subsequent heartbeats or status polling
- `k8s_job_runner` now consumes execution-job settings from server runtime config and can dispatch a real Kubernetes Job for build execution when an execution cluster is configured; if no execution cluster exists, the service must fall back instead of pretending parity
- execution-task rows now expose a first-class `artifacts[]` view built from task result payloads, so logs, image refs, and workspace evidence can be inspected from the console without parsing raw JSON
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
- PostgreSQL bootstrap schema is now consolidated into `migrations/postgres/0001_init.sql`; duplicate legacy root migration mirrors are migration debt and should stay removed
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
- the CRD top-level route now lands on an API-group catalog instead of a same-page CRD/resource split view; resource tables belong to the API-group detail page after selecting a kind
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
- cluster registration forms should not ask operators to hand-enter cluster IDs; backend registration generates the stable ID automatically, and the direct-kubeconfig form should avoid exposing kube context unless an explicit context-selection workflow is restored end-to-end
- cluster onboarding should present cloud or distribution choices such as `standard_kubernetes`, `gke`, `ack`, `tke`, and `aks` as provider-style metadata for UI display and filtering, while backend `region` semantics remain reserved for policy/runtime data
- platform resource metrics should default to global monitoring settings, may accept cluster-level Prometheus/Grafana overrides from cluster registration data when present, and should not imply automatic in-cluster Prometheus discovery when operators have not configured either path
- node resources should expose a standalone node detail workspace with YAML, labels/taints editing, and scheduled pod context instead of limiting all actions to list-row modals
- page bundles may export multiple route-level pages until reuse pressure justifies further splitting
- frontend consumes aggregated platform views only
- access control pages must bind to backend's real role/user/group/policy schema instead of placeholder table columns or fake form fields
- user create/update must persist role bindings and user-group bindings in the same submission so RBAC/scope decisions take effect immediately
- user-facing terminology under access control is `用户组` while persistence and policy matcher internals may continue using `team/teams`
- authenticated frontend navigation must consume a backend permission snapshot instead of relying on static route visibility alone
- backend permission snapshots and console/API permission checks now resolve from persisted role `permissionKeys`; static built-in role maps remain bootstrap defaults only and custom roles must be able to drive backend authorization without `admin` special-casing
- menu visibility is now a conjunction of backend visible menu bindings and frontend route permission keys, while page buttons should progressively consume either permission keys or backend `allowedActions`
- authenticated sidebar navigation is now backend-menu-driven instead of route-group-driven: `PermissionSnapshot.visibleMenus` must carry menu labels, `iconKey`, `section`, `sortOrder`, and `enabled`, and the web shell must build foldable parent/child navigation from that tree instead of flattening `system` / `access` / `settings` children into static route groups
- menu sections are now user-extensible from menu management itself: the section field may reuse known defaults or accept a new raw section key, and unknown section keys must still remain valid runtime group buckets with raw-label fallback in both the sidebar and menu-management filters
- menu-management changes to parent-child structure, icon key, section, sort order, enabled state, or explicit visibility overrides must invalidate the current permission snapshot so the active shell reflects the new navigation tree immediately; parent menus act as expand/collapse containers and leaf menus own navigation clicks
- access management and scope-grant CRUD must use explicit `access.*.(view|manage)` permission keys; scope-grant list/create/update/delete are principal-aware backend operations and are no longer safe to leave authenticated-only
- sidebar sibling ordering should honor backend visible-menu sort within each frontend group so menu-management sort changes affect the console without duplicating section headers
- monitoring and copilot APIs are no longer implicitly open to any authenticated user; user-facing reads and writes must check permission keys before hitting repository operations
- observability and AI pages should treat route visibility, button visibility, and backend API authorization as three separate gates that must stay aligned
- AI工作台 and 监控工作台 are first-class workbench switcher entries; their child menus belong inside their own workbench trees and must not remain duplicated under 平台工作台 / resource navigation
- AI工作台根入口 `/ai-workbench` is now the canonical session-first investigation surface; legacy `/ai-workbench/investigation` and `/ai-observe/workbench` paths should only remain as compatibility redirects instead of hosting a separate overview shell
- when the active workbench is AI, the global sidebar should not keep rendering duplicated AI child trees or the bottom system-management block; AI-specific function switching, session history, and tool-entry affordances belong inside the AI workbench page chrome so the right-side canvas can stay focused on conversation flow
- delivery catalog writes such as business lines, environments, application-environment bindings, workflow templates, and registry connections must enforce backend permission keys, not just frontend button hiding
- build-template reads/writes must enforce explicit `delivery.build-templates.(view|manage)` permission keys, and delivery navigation visibility should include build templates, workflow templates, release board, business lines, environments, and application-environment bindings through backend menu/permission resolution rather than relying on “unmapped menu” fallback
- AI settings now split into `settings.ai.view` and `settings.ai.manage`; the provider form and copilot control-plane sections must stay consistent with those keys
- AI observability center now uses a two-layer IA: `/ai-observe` is the AIOps overview entry, while `/ai-observe/workbench`, `/ai-observe/operations`, and `/ai-observe/tools` own investigation, inspection/automation, and tool/skill workflows
- Root cause analysis, performance analysis, and AI chat are now mode switches inside the single `/ai-observe/workbench` investigation workspace; they must not be reintroduced as separate first-class sibling menus in navigation
- AI investigation is now session-first: `ai_sessions.metadata` carries mode, scope, toolset, tags, summary, archive status, and analysis run references, and the workbench must treat a session as the primary investigation object
- AI workbench message flows now return structured envelopes with messages, tool calls, analysis artifacts, and session patch hints instead of plain assistant text only
- MCP capability control is now dual-entry: Settings > AI remains the global control plane for provider, adapters, data sources, profiles, and policies, while the AIOps workbench exposes session-level temporary toolset assembly
- `metrics.v1` and `traces.v1` have moved from registry-only placeholders to real execution backends, with Prometheus-backed metric analysis and Jaeger-backed trace hotspot analysis expected to remain available to the workbench
- AI automation policies can now declare supported analysis kinds (`root_cause`, `performance`, `trace`) instead of implying root-cause-only orchestration
- settings center should consistently use `settings.<domain>.view` for route visibility and `settings.<domain>.manage` for save/update actions instead of mixing permission keys with legacy admin-only checks
- system management should follow the same split: `system.<module>.view` for page access and `system.<module>.manage` for destructive or mutable actions such as session revocation, announcements, and menus
- audit logs now remain the durable broad-scope read/write/deny trail, while operation logs are a separate durable stream for authorized mutable actions only; `/api/v1/audit/logs` and `/api/v1/operations/logs` are principal-aware backend-authorized reads, and operation-log rows now persist actor/request context plus backend-owned `target_scope` payloads instead of frontend-only placeholders
- announcement management is now a real publish workflow instead of a draft-only CRUD surface: managers can create, edit, publish, withdraw, and delete announcements; publish resets per-user read receipts, and withdraw removes the announcement from the user inbox without deleting the historical publish timestamp
- the console shell header now exposes a bell-driven announcement center for users with `system.announcements.view`; the inbox is backed by persisted `announcement_receipts`, auto-opens the highest-priority unread published announcement once per shell load, and an announcement marked read must stop auto-popup behavior across refresh and re-login until it is explicitly re-published
- access control should remain visible as a top-level console menu entry for admins, while its child pages can stay as nested routes beneath that entry
- settings center is a single top-level menu with in-page tabs for identity and AI; cluster-level monitoring configuration should not remain as a separate settings-center submenu
- settings center now includes a branding tab for console-level brand assets and title metadata; branding settings are distinct from cluster-level monitoring settings and should be applied globally in the web shell
- the console shell theme keeps a fixed kubecrux theme variant with brand overrides, while the header may expose a light/dark mode toggle as a user preference; theme-brand switching should still stay disabled unless it is intentionally restored end-to-end
- frontend theme customization now uses `web/src/theme/app-theme.ts` as the single source for both antd `ThemeConfig` and shared `--kc-*` CSS variables; avoid duplicating theme tokens in `main.tsx` or standalone style files
- the console visual baseline has shifted from the older purple brand palette to a neutral shadcn-like grayscale palette, while still preserving light/dark mode support and shared CSS variable contracts for non-antd surfaces
- shared platform filters such as resource scope and workload search bars should use compact, square-edged controls rather than pill-shaped fields
- frontend migration baseline is now antd-first: new or migrated pages must import directly from `antd` and `@ant-design/icons`
- native antd migration is complete. Do not reintroduce the retired UI system's packages, compat layers, legacy token names, or wrapper APIs into the active `web` application.
- antd-first is the stable frontend baseline; `platform`, `access`, `delivery`, `observability`, `copilot`, `system`, `settings`, docs-facing web modules, plus the remaining shared/auth/routes tail files have all converged on native `antd` and `@ant-design/icons`
- active `web/src` code should stay free of retired design-system naming and semantics; keep any remaining history confined to cleanup work only
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
- when frontend theme or design-system tasks land, record whether any legacy naming or compatibility residue was removed and keep active code free of Semi Design references

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
Reusable repo-specific Codex skills may also live under `.agents/skills/` when the repository wants frontend, backend, or deployment guidance versioned alongside the codebase.

- `.codex/state/current_task.md` is the canonical task snapshot for any spawned thread
- `.codex/state/queue.md` tracks subtask ownership, priority, status, and dependencies
- `.codex/state/results/` stores concise role outputs instead of full transcripts or raw long logs
- `.codex/handoffs/` stores explicit handoff notes between `main`, `coder`, `tester`, and `reviewer`
- `.codex/prompts/` stores reusable role prompt templates for child threads
- `.agents/skills/` stores repo-local skill definitions and bundled references or assets for kubecrux-specific workflows such as frontend, backend, and deployment work
- `.agents/` should not duplicate `.codex/` task snapshots, handoffs, queues, or prompt templates; subagent execution state belongs under `.codex/` only
- multi-track migrations should assign disjoint write ownership by directory or module family in `queue.md`; shared foundation ownership must be resolved before compat-file deletion or docs migration begins
- child threads must not assume access to the full parent-thread conversation; they should rely on `current_task`, relevant handoff files, result files, and the referenced code files
- handoffs should pass only the minimum necessary context: current task summary, exact files to read, verification status, open risks, and one recommended next step
- when command output is long, keep only a concise summary plus the minimal failing or trailing excerpt needed for the next role
- threads should keep changes scoped to the assigned task and avoid widening work without updating the queue or handoff
- When the user provides a new feature request or bug report in natural language, the main orchestrator must first convert it into a concrete current_task snapshot before spawning coder/tester/reviewer subagents.
