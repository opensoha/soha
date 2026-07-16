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
| Packaging | Docker, Docker Compose, raw Kubernetes YAML; Helm charts live in `soha-helm` |

## Project Layout

```text
.
├── cmd/                 # server, agent, and future same-repo service entrypoints
├── configs/             # backend and agent configuration
├── internal/            # backend layers and domain modules
├── internal/staticassets # staged web artifacts for embedded release builds
├── migrations/          # PostgreSQL bootstrap and schema migrations
├── deploy/              # Docker, Compose, and raw Kubernetes assets
├── Makefile             # minimal local dev/build commands
└── agents.md            # engineering spec and project memory
```

## Quick Start

### Requirements

- Go 1.23+
- Node.js 20+
- Docker and Docker Compose
- PostgreSQL 18.4 with pgvector 0.8.5 when using an external database

### Install dependencies and start local services

The standard stack starts with `pgsql` as the PostgreSQL password and
`opensoha` as the initial `opensoha` administrator password:

```bash
make init
```

These are Soha's standard initial credentials across local, Docker, Compose,
Kubernetes, and Helm deployments. Override them only when an installation needs
different database or administrator credentials.

This installs Go dependencies, then starts the local PostgreSQL service from `deploy/docker-compose.yaml`. Frontend dependencies are managed in the sibling `../soha-web` repository.

The compose stack uses `pgvector/pgvector:0.8.5-pg18-trixie`, currently based on PostgreSQL 18.4 and the same Debian generation as the standard PostgreSQL 18.4 image, enables `vector` and `pg_trgm`, and preloads `pg_stat_statements`. It mounts the named volume at `/var/lib/postgresql`, which is required for PostgreSQL 18's default data directory layout. Override the image with `SOHA_POSTGRES_IMAGE` only with a PostgreSQL 18 image that provides these extensions and a compatible libc collation version. Existing local volumes created by PostgreSQL 16 cannot be reused by changing only the image tag; recreate disposable volumes or migrate data with `pg_dump`/`pg_restore` or `pg_upgrade`.

### Start the API and console

```bash
make
```

The default target starts the Go API and the Vite frontend together.

- Console: `http://localhost:5173`
- API: `http://localhost:8080`
- Config override: `SOHA_CONFIG_FILE=/abs/path/to/config.yaml`
- Local defaults live in `configs/config.yaml`; override them with environment variables or `SOHA_CONFIG_FILE`.

### Run services separately

```bash
make dev-api
make dev-web
```

For a direct server run without Make:

```bash
go run ./cmd/server
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

## Common Commands

```bash
make
make init
make dev-api
make dev-web
make build
make test
make deploy-image
```

## Deployment

Soha ships as a single-binary runtime by default: one application container serves the API and embedded SPA. Documentation is published from `soha-docs` and linked through the configured docs URL.

- [deploy/Dockerfile](./deploy/Dockerfile): multi-stage image build
- [deploy/docker-compose.yaml](./deploy/docker-compose.yaml): local stack with PostgreSQL and optional Hermes runner services
- [configs/config.yaml](./configs/config.yaml): default application config
- [deploy/deployment.yaml](./deploy/deployment.yaml): raw Kubernetes manifest baseline
- [deploy/kustomization.yaml](./deploy/kustomization.yaml): Kustomize entrypoint for image tag, namespace, and patch overrides without Helm

```bash
make deploy-image
docker compose -f deploy/docker-compose.yaml up -d --build
```

Run the application container without Compose when PostgreSQL is already
reachable. This example keeps every standard default explicit so each value can
be replaced independently before deployment:

The external PostgreSQL server must run PostgreSQL 18 and provide pgvector and
`pg_trgm`. The application migration user creates both extensions automatically;
when that user cannot create extensions, a database administrator must run:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

`pg_stat_statements` remains optional for external PostgreSQL. To enable it, an
administrator must add it to `shared_preload_libraries`, enable
`compute_query_id`, restart PostgreSQL, and create the extension in the Soha
database. Its absence never blocks Soha startup on an external database.

```bash
docker run -d \
  --name soha \
  --restart unless-stopped \
  -p 8080:8080 \
  --add-host host.docker.internal:host-gateway \
  -e SOHA_DATABASE_HOST=host.docker.internal \
  -e SOHA_DATABASE_PASSWORD=pgsql \
  -e SOHA_AUTH_DEV_PRINCIPAL_PASSWORD=opensoha \
  -e SOHA_AUTH_JWT_SECRET=soha-123456789012345678901234567890 \
  -e SOHA_RUNTIME_EXECUTION_RUNNER_TOKEN=soha-123456789012345678901234567890 \
  -e SOHA_MONITORING_WEBHOOK_TOKEN=soha-123456789012345678901234567890 \
  -e SOHA_SECURITY_CREDENTIAL_ENCRYPTION_KEY=soha-123456789012345678901234567890 \
  yshanchui/soha:latest
