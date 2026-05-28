# soha AI Gateway

## Goal

`soha AI Gateway` is the AI-native operations entry point for external AI coding tools, IDE agents, CI automation, and enterprise agent platforms.

It is not a CLI copy of the console and it must not let MCP touch databases or Kubernetes directly. Gateway receives external AI requests inside the soha security boundary and then reuses existing application services for queries, delivery actions, diagnosis, and evidence persistence.

## Boundary With AI Workbench

The existing `AI Workbench`, MCP adapters, Agent Runtime, and execution plane are reusable foundations, but they are not the Gateway layer itself:

- `AI Workbench`: internal soha AI workspace for sessions, toolsets, inspection, RCA, and analysis artifacts.
- `Agent Runtime`: durable runtime for internal or external agent providers, claim/callback, tool bindings, skill bindings, and artifact normalization.
- `MCP adapter`: capability directory for soha tools and external data sources.
- `execution plane`: durable runner/callback model for build, release, Docker, and virtualization tasks.
- `AI Gateway`: secure manifest and invocation boundary for external AI clients.

Standard flow:

```text
AI Client / soha-cli / MCP
  -> AI Gateway
  -> permissionKeys / scope grants / AI grants / risk policy / audit
  -> delivery, resource, copilot, docker, virtualization services
  -> execution plane or Agent Runtime when needed
```

## CLI, MCP, And Skills

`soha-cli` is the local entry point:

- `login`
- `profile`
- `context`
- `capabilities`
- `mcp start`
- `mcp install`
- `skill install`
- `diagnose`

The current code entry point is `cmd/soha-cli`, with the implementation in `internal/cli/sohacli`. The first version implements `login`, `profile`, `context`, `capabilities`, `mcp start`, `mcp install`, `skill list`, `skill install`, and `diagnose`.

The CLI owns authentication, config, MCP launch, and manual fallback commands only. Real platform actions must go through soha APIs. Local profiles are stored in `~/.soha/config.json` by default, written with `0600` permissions, and `profile show` only displays redacted tokens.

`soha MCP Server` reads `GET /api/v1/ai-gateway/capabilities` to dynamically expose tools, resources, prompts, and skills available to the current identity. MCP may hide unauthorized tools, but hidden tools are not the security boundary; backend application services must re-check permission, scope, grants, and risk policy on every invocation.

`soha Skills` tell AI clients how to work under soha conventions. The first files live under `skills/ai-gateway`. Skills do not grant permissions by themselves.

## Identity Model

AI Gateway has three identity layers:

1. Personal identity
   - `soha login` issues local CLI/MCP tokens.
   - It inherits user roles, permission keys, teams, and scope grants.
   - Gateway personal tokens use the `soha_pat_` opaque token prefix. The database stores only a hash and display prefix.
2. Service identity
   - `service_accounts` and `service_account_tokens` are for CI, webhooks, shared runners, and automation.
   - Service identities need explicit roles, scope grants, expiration, and revocation.
   - Service-account tokens use the `soha_sat_` opaque token prefix and resolve to a `service_account:<id>` principal.
3. AI client identity
   - `ai_clients` records callers such as Cursor, Codex, Claude Code, CI agents, and enterprise agent platforms.
   - Audit records must include user or service account, AI client, skill, and tool.

## Authorization Model

AI Gateway uses four authorization layers:

1. `permissionKeys`
   - Reuses the existing role permission system.
   - `ai.gateway.view` allows reading Gateway manifests.
   - `ai.gateway.invoke` allows invoking already-authorized tools through Gateway.
   - `ai.gateway.manage` allows managing AI clients, service accounts, tool grants, skill bindings, and access policies.
2. resource scopes
   - Reuses application, environment, business line, cluster, and namespace scope grants.
3. MCP tool grants
   - `mcp_tool_grants` controls which tools a subject and AI client may call.
   - Tool grants can only narrow access; they cannot bypass `permissionKeys`.
   - If no grant is configured, `permissionKeys` decide exposure; once allow grants exist, they form an allow-list; deny grants always win.
4. risk policy
   - `ai_access_policies` controls the Gateway risk boundary for subjects, roles, and AI clients.
   - Deny policies always win; once allow policies exist, they form an allow-list.
   - Policies can narrow capabilities by tool pattern, skill, risk level, and resource scope, and can mark matching tools as requiring approval.
   - Risk levels are `read`, `analyze`, `mutate`, `execute`, and `high`.
   - High-risk actions require approval, confirmation, redaction, or explicit denial.
5. skill bindings
   - `ai_gateway_skill_bindings` controls which soha Skills a subject, role, or AI client may use and which capability refs each skill may expose.
   - Skill bindings only narrow manifests and invocations. They do not grant permissions.

## First API

```http
GET /api/v1/ai-gateway/capabilities
POST /api/v1/ai-gateway/tools/:toolName/invoke
```

`capabilities` returns the visible manifest for the current identity. `tools/:toolName/invoke` is the unified invocation entry point for MCP, CLI, and external AI agents. Every invocation must re-check `ai.gateway.invoke`, the tool's domain permissions, scopes, AI grants, and risk policy before calling the owning application service.

