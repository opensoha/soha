---
name: kubecrux-backend
description: >-
  Implement and refactor kubecrux backend capabilities in `cmd/**`,
  `internal/**`, and `configs/**` for Go 1.23, Gin, PostgreSQL, Kubernetes
  `client-go`, and agent-connected clusters. Use when adding or changing HTTP
  routes, handlers, application services, repositories, policy checks,
  bootstrap wiring, cluster or resource aggregation, or delivery,
  observability, and control-plane APIs. This skill enforces the
  modular-monolith layers, platform view-model APIs instead of raw Kubernetes
  objects, explicit cluster and namespace scope semantics, permission-key
  aligned authorization, audit and operation logging for important actions, and
  direct-versus-agent cluster behavior rules.
---

# Kubecrux Backend

## Overview

Implement backend changes through the repository's layered Go architecture. Keep handlers thin, put behavior in application services, and expose aggregated platform-facing contracts instead of leaking raw infrastructure details.

## Workflow

1. Identify the change boundary first: transport, orchestration, policy, infrastructure, repository, or bootstrap.
2. Read the existing handler, service, repository, and route wiring before editing. Follow the current module rather than creating a parallel path.
3. Keep authorization, scope semantics, audit, and operation logging aligned with the behavior change.
4. For Kubernetes-facing work, decide whether the capability should use informer/cache, live query fallback, or agent mode, and make unsupported agent paths explicit.
5. Update tests, config defaults, deployment-facing manifests, and memory or docs in the same task when contracts or semantics change.
6. Validate with focused `go test` runs, or at minimum with the affected package tests and a repo build path.

## Non-Negotiables

- `internal/api` parses requests, maps errors, and returns HTTP responses. It must not own Kubernetes traversal or policy decisions.
- `internal/application` owns orchestration, scope handling, authorization checks, audit recording, and view-model shaping.
- `internal/repository` owns durable persistence details. Keep SQL and GORM concerns out of handlers and orchestration code.
- `internal/infrastructure` owns external clients and vendor-specific wiring such as Kubernetes managers, informer startup, agent HTTP clients, config loading, DB, Redis, Swagger, and MCP registries.
- `internal/bootstrap` wires dependencies and startup lifecycle. Do not hide new cross-module dependencies in ad hoc globals.
- Prefer domain or platform view models for API output. Do not return raw Kubernetes schema objects unless the route is explicitly a YAML or passthrough surface.

## Platform and Authorization Rules

- List endpoints must respect cluster scope and namespace scope. Empty namespace means all namespaces for namespaced resources.
- Cluster-scoped resources must ignore namespace filters instead of pretending to support them.
- Agent-mode gaps must surface as unsupported or degraded behavior, never as silent parity.
- Important reads, writes, and operational actions should record audit logs. Mutations should also record operation logs where the existing module already does so.
- Backend permission checks, route visibility, and menu visibility are related but separate. Keep permission keys aligned with frontend expectations.
- Prefer backend aggregation over frontend joins and namespace fan-out, especially for platform pages.

## Read These References When Needed

- `references/architecture.md`: module responsibilities, bootstrap wiring, and where common backend changes should land.
- `references/platform-rules.md`: cluster-access behavior, scope semantics, performance expectations, and backend verification prompts.

## Done Criteria

- Layer boundaries remain intact.
- Scope semantics and authorization behavior are explicit.
- New platform reads avoid unnecessary live-query or frontend fan-out regressions.
- Affected packages are tested, and memory or docs are updated when contracts changed.
