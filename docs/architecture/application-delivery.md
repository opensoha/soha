# Application Delivery

## Goal

kubecrux now owns application registration, multi-source build configuration, environment-scoped delivery orchestration, image replacement deployment, and deploy/release records.

## Current Implemented Surface

The repository now has a delivery control-plane baseline centered on four stable objects:

- applications
- build templates
- application-environment bindings
- execution records

- application CRUD and detail APIs:
  - `GET /api/v1/applications`
  - `POST /api/v1/applications`
  - `GET /api/v1/applications/:applicationID`
  - `GET /api/v1/applications/:applicationID/detail`
  - `PUT /api/v1/applications/:applicationID`
  - `DELETE /api/v1/applications/:applicationID`
- build-template APIs:
  - `GET /api/v1/build-templates`
  - `POST /api/v1/build-templates`
  - `PUT /api/v1/build-templates/:buildTemplateID`
  - `DELETE /api/v1/build-templates/:buildTemplateID`
- application-environment detail and target-candidate APIs:
  - `GET /api/v1/application-environments/:applicationEnvironmentID/detail`
  - `GET /api/v1/application-environments/target-candidates`
- delivery aggregate API:
  - `GET /api/v1/delivery/release-board`
- workflow approval APIs:
  - `POST /api/v1/workflows/:workflowRunID/approve`
  - `POST /api/v1/workflows/:workflowRunID/reject`
- GitLab browse APIs:
  - `GET /api/v1/integrations/gitlab/projects`
  - `GET /api/v1/integrations/gitlab/branches`
  - `GET /api/v1/integrations/gitlab/tags`
- current frontend routes:
  - `/applications`
  - `/applications/:applicationId`
  - `/build-templates`
  - `/application-environments`
  - `/application-environments/:applicationEnvironmentId`
  - `/workflow-templates`
  - `/release-board`
  - `/workflows`
  - `/releases`
  - `/registries`
- PostgreSQL tables:
  - `applications`
  - `application_build_sources`
  - `build_templates`
  - `application_environments`
  - `release_targets`
  - `workflow_templates`
  - `workflow_approvals`
  - `build_records`
  - `workflow_runs`
  - `deploy_records`

Application model now keeps:

- name
- repository metadata
- buildSources
- latest execution state via aggregate detail
- environment coverage via release-board aggregate

The backend still accepts legacy top-level application build fields for compatibility and migration, but the active web application center now edits delivery build configuration through `buildSources` only.

Build-source types:

- `repo_dockerfile`
- `platform_build_template`
- `external_pipeline`

DAG templates remain environment-scoped delivery orchestration templates and are not treated as build-source variants.

Current build surface:

- `GET /api/v1/builds`
- `POST /api/v1/builds/trigger`
- `build_records` now stores manual build requests plus worker-completed artifact metadata
- each accepted trigger also emits a unified build event into `event_stream`
- DAG `build` nodes now reuse the same build service path and can emit artifact metadata for downstream `deploy_update_image` nodes

The current model is not GitOps-only and not a fake mock pipeline. It is a real platform workflow where:

1. an application is registered in kubecrux
2. one or more build sources are attached to the application
3. an application-environment binding selects one build source, one workflow template, and explicit platform targets
4. a manual build or workflow build node runs through the build service
5. the produced artifact image is recorded on the build record metadata
6. deployment replaces the target Deployment image in Kubernetes
7. kubecrux records workflow, deploy, and release outcomes

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
  - deploy/release image rollout orchestration
  - deploy record lifecycle
- `internal/application/delivery`
  - aggregate detail/read models for application detail, application-environment detail, release board, and target candidates
- `internal/infrastructure/integration/scm`
  - source repository adapters
- `internal/infrastructure/integration/runner`
  - docker/buildx/kaniko/custom worker connector
- `internal/repository/app`
- `internal/repository/build`
- `internal/repository/release`

### Frontend

- `web/src/features/delivery/delivery-app-pages.tsx`
  - application list
  - application detail
  - build-template management
  - workflow approval/trigger surface
- `web/src/features/delivery/delivery-catalog-pages.tsx`
  - structured application-environment binding form
  - aggregated release board
  - application-environment delivery workspace

## Data Model Direction

PostgreSQL now holds:

- applications
- application_build_sources
- build_templates
- application_environments
- release_targets
- workflow_templates
- workflow_approvals
- build_records
- deploy_records
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

Release execution operates on platform views such as:

- target cluster
- target namespace
- workload kind
- workload name
- target container
- new image reference

v1 target binding remains explicit and Deployment-only. The browser no longer acts as the source of truth for workload names during the main binding flow.

This keeps frontend and application service code from dealing with raw manifest mutation everywhere.

## Execution Semantics

- `dev/test/pre` use deploy semantics in the UI even when they run the same orchestration model
- `prod` uses release semantics in the UI
- `manual_approval` nodes now suspend a workflow run with status `waiting_approval`
- approval resolution uses explicit approve/reject APIs and persists `workflow_approvals`
- arbitrary build commands are only allowed inside the dedicated build worker path, never inline in the Gin request lifecycle
