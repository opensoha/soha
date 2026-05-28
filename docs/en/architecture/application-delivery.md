# Application Delivery

## Goal

soha now owns application registration, delivery templates, environment-scoped orchestration, and execution records.

## Current Implemented Surface

The repository now has a real delivery control-plane baseline centered on stable control-plane objects:

- applications
- build templates
- application-environment bindings
- execution records
- delivery blueprints

- application CRUD APIs:
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
- delivery blueprint APIs:
  - `GET /api/v1/delivery/blueprints`
  - `POST /api/v1/delivery/blueprints`
  - `PUT /api/v1/delivery/blueprints/:blueprintID`
  - `POST /api/v1/delivery/blueprints/:blueprintID/render-spec`
  - `POST /api/v1/delivery/blueprints/:blueprintID/bootstrap-application`
- execution-plane APIs:
  - `GET /api/v1/delivery/release-bundles`
  - `GET /api/v1/delivery/execution-tasks`
  - `POST /api/v1/delivery/execution-tasks/:taskID/cancel`
  - `POST /api/v1/delivery/execution-tasks/:taskID/retry`
  - `GET /api/v1/delivery/execution-tasks/:taskID/runner-status`
- frontend routes:
  - `/applications`
  - `/delivery/blueprints`
  - `/build-templates`
  - `/application-environments`
  - `/release-board`
  - `/delivery/release-bundles`
  - `/delivery/execution-tasks`
  - `/delivery/approval-policies`

PostgreSQL now holds at least:

- `applications`
- `application_build_sources`
- `build_templates`
- `delivery_blueprints`
- `application_environments`
- `release_targets`
- `release_bundles`
- `execution_tasks`
- `execution_logs`
- `execution_callbacks`

## Delivery Blueprint Direction

`delivery_blueprints` is now a first-class control-plane object for enterprise AI coding and fast application onboarding.

A blueprint can combine:

- application draft metadata
- build sources
- environment-binding templates
- target templates
- scaffold file templates
- execution hints
- post-create actions

The rendered output is `RenderedDeliverySpec`, which is intended for:

- enterprise AI coding clients
- platform operators
- future onboarding automation

`bootstrap-application` is control-plane first:

- it creates or updates platform delivery objects
- it does not directly mutate the git repository in v1
- external AI or IDE clients may still materialize files in the repository

## Execution Direction

The platform should reserve two runtime layers:

- control plane in the API server
- execution plane in background workers or runners

The API server should never run long Docker builds inline in Gin handlers.

## Recommended Modules

### Backend

- `internal/application/app`
  - application registry
  - ownership and environment binding
- `internal/application/build`
  - build request orchestration
- `internal/application/execution`
  - execution-task lifecycle
- `internal/application/release`
  - deploy and release orchestration
- `internal/application/delivery`
  - aggregate views
  - blueprint rendering
  - blueprint-to-control-plane bootstrap

### Frontend

- `web/src/features/delivery/delivery-app-pages.tsx`
  - application and execution views
- `web/src/features/delivery/delivery-catalog-pages.tsx`
  - environment bindings and release board
- `web/src/features/delivery/delivery-blueprint-pages.tsx`
  - blueprint CRUD
  - rendered spec preview
  - platform bootstrap result inspection

## Near-Term Expectations

- application onboarding should prefer blueprints plus rendered spec instead of one-off manual form sequences
- build and release actions should continue flowing through release bundles and execution tasks
- platform-managed specification rendering should remain separate from repository file mutation
