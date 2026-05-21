# Database Schema

## PostgreSQL Tables

### users

Core identity profile.

Suggested JSONB fields:

- `tags`
- `preferences`

### teams

Organizational unit with optional JSONB metadata.

### projects

Project scope bound to teams and environments.

### roles

System and custom roles with JSONB capability descriptors.

### policies

ABAC policy definitions. JSONB fits:

- subject matcher
- target matcher
- conditions
- action list

### policy_bindings

Maps policies to users, teams, projects, or roles.

### clusters

Cluster registry metadata and health snapshot.

JSONB fits:

- labels
- capabilities
- health_snapshot

### cluster_credentials_meta

Credential metadata only, not raw secret material in phase 1.

JSONB fits:

- provider metadata
- auth plugin settings

### announcements

Announcement catalog with publish lifecycle, audience metadata, and active-window fields.

### announcement_receipts

Per-user read receipts for published announcements.

### audit_logs

Append-only durable audit store.

JSONB fits:

- request_meta
- target_meta
- decision_meta

### operation_logs

Operational task records for mutating workflows.

Expected fields now include actor context, request context, and backend-owned `target_scope` JSON for UI formatting.

### event_stream

Unified event envelope persistence.

JSONB fits:

- resource_ref
- payload
- correlation data

### build_records

Build history plus worker-emitted artifact metadata.

Expected metadata now includes:

- `applicationEnvironmentId`
- `buildSourceId`
- `artifact`
- `image`
- `variables`
- `triggeredByWorkflowRunId`

### deploy_records

Deploy and release history.

Expected metadata now includes:

- `applicationEnvironmentId`
- `actionKind`
- `previousImage`
- `image`
- `imageTag`

### application_build_sources

Per-application build-source registry.

### build_templates

Platform-managed Dockerfile and build-command templates.

### release_bundles

Immutable delivery version unit for promotion and audit.

### execution_tasks

Execution-plane task records for build, deploy, verification, and callback-driven provider work.

### execution_logs

Task-scoped execution logs persisted independently from build/release summary rows.

### execution_callbacks

Provider callback history used to advance execution-task state.

### virtualization_connections

KubeVirt and PVE connection records for the virtualization workbench. PVE secret material is stored only in `encrypted_credential`; API responses expose `credentialConfigured` instead of plaintext.

### virtualization_vms

Synchronized virtual machine assets keyed by provider, connection, and external ID.

### virtualization_images

Synchronized image and template assets discovered from KubeVirt CDI/PVC sources or PVE template, ISO, and storage content.

### virtualization_flavors

Standard VM sizing catalog managed by kubecrux or discovered from providers.

### virtualization_tasks

Virtualization task queue for `vm_create`, `vm_action`, and `asset_sync`. This domain does not reuse delivery `execution_tasks`.

### virtualization_task_logs

Task-scoped logs for virtualization operations and sync runs.

### workflow_approvals

Approval decisions for suspended workflow runs.

### approval_policies

Reusable approval configuration for multi-step delivery governance.

### notification_channels

Reserved for email, webhook, chat, or incident channel settings.

### saved_views

User or team saved filters, tables, and dashboards.

### user_preferences

Persistent UI preferences and defaults.

## PostgreSQL vs Redis

Use PostgreSQL when:

- durability matters
- relational search matters
- retention matters
- auditability matters

Use Redis when:

- data is short-lived
- low latency fanout matters
- cache invalidation can be event-driven
- distributed lock semantics are needed
