<h1 align="center">soha</h1>

<p align="center">
  <strong>A unified Kubernetes platform console for modern platform teams.</strong>
</p>

<p align="center">
  Operate clusters, ship applications, investigate incidents, and manage runtime work from one permission-aware control plane.
</p>

<p align="center">
  <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/Go-1.23-00ADD8?logo=go&logoColor=white"></a>
  <a href="https://react.dev/"><img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=111111"></a>
  <a href="https://ant.design/"><img alt="Ant Design" src="https://img.shields.io/badge/Ant%20Design-6-1677FF?logo=antdesign&logoColor=white"></a>
  <a href="https://kubernetes.io/"><img alt="Kubernetes" src="https://img.shields.io/badge/Kubernetes-client--go-326CE5?logo=kubernetes&logoColor=white"></a>
  <a href="https://www.postgresql.org/"><img alt="PostgreSQL" src="https://img.shields.io/badge/PostgreSQL-18.4-4169E1?logo=postgresql&logoColor=white"></a>
  <a href="https://docs.opensoha.dev/"><img alt="Docs" src="https://img.shields.io/badge/Docs-Docusaurus-3ECC5F?logo=docusaurus&logoColor=white"></a>
</p>

<p align="center">
  <a href="#overview">Overview</a>
  · <a href="#why-soha">Why soha</a>
  · <a href="#features">Features</a>
  · <a href="#quick-start">Quick Start</a>
  · <a href="#deployment">Deployment</a>
  · <a href="#contributing">Contributing</a>
</p>

<p align="center">
  <a href="./README.md">English</a> | <a href="./README-cn.md">简体中文</a>
</p>

## Overview

Soha is a control plane for platform teams operating Kubernetes and adjacent runtime infrastructure. This repository owns the open-source Go core/server and consumes the web console as a versioned build artifact.

The project is intentionally broader than a resource viewer. Soha connects cluster operations, application delivery, observability, runtime evidence, access control, AI investigation, virtualization, and Docker operations into one cohesive console.

## Why soha

- **One runtime**: ship the API and embedded console as a single application container when you want a compact deployment.
- **Operator-first workflows**: list-first resource pages, scoped actions, YAML, events, metrics, logs, and long-running operation records are treated as first-class surfaces.
- **Permission-aware by design**: menus, routes, buttons, API authorization, audit logs, and scope grants are modeled as separate but aligned control points.
- **Agent-ready architecture**: remote clusters, AI providers, Docker operations, and durable execution tasks can run through token-protected runner claim/callback paths.
- **Built to evolve**: platform, delivery, observability, AI, virtualization, and Docker workbench capabilities share one modular-monolith backend and one route-driven frontend shell.

## Features

| Area | What Soha Provides |
| --- | --- |
| Platform operations | Multi-cluster inventory, nodes, namespaces, workloads, network, storage, CRDs, Helm, YAML, logs, events, metrics, and action surfaces. |
| Application delivery | Applications, services, containers, build templates, workflow templates, release bundles, execution tasks, approvals, releases, registries, and delivery records. |
| Observability | Monitoring overview, alert inventory, alert events, notification policy, healing policy, on-call routing, schedules, escalations, and event streams. |
| AI workbench | Session-first chat, root-cause analysis, performance analysis, inspection review, MCP-backed evidence collection, toolsets, skills, and provider execution. |
| Agent runtime | Remote cluster mode, runner claim/callback APIs, execution heartbeats, task cancellation, Docker host runtime proxy endpoints, Docker operation callbacks, and provider-agnostic AI execution. |
| Virtualization | KubeVirt and Proxmox VE connections, VM lifecycle, image and flavor catalogs, console access, metrics, operations, and sync tasks. |
| Docker workbench | Docker host inventory, Compose projects, container management, services, port mappings, templates, single-container startup, agent-backed runtime logs, Shell access, volume browsing, and token-protected runner operations. |
| Access and system | Users, roles, organizations, policies, scope grants, menus, settings, announcements, audit logs, and operation logs. |

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
- future `cmd/**` entries: same-repo subservice entrypoints for specialized workloads such as security ingest or workers
- `internal/api`: domain route registration files, handlers, middleware, request parsing, response shaping
- `internal/application`: use-case orchestration, authorization, scope handling, audit, and view models
- `internal/policy`: RBAC, ABAC, and scope evaluation
- `internal/infrastructure`: config, database, Kubernetes, informer, agent, logger, Swagger, MCP
- `internal/repository`: durable persistence
- `internal/bootstrap`: dependency graph, migration, seed, and startup lifecycle wiring

