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
- `profile list|show|use`
- `context show|set`
- `capabilities`
- `tool call`
- `resource read`
- `prompt get`
- `token list|create|revoke`
- `service-account list|create|token-list|token-create|token-revoke`
- `audit list`
- `governance status`
- `approval list|timeline|approve|reject|cancel`
- `mcp start|install`
- `skill list|install`
- `diagnose`
- `completion bash|zsh`

The current code entry point is `cmd/soha-cli`, with the implementation in `internal/cli/sohacli`. The implemented command surface covers profile/context management, capability manifest inspection, manual Gateway tool calls, resource/prompt debugging, PAT and service-account token management, Gateway audit queries, governance status, approval timeline and decisions, MCP config generation, skill installation, diagnose, and shell completion.

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
   - Approved `delivery.actions.trigger` replay injects `aiGatewayApprovalRequestId` plus policy, tool, skill, and AI client linkage into delivery workflow variables. Workflow run metadata and `workflow_approvals.metadata` preserve that linkage, and related IDs include `workflowRunId` for traceability from Gateway approval to workflow manual approval. Console drilldown between `/ai-gateway` and `/workflows` uses `approvalRequestId`, `workflowRunId`, and `gatewayApprovalRequestId`; workflow rows targeted by `workflowRunId` auto-expand to show Gateway linkage, manual approval node detail, workflow node timeline, and the raw trace. The backend also exposes `GET /api/v1/ai-gateway/approval-requests/:requestID/timeline`, which aggregates the approval request, derived decision/stage trace, workflow/task linkage, and Gateway audit events for the same request.
5. skill bindings
   - `ai_gateway_skill_bindings` controls which soha Skills a subject, role, or AI client may use and which capability refs each skill may expose.
   - Skill bindings only narrow manifests and invocations. They do not grant permissions.

## First API

```http
GET /api/v1/ai-gateway/capabilities
POST /api/v1/ai-gateway/tools/:toolName/invoke
POST /api/v1/ai-gateway/resources/read
POST /api/v1/ai-gateway/prompts/get
GET /api/v1/ai-gateway/governance/status
```

`capabilities` returns the visible manifest for the current identity. `tools/:toolName/invoke` is the unified invocation entry point for MCP, CLI, and external AI agents. Every invocation must re-check `ai.gateway.invoke`, the tool's domain permissions, scopes, AI grants, and risk policy before calling the owning application service.

The Gateway manifest is generated from the `internal/application/aigateway` capability registry/providers. The current delivery, Kubernetes, and diagnosis catalog is registered as the default static provider; manifest filtering, tool/resource/prompt/skill lookup, resource-to-tool/prompt/skill relations, optional provider tool invokers, grant and binding construction, access-policy skill matching, governance high-risk scans, and resource related-capability metadata all use the same registry. New module capabilities should register a provider; custom tool execution must route through a provider invoker back to the owning application service and still reuse the Gateway authorization, risk-policy, redaction, and audit flow instead of copying static directories into handlers, repositories, or the local MCP process.

`resources/read` reads `soha://...` resources from the manifest and returns redacted resource descriptions, related tools/prompts/skills, required scopes, and request context. `prompts/get` returns soha prompt template messages and can combine the current skill, arguments, and context. Skill bindings constrain tools, resources, and prompts: resource/prompt visibility is derived from whether their related tools are inside the bound capability refs, read/get calls re-check the binding, and returned resource documents or prompt skill context show only bound capability refs. MCP `initialize` returns stable `instructions` that describe the local process as a Gateway proxy and state that it does not directly access PostgreSQL, Kubernetes, runner workspaces, kubeconfigs, Docker, or privileged prompt/resource content. MCP `tools/list` exposes manifest `inputSchema` and, when the manifest defines an output contract, `outputSchema`; `resources/list` exposes resource `contextSchema`, and `prompts/list` exposes prompt `argumentSchema`, `contextSchema`, and MCP `arguments[]` summaries. It also emits standard `annotations`: `read` tools are marked read-only and idempotent, `mutate` / `execute` / `high` tools are marked destructive, and soha tools default to `openWorldHint=true`. `tools/list` / `resources/list` / `prompts/list` also expose manifest `permissionKeys` and `requiredScopes` under `_meta.soha`; tool metadata includes `domain`, `action`, `mcpAdapterId`, `mcpToolName`, `riskLevel`, and `requiresApproval`. These fields help clients display expected authorization, scope, adapter mapping, input/output contracts, and risk before a call or read. Real tool calls, resource reads, and prompt reads still enter Gateway authorization, skill binding, redaction, AI grant, risk-policy, and audit boundaries; empty `tools/call.name`, `resources/read.uri/name`, or `prompts/get.name` values return JSON-RPC invalid-params errors without calling the backend.

