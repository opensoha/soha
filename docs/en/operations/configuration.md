# Configuration

## Source Of Truth

Backend configuration is file-first.

- primary file: `configs/config.yaml`
- loader: `internal/infrastructure/config`
- override mechanism: environment variables through Viper when needed

Frontend runtime is currently code-first rather than env-first.

- API base path is fixed to `/api/v1` in `web/src/services/api-client.ts`
- local development proxies `/api` to `http://localhost:8080` in `web/vite.config.ts`
- the docs surface inside the console points to `/docs/`
- there are no checked-in `web/.env.*` templates at the moment

## Backend Config Shape

```yaml
app:
http:
logger:
runtime:
database:
redis:
auth:
gitlab:
swagger:
mcp:
bootstrap:
kubernetes:
monitoring:
```

Key backend fields now used by the runtime:

- `http.cors_allowed_origins`: allowed browser origins for the frontend console
- `auth.jwt.secret`, `auth.jwt.access_ttl`, `auth.jwt.refresh_ttl`
- `auth.dev_principal.*`: bootstrap local account seed and, when enabled, the no-token development principal
- `auth.oidc.*`: issuer, client, callback, frontend redirect, and default role mapping
- `gitlab.enabled`, `gitlab.base_url`, `gitlab.token`, `gitlab.group_id`, `gitlab.per_page`, `gitlab.timeout`
- `runtime.workflow_workers`, `runtime.workflow_queue_size`, `runtime.workflow_node_parallelism`
- `runtime.cluster_sync_parallelism`, `runtime.copilot_inspection_parallelism`, `runtime.alert_upsert_batch_size`
- `runtime.execution_runner_token`: shared bearer token for delivery, Docker, and AI Agent Runtime runner claim/callback APIs
- `database.migration_path`: migration root directory (runtime resolves `migrations/<driver>/0001_init.sql`)
- `database.migration_file`: explicit SQL bootstrap file override; the current repo baseline points at the consolidated PostgreSQL bootstrap file
- `kubernetes.clusters[*].kubeconfig`: direct cluster file path for bootstrapped clusters
- `kubernetes.clusters[*].kubeconfig_data`: inline kubeconfig support when file paths are not used
- `mcp.default_timeout`: default timeout reused by MCP and agent HTTP integrations
- `monitoring.enabled`: toggles monitoring ingress endpoints
- `monitoring.webhook_token`: shared token accepted by the alert webhook endpoint

## Local Development Defaults

### PostgreSQL

- host: `localhost`
- port: `5432`
- database: `soha`
- user: `pgsql`
- password: `pgsql`

### Redis

- addr: `localhost:6379`
- password: empty
- db: `0`

## Identity Defaults

The default local bootstrap account comes from `auth.dev_principal` inside `config.yaml`.

- username: `admin`
- email: `admin@soha.local`
- password: `soha`

When `auth.enable_dev_auth` is `false`, this account is still seeded into PostgreSQL for real password login. The flag only controls whether the backend accepts an automatic development principal when no bearer token is present. The runtime no longer keeps a legacy bootstrap migration or password-login fallback path.

## Operational Conventions

- request ID header: `X-Request-Id`
- health check: `GET /healthz`
- readiness check: `GET /readyz`
- versioned API base path: `/api/v1`
- swagger route reservation: `/swagger/*any`
- config override file: `SOHA_CONFIG_FILE=/abs/path/to/config.yaml`
- frontend local dev port: `5173`
- docs local dev port: `3000`

## Database Bootstrap

On startup the backend can:

1. run SQL migration file
2. create schema only from the migration file
3. seed bootstrap user from `auth.dev_principal`
4. seed password hash and role bindings for the bootstrap account
5. seed default RBAC roles
6. seed default ABAC policies
7. seed configured clusters into `clusters`
8. seed file-configured direct cluster metadata into `cluster_credentials_meta`

## Cluster Registration API

The runtime now supports two persisted cluster connection modes:

- `direct_kubeconfig`
  - request payload includes `kubeconfig` and optional `context`
  - kubeconfig is validated before the cluster is registered into `cluster-manager`
  - informer/cache is started dynamically for the new cluster
- `agent`
  - request payload includes `agentEndpoint` and optional `agentToken`
  - Gin uses the stored endpoint and token to pull summary, list resources, and send execution actions

