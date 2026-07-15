# soha Engineering Spec

## 1. Purpose

This file is the repository-level engineering memory for `soha`.
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

`soha` is a multi-cluster Kubernetes platform console.
It is not only a resource viewer. It is intended to become a unified control plane for:

- platform management
- Kubernetes dashboard capability
- application delivery
- observability and alert collaboration
- access control
- system management
- AI-assisted analysis

Project summary:

- `soha` is a multi-cluster Kubernetes platform console
- backend baseline: Go + Gin + PostgreSQL + `client-go`
- frontend baseline: React 18 + TypeScript 5 + Ant Design 6 + Vite + React Router + TanStack Query 5 + Zustand 5
- docs baseline: Docusaurus

## 3. Engineering Baseline

### Backend

- language: Go
- HTTP framework: Gin
- persistence: PostgreSQL
- runtime coordination: PostgreSQL-backed durable records and in-process state
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

- docs source repository: `github.com/opensoha/soha-docs`
- architecture documents live in the docs repository under `architecture/`
- this file is the top-level execution memory

### Deployment

- `deploy/Dockerfile` owns the single-project image build path
- `deploy/docker-compose.yaml` owns local app-plus-PostgreSQL startup and optional Hermes runner services
- `deploy/deployment.yaml` owns the raw Kubernetes manifest baseline
- `deploy/kustomization.yaml` owns the lightweight Kustomize entrypoint for raw manifest customization
- Helm chart sources live in `opensoha/soha-helm`

## 4. System Architecture

The system is organized as a modular monolith with clear layers.

1. Presentation layer
   - `github.com/opensoha/soha-web` frontend console source
   - `github.com/opensoha/soha-docs` documentation site source
   - `internal/staticassets/web/dist` embedded web artifact staging path
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
   - config, logger, kubernetes, informer, database, mcp, swagger
6. Repository layer
   - `internal/repository`
   - durable storage access
7. External systems
   - Kubernetes clusters
   - PostgreSQL
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

The frontend active baseline is the Vite application in `github.com/opensoha/soha-web`. The `soha` repository consumes only the built `dist` artifact.

- active source repository: `github.com/opensoha/soha-web`
- `src/App.tsx`
  - app bootstrap and theme application
- `src/layouts`
  - shared shell layout and navigation chrome
- `src/routes`
  - React Router route registration and route metadata
- `src/features`
  - soha business logic and route-level feature implementations
- `src/components`
  - shared antd primitives and complex reusable widgets
- `src/services`
  - API clients and auth/runtime helpers
- `src/stores`
  - auth, preference, platform scope state
- `src/utils`
  - cross-page helpers and theme/branding helpers

Current structure summary:

- `deploy/Dockerfile`, `deploy/docker-compose.yaml`, `deploy/deployment.yaml`, and `deploy/kustomization.yaml` are the canonical repo-local deployment assets
- `github.com/opensoha/soha-web` is the only active frontend source target
- `internal/staticassets/web/dist` is the only web artifact path embedded by `soha`
- `src/routes/**` defines the active route surface for the Vite app
- `src/features/**` provides soha business logic behind that route surface
- `internal/api`, `internal/application`, `internal/policy`, `internal/infrastructure`, and `internal/repository` keep strict backend layer responsibilities
- `github.com/opensoha/soha-docs` owns the public architecture document set

### 6.2 UI and State Rules

- Ant Design is the primary component system
- custom theme tokens under `soha-web/src/theme/app-theme.ts` remain the single source for light/dark/system mode and shared CSS variables
- Zustand stores lightweight local UI/runtime preferences
- TanStack Query owns server data lifecycle
- route metadata in `soha-web/src/routes/meta.ts` drives navigation, breadcrumb, and permission-aware route behavior
- platform pages must share persisted cluster/namespace scope

#### 6.2.1 Ant Design LLM Context

Keep this Codex prompt active for frontend work:

`阅读 https://ant.design/llms-full.txt 并理解 Ant Design 组件库，在编写 Ant Design 代码时使用这些知识。`

Use official Ant Design LLM resources as the reference source before writing or refactoring `antd` code:

- `https://ant.design/llms.txt` for the documentation index
- `https://ant.design/llms-full.txt` and `https://ant.design/llms-full-cn.txt` for full component documentation
- `https://ant.design/llms-semantic.md` and `https://ant.design/llms-semantic-cn.md` when customizing semantic DOM slots, `classNames`, or `styles`
- component Markdown docs such as `https://ant.design/components/button.md` or `https://ant.design/components/button-cn.md`, replacing `button` with the target component route
- component semantic docs such as `https://ant.design/components/button/semantic.md` or `https://ant.design/components/button-cn/semantic.md` when component structure matters

