---
layout: home

hero:
  name: kubecrux
  text: Multi-cluster Kubernetes platform console
  tagline: Built for platform teams that need converged identity, authorization, cluster access, release workflows, alert routing, and AI inspection capabilities.
  actions:
    - theme: brand
      text: Architecture
      link: /en/architecture/
    - theme: alt
      text: Local Development
      link: /en/development/local-development
    - theme: alt
      text: API Overview
      link: /en/api/overview

features:
  - title: Platform Views Instead of Raw Kubernetes
    details: kubecrux exposes aggregated workload, infrastructure, audit, and event views so the frontend is not forced to absorb raw Kubernetes object complexity.
  - title: Converged Backend Architecture
    details: The Go backend is organized as a modular monolith with API, application, policy, infrastructure, repository, and bootstrap layers.
  - title: Real Product Loops Are In Place
    details: The repository now includes access management groundwork, release center MVP, alert routes, storage views, and platform-native AI inspection flows.
  - title: Docs Live With the Code
    details: The docs site is built with VitePress from the repository docs directory and evolves in lockstep with implementation.
---

## Why kubecrux

kubecrux is not a thin Kubernetes Dashboard wrapper. It is a platform control surface that sits above cluster APIs and provides:

- Multi-cluster access and health awareness
- Aggregated workload and infrastructure views
- Unified audit and event center
- RBAC + ABAC based access control
- Application build and release center
- Alert routes and notification channels
- Platform-native AI inspection tasks and inspection runs
- Stable extension points for agent mode and MCP adapters

## Repository Shape

- `web`: Vite + React + TypeScript frontend console
- `cmd` + `internal`: Go modular monolith backend and agent runtime
- `configs`: backend and agent configuration files
- `docs`: VitePress documentation site maintained with the codebase

## Quick Start

### Backend

```bash
docker compose up -d postgres redis
go run ./cmd/server
```

### Frontend

```bash
cd web
npm install
npm run dev
```

The active frontend has converged to a single Vite SPA under `web/`. The current shape is:

- `src/main.tsx`: Query Client and `BrowserRouter` bootstrap
- `src/routes/index.tsx`: lazy route registry
- `src/routes/meta.ts`: sidebar and breadcrumb metadata
- `src/layouts/app-layout.tsx`: Semi Design console shell
- `src/features/*`: page modules grouped by platform, delivery, observability, access, system, and settings
- `src/services/api-client.ts`: `/api/v1` client with refresh retry logic
- `src/stores/*`: persisted auth, platform scope, and preference state

### Docs

```bash
cd docs
npm install
npm run docs:dev
```

## Local Dependencies

### PostgreSQL

- host: `localhost`
- port: `5432`
- database: `kubecrux`
- username: `pgsql`
- password: `pgsql`

### Redis

- host: `localhost`
- port: `6379`
- password: none

## Current Frontend Surface

- platform: overview, clusters, workloads, network, storage, extensions, helm
- delivery: applications, workflows, releases, registries
- observability: monitoring, alerts, notifications, on-call, events, AI chat
- control plane: access management, system utilities, settings, embedded docs

## Recommended Entry Points

- Repository engineering memory source: repository-root `agents.md`
- [Architecture Entry](/en/architecture/)
- [AI Copilot](/en/architecture/ai-copilot)
- [Application Delivery](/en/architecture/application-delivery)
- [Monitoring And Alerting](/en/architecture/monitoring-and-alerting)
- [Authorization](/en/architecture/authorization)
- [Configuration](/en/operations/configuration)
- [MCP](/en/operations/mcp)
