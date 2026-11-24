<h1 align="center">soha</h1>

<p align="center">
  <strong>Soha helps small and medium-sized businesses solve everyday operations challenges.</strong>
</p>

<p align="center">
  Help small teams manage, maintain, and keep their business systems running.
</p>

<p align="center">
  <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/Go-1.27.4-00ADD8?logo=go&logoColor=white"></a>
  <a href="https://react.dev/"><img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=111111"></a>
  <a href="https://ant.design/"><img alt="Ant Design" src="https://img.shields.io/badge/Ant%20Design-6-1677FF?logo=antdesign&logoColor=white"></a>
  <a href="https://kubernetes.io/"><img alt="Kubernetes" src="https://img.shields.io/badge/Kubernetes-client--go-326CE5?logo=kubernetes&logoColor=white"></a>
  <a href="https://www.postgresql.org/"><img alt="PostgreSQL" src="https://img.shields.io/badge/PostgreSQL-18.4-4169E1?logo=postgresql&logoColor=white"></a>
  <a href="https://docs.opensoha.com/"><img alt="Docs" src="https://img.shields.io/badge/Docs-Nextra-111111?logo=nextdotjs&logoColor=white"></a>
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

Soha helps small and medium-sized businesses with self-managed systems and limited operations staff bring resource access, daily maintenance, application delivery, and incident handling into one permission-aware workspace. This repository is the canonical development source for Core, Web, Contracts, CLI, Agent, Connectors, and Skills; the server embeds the Web build from the same source revision.