Relevant routes:

- `POST /api/v1/clusters`
- `POST /api/v1/clusters/:clusterID/workloads/deployments/restart`
- `POST /api/v1/clusters/:clusterID/workloads/deployments/scale`

## Monitoring Runtime

The monitoring runtime currently expects file-configured values under:

```yaml
monitoring:
  enabled: true
  webhook_token: dev-alert-webhook-token
```

Current persistence layout:

- `alert_instances`: normalized inbound alert inventory
- `notification_channels`: downstream channel definitions
- `event_stream`: normalized alert activity for the unified event center

Current public or protected routes:

- `POST /api/v1/integrations/alerts/webhook`
- `GET /api/v1/monitoring/summary`
- `GET /api/v1/alerts`
- `GET /api/v1/notification-channels`
- `POST /api/v1/notification-channels`
- `PUT /api/v1/notification-channels/:channelID`

## Application Registry And GitLab

Application management now uses one PostgreSQL-backed registry table:

- `applications`
- `build_records`
- `ai_sessions`
- `ai_messages`

GitLab integration is configured in `config.yaml`:

```yaml
gitlab:
  enabled: false
  base_url: https://gitlab.com/api/v4
  token: ""
  group_id: ""
  per_page: 50
  timeout: 10s
```

When enabled, soha serves these routes from the backend:

- `GET /api/v1/applications`
- `POST /api/v1/applications`
- `GET /api/v1/applications/:applicationID`
- `PUT /api/v1/applications/:applicationID`
- `DELETE /api/v1/applications/:applicationID`
- `GET /api/v1/integrations/gitlab/projects`
- `GET /api/v1/integrations/gitlab/branches`
- `GET /api/v1/integrations/gitlab/tags`

Build and AI routes now also exist:

- `GET /api/v1/builds`
- `POST /api/v1/builds/trigger`
- `GET /api/v1/copilot/insights`
- `GET /api/v1/copilot/sessions`
- `GET /api/v1/copilot/sessions/:sessionID`
- `POST /api/v1/copilot/sessions`
- `PATCH /api/v1/copilot/sessions/:sessionID`
- `DELETE /api/v1/copilot/sessions/:sessionID`
- `GET /api/v1/copilot/sessions/:sessionID/messages`
- `POST /api/v1/copilot/sessions/:sessionID/messages`
- `POST /api/v1/copilot/sessions/:sessionID/analyze`
- `GET /api/v1/copilot/agent-providers`
- `GET /api/v1/copilot/agent-runs`
- `POST /api/v1/copilot/agent-runs/claim`
- `POST /api/v1/copilot/agent-runs/callback`
- `POST /api/v1/copilot/agent-runs/tool-call`

The browser never talks to GitLab directly. Tokens stay in backend configuration.

## Agent Runner Config

`cmd/agent` can also claim AI Agent Runtime work from the control plane. The server and agent must share the same `runtime.execution_runner_token` / `control_plane.bearer_token` value.

Minimal Hermes runner config:

```yaml
control_plane:
  enabled: true
  base_url: http://127.0.0.1:8080
  bearer_token: demo-execution-runner-token
  agent_id: local-agent
  agent_runtime:
    enabled: true
    worker_id: local-hermes-runner
    provider_ids:
      - hermes
    provider_kinds:
      - hermes
    hermes_command: hermes
    workspace_root: ./.soha/agent-runtime
    poll_interval: 5s
```

The AI workbench and automation policy choose `agentProviderId`; the runner only executes matching `AgentRun` rows. Provider-specific commands and workspaces belong in agent config, not in browser payloads. External providers should invoke soha read-only tools through `/api/v1/copilot/agent-runs/tool-call`, which validates the runner token, per-run callback token, and `AgentRun.toolBindings` snapshot before recording `ToolExecution`. The built-in Hermes/CLI POC prefetches a small read-only tool context into the provider prompt: events, logs, metrics, traces, delivery releases, delivery builds, and alerts are currently executable; Docker, virtualization, execution-task, platform-resource, and on-call route bindings remain catalog contracts until their readers/adapters are wired. Richer provider-native or MCP client tool protocols should still terminate at the same soha tool-call gateway.
