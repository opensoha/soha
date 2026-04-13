# API Overview

## Versioning

Phase 1 uses `/api/v1`.

## Domain Groups

- `/healthz`
- `/clusters`
- `/workloads`
- `/events`
- `/audit`
- `/access`

## Principles

- frontend consumes aggregated DTOs
- authorization happens server-side
- audit is written for both reads and mutations where relevant
- raw Kubernetes objects are hidden behind dedicated detail endpoints