Soha evolves around practical operations needs across internal network and identity access, Kubernetes, virtualization, Docker, application delivery, observability, and AI-assisted investigation. Supported platforms, deployment requirements, and experimental boundaries are documented in the [published documentation](https://docs.opensoha.com/).

## Why soha

- **One runtime**: ship the API and embedded console as a single application container when you want a compact deployment.
- **Everyday operations workflows**: start from resources and applications to maintain systems, deliver changes, and investigate issues, with scoped actions, events, metrics, logs, and durable operation records.
- **Permission-aware by design**: menus, routes, buttons, API authorization, audit logs, and scope grants are modeled as separate but aligned control points.
- **Agent-ready architecture**: remote clusters, AI providers, Docker operations, and durable execution tasks run through token-protected runner claim/callback paths.
- **Extend as needs grow**: network access, resources, delivery, observability, and AI share one modular backend and frontend shell.

## Features

| Area | What Soha Provides |
| --- | --- |
| Internal network and identity access | Application directory, identity providers, Outposts, sites and network resources, access policies, and managed VPN and proxy access through the App. |
| Platform operations | Multi-cluster inventory, nodes, namespaces, workloads, network, storage, CRDs, Helm, YAML, logs, events, metrics, and action surfaces. |
| Application delivery | Applications, services, containers, build templates, workflow templates, release bundles, execution tasks, approvals, releases, registries, and delivery records. |
| Observability | Monitoring overview, alert inventory, alert events, notification policy, healing policy, on-call routing, schedules, escalations, and event streams. |
| AI workbench | Session-first chat, root-cause analysis, performance analysis, inspection review, MCP-backed evidence collection, toolsets, skills, and provider execution. |
| Agent runtime | Remote cluster mode, runner claim/callback APIs, execution heartbeats, task cancellation, Docker host runtime proxy endpoints, and provider-agnostic AI execution. |
| Virtualization | KubeVirt and Proxmox VE connections, VM lifecycle, image and flavor catalogs, console access, metrics, operations, and sync tasks. |
| Docker workbench | Docker host inventory, Compose projects, container management, services, port mappings, templates, runtime logs, Shell access, and volume browsing. |
| Access and system | Users, roles, organizations, policies, scope grants, menus, settings, announcements, audit logs, and operation logs. |

## Architecture

Soha follows a modular-monolith backend and a route-driven frontend shell:

```text
Browser Console (React 18 + Vite + Ant Design)
        |
        v
Gin API Server (cmd/server)
  ├── internal/api            # routes, handlers, middleware, DTO mapping
  ├── internal/application    # orchestration, authorization, scope, audit
  ├── internal/policy         # RBAC, ABAC, and scope evaluation
  ├── internal/infrastructure # config, DB, K8s, agent, MCP, logging
  └── internal/repository     # PostgreSQL persistence
```

- **Monorepo components**: Core (`github.com/opensoha/soha`) lives at the root; `contracts/`, `web/`, `cli/`, `agent/`, `connectors/`, and `skills/` live in this repository with independent component tags (`contracts/v*`, `cli/v*`, `agent/v*`, `connectors/v*`, `skills/v*`; Core uses `v*`). The six former component repositories remain read-only historical archives—open new issues and pull requests here.
- **Web runtime modes**: `embed` (release default, staged in `internal/staticassets/web/dist`), `dir`, and `proxy`.
- **Documentation**: maintained in [`opensoha/soha-docs`](https://github.com/opensoha/soha-docs) and published at [`https://docs.opensoha.com/`](https://docs.opensoha.com/) (`/docs/` redirects there by default).

## Tech Stack

| Layer | Stack |
| --- | --- |
| Backend | Go 1.27.4, Gin, PostgreSQL 18.4 (`pgvector` 0.8.5), Kubernetes `client-go` |
| Frontend | React 18, TypeScript 5, Vite 6, React Router 6, TanStack Query 5, Zustand 5, Ant Design 6, Tailwind CSS 4 |
| Docs | Next.js 16 and Nextra 4 (`opensoha/soha-docs`) |
| Packaging | Docker, Docker Compose, raw Kubernetes / Kustomize, Native Linux archive; Helm charts in `opensoha/soha-helm` |

## Project Layout

```text
.
├── contracts/            # public schemas and generated Go/TypeScript SDKs
├── web/                  # React console source
├── cli/                  # standalone CLI and npm launcher
├── agent/                # standalone agent, outpost, and DSH runtimes
├── connectors/           # official connector packages
├── skills/               # official runtime skills, presets, and profiles
├── cmd/                  # Core, network-control, ingest, and gateway entrypoints
├── configs/              # backend configuration templates
├── internal/             # backend layers, domain modules, and staged web assets
├── migrations/           # PostgreSQL bootstrap and schema migrations
├── deploy/               # Docker, Compose, raw Kubernetes, and lab assets
├── Makefile              # local development, build, and verification targets
└── .agents/skills/       # development collaboration skills for Core and components
```

## Quick Start

### 1. Try with Docker Compose

Start a local trial stack with the published Core image (API + embedded Web console):

```bash
git clone --branch v0.1.13 --depth 1 https://github.com/opensoha/soha.git
cd soha
SOHA_IMAGE=ghcr.io/opensoha/soha:v0.1.13 \
  docker compose -f deploy/docker-compose.yaml up -d --no-build --wait soha
```

Open `http://localhost:8080/` and sign in with the default local bootstrap account (`opensoha` / `opensoha`).

> **Security notice:** Local, Docker, Kubernetes, and Helm installs share standard initial credentials (`pgsql`, `opensoha`) and public default system keys for zero-config startup. **Replace all initial credentials and the four system keys before exposing Soha outside loopback.** See [First Deploy](https://docs.opensoha.com/en/docs/tutorials/first-deploy) and [Configuration](https://docs.opensoha.com/en/docs/operations/configuration).

### 2. Local Source Development

Requirements: **Go 1.27.4+**, **Node.js 22.23.4** (Agent DSH uses its own Node.js 24 runtime under `agent/runtimes/dsh/`), and **PostgreSQL 18.4** with `pgvector` 0.8.5 (or Docker to run PostgreSQL locally).

```bash
cp configs/config.example.yaml configs/config.yaml
npm ci
make init          # or: make init DEV_DB_MODE=local (starts Compose PostgreSQL)
make               # starts Go API (:8080) and Vite console (:5173)
```

- Console: `http://localhost:5173` · API: `http://localhost:8080`
- Run separately: `make dev-api` / `make dev-web` (or `cd agent && go run ./cmd/agent` for the agent runtime)
- Full setup & database notes: [Local Development](https://docs.opensoha.com/en/docs/development/local-development)

### Common Commands

```bash
make                        # run API and Web dev servers together
make build                  # build Web, stage assets, and compile server binaries
make test                   # run Core Go unit tests
make test-cli               # run CLI tests
make test-agent             # run Agent tests
make check-connectors       # validate Connectors package and manifests
make check-skills           # validate runtime skills and catalogs
make deploy-image           # build the local Core container image
```

## Deployment

Full installation, upgrade, secret rotation, and runtime topology guides are maintained in the [published documentation](https://docs.opensoha.com/en/docs/operations/deployment) so deployment instructions stay in one place:

| Delivery Path | When To Use | Guide & Assets |
| --- | --- | --- |
| **Docker Compose** | Local trials and single-host previews | [First Deploy](https://docs.opensoha.com/en/docs/tutorials/first-deploy) · [deploy/docker-compose.yaml](./deploy/docker-compose.yaml) |
| **Standalone `docker run`** | Single container with an existing external PostgreSQL 18 | [Deployment Guide](https://docs.opensoha.com/en/docs/operations/deployment) · [deploy/Dockerfile](./deploy/Dockerfile) |
| **Helm Charts** *(Recommended)* | Production Kubernetes (`soha`, `soha-agent`, `soha-operator`) | [Deployment Guide](https://docs.opensoha.com/en/docs/operations/deployment) · [`opensoha/soha-helm`](https://github.com/opensoha/soha-helm) |
| **Raw Kubernetes / Kustomize** | Lightweight manifest customization without Helm | [Deployment Guide](https://docs.opensoha.com/en/docs/operations/deployment) · [deploy/kustomization.yaml](./deploy/kustomization.yaml) |
| **Native Linux Archive** | Host binary + SQL migrations tarball (`soha-linux-amd64.tar.gz`) | [deploy/native/README.md](./deploy/native/README.md) |
| **Agent, Outpost & DSH** | Remote cluster agent, Identity Outpost, Chat/Heya DSH cutover | [Agent Runtime](https://docs.opensoha.com/en/docs/operations/agent-runtime) · [deploy/dsh-hindsight/README.md](./deploy/dsh-hindsight/README.md) |
| **Network Access Runtimes** | `network-control`, `ingest`, WireGuard `network-gateway`, FreeRADIUS | [Network Access Operations](https://docs.opensoha.com/en/docs/operations/network-access) · [deploy/network-runtime.yaml](./deploy/network-runtime.yaml) |

Internal lab fixtures and shared cluster dev scripts live alongside their assets in [deploy/network-test/README.md](./deploy/network-test/README.md) and [deploy/remote-dev/README.md](./deploy/remote-dev/README.md).

## Documentation

- [Published Documentation](https://docs.opensoha.com/) ([source](https://github.com/opensoha/soha-docs))
- [Configuration & System Keys](https://docs.opensoha.com/en/docs/operations/configuration)
- [Backend Engineering Rules](./.agents/skills/soha-backend/SKILL.md)
- [Deployment Rules](./.agents/skills/soha-deploy/SKILL.md)

## Development Principles

- **Thin handlers, rich application services**: handlers parse/shape HTTP; application services own orchestration, authorization, scope semantics, audit, and view models.
- **Modular startup and routing**: add domain route files under `internal/api/routes` and focused wiring under `internal/bootstrap`.
- **Task-backed long-running work**: build, release, Docker, Compose, VM control, and provider execution run through durable tasks and callbacks.
- **Contract-first boundaries**: public schemas and generated SDKs live in `contracts/`; platform APIs return Soha DTOs rather than raw Kubernetes objects (except explicit YAML/passthrough routes).
- **Aligned access gates**: module visibility, menu visibility, and backend API authorization remain distinct but synchronized gates.
- **Never hand-edit generated outputs**: update source schemas/code and regenerate SDKs or staged Web artifacts (`npm run build:web`).

## Contributing

Issues and pull requests are welcome. Before making larger backend or deployment changes, read the [backend](./.agents/skills/soha-backend/SKILL.md) and [deployment](./.agents/skills/soha-deploy/SKILL.md) rules.

Key validation checks before submitting:

```bash
npm ci
make test
npm run typecheck --workspace web
npm run build:web
npm run check:internal-consumers
```

## Project Status

Soha is under active development. Platform operations, delivery, observability, AI, network access, virtualization, and Docker workbench surfaces evolve together, with maturity and environment requirements documented per module.

## License

Soha is licensed under the Apache License 2.0. See [LICENSE](./LICENSE) for the full text.