Do not rely only on memory for component APIs. Confirm Ant Design 6 props, tokens, semantic slots, and examples through official LLM docs or local antd knowledge tools before changing UI.

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
- application groups
- application-scoped environment tags
- application environments
- workflow templates
- release board
- workflows
- releases
- registries

Design expectation:

- delivery remains platform-native
- application delivery now centers on four stable objects: applications, build templates, application-environment bindings, and execution records
- the next enterprise execution-plane baseline is now started in code: `release_bundles`, `execution_tasks`, `execution_logs`, and `execution_callbacks` exist as first-class delivery control-plane objects; approvals are modeled as workflow template `manual_approval` nodes and `workflow_approvals`
- applications may own multiple build sources (`repo_dockerfile`, `platform_build_template`, `external_pipeline`), while each application-environment binding selects one concrete build source plus one workflow template and one or more explicit release targets
- the developer/tester-facing DevOps workbench design is now application-centered: applications contain service components, services contain container definitions, environments carry per-service runtime bindings, and CI/CD DAGs produce immutable release bundles plus service/container-level artifacts
- development flows prioritize self-service branch or commit build, single-service deploy, logs, events, retry, rollback, and dev/test environment feedback from the application detail workspace
- testing flows prioritize environment matrices, candidate release bundles, service/version diff, test plans, test runs, reports, quality gates, and promotion decisions before pre-production or production release
- application services and service containers are the next delivery-domain model expansion; they should map to existing build sources and release targets first instead of replacing `applications`, `application_build_sources`, `application_environments`, or `release_targets` abruptly
- application service and container CRUD now has a first backend/frontend baseline: `application_services` owns service components under an application, `application_service_containers` owns service-local containers, and `/api/v1/applications/:applicationID/services` is the stable management surface consumed by the application detail workspace
- CI/CD workflow templates should evolve from `release_dag` toward a compatible `delivery_dag` schema with explicit node inputs/outputs, service selectors, environment selectors, parallel branches, failure branches, approval nodes, rollback nodes, and artifact-producing build/test/deploy nodes
- release bundles remain the immutable version unit and must be able to expose images, packages, SBOM/signing data, test reports, scan reports, rollout evidence, and service/container source metadata without requiring users to parse raw execution-task JSON
- application-environment bindings now also carry strategy/promotion/approval/artifact policy references, and release targets now carry `targetKind`, `executorKind`, `groupKey`, `waveKey`, `regionKey`, and `configRef` as the stable enterprise target contract
- legacy top-level application build fields may remain as backend compatibility/migration inputs, but the active application-center UI should no longer expose them as the primary editing surface once `buildSources` is available
- application-environment binding `buildPolicy` and `releasePolicy` are now structured contracts rather than free-form JSON blobs; the frontend should edit them with typed controls instead of raw JSON textareas wherever possible
- platform-managed build templates are now a first-class delivery object with arbitrary build-command payloads, but those commands are only allowed inside the dedicated build worker path and must never execute inline in API handlers
- release-board and application/application-environment detail pages now prefer backend aggregate endpoints instead of frontend fan-out joins across builds, workflows, and releases
- the application detail workspace now has a P1 self-service delivery action entrypoint backed by `POST /api/v1/applications/:applicationID/delivery-actions`; build, deploy, build_deploy, workflow, and verify actions are orchestrated in the delivery application service, while verify runs through workflow validation mode that filters the bound DAG to validation/check nodes only
- workflow `manual_approval` now pauses runs with status `waiting_approval`, and approve/reject decisions persist to `workflow_approvals`
- build and release entrypoints now begin dual-writing `releaseBundleId` and `executionTaskId`, so the execution plane can evolve independently from the current synchronous delivery path
- the active delivery web surface now exposes minimal enterprise control-plane pages for release bundles, execution tasks, and execution logs so the new execution-plane objects are inspectable from the console instead of remaining backend-only
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
- standalone business line and delivery environment management are removed from the active delivery IA, HTTP surface, permission/menu surface, and catalog repository CRUD contracts; application grouping and application-scoped environment tags are maintained from the application center, while historical `businessLineId` and global `delivery_environments` data remain compatibility scope data rather than user-facing master data
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
- alert integrations are Soha-owned ingress adapters and registry records: keep the legacy normalized webhook, add provider-native Alertmanager/Grafana/Generic Webhook normalization, but do not reimplement a full external alerting engine inside Soha
- on-call collaboration follows a Grafana IRM-style model: alert integrations enter ordered on-call routes, routes match alert labels/context, grouping keys produce alert groups, and matched routes target escalation chains or schedules
- the on-call workspace is alert-task-first: the primary surface must be generated from active alert events and backend route resolution, while manual route parsing belongs to diagnostics or route editing rather than the first-screen workflow
- on-call routes are backend-owned operational contracts exposed through `/api/v1/oncall/routes` with `/api/v1/oncall/assignment-rules` kept as a compatibility alias; business line, service, and duty role (`dev`, `qa`, `ops`, `sre`, `security`, `owner`) are optional route match labels rather than the primary IA
- alert notification and self-healing approval flows may resolve current on-call from explicit notification policy `oncallRef` first, then fall back to matching IRM routes derived from alert event integration metadata and labels such as `businessLineId`, `alertCategory`, `service`, and `role`

