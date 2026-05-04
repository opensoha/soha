[English](./README.md) | [简体中文](./README-cn.md)

# kubecrux

> A multi-cluster Kubernetes platform console for platform operations, application delivery, observability, access control, and AI-assisted investigation.

kubecrux is a full-stack platform console built around a Go backend, a React + Ant Design frontend, and in-repo Docusaurus documentation. It is designed to grow beyond a resource viewer into a unified control plane for multi-cluster platform teams.

## Highlights

- Multi-cluster Kubernetes console with direct-cluster and agent-connected runtime models
- Platform management covering clusters, nodes, namespaces, workloads, network, storage, CRDs, and Helm
- Delivery domain for applications, workflows, releases, registries, and catalog-style master data
- Observability and AI workbench direction for alerts, events, root-cause analysis, and investigation workflows
- Access control and system management with permission-aware menus, audit logs, operation logs, and settings
- Single-project deployment assets for Docker, Docker Compose, raw Kubernetes YAML, and Helm

## Architecture

The repository follows a modular-monolith backend and a route-driven frontend shell.

### Backend

- `cmd/server`: API entrypoint
- `cmd/agent`: remote cluster agent entrypoint
- `internal/api`: routes, handlers, middleware, request parsing, response shaping
- `internal/application`: use-case orchestration and platform-facing view models
- `internal/policy`: RBAC, ABAC, and scope evaluation
- `internal/infrastructure`: config, database, Redis, Kubernetes, informer, agent, logger, Swagger, MCP
- `internal/repository`: durable persistence
- `internal/bootstrap`: dependency graph, migration, seed, and startup wiring

### Frontend

- `web`: active React 18 + TypeScript 5 + Vite console
- `web/src/routes`: route registry and route metadata
- `web/src/layouts`: shell layout
- `web/src/features`: route-level business modules
- `web/src/components`: shared reusable UI primitives and widgets
- `web/src/services`: API access helpers
- `web/src/stores`: auth, preferences, and platform scope state
- `web/src/theme/semi-theme.ts`: shared theme tokens and CSS-variable baseline

### Docs

- `docs`: Docusaurus site for architecture, development, API, and operations guidance

## Tech Stack

### Backend

- Go 1.23
- Gin
- PostgreSQL
- Redis
- Kubernetes `client-go`

### Frontend

- React 18
- TypeScript 5
- Vite 6
- React Router 6
- TanStack Query 5
- Zustand 5
- Ant Design 6
- Tailwind CSS 4

### Documentation

- Docusaurus 3

## Project Structure

```text
.
├── cmd/                 # server and agent entrypoints
├── configs/             # backend and agent config files
├── deploy/              # image build, compose, k8s, and Helm deployment assets
├── docs/                # Docusaurus docs site
├── internal/            # backend layers
├── migrations/          # SQL bootstrap and schema migrations
├── web/                 # active frontend app
├── AGENTS.md            # engineering spec and repo memory
├── Makefile             # common dev/build/deploy commands
└── docker-compose.yml   # lightweight local PostgreSQL bootstrap
```

## Features In Scope

Current product scope centers on:

- Platform management: clusters, nodes, namespaces, workloads, network, storage, CRDs, Helm
- Application delivery: applications, build templates, workflow templates, releases, registries
- Observability: monitoring, alerts, notifications, events, AI-assisted workflows
- Control plane: users, roles, teams, policies, menus, announcements, audit logs, settings

## Quick Start

### Requirements

- Go 1.23+
- Node.js 20+
- Docker and Docker Compose for local infrastructure or deployment tests
- PostgreSQL 16 for local backend development

### 1. Start local PostgreSQL

```bash
docker compose up -d postgres
```

The root `docker-compose.yml` is intentionally minimal and only boots the local development database.

### 2. Start the backend

```bash
go run ./cmd/server
```

The default backend config is [configs/config.yaml](./configs/config.yaml). Override with `KC_CONFIG_FILE=/abs/path/to/config.yaml` when needed.

### 3. Start the frontend

```bash
cd web
npm install
npm run dev
```

The frontend runs on `http://localhost:5173` and proxies `/api` to `http://localhost:8080`.

### 4. Start the docs site (optional)

```bash
cd docs
npm install
npm run dev
```

The docs site is served at `http://localhost:3000/docs/`.

### 5. Start the remote agent (optional)

```bash
go run ./cmd/agent
```

The default agent config is [configs/agent.config.yaml](./configs/agent.config.yaml). Override with `KC_AGENT_CONFIG_FILE=/abs/path/to/agent.config.yaml` when needed.

## Common Commands

```bash
make dev-api
make dev-web
make dev-docs
make build
make test-api
make test-web
```

## Deployment

The canonical deployment assets live under [deploy](./deploy/).

- [deploy/docker/Dockerfile.single-project](./deploy/docker/Dockerfile.single-project): multi-stage image build for the API server with embedded SPA and docs
- [deploy/compose/docker-compose.single-project.yml](./deploy/compose/docker-compose.single-project.yml): local full-stack startup with kubecrux plus PostgreSQL
- [deploy/k8s/kubecrux-single-project.yaml](./deploy/k8s/kubecrux-single-project.yaml): raw Kubernetes manifest set
- [deploy/helm/kubecrux](./deploy/helm/kubecrux): Helm chart for repeatable installs
- [deploy/config/config.api.single-project.yaml](./deploy/config/config.api.single-project.yaml): starter application config for single-project deployment

Example commands:

```bash
make deploy-image
make deploy-compose-up
make deploy-compose-config
make deploy-helm-lint
```

Or directly:

```bash
docker build -f deploy/docker/Dockerfile.single-project -t kubecrux:single-project .
docker compose -f deploy/compose/docker-compose.single-project.yml up -d --build
helm lint deploy/helm/kubecrux
```

## Documentation

Primary project docs live in [docs](./docs/).

- [Engineering Spec](./AGENTS.md)
- [Architecture Overview](./docs/architecture/index.md)
- [Application Delivery](./docs/architecture/application-delivery.md)
- [Authorization](./docs/architecture/authorization.md)
- [Monitoring and Alerting](./docs/architecture/monitoring-and-alerting.md)
- [Configuration](./docs/operations/configuration.md)
- [Deployment](./docs/operations/deployment.md)
- [Agent Runtime](./docs/operations/agent-runtime.md)
- [MCP](./docs/operations/mcp.md)

## Contributing

Issues and pull requests are welcome. Before sending larger changes, align the implementation with [AGENTS.md](./AGENTS.md), especially the rules around:

- backend layer boundaries
- frontend route and theme ownership
- scope and authorization semantics
- memory and documentation updates for non-trivial changes

## Project Status

kubecrux is under active development. The platform, delivery, observability, and AI surfaces are evolving together, so some areas are more mature than others. The current engineering baseline and scope decisions are tracked in [AGENTS.md](./AGENTS.md).

## License

This repository does not currently include a top-level `LICENSE` file. Clarify the licensing terms before external redistribution or reuse.