See the published docs for the current route, bootstrap, multi-`cmd`, and reserved security-ingest boundary conventions.

### Frontend

- Source repository: `github.com/opensoha/soha-web`
- Build artifact: `dist`
- `soha` release staging path: `internal/staticassets/web/dist`
- Runtime modes: `embed`, `dir`, and `proxy`

### Documentation

- Source repository: `github.com/opensoha/soha-docs`
- Published docs URL: `https://docs.opensoha.dev/`
- `soha` redirects `/docs/` to the configured external docs URL by default

### Agent And CLI

- Agent repository: `github.com/opensoha/soha-agent`
- CLI repository: `github.com/opensoha/soha-cli`
- `soha` core exposes the control-plane APIs; agent and CLI clients consume those APIs through contracts and HTTP boundaries.

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
├── cmd/                 # server, agent, and future same-repo service entrypoints
├── configs/             # backend and agent configuration
├── internal/            # backend layers and domain modules
├── internal/staticassets # staged web artifacts for embedded release builds
├── migrations/          # PostgreSQL bootstrap and schema migrations
├── deploy/              # Docker, Compose, raw Kubernetes, and Helm assets
├── Makefile             # common dev/build/deploy commands
└── agents.md            # engineering spec and project memory
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

This installs Go dependencies, then starts the local PostgreSQL service from `deploy/docker-compose.yaml`. The development helper can also start a local k3s debug cluster and write its kubeconfig under `./.dev/k3s/kubeconfig.yaml`. Frontend and docs dependencies live in `../soha-web` and `../soha-docs`; use `make init-web` or `make init-docs` when those sibling repositories are present.

The compose stack uses `postgres:18.4` and mounts the named volume at `/var/lib/postgresql`, which is required for PostgreSQL 18's default data directory layout. Existing local volumes created by PostgreSQL 16 cannot be reused by changing only the image tag; recreate disposable volumes or migrate data with `pg_dump`/`pg_restore` or `pg_upgrade`.

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
cd ../soha-agent
go run ./cmd/agent
```

The default agent config is in the sibling `soha-agent` repository at `configs/agent.config.yaml`. Override it with:

```bash
SOHA_AGENT_CONFIG_FILE=/abs/path/to/agent.config.yaml go run ./cmd/agent
```

The same agent binary can also expose Docker host runtime APIs used by Docker
Workbench for project logs, interactive Shell sessions, and volume file browsing.
Configure host records with the agent runtime endpoint and bearer token. Browser
WebSocket streams still go through the control plane and use short-lived stream
tickets instead of query-string access tokens.

### Deploy the Hermes Agent runner with Docker

When Hermes is used as an external provider, run the derived `soha-agent` image from the unified compose stack. The image is built from sibling repository `../soha-agent`, extends the official `nousresearch/hermes-agent` image, adds the `soha-agent` runner, and connects back to the control plane through the Agent Runtime claim/callback protocol.

```bash
make init-hermes
```

For local `make dev`, it connects from the container to the host API at `http://host.docker.internal:8080` and reports its runtime endpoint as `http://127.0.0.1:18080`. Override these when needed:

```bash
HERMES_CONTROL_PLANE_URL=http://host.docker.internal:8080 \
SOHA_EXECUTION_RUNNER_TOKEN=replace-with-runtime-token \
make init-hermes
```

If Hermes needs one-time provider setup, run the setup profile directly:

```bash
docker compose -f deploy/docker-compose.yaml --profile hermes-setup run --rm hermes-agent-setup
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
make init-hermes
make deploy-image
make deploy-compose-up
```

## Deployment

Soha ships as a single-binary runtime by default: one application container serves the API and embedded SPA. Documentation is published from `soha-docs` and linked through the configured docs URL.

- [deploy/Dockerfile](./deploy/Dockerfile): multi-stage image build
- `../soha-agent/deploy/Dockerfile.hermes-agent-runner`: Hermes Agent Runtime runner image
- [deploy/docker-compose.yaml](./deploy/docker-compose.yaml): local stack with PostgreSQL, k3s, and optional Hermes runner services
- [configs/config.yaml](./configs/config.yaml): default application config
- [configs/config.compose.yaml](./configs/config.compose.yaml): compose app-container config with PostgreSQL service host and no host-local kubeconfig seed
- [deploy/deployment.yaml](./deploy/deployment.yaml): raw Kubernetes manifest baseline
- [deploy/kustomization.yaml](./deploy/kustomization.yaml): Kustomize entrypoint for image tag, namespace, and patch overrides without Helm