`governance/status` is the read-only operating health endpoint for Gateway administrators. It defaults to the recent 24-hour audit window and accepts explicit `windowHours` values from 1 to 168. It summarizes token last-used and expiration state, AI client registration approval state, recent audit metrics, repeated deny/failure findings, pending approvals, pending approval SLA state, and budget/rate-limit/redaction/resource-scope policy coverage. Coverage returns total and active counts, and configured states are contributed only by enabled access policies, unexpired tool grants, and enabled skill bindings. `redactionPolicyState` treats input `redactionPolicy` / `sensitiveDataRedaction`, `outputRedactionPolicy`, and response/output redaction aliases as active coverage; disabled or expired records remain visible as inventory but do not mark coverage as configured. When no active access policy or tool grant has concrete `resourceScopes`, the `resource_scope_coverage` health check degrades and recommends adding scopes before broad cross-environment Gateway access. It also scans the current capability registry to warn when active allow access policies or unexpired allow tool grants can cover high-risk Gateway tools without approval, human confirmation, or dry-run guardrails, exposing those cases as findings, the `high_risk_guardrails` health check, and recommendations. High-risk allow policies or grants that lack concrete `resourceScopes` constraints are also exposed as `high_risk_allow_without_resource_scope` / `high_risk_grant_without_resource_scope` findings and the `high_risk_resource_scopes` health check. Pending approvals expose due-soon, stale, oldest-pending age, and next-due fields; `approval_sla_due_soon`, `stale_gateway_approvals`, and `overdue_gateway_approvals` findings identify approval queue pressure without exposing tool input.

The `/ai-gateway` Console Governance tab consumes this endpoint directly and can switch between 1h, 6h, 24h, 48h, and 7d windows for health checks, recent calls, token/client/approval summaries, policy coverage, top tools / AI clients / actors, anomaly findings, and recommendations.

The directly invokable tools currently cover delivery, delivery context analysis, and bounded Kubernetes diagnosis:

- `delivery.applications.list`
- `delivery.applications.detail`
- `delivery.applications.create`
- `delivery.application_environments.list`
- `delivery.application_services.list`
- `delivery.build_sources.list`
- `delivery.release_targets.list`
- `delivery.actions.trigger`
- `delivery.release_bundles.list`
- `delivery.execution_tasks.list`
- `delivery.execution_logs.list`
- `delivery.approval_policies.list`
- `delivery.workflow_templates.list`
- `delivery.release_context.diff`
- `delivery.rollback.context`
- `k8s.pods.list`
- `k8s.pods.logs`
- `k8s.pods.describe`
- `k8s.deployments.list`
- `k8s.deployments.rollout_status`
- `k8s.deployments.events`
- `k8s.services.list`
- `k8s.services.backends`
- `k8s.routes.context`
- `k8s.storage.context`
- `k8s.nodes.detail`
- `k8s.events.list`
- `diagnosis.release_failure.analyze`

Kubernetes tools read through `internal/application/resource`, so they keep the platform view-model contract, direct/agent cluster boundaries, cluster/namespace scope, and resource permissions. Enhanced diagnosis tools still return soha view models instead of raw Kubernetes objects: pod describe aggregates containers, conditions, volumes, and related resources; deployment tools read rollout status and bounded events; service tools aggregate selectors, backend pods, and ingress hints; route context aggregates Ingress and Gateway API typed routes with capability warnings when optional Gateway API resources are unavailable; storage context covers PVC/PV/StorageClass state; node detail returns conditions, taints, resource summary, and scheduled pods.

