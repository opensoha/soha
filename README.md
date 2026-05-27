[English](./README.md) | [简体中文](./README-cn.md)

# kubecrux

> A multi-cluster Kubernetes platform console for platform operations, application delivery, observability, access control, and AI-assisted investigation.

kubecrux is a full-stack platform console built around a Go backend, a React + Ant Design frontend, and in-repo Docusaurus documentation. It is designed to grow beyond a resource viewer into a unified control plane for multi-cluster platform teams.

## Highlights

- Multi-cluster Kubernetes console with direct-cluster and agent-connected runtime models
- Platform management covering clusters, nodes, namespaces, workloads, network, storage, CRDs, and Helm
- Delivery workbench for applications, build templates, workflow templates, release bundles, execution tasks, approval policies, registries, and catalog-style master data
- Monitoring workbench with alerts, events, notification policy, healing policy, and on-call collaboration surfaces
- Session-first AI workbench for chat, root-cause analysis, performance analysis, inspections, MCP-backed evidence collection, tool or skill assembly, and Agent Runtime provider execution
- Pluggable AI Agent Runtime with Hermes as the first external provider while kubecrux owns capabilities, tool bindings, skills, budgets, audit, and `AnalysisArtifact` output
- Virtualization workbench for KubeVirt and Proxmox VE inventory, VM lifecycle, images, flavors, console access, metrics, operations, and sync tasks
- Docker workbench for Docker hosts, Compose projects, services, port mappings, templates, and token-protected runner operations
- Access control and system management with permission-aware menus, audit logs, operation logs, and settings
- Single-project deployment assets for Docker, Docker Compose, raw Kubernetes YAML, and Helm

## Architecture

The repository follows a modular-monolith backend and a route-driven frontend shell.

### Backend