### 7.4 Access / System / Settings

- user, role, team, policy management
- scope grants
- online users
- announcements
- menus
- audit logs
- operation logs
- login settings
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
- identity bootstrap baseline is a single `admin / soha` seed from `auth.dev_principal`; legacy bootstrap migration and login fallback are removed
- PostgreSQL bootstrap schema is now consolidated into `migrations/postgres/0001_init.sql`; duplicate legacy root migration mirrors are migration debt and should stay removed
- deployment assets pin pgvector 0.8.5 on PostgreSQL 18.4, enable `vector` and `pg_trgm`, and preload `pg_stat_statements`; compose, raw Kubernetes, and Helm must mount persistent data at `/var/lib/postgresql` because PostgreSQL 18 stores the default `PGDATA` below `/var/lib/postgresql/18/docker`
- local `make dev` startup no longer creates or manages Kubernetes clusters; users provide their own cluster and register it explicitly when needed
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
- Helm Charts is now backend-backed through `/api/v1/clusters/:clusterID/helm/charts` using Artifact Hub package search/detail/default-values APIs; the console opens a right-side package/install drawer, and direct-kubeconfig clusters can install charts through the Helm SDK while agent-connected clusters must surface install as unsupported
- Helm Chart install must preflight same-namespace Helm release history before calling the SDK; an occupied release name is an operator input conflict, not cluster unavailability, but a same release/chart/version record already in `deployed` state should be treated as an idempotent install success and the console must reconcile observed deployed releases instead of showing a false failure
- Helm Releases must expose values.yaml view/edit/diff/apply and release delete as operational actions; values update and uninstall are direct-kubeconfig Helm SDK capabilities, agent-connected clusters must show them as unsupported, and destructive release delete should use lightweight inline confirmation such as antd Popconfirm instead of a modal dialog
- Helm Release values.yaml should use a fixed two-panel editor: the left panel is the editable values draft, the right panel shows Helm's current runtime values with automatic diff against the draft; avoid separate Edit/Diff mode buttons for this workflow
- network platform navigation no longer exposes HTTP Routes; unsupported resource families should be removed end-to-end from routes, menu seeds, and API handlers instead of staying as dead entries
- network now lands on a `网络拓扑` workspace before the raw resource lists; the topology view may combine live ingress, Gateway API, service, and backend relationships with explicitly pending route placeholders, but it must not present missing backend aggregation as if it were verified
- topology preview pages may fall back to clearly labeled demo traces when the current scope has no live entry path, so interaction and layout review do not depend on a pre-populated cluster
- network topology should use one layered left-to-right graph for entry, route, service, and backend relationships across both ingress-controller routes and Gateway API HTTPRoutes; gateways without visible HTTPRoute bindings may remain in the same graph as pending-route placeholders, while backend pods may stay collapsed into summary nodes until drill-down
- platform overview should expose cluster-aware pod runtime cards instead of only fleet and alert counters
- platform overview runtime cards must consume a backend workload overview aggregation endpoint and keep the active cluster/namespace scope visible; frontend should not fetch all Pod rows just to render dashboard summaries
- service pages should evolve from plain tables to operational workspaces when selector, metrics, and event context already exist
- cluster management remains the registration surface, but each cluster row should drill into a lightweight detail page for labels, version, health, and handoff into node operations
- cluster detail pages should keep compact page-scoped card spacing, and node snapshot tables should avoid redundant explanatory copy once the node drill-down action is clear
- cluster registration forms should not ask operators to hand-enter cluster IDs; backend registration generates the stable ID automatically, and the direct-kubeconfig form should avoid exposing kube context unless an explicit context-selection workflow is restored end-to-end
- cluster onboarding should present cloud or distribution choices such as `standard_kubernetes`, `gke`, `ack`, `tke`, and `aks` as provider-style metadata for UI display and filtering, while backend `region` semantics remain reserved for policy/runtime data
- platform resource metrics should default to global monitoring settings, may accept cluster-level Prometheus/Grafana overrides from cluster registration data when present, and should not imply automatic in-cluster Prometheus discovery when operators have not configured either path
- node resources should expose a standalone node detail workspace with YAML, labels/taints editing, and scheduled pod context instead of limiting all actions to list-row modals
- page bundles may export multiple route-level pages until reuse pressure justifies further splitting
- frontend consumes aggregated platform views only
- access control pages must bind to backend's real role/user/group/policy schema instead of placeholder table columns or fake form fields
- user create/update must persist role bindings and user-group bindings in the same submission so RBAC/scope decisions take effect immediately
- user-facing terminology under access control is now `组织`; persistence, API compatibility, and policy matcher internals may continue using `team/teams` while organization-tree fields such as parent, path, source, and external ID carry the enterprise org semantics
- authenticated frontend navigation must consume a backend permission snapshot instead of relying on static route visibility alone
- backend permission snapshots and console/API permission checks now resolve from persisted role `permissionKeys`; static built-in role maps remain bootstrap defaults only and custom roles must be able to drive backend authorization without `admin` special-casing
- menu visibility is now a conjunction of backend visible menu bindings and frontend route permission keys, while page buttons should progressively consume either permission keys or backend `allowedActions`
- authenticated sidebar navigation is now backend-menu-driven instead of route-group-driven: `PermissionSnapshot.visibleMenus` must carry menu labels, `iconKey`, `section`, `sortOrder`, and `enabled`, and the web shell must build foldable parent/child navigation from that tree instead of flattening `system` / `access` / `settings` children into static route groups
- menu sections are now user-extensible from menu management itself: the section field may reuse known defaults or accept a new raw section key, and unknown section keys must still remain valid runtime group buckets with raw-label fallback in both the sidebar and menu-management filters
- menu-management changes to parent-child structure, icon key, section, sort order, enabled state, or explicit visibility overrides must invalidate the current permission snapshot so the active shell reflects the new navigation tree immediately; parent menus act as expand/collapse containers and leaf menus own navigation clicks
- system workspace entry is now header-driven: the shell header exposes a settings icon beside the announcement bell, and entering `/system` or `/settings` should reuse the main left navigation column for system/access/settings menus instead of rendering a separate bottom-left system-management dock
- access management and scope-grant CRUD must use explicit `access.*.(view|manage)` permission keys; scope-grant list/create/update/delete are principal-aware backend operations and are no longer safe to leave authenticated-only
- sidebar sibling ordering should honor backend visible-menu sort within each frontend group so menu-management sort changes affect the console without duplicating section headers
- monitoring and copilot APIs are no longer implicitly open to any authenticated user; user-facing reads and writes must check permission keys before hitting repository operations
- observability and AI pages should treat route visibility, button visibility, and backend API authorization as three separate gates that must stay aligned
- monitoring OnCall is production-usable as an IRM routing surface: the `值班协同` page must lead with active on-call tasks derived from alert events, then expose integration-aware routes, grouping keys, escalation chains, schedules, and rotations; route resolution must prefer backend matching over frontend-only filtering
- the user-facing Kubernetes operations workbench name is `k8s工作台`; the internal workbench id may remain `platform` for compatibility, but visible shell, module, permission-catalog, and documentation labels should not regress to `平台工作台`
- the network workspace now exposes Gateway API as a grouped menu with GatewayClasses, Gateways, HTTPRoutes, BackendTLSPolicies, GRPCRoutes, and ReferenceGrants; these resources are optional CRDs, so list surfaces must distinguish an installed-but-empty scope from an unavailable Gateway API family without crashing the whole network menu
- AI工作台 and 监控工作台 are first-class workbench switcher entries; their child menus belong inside their own workbench trees and must not remain duplicated under k8s工作台 / resource navigation
- Docker 工作台 is now a first-class, module-gated resource workbench at `/docker`; it owns Docker host inventory, PVE-backed quick host provisioning requests, Compose project management, service inventory, port mapping, templates, and Docker operation records, and its menus/routes/permissions must stay aligned with `modules.docker.enabled` and `docker.*.(view|manage|deploy)` permission keys
- Docker 工作台 control-plane APIs persist desired state and enqueue operations; Docker Engine and Compose execution must stay outside API handlers and run through the token-protected agent runner claim/callback path at `/api/v1/docker/operations/claim`, `/api/v1/docker/operations/:id/runner-status`, and `/api/v1/docker/operation-callbacks`
- The soha agent can optionally enable `control_plane.docker` to claim Docker operations, materialize Compose workspaces under `compose_root`, run whitelisted `docker compose` actions, and callback logs/runtime service state to the Docker operation record
- Docker 工作台 single-container startup is modeled as a generated `single_container` Compose project plus service and port-mapping records; domain access belongs to `docker_port_mappings` via `domainName`, `domainScheme`, `domainTlsEnabled`, and `accessUrl`, and generated Compose may include Traefik labels when a domain is provided while DNS/reverse-proxy ownership remains external to API handlers
- Docker 工作台 should remain independent from the virtualization workbench, but its host quick-create flow may call the virtualization application service through a narrow `HostProvisioner` adapter when a `virtualizationConnectionId` is provided, so PVE/KubeVirt VM creation is queued in the virtualization worker and linked back to the Docker host provision operation
- AI工作台根入口 `/ai-workbench` is now the canonical session-first investigation surface; legacy `/ai-workbench/investigation` and `/ai-observe/workbench` paths should only remain as compatibility redirects instead of hosting a separate overview shell
- when the active workbench is AI, the global sidebar should not keep rendering duplicated AI child trees or the bottom system-management block; AI-specific function switching, session history, and tool-entry affordances belong inside the AI workbench page chrome so the right-side canvas can stay focused on conversation flow
- delivery catalog writes such as application-environment bindings, workflow templates, and registry connections must enforce backend permission keys, not just frontend button hiding
- build-template reads/writes must enforce explicit `delivery.build-templates.(view|manage)` permission keys, and delivery navigation visibility should include build templates, workflow templates, release board, and application-environment bindings through backend menu/permission resolution rather than relying on “unmapped menu” fallback
- AI settings now split into `settings.ai.view` and `settings.ai.manage`; the provider form and copilot control-plane sections must stay consistent with those keys
- AI observability center now uses a two-layer IA: `/ai-observe` is the AIOps overview entry, while `/ai-observe/workbench`, `/ai-observe/operations`, and `/ai-observe/tools` own investigation, inspection/automation, and tool/skill workflows
- Root cause analysis, performance analysis, and AI chat are now mode switches inside the single `/ai-observe/workbench` investigation workspace; they must not be reintroduced as separate first-class sibling menus in navigation
- AI investigation is now session-first: `ai_sessions.metadata` carries mode, scope, toolset, tags, summary, archive status, and analysis run references, and the workbench must treat a session as the primary investigation object
- AI workbench message flows now return structured envelopes with messages, tool calls, analysis artifacts, and session patch hints instead of plain assistant text only
- MCP capability control is now dual-entry: Settings > AI remains the global control plane for provider, adapters, data sources, profiles, and policies, while the AIOps workbench exposes session-level temporary toolset assembly
- soha AI Gateway is the external AI-native operations entry point for the standalone `soha` CLI, MCP clients, and AI coding agents; it must expose caller-specific capabilities through `/api/v1/ai-gateway/capabilities` while keeping real actions inside backend application services, permission checks, scope grants, risk policy, and audit logging
- AI Gateway is a standalone workbench at `/ai-gateway` with module id `aiGateway` and menu id `ai-gateway`; `/ai-workbench/gateway` may exist only as a hidden compatibility redirect and must not be reintroduced as a formal AI工作台 child route, seed menu, doc path, or test expectation
- AI Gateway console navigation is route/menu driven under its own workbench: `/ai-gateway` redirects to `/ai-gateway/overview`, and `overview`, `manifest`, `clients`, `tokens`, and `governance` are child menus seeded by the backend and rendered by the shared antd sidebar instead of page-local top-level Tabs
- AI工作台 remains focused on investigation, chat, root-cause/performance analysis, inspection, tools/skills, and AI settings; Gateway-oriented external clients, MCP access, tokens, service accounts, grants, policies, approvals, governance, and audit belong to the standalone AI Gateway workbench
- AI Gateway reuses AI Workbench, MCP adapters, Agent Runtime, and the execution plane as capability foundations, but it is a separate security and protocol layer for external callers; Gateway tool grants and skills can only narrow access and must not bypass `permissionKeys`
- AI Gateway tool invocation must enter through `/api/v1/ai-gateway/tools/:toolName/invoke`, re-check `ai.gateway.invoke` plus the owning domain permission keys on every request, and route to the owning application service; delivery tools route through delivery/application services, and k8s read-only diagnosis tools route through the platform resource service
- AI Gateway introduces `ai.gateway.view`, `ai.gateway.invoke`, and `ai.gateway.manage`; these keys must be combined with the owning domain permissions such as `delivery.*`, `workspace.resource.view`, or `platform.*` before any MCP tool is exposed or invoked
- AI Gateway MCP tool grants are a narrowing layer: no grant keeps the `permissionKeys` baseline, any allow grant creates an allow-list for the subject/client, and deny grants must always take precedence
- AI Gateway tool-grant management must support `user`, `service_account`, `role`, `team`/organization, and `ai_client` subjects; runtime evaluation combines current subject grants, organization grants, role grants, and AI-client grants before exposing manifest tools or invoking a tool
- AI Gateway access policies and `ai_gateway_skill_bindings` are now runtime controls, not only schema placeholders: access policies can deny or allow-list tools/skills by subject, organization, role, AI client, tool pattern, skill id, and risk level, while skill bindings narrow exposed skills and capability refs without granting new `permissionKeys`
- AI Gateway personal and service-account tokens are opaque `soha_pat_` / `soha_sat_` credentials; only hashes and display prefixes may be persisted, token values are returned once, and parsed token permission caps must intersect with current role-derived `permissionKeys`
- The standalone `github.com/opensoha/soha-cli` repository owns the `soha` CLI. It may handle login, local profile/context config, Gateway capability inspection, and MCP stdio proxying, but it must not import `soha/internal/...` or call PostgreSQL, Kubernetes, Docker, execution runners, or delivery internals directly
- `soha` CLI local profiles must keep token files private (`0600` config file, `0700` config directory), never print full access/refresh/service tokens, and route MCP `tools/call` only through `/api/v1/ai-gateway/tools/:toolName/invoke` so backend permission checks, tool grants, risk policy, and audit stay authoritative
- soha AI Gateway product Skills live in the standalone `github.com/opensoha/soha-skills` repository; they are AI-readable workflows only, may be installed by `soha skill install`, and must never be treated as permission grants or a way to bypass Gateway manifest/tool authorization
- The standalone `github.com/opensoha/soha-agent` repository owns the remote cluster agent and Agent Runtime runner binary. It must consume control-plane behavior through HTTP, Agent protocol contracts, generated SDKs, or released artifacts, and must not import `soha/internal/...`
- AI Gateway k8s tools (`k8s.pods.*`, `k8s.deployments.list`, `k8s.services.list`, `k8s.events.list`) must stay read-only and route through `internal/application/resource`; release-failure diagnosis may aggregate delivery execution and runtime context, but Gateway must keep basic log redaction before returning MCP/CLI outputs
- AI Gateway tool invocations must dual-write generic audit logs and `ai_gateway_audit_logs`; the dedicated row should include actor, service-account or user identity, AI client, skill, tool, risk level, scope IDs, result, and related IDs, but not full tool input, raw logs, kubeconfig, environment variables, or token values
- AI Agent Runtime is now the stable abstraction for external agent execution: pages, automation policy, and business modules must depend on soha `AgentProvider`, `AgentRun`, `AgentCapability`, `AgentToolBinding`, `AgentSkillBinding`, toolset, analysis profile, and `AnalysisArtifact` contracts instead of calling Hermes or another provider directly
- Hermes Agent is only the first external provider behind the runner claim/callback path; future OpenClaw, internal agent, or third-party providers must be added by extending provider adapters, tool bindings, skill bindings, and runner executors, not by rewriting AI workbench pages or business analysis flows
- Agent Runtime capabilities should turn existing logs, metrics, traces, platform events, delivery context, on-call context, Docker context, and virtualization context into soha capability and MCP/tool entries; skills remain platform-level methodology definitions that may map to Hermes skills, MCP capabilities, prompt templates, or future provider-native skill systems
- Hermes provider execution should pass `AgentSkillBinding.providerSkillRef` to the Hermes CLI as the provider-native skill argument, falling back to soha skill ids only when no provider skill ref exists
- External Agent Runtime providers must invoke soha read-only tools through the runner `agent-runs/tool-call` gateway using the runner token and per-run callback token; they must not bypass soha data-source credentials, scope, toolset, or `AgentRun.toolBindings` snapshots
- `AgentRun.toolBindings` snapshots must be filtered by the creator or system principal's runtime permission keys before runner claim; provider tokens cannot expand tools, data sources, or business context beyond that snapshot
- Session/profile toolsets may reference exact adapter ids such as `logs.v1` or source-kind aliases such as `logs`, and adapter matching must remain explicit and test-covered
- Hermes/CLI provider POC may prefetch a small read-only tool context into the provider prompt; currently executable prefetch/tool-call backends include events, logs, metrics, traces, delivery releases, delivery builds, execution tasks, platform resource snapshots, Docker operation/service context, virtualization operations, alerts, and OnCall route resolution
- Agent Runtime runners must keep long-running provider commands alive with `running` heartbeat callbacks and must stop the local provider process if the control plane returns a terminal status such as `canceled` or `callback_timeout`
- Agent Runtime runner smoke coverage should preserve the full claim, running callback, tool-call prefetch, CLI provider execution, completed callback, and `AnalysisArtifact` protocol chain even when a real Hermes binary is unavailable locally
- future provider-native or MCP client tool protocols must still terminate at the same soha `agent-runs/tool-call` gateway
- Agent Runtime output must be normalized into soha `AnalysisArtifact` with evidence, hypotheses, recommendations, graph, tool-execution records, and data-source snapshots; runner-side synthesized artifacts must preserve provider structured fields plus runner tool records, and provider-native output should not leak directly into frontend contracts
- Continuous AI analysis is scheduled and audited by soha automation policy; Hermes cron or provider-native schedulers are optional experiments and must not become the platform source of truth for policy matching, dedup, cooldown, budget, permission, or audit behavior
- soha owns permissions, menus, audit, budget, data redaction, and operation boundaries for Agent Runtime. Agents are pluggable executors only, and high-risk write actions must still route through the owning module's durable operation or approval flow
- `metrics.v1` and `traces.v1` have moved from registry-only placeholders to real execution backends, with Prometheus-backed metric analysis and Jaeger-backed trace hotspot analysis expected to remain available to the workbench
- AI automation policies can now declare supported analysis kinds (`root_cause`, `performance`, `trace`, `inspection_review`) and select an `agentProviderId` instead of implying root-cause-only or internal-only orchestration
- settings center should consistently use `settings.<domain>.view` for route visibility and `settings.<domain>.manage` for save/update actions instead of mixing permission keys with legacy admin-only checks
- system management should follow the same split: `system.<module>.view` for page access and `system.<module>.manage` for destructive or mutable actions such as session revocation, announcements, and menus
- audit logs now remain the durable broad-scope read/write/deny trail, while operation logs are a separate durable stream for authorized mutable actions only; `/api/v1/audit/logs` and `/api/v1/operations/logs` are principal-aware backend-authorized reads, and operation-log rows now persist actor/request context plus backend-owned `target_scope` payloads instead of frontend-only placeholders
- announcement management is now a real publish workflow instead of a draft-only CRUD surface: managers can create, edit, publish, withdraw, and delete announcements; publish resets per-user read receipts, and withdraw removes the announcement from the user inbox without deleting the historical publish timestamp
- the console shell header now exposes a bell-driven announcement center for users with `system.announcements.view`; the inbox is backed by persisted `announcement_receipts`, auto-opens the highest-priority unread published announcement once per shell load, and an announcement marked read must stop auto-popup behavior across refresh and re-login until it is explicitly re-published
- settings center navigation exposes access users, roles, user groups, and policies as direct sibling menu entries; `/access` may remain a hidden route container, but the sidebar should not render `访问控制` as a collapsible parent
- system log navigation labels should use the full names `操作日志` and `审计日志` instead of shortened `操作` and `审计`
- settings center remains the system-workspace container entry, but login settings and branding settings now resolve as independent child menu routes under `/settings/login` and `/settings/branding` instead of sharing one tab-only surface
- login settings are now the active console identity-management surface: the route title is `登陆设置`, it supports concurrent provider configuration for OIDC, 飞书 OAuth2, 钉钉 OAuth2, 企业微信 OAuth2, generic OAuth2, and SAML metadata, and the login page should list every enabled third-party provider rather than assuming a single OIDC source
- the backend login-provider contract must stay backward-compatible with the legacy single-OIDC setting key while multi-provider settings are taking over; runtime OIDC flows may still reuse the legacy config resolver, but the stored source of truth is the multi-provider login settings document
- OIDC, OAuth2, 飞书, 钉钉, and 企业微信 login providers may supplement roles and organizations at successful login through `roleField`, `organizationField`, `syncRolesOnLogin`, `syncOrgsOnLogin`, and `append` or `replace_external` modes; this only matches existing local roles and organizations, and full third-party directory sync remains a separate future connector capability
- SAML is currently configuration-visible but runtime-incomplete: the settings model and menu/login entry may expose it, but the server must not imply that ACS/assertion handling is already implemented
- branding settings remain a dedicated console-level child menu under settings; they are distinct from cluster-level monitoring settings and should be applied globally in the web shell
- the console shell theme keeps a fixed soha theme variant with brand overrides, while the header may expose a light/dark mode toggle as a user preference; theme-brand switching should still stay disabled unless it is intentionally restored end-to-end
- frontend theme customization now uses `soha-web/src/theme/app-theme.ts` as the single source for both antd `ThemeConfig` and shared `--soha-*` CSS variables; avoid duplicating theme tokens in `main.tsx` or standalone style files
- the console visual baseline has shifted from the older purple brand palette to a neutral shadcn-like grayscale palette, while still preserving light/dark mode support and shared CSS variable contracts for non-antd surfaces
- shared platform filters such as resource scope and workload search bars should use compact, square-edged controls rather than pill-shaped fields
- frontend migration baseline is now antd-first: new or migrated pages must import directly from `antd` and `@ant-design/icons`
- native antd migration is complete. Do not reintroduce the retired UI system's packages, compat layers, legacy token names, or wrapper APIs into the active `web` application.
- antd-first is the stable frontend baseline; `platform`, `access`, `delivery`, `observability`, `copilot`, `system`, `settings`, docs-facing web modules, plus the remaining shared/auth/routes tail files have all converged on native `antd` and `@ant-design/icons`
- active `soha-web/src` code should stay free of retired design-system naming and semantics; keep any remaining history confined to cleanup work only
- docs migration baseline is Docusaurus-first; new docs-site work must target Docusaurus config, sidebars, and MDX component conventions instead of VitePress
- virtualization lab environments should treat KubeVirt and PVE as separate runtime planes: KubeVirt requires a user-provided Kubernetes cluster with KubeVirt/CDI installed, while PVE may run only as a full KubeVirt VM or as an external bare-metal host, Debian host, or nested lab VM connected through the PVE API
- PVE-in-Kubernetes is supported only through the KubeVirt VM lab path; privileged Pod experiments that mutate node kernel, networking, storage, or systemd behavior must not be documented as a valid runtime path
- local virtualization development no longer ships Mac-local KubeVirt/PVE make targets; Docker Desktop for macOS, especially Apple Silicon, is not a reliable validation path for nested virtualization, so KubeVirt and PVE functional validation should connect to real external servers or dedicated lab hosts
- KubeVirt and PVE control flows must remain regular backend operations that work without an AI provider; MCP skills are allowed only as an optional AI-assisted troubleshooting layer after model integration is configured

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
- update `github.com/opensoha/soha-docs` `architecture/*` when the public architecture description changes materially
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
- `github.com/opensoha/soha-docs` `architecture/index.md`
- `github.com/opensoha/soha-docs` `architecture/application-delivery.md`
- `github.com/opensoha/soha-docs` `architecture/authorization.md`
- `github.com/opensoha/soha-docs` `architecture/monitoring-and-alerting.md`

