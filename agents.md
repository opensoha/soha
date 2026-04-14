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
- UI system: Semi Design
- utility styling: Tailwind CSS 3
- charts/visualization: ECharts
- test baseline: Vitest

### Docs

- in-repo docs site: VitePress
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

### 6.2 UI and State Rules

- Semi Design is the primary component system
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
- pod detail is now expected to be an operational workspace, not only a static detail page
- workload list pages should support search/filter first, then batch action surfaces where backend capability already exists
- platform overview should expose cluster-aware pod runtime cards instead of only fleet and alert counters
- service pages should evolve from plain tables to operational workspaces when selector, metrics, and event context already exist
- page bundles may export multiple route-level pages until reuse pressure justifies further splitting
- frontend consumes aggregated platform views only

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