```

The bootstrap password is inserted only when the `opensoha` user's password
credential does not already exist, so routine restarts never reset a changed
administrator password.

The JWT, runner, webhook, and credential-encryption settings all default to the
public value `soha-123456789012345678901234567890`. This makes local, raw Docker,
Compose, Kubernetes, and Helm startup deterministic, but it is not secure for a
public installation. Override all four settings before exposing Soha. Prefer
separate high-entropy values and keep the same configured values on every Soha
replica. Changing the credential-encryption key does not re-encrypt existing
records: migrate every stored ciphertext to the new key before restarting with
the replacement, or those credentials will become unreadable.

Soha does not require a SecretStore volume, secret bundle, writer lease, or
secret lifecycle CLI. Multiple API containers may use the same database when
they receive the same configuration; use different host ports for parallel raw
Docker instances and a shared load balancer for normal multi-replica delivery.

Recommended boundaries:

- Docker image: use the public Docker Hub image `yshanchui/soha`; local builds default to the `local` tag.
- Agent images: use `yshanchui/soha-agent` and `yshanchui/soha-hermes-agent` from the sibling `soha-agent` repository.
- CLI tool image: use `yshanchui/soha-cli` from the sibling `soha-cli` repository for multi-stage builds and operational containers. It is an image artifact, not a Helm workload.
- Docker Compose: use for local development and single-node trials, not as the primary production orchestrator.
- Helm: use as the primary Kubernetes delivery path. `soha-helm` publishes `soha`, `soha-agent`, and `soha-hermes-agent` charts.
- Kustomize: keep as a lightweight raw YAML customization entrypoint, avoiding a second full Kubernetes template set.

Build the image:

```bash
make deploy-image IMAGE_TAG=v0.1.0

# When proxy.golang.org is unstable:
make deploy-image IMAGE_TAG=v0.1.0 GOPROXY=https://goproxy.cn,direct
```

Install with Helm:

```bash
helm repo add opensoha https://raw.githubusercontent.com/opensoha/soha-helm/gh-pages
helm repo update
helm install soha opensoha/soha --namespace soha --create-namespace
helm install soha-agent opensoha/soha-agent \
  --namespace soha \
  --set-string config.controlPlane.baseUrl="https://soha.example.com"
helm install soha-hermes-agent opensoha/soha-hermes-agent \
  --namespace soha \
  --set-string controlPlane.baseUrl="https://soha.example.com"
```

The agent charts read only `soha-config/execution-runner-token` by default and
generate their own inbound agent token when needed. Kubernetes Secret
references cannot cross namespaces; for a separate agent namespace, sync only
that runner key with an External Secrets controller and set
`secrets.controlPlaneExistingSecret.name/key` accordingly.

The chart keeps deployment configuration in Kubernetes Secrets so installations
can override the four public defaults without modifying the image. Keep all
replicas on the same values during a rollout.

To copy the CLI into another image, use the tool image directly:

```Dockerfile
COPY --from=yshanchui/soha-cli:v0.1.0 /usr/local/bin/soha /usr/local/bin/soha
```

Helm chart sources and Artifact Hub publishing live in `opensoha/soha-helm`.

Apply the raw Kubernetes baseline:

```bash
kubectl apply -k deploy
```

The raw manifest includes the standard `pgsql`/`opensoha` initial credentials
and the four public system-key defaults in `soha-app-config`. Replace them with
an overlay or external Secret integration before a public rollout.

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
make test
cd ../soha-web && npm run typecheck && npm run build
```

## Project Status

Soha is under active development. The platform, delivery, observability, AI, virtualization, and Docker workbench surfaces are evolving together, so some areas are more mature than others.

## License

Soha is licensed under the Apache License 2.0. See
[LICENSE](./LICENSE) for the full license text.