Delivery tools read or trigger capabilities only through owning application services. Application and service/container context comes from application/delivery services, workflow templates from catalog service, and release bundles, execution tasks, execution logs, and approval policies from delivery service. `delivery.release_context.diff` and `delivery.rollback.context` generate candidate release, promotion, and rollback context only; real build, deploy, and rollback actions must go through `delivery.actions.trigger`, risk policy, approvals, and the execution/workflow plane. `action=rollback` runs the delivery workflow rollback mode, filters the bound workflow template to `rollback_to_previous` DAG nodes only, and carries `releaseBundleId` into workflow variables and run metadata.

`diagnosis.release_failure.analyze` persists artifacts through the Copilot application service into `ai_agent_runs.analysis_artifacts`. Gateway collects, redacts, and summarizes evidence, then stores application, environment, release bundle, execution task, cluster, namespace, workload, and pod links plus hypotheses, recommendations, next checks, tool execution snapshots, and data-source snapshots. It does not persist raw logs, secrets, kubeconfigs, or environment variables. When a caller requests external deep analysis with `agentProviderId` / `providerId` or `deepAnalysis` / `externalAnalysis`, Gateway sends the same redacted context to the Copilot application service to create a `queued` AgentRun and returns the `agentRunId`, provider metadata, and `agent_runtime_claim_callback` runtime marker; the Agent Runtime runner later claim/callbacks and writes artifacts back, so Gateway never calls an external provider synchronously. Agent Runtime callback artifacts are merged incrementally by stable `runId + kind + title` identity, preserving existing artifact order and replacing only matching artifacts so later provider callbacks do not overwrite earlier analysis artifacts. The Agent Runtime runner also sends a synthesized artifact with failed/callback_timeout callbacks when provider commands fail or time out, carrying run, provider, capability, scope, tool execution, error status, and retry guidance, with basic sensitive-text redaction applied to failed error messages, logs, tool output, and artifact snapshots before callback, so the control plane does not depend only on server-side fallback synthesis. Copilot now writes stable artifact links such as `sessionId`, `rootCauseRunId`, `inspectionRunId`, and `agentRunId` / `agentRuntimeId` for root-cause, performance, trace, inspection-review, and Agent Runtime artifacts. External Agent Runtime callback usage is normalized into numeric `providerUsage` / `usage` summaries, including Cohere billed-unit preference, search/classification/embedding/rerank/request/response usage units, Brave Search / SerpAPI query and search-credit aliases, Browserbase session/minute/page-load aliases, Exa/Jina/Unstructured/LlamaCloud document-page/parse-page/character/chunk aliases, Helicone request-count aliases, and common token/cost aliases; raw provider payloads and model strings stay out of artifacts. The AI Workbench artifact history renders those links as session, root-cause run, inspection run, and Agent Run entry points, including direct inspection-run positioning at `/ai-workbench/inspection?view=runs&inspectionRunId=...`.

Credential endpoints:

```http
GET  /api/v1/ai-gateway/personal-access-tokens
POST /api/v1/ai-gateway/personal-access-tokens
POST /api/v1/ai-gateway/personal-access-tokens/:tokenID/revoke
POST /api/v1/ai-gateway/personal-access-tokens/:tokenID/rotate
GET  /api/v1/ai-gateway/service-accounts
POST /api/v1/ai-gateway/service-accounts
POST /api/v1/ai-gateway/service-accounts/:serviceAccountID/tokens
POST /api/v1/ai-gateway/service-account-tokens/:tokenID/revoke
POST /api/v1/ai-gateway/service-account-tokens/:tokenID/rotate
```

