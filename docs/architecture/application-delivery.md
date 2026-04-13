# Application Delivery

## Goal

kubecrux should own application registration, manual build execution, image replacement deployment, and release records.

## Current Implemented Surface

The repository now has a real delivery control-plane baseline.

- application CRUD APIs:
  - `GET /api/v1/applications`
  - `POST /api/v1/applications`
  - `GET /api/v1/applications/:applicationID`
  - `PUT /api/v1/applications/:applicationID`
  - `DELETE /api/v1/applications/:applicationID`
- GitLab browse APIs:
  - `GET /api/v1/integrations/gitlab/projects`
  - `GET /api/v1/integrations/gitlab/branches`
  - `GET /api/v1/integrations/gitlab/tags`
- current frontend routes:
  - `/applications`
  - `/workflows`
  - `/releases`
  - `/registries`
- PostgreSQL table:
  - `applications`

Current frontend application view fields:

- name
- repo
- branch
- status
- lastDeployedAt

Current build surface:

- `GET /api/v1/builds`
- `POST /api/v1/builds/trigger`
- `build_records` now stores manual build trigger requests
- each accepted trigger also emits a unified build event into `event_stream`
- the current refactored frontend does not yet expose a dedicated build history page

The current desired model is not GitOps-only and not a fake mock pipeline. It is a real platform workflow where:

1. an application is registered in kubecrux
2. build configuration is attached to the application
3. a manual build is triggered
4. the build runs a full Docker-based process
5. the produced image is recorded
6. deployment replaces the workload image in Kubernetes
7. kubecrux records the release and deployment outcome

## Recommended Modules

### Backend

- `internal/application/app`
  - application registry
  - ownership and environment binding
  - GitLab repository selection orchestration
- `internal/application/build`
  - build request orchestration
  - manual run execution
  - build record lifecycle
- `internal/application/release`
  - image rollout orchestration
  - deploy record lifecycle
- `internal/infrastructure/integration/scm`
  - source repository adapters
- `internal/infrastructure/integration/runner`
  - docker build executor or worker connector
- `internal/repository/app`
- `internal/repository/build`
- `internal/repository/release`

### Frontend

- `web/src/features/delivery/delivery-pages.tsx`
  - applications CRUD
  - workflow trigger surface
  - releases list and deploy action
  - registry connection management
- future:
  - dedicated build history page
  - richer delivery detail views
  - shared delivery form and record-table abstractions

## Data Model Direction

PostgreSQL should hold:

- applications
- application_env_bindings
- build_records
- build_steps
- deploy_records
- release_records
- registry_credentials_meta

Redis should hold:

- running build heartbeat
- distributed execution locks
- short-lived live log stream state

## Execution Direction

The platform should reserve two runtime layers:

- control plane in the API server
- execution plane in background workers or runners

The API server should never run long Docker builds inline in Gin handlers.

## Kubernetes Delivery Direction

Release execution should operate on platform views such as:

- target cluster
- target namespace
- workload kind
- workload name
- target container
- new image reference

This keeps frontend and application service code from dealing with raw manifest mutation everywhere.

## Next Step Reserve

The next increment should add:

- manual build trigger contract
- build record lifecycle on `build_records`
- docker build execution runner integration
- image replacement deployment contract
- deploy and release records
