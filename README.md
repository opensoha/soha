[English](./README.md) | [简体中文](./README-cn.md)

<p align="center">
  <strong>Soha</strong>
</p>

<p align="center">
  A multi-cluster Kubernetes platform console for operations, delivery, observability, access control, and AI-assisted investigation.
</p>

<p align="center">
  <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/Go-1.23-00ADD8?logo=go&logoColor=white"></a>
  <a href="https://react.dev/"><img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=111111"></a>
  <a href="https://ant.design/"><img alt="Ant Design" src="https://img.shields.io/badge/Ant%20Design-6-1677FF?logo=antdesign&logoColor=white"></a>
  <a href="https://kubernetes.io/"><img alt="Kubernetes" src="https://img.shields.io/badge/Kubernetes-client--go-326CE5?logo=kubernetes&logoColor=white"></a>
  <a href="https://www.postgresql.org/"><img alt="PostgreSQL" src="https://img.shields.io/badge/PostgreSQL-18.4-4169E1?logo=postgresql&logoColor=white"></a>
  <a href="./docs/"><img alt="Docs" src="https://img.shields.io/badge/Docs-Docusaurus-3ECC5F?logo=docusaurus&logoColor=white"></a>
</p>

<p align="center">
  <a href="#features">Features</a>
  · <a href="#architecture">Architecture</a>
  · <a href="#quick-start">Quick Start</a>
  · <a href="#deployment">Deployment</a>
  · <a href="#contributing">Contributing</a>
</p>

## Overview

Soha is a full-stack control plane for platform teams that operate Kubernetes at scale. It combines a Go API server, a React + Ant Design console, and in-repo Docusaurus documentation into one deployable project.

The project is designed to grow beyond a resource viewer. Soha brings cluster operations, application delivery, alerts, runtime evidence, permissions, AI investigation, virtualization, and Docker operations into one permission-aware console.

## Features

| Area | What Soha Provides |
| --- | --- |
| Platform operations | Multi-cluster inventory, nodes, namespaces, workloads, network, storage, CRDs, Helm, YAML, logs, events, metrics, and action surfaces. |
| Application delivery | Applications, services, containers, build templates, workflow templates, release bundles, execution tasks, approvals, releases, registries, and delivery records. |
| Observability | Monitoring overview, alert inventory, alert events, notification policy, healing policy, on-call routing, schedules, escalations, and event streams. |
| AI workbench | Session-first chat, root-cause analysis, performance analysis, inspection review, MCP-backed evidence collection, toolsets, skills, and provider execution. |
| Agent runtime | Remote cluster mode, runner claim/callback APIs, execution heartbeats, task cancellation, Docker operation callbacks, and provider-agnostic AI execution. |
| Virtualization | KubeVirt and Proxmox VE connections, VM lifecycle, image and flavor catalogs, console access, metrics, operations, and sync tasks. |
| Docker workbench | Docker host inventory, Compose projects, services, port mappings, templates, single-container startup, and token-protected runner operations. |
| Access and system | Users, roles, groups, policies, scope grants, menus, settings, announcements, audit logs, and operation logs. |

## Architecture

Soha follows a modular-monolith backend and a route-driven frontend shell.

```text
Browser Console
      |
      v
React 18 + Vite + Ant Design
      |
      v
Gin API Server
      |
      +--> Application services
      +--> Policy engine
      +--> Repositories
      +--> Kubernetes / Agent / Docker / Virtualization / MCP integrations
      |
      v
PostgreSQL + Kubernetes clusters
```

### Backend

- `cmd/server`: API server entrypoint
- `cmd/agent`: remote cluster agent and runner entrypoint
- `internal/api`: routes, handlers, middleware, request parsing, response shaping
- `internal/application`: use-case orchestration, authorization, scope handling, audit, and view models
- `internal/policy`: RBAC, ABAC, and scope evaluation
- `internal/infrastructure`: config, database, Kubernetes, informer, agent, logger, Swagger, MCP
- `internal/repository`: durable persistence
- `internal/bootstrap`: dependency graph, migration, seed, and startup wiring

### Frontend

- `web`: React 18 + TypeScript 5 + Vite console
- `web/src/routes`: route registry and metadata
- `web/src/layouts`: console shell
- `web/src/features`: route-level business modules
- `web/src/components`: shared UI primitives and widgets
- `web/src/services`: API helpers
- `web/src/stores`: auth, preferences, and platform scope state
- `web/src/theme/app-theme.ts`: Ant Design theme tokens and shared CSS variables

### Documentation

- `docs`: Docusaurus site for architecture, development, API, and operations guidance

## Tech Stack