- `cmd/server`: API entrypoint
- `cmd/agent`: remote cluster agent and local runner entrypoint
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
- `web/src/theme/app-theme.ts`: shared theme tokens and CSS-variable baseline

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
├── docs/                # Docusaurus docs site
├── internal/            # backend layers
├── migrations/          # SQL bootstrap and schema migrations
├── web/                 # active frontend app
├── AGENTS.md            # engineering spec and repo memory
├── chart/               # Helm chart
├── Dockerfile           # single-project image build entry
├── Makefile             # common dev/build/deploy commands
├── deployment.yaml      # raw Kubernetes manifest
└── docker-compose.yaml  # local compose stack for kubecrux and PostgreSQL
```

## Features In Scope

Current product scope centers on:

- Platform management: clusters, nodes, namespaces, workloads, network, storage, CRDs, Helm
- Application delivery: applications, application services and containers, build templates, workflow templates, release bundles, execution tasks, execution logs and artifacts, approval policies, releases, registries
- Observability: monitoring, alerts, notification policy, self-healing policy, on-call routes, schedules, escalations, events
- AI workbench: session-based investigation, root-cause, performance, trace and inspection-review analysis, MCP data sources, tools, skill registry, analysis profiles, automation policies, and Agent Runtime provider selection
- Virtualization: KubeVirt and Proxmox VE connections, VMs, images, flavors, console and metrics, operations, and sync
- Docker workbench: host inventory, quick host provisioning through the virtualization adapter, Compose projects, single-container startup, services, ports, templates, operation records, and agent-runner callbacks
- Control plane: users, roles, teams, policies, menus, announcements, audit logs, settings

## Current Workbench Surface

- Platform workbench: `/`, `/clusters`, `/workloads/**`, `/network/**`, `/storage/**`, `/extensions/**`, `/helm/**`
- Delivery workbench: `/applications`, `/application-management`, `/build-templates`, `/delivery/release-bundles`, `/delivery/execution-tasks`, `/delivery/approval-policies`, `/workflow-templates`, `/release-board`, `/registries`
- Monitoring workbench: `/monitoring-workbench/**`
- AI workbench: `/ai-workbench`, `/ai-workbench/chat`, `/ai-workbench/root-cause`, `/ai-workbench/performance`, `/ai-workbench/inspection`, `/ai-workbench/tool-settings`, `/ai-workbench/model-settings`
- Virtualization workbench: `/virtualization/**`
- Docker workbench: `/docker/**`
- System, access, and settings: `/system/**`, `/access/**`, `/settings/**`

`/api/v1/modules` reports module status from `configs/config.yaml` under `modules.*.enabled`. Route visibility, backend visible menus, and backend permission checks are separate gates and must stay aligned.

## Quick Start

### Requirements

- Go 1.23+
- Node.js 20+
- Docker and Docker Compose for local infrastructure or deployment tests
- PostgreSQL 16 for local backend development

### 1. Initialize local development dependencies

```bash
make init
```

This runs `go mod tidy`, installs the `web` and `docs` npm dependencies, then boots the `pgsql` container from the root `docker-compose.yaml` and waits until PostgreSQL is ready.
It also starts a local `k3s server` debug cluster, writes its kubeconfig to `./.dev/k3s/kubeconfig.yaml`, and the default development config registers it as `local-k3s`. For KubeVirt labs, the underlying Linux node must still expose `/dev/kvm`; Proxmox VE can be connected as a KubeVirt VM or an external host through its API, but it must not be deployed as a regular Pod/workload inside k3s.

To run a Proxmox VE lab VM inside KubeVirt:

```bash
make init-pve-vm
```

After the ISO installer finishes through the VNC console, run `make pve-vm-boot-root` to boot from the installed root disk. The PVE API is exposed at `https://127.0.0.1:8006` by default.

### 2. Start the backend and frontend

```bash
make
```

The default `make` target starts the Go API and the Vite frontend together. The backend still reads [configs/config.yaml](./configs/config.yaml), and you can override it with `KC_CONFIG_FILE=/abs/path/to/config.yaml` when needed.

### 3. Run backend or frontend separately when needed

```bash
make dev-api
make dev-web
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
make
make init
make dev-api
make dev-web
make dev-docs
make build
make test-api
make test-web
make init-cluster-kubevirt
make init-kubevirt
make init-cdi
make deploy-pve-mock
make init-pve-vm
```

## Engineering Rules

- Keep backend transport thin. Handlers parse, bind, and map errors; application services own orchestration, permission checks, scope semantics, audit, operation logs, and view-model shaping.
- Do not run build, release, Docker, Compose, or VM-control shell commands inside API handlers. Durable work is queued as execution, Docker, or virtualization operations and completed through runner claim, status, callback, cancel, and retry paths.
- Frontend work belongs in `web`. `old_web` and `web_pro_backup` are reference-only. Use native `antd` and `@ant-design/icons`; do not reintroduce Semi Design or a parallel design system.
- Route changes must update `web/src/routes/index.tsx`, `web/src/routes/meta.ts`, permission catalog or backend menu seeds when needed, and related tests together.
- Permission visibility is not authorization. Frontend buttons may hide unavailable actions, but backend services must enforce explicit permission keys.
- Platform APIs return aggregated kubecrux DTOs, not raw Kubernetes objects, except YAML or explicit passthrough routes. Empty namespace means all namespaces for namespaced resources; cluster-scoped resources ignore namespace filters.
- Prefer backend aggregation and informer/cache reads over browser namespace fan-out or repeated live cluster queries.
- AI investigation should use `/ai-workbench` as the canonical session-first surface. Legacy `/ai-observe/**`, `/chat`, and old AI workbench paths should remain compatibility redirects only.
- AI Agent Runtime must keep pages, automation policy, and business modules bound to kubecrux provider/capability/tool/skill contracts. Hermes is only a provider runner behind claim/callback APIs, and agent output must be converted back to `AnalysisArtifact`.
- Docker Engine and Compose execution are agent-runner responsibilities. The API persists desired state and operation records, then exposes token-protected claim, runner-status, and callback paths.
- KubeVirt and PVE lab work requires a real virtualization runtime. Docker Desktop on macOS can validate control-plane paths, but it does not provide a production-like KVM environment.

## Common Pitfalls

- Adding a menu item without a matching permission key, backend seed menu, and route metadata will make navigation inconsistent.
- Fetching one request per namespace from the browser usually means the backend aggregate endpoint is missing or should be expanded.
- Treating module visibility as security is incorrect; disabled modules, menu visibility, and permission checks solve different problems.
- Returning raw Kubernetes objects makes the UI brittle and leaks infrastructure schema into the platform contract.
- Leaving delivery or Docker business records at `queued` while execution tasks finish creates split-brain status. Callback handlers must backfill related records.
- Adding AI tools, skills, or external agent providers without budget, timeout, permission, redaction, and callback boundaries can make session analysis unpredictable.
- Editing generated docs build output or generated frontend artifacts is usually the wrong target; update source files instead.

## Deployment

The main image and local compose assets now live at the repo root.

- [Dockerfile](./Dockerfile): multi-stage image build for the API server with embedded SPA and docs
- [docker-compose.yaml](./docker-compose.yaml): local full-stack startup with kubecrux plus PostgreSQL
- [configs/config.yaml](./configs/config.yaml): default application config used by local development and the container image
- [deployment.yaml](./deployment.yaml): raw Kubernetes manifest set
- [chart](./chart): Helm chart for repeatable installs

Example commands:

```bash
make deploy-image
make deploy-compose-up
make deploy-compose-config
make deploy-helm-lint
```

Or directly:

```bash
docker build -t kubecrux:single-project .
docker compose -f docker-compose.yaml up -d --build
helm lint chart
```

## Documentation

Primary project docs live in [docs](./docs/).

- [Engineering Spec](./AGENTS.md)
- [Architecture Overview](./docs/architecture/index.md)
- [Login And Identity Flow](./docs/architecture/login-and-identity.md)
- [Application Delivery](./docs/architecture/application-delivery.md)
- [AI Copilot](./docs/architecture/ai-copilot.md)
- [Authorization](./docs/architecture/authorization.md)
- [Monitoring and Alerting](./docs/architecture/monitoring-and-alerting.md)
- [Configuration](./docs/operations/configuration.md)
- [Deployment](./docs/operations/deployment.md)
- [Agent Runtime](./docs/operations/agent-runtime.md)
- [Virtualization Lab Runbook](./docs/operations/virtualization-lab-runbook.md)
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