## 15. Codex Collaboration Baseline

This repository may use repo-local collaboration files under `.codex/` for isolated Codex threads or agents.
`agents.md` remains the engineering baseline; `.codex/` carries task execution context.
Reusable repo-specific Codex skills may also live under `.agents/skills/` when the repository wants backend or deployment guidance versioned alongside the codebase. Frontend guidance belongs in the sibling `soha-web` repository.

- `.codex/state/current_task.md` is the canonical task snapshot for any spawned thread
- `.codex/state/queue.md` tracks subtask ownership, priority, status, and dependencies
- `.codex/state/results/` stores concise role outputs instead of full transcripts or raw long logs
- `.codex/handoffs/` stores explicit handoff notes between `main`, `coder`, `tester`, and `reviewer`
- `.codex/prompts/` stores reusable role prompt templates for child threads
- `.agents/skills/` stores repo-local skill definitions and bundled references or assets for soha-specific backend and deployment workflows
- `.agents/` should not duplicate `.codex/` task snapshots, handoffs, queues, or prompt templates; subagent execution state belongs under `.codex/` only
- multi-track migrations should assign disjoint write ownership by directory or module family in `queue.md`; shared foundation ownership must be resolved before compat-file deletion or docs migration begins
- child threads must not assume access to the full parent-thread conversation; they should rely on `current_task`, relevant handoff files, result files, and the referenced code files
- handoffs should pass only the minimum necessary context: current task summary, exact files to read, verification status, open risks, and one recommended next step
- when command output is long, keep only a concise summary plus the minimal failing or trailing excerpt needed for the next role
- threads should keep changes scoped to the assigned task and avoid widening work without updating the queue or handoff
- When the user provides a new feature request or bug report in natural language, the main orchestrator must first convert it into a concrete current_task snapshot before spawning coder/tester/reviewer subagents.
