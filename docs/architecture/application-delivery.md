# Application Delivery

## Goal

soha now owns application registration, multi-source build configuration, environment-scoped delivery orchestration, image replacement deployment, and deploy/release records.

The developer/tester-facing DevOps workbench design is documented separately in [应用交付 DevOps 工作台方案](./delivery-devops-workbench.md). This file remains the implemented application-delivery baseline and compatibility contract.

## Current Implemented Surface

The repository now has a delivery control-plane baseline centered on four stable objects:

- applications
- build templates
- application-environment bindings
- execution records
- delivery blueprints

- application CRUD and detail APIs:
  - `GET /api/v1/applications`
  - `POST /api/v1/applications`
  - `GET /api/v1/applications/:applicationID`
  - `GET /api/v1/applications/:applicationID/detail`
  - `PUT /api/v1/applications/:applicationID`
  - `DELETE /api/v1/applications/:applicationID`
- application service and container APIs:
  - `GET /api/v1/applications/:applicationID/services`
  - `POST /api/v1/applications/:applicationID/services`
  - `GET /api/v1/applications/:applicationID/services/:serviceID`
  - `PUT /api/v1/applications/:applicationID/services/:serviceID`
  - `DELETE /api/v1/applications/:applicationID/services/:serviceID`
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
  - `GET /api/v1/delivery/release-bundles/:bundleID`
  - `GET /api/v1/delivery/execution-tasks`
  - `GET /api/v1/delivery/execution-tasks/:taskID`
  - `POST /api/v1/delivery/execution-tasks/:taskID/cancel`
  - `POST /api/v1/delivery/execution-tasks/:taskID/retry`
  - `GET /api/v1/delivery/execution-tasks/:taskID/runner-status`
  - `GET /api/v1/delivery/approval-policies`
  - `POST /api/v1/delivery/approval-policies`
  - `PUT /api/v1/delivery/approval-policies/:approvalPolicyID`
  - `DELETE /api/v1/delivery/approval-policies/:approvalPolicyID`
  - `POST /api/v1/delivery/execution-callbacks`
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
  - `/delivery/blueprints`
  - `/applications/:applicationId`
  - `/build-templates`
  - `/delivery/release-bundles`
  - `/delivery/execution-tasks`
  - `/delivery/approval-policies`
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
- `application_services`
- `application_service_containers`
- `build_templates`
- `delivery_blueprints`
- `release_bundles`
- `execution_tasks`
- `execution_logs`
- `execution_callbacks`
- `application_environments`
- `release_targets`
- `workflow_templates`
- `workflow_approvals`
- `approval_policies`
- `build_records`
- `workflow_runs`
- `deploy_records`

Application model now keeps:

- name
- repository metadata
- buildSources
- latest execution state via aggregate detail
- environment coverage via release-board aggregate

Application services now keep:

- service key and display name
- service kind such as Kubernetes workload, Helm release, external service, or job
- optional service-level repository and build-source override
- service-owned container definitions with image repository, tag template, Dockerfile path, build context, runtime ports, and metadata

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

1. an application is registered in soha
2. one or more build sources are attached to the application
3. an application-environment binding selects one build source, one workflow template, and explicit platform targets
4. a manual build or workflow build node creates a release bundle and execution task in the execution plane
5. the produced artifact image is recorded on both the build record and the release bundle metadata
6. deployment replaces the target Deployment image in Kubernetes while execution task state is advanced in parallel
7. soha records workflow, execution-task, deploy, and release outcomes

企业 AI coding 场景下，`delivery_blueprints` 作为控制平面模板对象存在，而不是仓库文件本身：

- 蓝图组合应用草稿、build sources、环境绑定模板、目标模板与文件模板
- `render-spec` 返回 `RenderedDeliverySpec`
- `bootstrap-application` 只负责创建或更新平台控制面对象
- v1 不在 API 服务端直接改 Git 仓库文件

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
- `internal/application/execution`
  - release bundle lifecycle
  - execution task lifecycle
  - callback and approval-policy control plane
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
  - release-bundle list
  - execution-task list and log inspection
  - approval-policy management
  - workflow approval/trigger surface