The first directly invokable tools cover delivery and read-only Kubernetes diagnosis:

- `delivery.applications.list`
- `delivery.applications.create`
- `delivery.application_environments.list`
- `delivery.actions.trigger`
- `delivery.release_bundles.list`
- `delivery.execution_tasks.list`
- `k8s.pods.list`
- `k8s.pods.logs`
- `k8s.deployments.list`
- `k8s.services.list`
- `k8s.events.list`
- `diagnosis.release_failure.analyze`

Kubernetes tools read through `internal/application/resource`, so they keep the platform view-model contract, direct/agent cluster boundaries, cluster/namespace scope, and resource permissions. The release-failure diagnosis tool collects delivery execution, release bundle, Pod, Deployment, Service, Event, and log context; Gateway applies basic sensitive-field redaction to log outputs.

Credential endpoints:

```http
GET  /api/v1/ai-gateway/personal-access-tokens
POST /api/v1/ai-gateway/personal-access-tokens
POST /api/v1/ai-gateway/personal-access-tokens/:tokenID/revoke
GET  /api/v1/ai-gateway/service-accounts
POST /api/v1/ai-gateway/service-accounts
POST /api/v1/ai-gateway/service-accounts/:serviceAccountID/tokens
POST /api/v1/ai-gateway/service-account-tokens/:tokenID/revoke
```

AI client and tool grant management endpoints:

```http
GET    /api/v1/ai-gateway/ai-clients
POST   /api/v1/ai-gateway/ai-clients
PUT    /api/v1/ai-gateway/ai-clients/:clientID
GET    /api/v1/ai-gateway/tool-grants
POST   /api/v1/ai-gateway/tool-grants
DELETE /api/v1/ai-gateway/tool-grants/:grantID
GET    /api/v1/ai-gateway/access-policies
POST   /api/v1/ai-gateway/access-policies
PUT    /api/v1/ai-gateway/access-policies/:policyID
DELETE /api/v1/ai-gateway/access-policies/:policyID
GET    /api/v1/ai-gateway/skill-bindings
POST   /api/v1/ai-gateway/skill-bindings
PUT    /api/v1/ai-gateway/skill-bindings/:bindingID
DELETE /api/v1/ai-gateway/skill-bindings/:bindingID
```

`tool-grants` supports `user`, `service_account`, `role`, and `ai_client` subjects. At runtime, Gateway combines grants from the current subject, roles, and AI client: deny wins, and any allow grant creates an allow-list.

`access-policies` and `skill-bindings` support the same `user`, `service_account`, `role`, and `ai_client` subjects. At runtime, Gateway combines enabled records from the current subject, roles, and AI client: access policies narrow tools and skills by deny/allow semantics, and skill bindings then narrow manifests and invocations by bound skill/capability refs. These controls run after `permissionKeys`, so they cannot expand RBAC or scope grants.

Recommended headers:

- `Authorization: Bearer <token>`
- `X-Soha-AI-Client-ID`
- `X-Soha-AI-Client`
- `X-Soha-Skill-ID`
- `X-Soha-Source`

The response returns currently available:

- `tools`
- `resources`
- `prompts`
- `skills`
- `permissionKeys`
- caller context
- manifest summary

The first manifest covers delivery and read-only Kubernetes diagnosis:

- application list and create
- application environment bindings
- build/deploy/build_deploy/workflow/verify trigger entry point
- release bundle and execution task queries
- Pod, Deployment, Service, Event, and log read-only diagnosis
- release failure diagnosis context generation

## Data Objects

Incremental migration `0015_ai_gateway.sql` creates the Gateway control-plane tables:

- `personal_access_tokens`
- `service_accounts`
- `service_account_tokens`
- `ai_clients`
- `ai_access_policies`
- `mcp_tool_grants`
- `ai_gateway_skill_bindings`
- `ai_gateway_audit_logs`

These tables belong to CLI/MCP/service-account/AI-client enterprise access. Existing AI Workbench objects such as `ai_agent_runs`, tool bindings, skill bindings, and analysis artifacts remain owned by Copilot/Agent Runtime.

Every Gateway tool invocation is recorded in both the generic audit log and the dedicated `ai_gateway_audit_logs` table. The dedicated row captures actor type and ID, AI client, skill, tool, risk level, resource scope, request/result, and redacted related metadata; it must not store tokens, kubeconfigs, environment variables, raw log bodies, or full tool input.

## Engineering Rules

- Gateway handlers parse transport and return DTOs only.
- `internal/application/aigateway` owns manifest, authorization, audit, and tool invocation orchestration.
- Real actions must call the owning application service.
- Build, release, Docker, and virtualization work must reuse durable execution/operation/task models.
- AI analysis must reuse Copilot/Agent Runtime `AgentRun` and `AnalysisArtifact`.
- Tokens, secrets, kubeconfigs, and environment variables must not be written into logs, audit metadata, or AI artifacts.
