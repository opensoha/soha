---
id: index
slug: /
title: kubecrux Docs
description: Architecture, development, API, and operations documentation for the kubecrux platform console.
---

# kubecrux Docs

kubecrux is a multi-cluster Kubernetes platform console. It is not a thin wrapper around the upstream Kubernetes Dashboard. The product is meant to act as a unified control surface for platform teams across cluster access, workload operations, delivery workflows, authorization, alert collaboration, and AI-assisted analysis.

## Site Baseline

- The docs site is built with Docusaurus and evolves with the repository
- Chinese is the default docs set and English is exposed under `/docs/en/`
- Local docs development runs at `http://localhost:3000/docs/`
- Production serving is expected at same-origin `/docs/`

## Start Here

- [Architecture Entry](./architecture/index.md)
- [Local Development](./development/local-development.md)
- [API Overview](./api/overview.md)
- [Operations Configuration](./operations/configuration.md)
- [Roadmap](./roadmap/index.md)

## Current Product Surface

- Platform management: clusters, nodes, namespaces, workloads, network, storage, extensions, and Helm
- Delivery: applications, environments, workflows, releases, and registries
- Observability: monitoring, alerts, notifications, events, and AI observation workflows
- Access and system management: users, roles, groups, policies, menus, audit, and settings

## Repository Layout

- `cmd`: server and agent entrypoints
- `internal`: backend API, application, policy, infrastructure, and repository layers
- `web`: React 18 + Vite 6 + TypeScript 5 console
- `docs`: Docusaurus documentation site
- `configs`: server and agent configuration
- `migrations`: schema bootstrap and migration SQL

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

### Docs

```bash
cd docs
npm install
npm run dev
```

When `web` is running at the same time, Vite proxies same-origin `/docs/` traffic to `http://localhost:3000/docs/`.

## Recommended Reading

- Repository engineering spec: root `AGENTS.md`
- [Architecture Entry](./architecture/index.md)
- [Application Delivery](./architecture/application-delivery.md)
- [Monitoring And Alerting](./architecture/monitoring-and-alerting.md)
- [Authorization](./architecture/authorization.md)
- [AI Copilot](./architecture/ai-copilot.md)
- [MCP Integration](./architecture/mcp-integration.md)
