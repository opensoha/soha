# kubecrux

kubecrux is a multi-cluster Kubernetes platform console. The frontend is a Vite + React single-page application, the backend is a Go modular monolith, and the documentation site is maintained in-repo with Docusaurus.

## Repository Layout

- `web`: React 18 + Vite 6 + TypeScript 5 console
- `cmd`: backend server and agent entrypoints
- `configs`: backend and agent configuration files
- `internal`: API, application, policy, infrastructure, and repository layers
- `migrations`: SQL bootstrap and schema migrations
- `docs`: Docusaurus documentation site

## Frontend Summary

- runtime: React 18, Vite 6, TypeScript 5, React Router 6
- data and local state: TanStack Query 5, Zustand 5
- UI stack: Ant Design 6, Tailwind CSS 4, Ant Design Charts, ECharts legacy transition
- shell model: Ant Design `Layout` + `Menu` + `Breadcrumb` with route metadata driven navigation
- theme model: fixed console theme baseline with persisted user light/dark preference
- styling model: Tailwind utilities complement shared global shell and page styles
- bootstrap: `web/src/main.tsx` creates the Query client and `BrowserRouter`
- route registry: `web/src/routes/index.tsx` lazy-loads page modules by named export
- navigation metadata: `web/src/routes/meta.ts` drives sidebar groups and breadcrumbs
- shell layout: `web/src/layouts/app-layout.tsx`
- page modules: `web/src/features/*`
- page bundling: `workloads-pages.tsx`, `network-storage-pages.tsx`, and `extensions-pages.tsx` each export multiple route-level pages intentionally
- API client: `web/src/services/api-client.ts` uses same-origin `/api/v1` and retries once after refresh
- persisted client state: `web/src/stores/auth-store.ts`, `web/src/stores/platform-scope-store.ts`, `web/src/stores/preferences-store.ts`

## Current Console Surface

- overview:
  - `/`
- platform:
  - `/clusters`
  - `/clusters/:clusterId`
  - `/cluster-resources/nodes`
  - `/cluster-resources/nodes/:nodeName`
  - `/cluster-resources/namespaces`
  - `/workloads/deployments`
  - `/workloads/pods`
  - `/workloads/statefulsets`
  - `/workloads/daemonsets`
  - `/workloads/jobs`
  - `/workloads/cronjobs`
  - `/network/services`
  - `/network/ingresses`
  - `/network/gateways`
  - `/storage/persistentvolumeclaims`
  - `/storage/persistentvolumes`
  - `/storage/storageclasses`
  - `/extensions`
  - `/helm/releases`
  - `/helm/charts`
- delivery:
  - `/applications`
  - `/workflows`
  - `/releases`
  - `/registries`
- observability:
  - `/observability/monitoring`
  - `/observability/alerts`
  - `/observability/notifications`
  - `/observability/oncall`
  - `/observability/events`
  - `/chat`
- control plane:
  - `/access/users`
  - `/access/roles`
  - `/access/teams`
  - `/access/policies`
  - `/system/online-users`
  - `/system/announcements`
  - `/system/menus`
  - `/system/audit`
  - `/system/operations`
  - `/settings/identity`
  - `/settings/monitoring`
  - `/settings/ai`
  - `/docs`

## Backend Summary

- `cmd/server`: HTTP API entrypoint
- `cmd/agent`: agent-mode cluster connector
- `internal/api`: routes, handlers, middleware, DTOs, and HTTP responses
- `internal/application`: use-case orchestration
- `internal/policy`: RBAC, ABAC, and scope evaluation
- `internal/infrastructure`: config, logger, PostgreSQL, Redis, Kubernetes, Swagger, and MCP
- `internal/repository`: durable persistence
- `internal/bootstrap`: dependency graph, migration, seed, and runtime startup

## Configuration

### Backend

Backend configuration is file-first.

- primary file: [configs/config.yaml](configs/config.yaml)
- optional override: `KC_CONFIG_FILE=/abs/path/to/config.yaml`

### Agent

- primary file: [configs/agent.config.yaml](configs/agent.config.yaml)
- optional override: `KC_AGENT_CONFIG_FILE=/abs/path/to/agent.config.yaml`

### Frontend

The current frontend is same-origin by default.

- API base path is fixed to `/api/v1` in [web/src/services/api-client.ts](web/src/services/api-client.ts)
- local development proxies `/api` to `http://localhost:8080` through [web/vite.config.ts](web/vite.config.ts)
- the in-app docs page opens the Docusaurus site at `/docs/`

If you deploy the SPA and API on separate origins, add a reverse proxy or adjust the frontend client implementation.

## Local Development

### Dependencies

#### PostgreSQL

- Host: `localhost`
- Port: `5432`
- Database: `kubecrux`
- Username: `pgsql`
- Password: `pgsql`

### Start Backend

```bash
docker compose up -d postgres
go run ./cmd/server
```

### Start Agent

```bash
go run ./cmd/agent
```

### Start Frontend

```bash
cd web
npm install
npm run dev
```

### Start Docs

```bash
cd docs
npm install
npm run dev
```

The Docusaurus dev server is served at `http://localhost:3000/docs/`.

Optional shortcuts:

```bash
make dev-api
make dev-web
make dev-docs
```

## Runtime Notes

- password login and OIDC callback exchange are both handled in the SPA
- workloads, network, storage, and extensions reuse persisted cluster and namespace scope from Zustand
- the docs page can be opened inside the console or directly through `/docs/`
- the backend still owns auth, authorization, audit, cluster registry, monitoring, delivery, MCP, and agent integration

## Documentation

Primary architecture and operations documents live in `docs/`.

- [Engineering Spec](agents.md)
- [Architecture Entry](docs/architecture/index.md)
- [Application Delivery](docs/architecture/application-delivery.md)
- [Monitoring And Alerting](docs/architecture/monitoring-and-alerting.md)
- [AI Copilot](docs/architecture/ai-copilot.md)
- [Authorization](docs/architecture/authorization.md)
- [Configuration](docs/operations/configuration.md)
- [Agent Runtime](docs/operations/agent-runtime.md)
- [MCP](docs/operations/mcp.md)