- `web/src/features/delivery/delivery-catalog-pages.tsx`
  - structured application-environment binding form
  - aggregated release board
  - application-environment delivery workspace
- `web/src/features/delivery/delivery-blueprint-pages.tsx`
  - blueprint CRUD
  - rendered spec preview
  - platform bootstrap result inspection

## Data Model Direction

PostgreSQL now holds:

- applications
- application_build_sources
- application_services
- application_service_containers
- build_templates
- release_bundles
- execution_tasks
- execution_logs
- execution_callbacks
- application_environments
- release_targets
- workflow_templates
- workflow_approvals
- approval_policies
- build_records
- deploy_records
- registry_credentials_meta

Runtime execution state should be represented by durable rows plus runner callbacks:

- running build heartbeat in `execution_tasks`
- terminal status and retry ownership in `execution_tasks`
- live and retained log evidence in `execution_logs` and artifacts

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
- approval resolution uses explicit approve/reject APIs and persists `workflow_approvals`; when a workflow was started by AI Gateway approval replay, the approval row metadata keeps the Gateway approval request linkage
- build and release actions now start creating `releaseBundleId` and `executionTaskId` metadata so the execution plane can evolve without breaking the current UI flow
- execution tasks now expose a minimal provider lifecycle: `queued`, `dispatching` or `running`, then callback-driven or direct completion, and task logs are queryable from the delivery control plane
- `ci_agent_runner` now has a first runnable path: the control plane exposes task claim and callback endpoints, the agent can poll for `ci_agent_runner` tasks, execute command payloads locally, and push logs/results back through execution callbacks
- execution-task claim and callback handling now route through the dedicated execution application service, so heartbeat timestamps, release-bundle status, and build/deploy record status stay synchronized from one orchestration path
- `execution_tasks.last_heartbeat_at` is now persisted and reflected in the execution-task page, and callback-driven task updates backfill `build_records` and `deploy_records` with the current execution-task status
- `ci_agent_runner` payloads are now workspace-aware: build/release tasks may carry `workspace.path`, `workspace.commandDir`, `workspace.checkout`, and `workspace.artifactFiles`, and the agent can prepare a local workspace, perform git fetch or checkout, run commands in that directory, and report structured artifact summaries back through callbacks
- asynchronous build callbacks now normalize bundle status to `ready` and update `release_bundles.artifact_ref` or `artifact_digest` from the callback payload instead of leaving build bundles stuck at generic task status values
- the execution application service now runs a background timeout sweep: `dispatching` or `running` tasks whose heartbeat exceeds `timeout_seconds` are marked `callback_timeout`, emit execution logs, and backfill bundle, build-record, and deploy-record failure state automatically
- execution tasks now expose `cancel` and `retry` control-plane actions; retry rotates the callback token before re-queueing, and `ci_agent_runner` now inspects callback responses during heartbeat so a server-side cancel can stop the local shell process instead of only changing task status in the database
- the agent process now keeps an in-memory active-task registry and exposes agent-local runtime APIs for listing active execution tasks, fetching a single active task, and canceling it with the agent bearer token; this is the local runtime surface that later control-plane initiated stop flows can reuse
- execution-task claim now carries a runner runtime endpoint, and the control plane will attempt a direct runtime cancel call before falling back to runner-side status polling; this makes `ci_agent_runner` cancellation proactive instead of heartbeat-only
- `k8s_job_runner` is now wired to server runtime execution-cluster settings (`runtime.execution_job_cluster_id`, `runtime.execution_job_namespace`, `runtime.execution_job_image`, `runtime.execution_job_git_image`, `runtime.execution_job_ttl_seconds`) and the build path can dispatch an isolated Kubernetes Job when that execution cluster is configured
- execution-task DTOs now normalize artifact payloads into a first-class `artifacts[]` view so image outputs, workspace files, and Kubernetes Job evidence are inspectable without manually parsing `result` JSON
- `release_targets` now carry enterprise contract fields such as `targetKind`, `executorKind`, `groupKey`, `waveKey`, `regionKey`, and `configRef`, even though the active UI still defaults to Kubernetes Deployment targets
- arbitrary build commands are only allowed inside the dedicated build worker path, never inline in the Gin request lifecycle