PAT/SAT rows store only hashes and display prefixes. Create and rotate responses return the plaintext token once. Revocation and expiration are enforced by identity token parsing. Rotation copies the previous token scopes, permission keys, and metadata, revalidates permission keys against the current user or service-account roles, creates the replacement token, and revokes the previous token. The optional request body may provide `expiresAt`; otherwise the replacement keeps the previous future expiration, and an already expired token receives a default 90-day replacement window.

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
GET    /api/v1/ai-gateway/audit-logs
GET    /api/v1/ai-gateway/governance/status
GET    /api/v1/ai-gateway/approval-requests
GET    /api/v1/ai-gateway/approval-requests/:requestID/timeline
POST   /api/v1/ai-gateway/approval-requests/:requestID/approve
POST   /api/v1/ai-gateway/approval-requests/:requestID/reject
POST   /api/v1/ai-gateway/approval-requests/:requestID/cancel
```

`tool-grants` supports `user`, `service_account`, `role`, and `ai_client` subjects. At runtime, Gateway combines grants from the current subject, roles, and AI client: deny wins, and any allow grant creates an allow-list.

`access-policies` and `skill-bindings` support the same `user`, `service_account`, `role`, and `ai_client` subjects. At runtime, Gateway combines enabled records from the current subject, roles, and AI client: access policies narrow tools and skills by deny/allow semantics, and skill bindings then narrow manifests and invocations by bound skill/capability refs. These controls run after `permissionKeys`, so they cannot expand RBAC or scope grants.

Tool invocation extracts standard resource scope from input, including the compatibility scope field `businessLineId`, delivery fields such as `applicationId`, `applicationEnvironmentId`, and `environmentId`, plus runtime fields such as `clusterId`, `namespace`, `releaseBundleId`, and `executionTaskId`. In the current model, `businessLineId` and the global `environmentId` should be treated primarily as historical scope or compatibility fields; new control-plane UX should prefer application grouping, application-environment bindings, and environment tags. `mcp_tool_grants.resource_scopes` and `ai_access_policies.resource_scopes` are enforced before invocation; the manifest can conservatively show potential capability, but the actual call must pass scoped grants/policies and the owning business service authorization.

Risk policy runs before a tool reaches the owning service. `deny` blocks immediately; `require_approval` returns `pending_approval` and an approval request id; `require_human_confirm` returns `pending_human_confirm`; `dry_run_only` returns `dry_run` without executing a real mutation. Persistent approvals can configure candidate users, roles, teams, `onCallRef`, change windows, `approvalMode=all|any`, staged approval, total quorum, and role/team quorum. Approval decisions reject non-candidate operators, enforce change windows for approve, record unique approver decisions, and replay the tool only after the required stage or global quorum is satisfied. Approval list responses include an `approvalTrace` derived from `relatedIds.approvalRouting`, and `GET /api/v1/ai-gateway/approval-requests/:requestID/timeline` returns the request, trace, decision/stage nodes, workflow/task links, and matching Gateway audit events. `GET /api/v1/ai-gateway/audit-logs` supports `approvalRequestId` filtering. AI client registration approval uses the same approval-request table but activates or rejects the client instead of replaying a tool.

`ai_access_policies.conditions` are enforced before invocation. `rateLimit` supports PostgreSQL-backed fixed-window counters, audit-backed `sliding_window` / `rolling_window`, and PostgreSQL-backed GCRA/token-bucket state; setting `ai_gateway.rate_limit.backend=redis` and `ai_gateway.rate_limit.redis.addr` makes Gateway use Redis INCR/EXPIRE and Lua GCRA as the high-throughput shared limiter first, then fall back to PostgreSQL and, for old databases without the rate-limit tables, the audit-window counter. `budget` supports invocation-count, token, and cost budgets based on redacted Gateway audit metadata. Gateway normalizes numeric provider usage from common `usage`, `tokenUsage`, `aiUsage`, `providerUsage`, `llmUsage`, `metering`, `billing`, and `costUsage` shapes, including Gemini/Vertex cached content, tool-use prompt, and thoughts token fields, Ollama, Anthropic cache input tokens, OpenAI cached/audio/reasoning/prediction token details, Bedrock text/image/audio fields, Cohere billed units with same-container `billed_units` preferred over `tokens` to avoid double counting, search/classification/embedding/rerank/request/response usage units, Brave Search / SerpAPI query and search-credit aliases, Browserbase session/minute/page-load aliases, Exa/Jina/Unstructured/LlamaCloud document-page/parse-page/character/chunk aliases, Helicone request-count aliases, billable/billed/usage aliases, multimodal text/image/audio/video aliases, cache read/write/hit/miss aliases, DashScope/Moonshot/Zhipu/Qianfan-style `*_tokens_count` / `*_token_usage` variants, generic `promptCost` / `inputCost` / `completionCost` / `outputCost`, and cost/costMicros/costCents fields. Raw provider payloads and model strings are not stored.

Redaction policy can reject or sanitize sensitive input before the owning service sees it. It supports field paths, allow-list fields, replacement text, format-preserving masks, regex value rules, built-in secret classifiers, per-tool rules, and output redaction via `outputRedactionPolicy` or rule `target=output|both`. Built-in classifiers cover AI/provider tokens plus agent tooling/search/RAG, China cloud model providers, observability, and DevOps integration tokens such as OpenRouter, Fireworks AI, Voyage AI, Brave Search, SerpAPI, Browserbase, Exa, Jina AI, Unstructured, LlamaCloud, Helicone, DashScope, Moonshot, Zhipu AI, SiliconFlow, Tencent Hunyuan, Baidu Qianfan, Volcengine Ark, Grafana, Sentry, New Relic, Azure OpenAI, Azure DevOps PAT, Datadog, PagerDuty, PostHog, Splunk, Elastic, and Terraform Cloud. Redaction hits are written to audit metadata as summaries with target, field path, match type, classifier name, and count only; matched values are not stored.

The `/ai-gateway` Console access-policy drawer now edits common approval routing and `conditions` with structured controls: `approvalPolicyRef`, candidate users/roles/teams, `onCallRef`, `approvalMode=all|any`, `requiredApprovals`, change-window bounds, fixed-window, sliding-window, or GCRA/token-bucket `rateLimit`, daily invocation/token/cost `budget`, input `redactionPolicy`, and `outputRedactionPolicy` field lists. More detailed staged approval routing, delivery approval-policy metadata extensions, and per-tool redaction `rules[]` remain API or cookbook JSON configuration.

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

The manifest covers delivery and read-only Kubernetes diagnosis:

- application list and create
- application environment bindings
- build/deploy/build_deploy/workflow/verify/rollback trigger entry point
- release bundle and execution task queries
- Pod, Deployment, Service, Ingress/Gateway route, Storage, Node, Event, and log read-only diagnosis
- release failure diagnosis context generation

## Data Objects

Incremental migrations `0015_ai_gateway.sql`, `0016_ai_gateway_approval_requests.sql`, `0017_ai_gateway_rate_limit_counters.sql`, and `0018_ai_gateway_rate_limit_states.sql` create the Gateway control-plane tables, and `0019_workflow_approval_metadata.sql` adds workflow manual approval metadata used for Gateway approval traceability:

- `personal_access_tokens`
- `service_accounts`
- `service_account_tokens`
- `ai_clients`
- `ai_access_policies`
- `mcp_tool_grants`
- `ai_gateway_skill_bindings`
- `ai_gateway_audit_logs`
- `ai_gateway_approval_requests`
- `ai_gateway_rate_limit_counters`
- `ai_gateway_rate_limit_states`

These tables belong to CLI/MCP/service-account/AI-client enterprise access. The Redis rate-limit backend is only a high-throughput runtime limiter; it does not replace audit or approval tables and does not persist tool input. Existing AI Workbench objects such as `ai_agent_runs`, tool bindings, skill bindings, and analysis artifacts remain owned by Copilot/Agent Runtime.

Every Gateway tool invocation is recorded in both the generic audit log and the dedicated `ai_gateway_audit_logs` table. The dedicated row captures actor type and ID, AI client, skill, tool, risk level, resource scope, request/result, and redacted related metadata; it must not store tokens, kubeconfigs, environment variables, raw log bodies, or full tool input.

## Engineering Rules

- Gateway handlers parse transport and return DTOs only.
- `internal/application/aigateway` owns manifest, authorization, audit, and tool invocation orchestration.
- Real actions must call the owning application service.
- Build, release, Docker, and virtualization work must reuse durable execution/operation/task models.
- AI analysis must reuse Copilot/Agent Runtime `AgentRun` and `AnalysisArtifact`.
- Tokens, secrets, kubeconfigs, and environment variables must not be written into logs, audit metadata, or AI artifacts.