```bash
make deploy-image
docker compose -f deploy/docker-compose.yaml up -d --build
```

Recommended boundaries:

- Docker image: publish to Docker Hub as `yshanchui/soha`; local builds default to the `local` tag.
- Agent images: publish `yshanchui/soha-agent` and `yshanchui/soha-hermes-agent` from the sibling `soha-agent` repository.
- CLI tool image: publish `yshanchui/soha-cli` from the sibling `soha-cli` repository for multi-stage builds and operational containers. It is an image artifact, not a Helm workload.
- Docker Compose: use for local development, smoke tests, and single-node trials, not as the primary production orchestrator.
- Helm: use as the primary Kubernetes delivery path. `soha-helm` publishes `soha`, `soha-agent`, and `soha-hermes-agent` charts.
- Kustomize: keep as a lightweight raw YAML customization entrypoint, avoiding a second full Kubernetes template set.

Build and push the image:

```bash
make deploy-image IMAGE_TAG=v0.1.0
make deploy-image-push IMAGE_TAG=v0.1.0 PUSH_LATEST=1

# When proxy.golang.org is unstable:
make deploy-image IMAGE_TAG=v0.1.0 GOPROXY=https://goproxy.cn,direct
```

Install with Helm:

```bash
helm repo add opensoha https://raw.githubusercontent.com/opensoha/soha-helm/main
helm repo update
helm install soha opensoha/soha --namespace soha --create-namespace
helm install soha-agent opensoha/soha-agent \
  --namespace soha-agent \
  --create-namespace \
  --set-string secrets.agentBearerToken="$SOHA_AGENT_BEARER_TOKEN" \
  --set-string secrets.controlPlaneBearerToken="$SOHA_EXECUTION_RUNNER_TOKEN" \
  --set-string config.controlPlane.baseUrl="https://soha.example.com"
helm install soha-hermes-agent opensoha/soha-hermes-agent \
  --namespace soha-agent \
  --create-namespace \
  --set-string secrets.controlPlaneBearerToken="$SOHA_EXECUTION_RUNNER_TOKEN" \
  --set-string controlPlane.baseUrl="https://soha.example.com"
```

To copy the CLI into another image, use the tool image directly:

```Dockerfile
COPY --from=yshanchui/soha-cli:v0.1.0 /usr/local/bin/soha /usr/local/bin/soha
```

Helm chart sources and Artifact Hub publishing live in `opensoha/soha-helm`.

Render with Kustomize:

```bash
kubectl kustomize deploy
kubectl apply -k deploy
```

## Documentation

- [Engineering Spec](./agents.md)
- [Published Docs](https://docs.opensoha.dev/)
- [Docs Source](https://github.com/opensoha/soha-docs)

## Development Principles

- Backend handlers stay thin. Application services own orchestration, authorization, scope semantics, audit, operation logs, and frontend-facing view models.
- Keep central startup and route files thin. Add domain route files under `internal/api/routes` and concern-specific bootstrap files under `internal/bootstrap` instead of growing one monolithic file.
- Split oversized Go files by stable behavior domains first. Platform handlers, platform resource services, and AI Gateway are organized into focused same-package files; protect execution-plane state transitions with unit tests.
- Long-running work is task-backed. Build, release, Docker, Compose, VM control, and provider execution run through durable tasks and callback paths.
- Future security workbench APIs should keep management, client, and ingest boundaries separate: `/api/v1/security/**`, `/api/client/v1/**`, and `/api/ingest/v1/**`.
- Frontend work belongs in `github.com/opensoha/soha-web`. Routes, metadata, permissions, backend menus, and tests should stay aligned across the artifact boundary.
- Platform APIs return Soha DTOs, not raw Kubernetes objects, except YAML or explicit passthrough routes.
- Module visibility, menu visibility, and backend authorization are separate gates.
- Generated artifacts should not be hand-edited. Update source files and rebuild instead.

## Contributing

Issues and pull requests are welcome. For larger changes, read [agents.md](./agents.md) first so backend layering, frontend routing, authorization, scope handling, and documentation updates stay consistent.

Useful validation commands:

```bash
go test ./...
cd ../soha-web && npm run typecheck && npm run build
cd ../soha-docs && npm test && npm run build
```

## Project Status

Soha is under active development. The platform, delivery, observability, AI, virtualization, and Docker workbench surfaces are evolving together, so some areas are more mature than others.

## License

Soha is licensed under the Apache License 2.0. See
[LICENSE](./LICENSE) for the full license text.