| Layer | Stack |
| --- | --- |
| Backend | Go 1.23, Gin, PostgreSQL, Kubernetes `client-go` |
| Frontend | React 18, TypeScript 5, Vite 6, React Router 6, TanStack Query 5, Zustand 5, Ant Design 6, Tailwind CSS 4 |
| Docs | Docusaurus 3 |
| Packaging | Docker, Docker Compose, raw Kubernetes YAML, Helm |

## Project Layout

```text
.
├── cmd/                 # server and agent entrypoints
├── configs/             # backend and agent configuration
├── docs/                # Docusaurus documentation
├── internal/            # backend layers and domain modules
├── migrations/          # PostgreSQL bootstrap and schema migrations
├── web/                 # active frontend app
├── chart/               # Helm chart
├── Dockerfile           # single-project image build
├── Makefile             # common dev/build/deploy commands
├── deployment.yaml      # raw Kubernetes manifest
└── docker-compose.yaml  # local compose stack
```

## Quick Start

### Requirements

- Go 1.23+
- Node.js 20+
- Docker and Docker Compose
- PostgreSQL 18.4 for local backend development

### Install dependencies and start local services

```bash
make init
```

This installs Go, frontend, and docs dependencies, then starts the local PostgreSQL service from `docker-compose.yaml`. The development helper can also start a local k3s debug cluster and write its kubeconfig under `./.dev/k3s/kubeconfig.yaml`.

The compose stack uses `postgres:18.4`. Existing local volumes created by PostgreSQL 16 cannot be reused by changing only the image tag; recreate disposable volumes or migrate data with `pg_dump`/`pg_restore` or `pg_upgrade`.

### Start the API and console

```bash
make
```

The default target starts the Go API and the Vite frontend together.

- Console: `http://localhost:5173`
- API: `http://localhost:8080`
- Config override: `SOHA_CONFIG_FILE=/abs/path/to/config.yaml`

### Run services separately

```bash
make dev-api
make dev-web
make dev-docs
```

### Start the agent runtime

```bash
go run ./cmd/agent
```

The default agent config is [configs/agent.config.yaml](./configs/agent.config.yaml). Override it with:

```bash
SOHA_AGENT_CONFIG_FILE=/abs/path/to/agent.config.yaml go run ./cmd/agent
```

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
make deploy-image
make deploy-compose-up
make deploy-helm-lint
```

## Deployment

Soha ships as a single-project runtime by default: one application container serves the API, embedded SPA, and docs.

- [Dockerfile](./Dockerfile): multi-stage image build
- [docker-compose.yaml](./docker-compose.yaml): local full-stack stack with PostgreSQL
- [configs/config.yaml](./configs/config.yaml): default application config
- [deployment.yaml](./deployment.yaml): raw Kubernetes manifest baseline
- [chart](./chart): Helm chart

```bash
docker build -t soha:single-project .
docker compose -f docker-compose.yaml up -d --build
helm lint chart
```

## Documentation

- [Engineering Spec](./AGENTS.md)
- [Architecture Overview](./docs/architecture/index.md)
- [Application Delivery](./docs/architecture/application-delivery.md)
- [AI Copilot](./docs/architecture/ai-copilot.md)
- [Authorization](./docs/architecture/authorization.md)
- [Monitoring and Alerting](./docs/architecture/monitoring-and-alerting.md)
- [Configuration](./docs/operations/configuration.md)
- [Deployment](./docs/operations/deployment.md)
- [Agent Runtime](./docs/operations/agent-runtime.md)
- [MCP](./docs/operations/mcp.md)

## Development Principles

- Backend handlers stay thin. Application services own orchestration, authorization, scope semantics, audit, operation logs, and frontend-facing view models.
- Long-running work is task-backed. Build, release, Docker, Compose, VM control, and provider execution run through durable tasks and callback paths.
- Frontend work belongs in `web`. Routes, metadata, permissions, backend menus, and tests should stay aligned.
- Platform APIs return Soha DTOs, not raw Kubernetes objects, except YAML or explicit passthrough routes.
- Module visibility, menu visibility, and backend authorization are separate gates.
- Generated artifacts should not be hand-edited. Update source files and rebuild instead.

## Contributing

Issues and pull requests are welcome. For larger changes, read [AGENTS.md](./AGENTS.md) first so backend layering, frontend routing, authorization, scope handling, and documentation updates stay consistent.

Useful validation commands:

```bash
go test ./...
cd web && npm run typecheck && npm run build
cd docs && npm run build
```

## Project Status

Soha is under active development. The platform, delivery, observability, AI, virtualization, and Docker workbench surfaces are evolving together, so some areas are more mature than others.

## License

This repository does not currently include a top-level `LICENSE` file. Add one before a public release if the project is intended for open-source distribution.
