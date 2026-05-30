# soha-cli

`soha-cli` is the local entry point for soha AI Gateway. It handles login, local profiles, AI client context, MCP stdio startup, and capability inspection for the current identity.

The CLI does not execute real platform actions. MCP tool calls are proxied to the soha backend:

```text
soha-cli mcp start
  -> GET /api/v1/ai-gateway/capabilities
  -> POST /api/v1/ai-gateway/tools/:toolName/invoke
  -> POST /api/v1/ai-gateway/resources/read
  -> POST /api/v1/ai-gateway/prompts/get
  -> backend permissionKeys / scope grants / MCP tool grants / access policies / skill bindings / audit
  -> owning application service
```

## Build And Help

```bash
go run ./cmd/soha-cli help
go build -o ./bin/soha-cli ./cmd/soha-cli
```

Current Gateway command surface:

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
- `approval list|timeline|approve|reject|cancel`
- `governance status`
- `mcp start`
- `mcp install`
- `skill list`
- `skill install`
- `diagnose`
- `completion bash|zsh`

`skill install` installs AI-readable workflow guidance only; it does not grant extra permissions. Authorization still comes from Gateway manifests, permission keys, scope grants, and MCP tool grants.

End-to-end MCP client, CI service account, delivery/k8s workflow, access policy, and skill binding examples are in [AI Gateway Examples](./ai-gateway-examples.md).

## Login

```bash
soha-cli login \
  --server http://localhost:8080 \
  --login admin \
  --profile local \
  --ai-client codex \
  --ai-client-id codex-local
```

If `--password` is omitted, the CLI reads the password from standard input.

The default local config path is:

```text
~/.soha/config.json
```

Set `SOHA_CONFIG=/abs/path/config.json` to override it. The config file is written with `0600` permissions and the parent directory is created with `0700` permissions.

`profile show` displays redacted tokens only. Do not write full tokens, kubeconfigs, environment variables, or service-account secrets to logs, issues, AI conversations, or diagnostic attachments.

## Profile And Context

```bash
soha-cli profile list
soha-cli profile show local
soha-cli profile use local
```

AI client context is sent to Gateway as headers for audit and tool-grant filtering:

```bash
soha-cli context set \
  --profile local \
  --ai-client-id codex-local \
  --ai-client Codex \
  --skill-id delivery-developer \
  --source soha-cli
```

Headers:

- `X-Soha-AI-Client-ID`
- `X-Soha-AI-Client`
- `X-Soha-Skill-ID`
- `X-Soha-Source`

## Capability Check

```bash
soha-cli capabilities --profile local
soha-cli capabilities --profile local --json
soha-cli capabilities --profile local --output names
soha-cli capabilities --profile local --output inputs
```

`capabilities` calls `GET /api/v1/ai-gateway/capabilities` and prints the current tools, resources, prompts, skills, permission keys, and manifest summary. `--output inputs` summarizes each tool's required fields and properties from the manifest `inputSchema` and `outputSchema`, which lets operators check MCP client arguments and expected output fields without expanding the full JSON manifest.

If a capability is missing, check:

- The profile is logged in to the intended server.
- The user or token has `ai.gateway.view`.
- The business tool also has the required domain permission, such as `delivery.*`.
- `mcp_tool_grants` did not narrow the tool away.
- `ai_access_policies` or `ai_gateway_skill_bindings` did not narrow the current AI client, role, or subject away.

## Tool Call

Manually call a Gateway tool as a fallback:

```bash
soha-cli tool call k8s.pods.list \
  --profile local \
  --input ./pod-list-input.json
```

`--input -` reads JSON from standard input. `--input-json '{"clusterId":"cluster-a","namespace":"prod"}'` is also supported. The CLI only calls `POST /api/v1/ai-gateway/tools/:toolName/invoke`; it does not access databases, Kubernetes, or runners directly.

## Resources And Prompts

Manually read a Gateway MCP resource or prompt when debugging the context an MCP client can see:

```bash
soha-cli resource read soha://delivery/applications \
  --profile local \
  --context-json '{"applicationId":"app-1"}'

soha-cli prompt get soha.delivery.plan_release \
  --profile local \
  --arguments-json '{"applicationId":"app-1"}' \
  --context-json '{"environmentId":"dev"}'
```

`resource read` only calls `POST /api/v1/ai-gateway/resources/read`; `prompt get` only calls `POST /api/v1/ai-gateway/prompts/get`. `--context -` and `--arguments -` read JSON from standard input, and a single command may use standard input for only one JSON source. Output is passed through the CLI redaction rules.

## Tokens And Service Accounts

Personal tokens:

```bash
soha-cli token list --profile local
soha-cli token create --profile local --name codex-local --permission-keys ai.gateway.view,ai.gateway.invoke
soha-cli token revoke --profile local pat-123
```

Service accounts:

```bash
soha-cli service-account list --profile local
soha-cli service-account create --profile local --name ci-runner --role-ids delivery-operator
soha-cli service-account token-list --profile local
soha-cli service-account token-create --profile local --service-account-id sa-123 --name ci-token
soha-cli service-account token-revoke --profile local sat-123
```

Plaintext token values are returned only once by create commands. `token-list` returns service account token id, owner service account, prefix, permissions, scopes, expiration, last-used, and revocation state only. List, audit, and diagnose output is redacted by default and does not show token hashes, kubeconfigs, environment variables, or secret-looking fields.

## Audit

Query Gateway audit:

```bash
soha-cli audit list --profile local --tool-name diagnosis.release_failure.analyze --limit 20
soha-cli audit list --profile local --actor user-1 --from 2026-05-29T00:00:00Z
```

Supported filters include actor, actor type, AI client, skill, tool, risk level, result, action, from/to, and limit. Timestamps use RFC3339.

## Governance

Query Gateway governance health and operating metrics:

```bash
soha-cli governance status --profile local
soha-cli governance status --profile local --window-hours 48 --json
```

`governance status` calls `GET /api/v1/ai-gateway/governance/status` and defaults to the last 24 hours of audit data. Explicit `--window-hours` values must be between 1 and 168 so direct API requests cannot trigger an abnormal scan window. Output includes health, health checks, success/deny/failure counts, pending approval count, approval SLA summary, PAT/service-token last-used and expiration findings, AI client registration approval state, budget/rate-limit/redaction/resource-scope coverage, and abnormal-call findings. Text output prints each health check status, name, count, and redacted message. It also includes finding actor, subject, AI client, approval request, policy, grant, tool, and risk level, plus structured `recommendationAction` action, target, refs, and count so operators can locate `approval_sla_due_soon`, `stale_gateway_approvals`, `high_risk_allow_without_approval` / `high_risk_grant_without_approval`, and `high_risk_allow_without_resource_scope` / `high_risk_grant_without_resource_scope` warnings. Text output also includes a redaction hit summary with total matches, redacted audit count, input/output targets, and top match types, classifiers, field paths, policies, and tools. `policyCoverage` shows total and active policy/grant/binding counts, and `redactionPolicyState` treats input `redactionPolicy`, `outputRedactionPolicy`, and response/output redaction aliases as configured coverage; disabled policies, expired grants, or disabled bindings do not mark coverage state as configured. When no active `resourceScopes` are configured, the backend reports the `resource_scope_coverage` health check and recommendation action so operators can add cross-environment access constraints. `--json` preserves the complete redacted DTO, including approvals, riskCounts, pendingApprovalClientIds, redaction summary, activeAccessPolicies, activeToolGrants, resourceScopeState, resourceScopedAccessPolicies, `recommendationActions`, and token due/stale day fields. Configured `rateLimit`, invocation-count `budget`, token/cost budgets, strict or sanitize input redaction, output redaction, and `resourceScopes` are enforced in the backend before invocation; the CLI only displays redacted governance summaries.

## Approval Requests

After governance output or Console shows an approval request id, the CLI can query and decide it:

```bash
soha-cli approval list --profile local --status pending --tool-name delivery.actions.trigger
soha-cli approval timeline approval-123 --profile local
soha-cli approval approve approval-123 --profile local --comment "approved in change window"
soha-cli approval reject approval-123 --profile local --comment "missing rollback plan"
soha-cli approval cancel approval-123 --profile local --comment "duplicate request"
```

`approval list` proxies to `GET /api/v1/ai-gateway/approval-requests` and supports `id`, `status`, `actor`, `actor-type`, `ai-client-id`, `skill-id`, `tool-name`, `risk-level`, `strategy`, `from`, `to`, and `limit` filters. `approval timeline` proxies to `GET /api/v1/ai-gateway/approval-requests/:requestID/timeline` and returns the backend-derived `approvalTrace`, decisions, stage history, workflow/task links, and Gateway audit events for the same approval request. `audit list` also supports `--approval-request-id` for approval-scoped audit review. `approve`, `reject`, and `cancel` proxy to the existing approval APIs and send `--comment` as the decision comment. Candidate approvers, change windows, staged quorum, AI client activation, and tool replay still run in the backend Gateway application service. CLI output redacts approval tool input, decision comments, execution output, and related metadata.

## MCP stdio Server

Start locally:

```bash
soha-cli mcp start --profile local
```

Generate MCP client configuration:

```bash
soha-cli mcp install \
  --profile local \
  --command /usr/local/bin/soha-cli \
  --ai-client-id codex-local \
  --ai-client Codex \
  --skill-id k8s-sre
```

Output shape:

```json
{
  "mcpServers": {
    "soha": {
      "command": "/usr/local/bin/soha-cli",
      "args": ["mcp", "start", "--profile", "local", "--ai-client-id", "codex-local", "--ai-client", "Codex", "--skill-id", "k8s-sre"]
    }
  }
}
```

If `--profile` is omitted, `mcp install` uses the current profile. `--ai-client-id`, `--ai-client`, and `--skill-id` are written into the generated `mcp start` args to pin audit source, AI client context, and skill-binding context.

The MCP server supports:

- `initialize`
- `tools/list`
- `tools/call`
- `resources/list`
- `resources/read`
- `prompts/list`
- `prompts/get`

`initialize` returns the soha MCP safety boundary: the local process is only an AI Gateway proxy and never directly accesses PostgreSQL, Kubernetes, Docker, runner workspaces, local kubeconfigs, or privileged prompt/resource content. `tools/list`, `resources/list`, and `prompts/list` are generated from the Gateway manifest. Current default Gateway tools expose structured `inputSchema`, and expose `outputSchema` when the manifest defines an output contract, so MCP clients can identify fields such as `applicationId`, `action`, `clusterId`, `namespace`, `podName`, `deploymentName`, `serviceName`, `nodeName`, and `taskId` as well as expected output fields. `resources/list` exposes resource `contextSchema`; `prompts/list` exposes prompt `argumentSchema`, `contextSchema`, and standard MCP `arguments[]` summaries. `tools/list` emits standard MCP `annotations`: `read` tools are marked read-only and idempotent, `mutate`, `execute`, and `high` tools are marked destructive, and soha tools default to `openWorldHint=true`. All three list responses include `permissionKeys` and `requiredScopes` under `_meta.soha`; tool entries also include `domain`, `action`, `mcpAdapterId`, `mcpToolName`, `riskLevel`, and `requiresApproval` so clients can display expected authorization, scope, adapter mapping, and risk before calls or reads. `tools/call` proxies only to `POST /api/v1/ai-gateway/tools/:toolName/invoke`, `resources/read` proxies only to `POST /api/v1/ai-gateway/resources/read`, and `prompts/get` proxies only to `POST /api/v1/ai-gateway/prompts/get`; missing tool names, resource URIs/names, or prompt names return JSON-RPC invalid-params errors without calling the backend.

`resources/read` uses manifest URIs such as `soha://delivery/applications`, `soha://delivery/execution-tasks`, and `soha://k8s/runtime`. `prompts/get` supports `soha.delivery.plan_release` and `soha.k8s.diagnose_workload`, and sends MCP arguments and context to the backend Gateway so prompt messages are assembled behind the same redaction and authorization boundary. Resources and prompts are also constrained by skill bindings: when `--skill-id` points to a bound skill, the backend only allows resources/prompts related to that binding's capability refs.

First-version invokable tools:

- `delivery.applications.list`
- `delivery.applications.detail`
- `delivery.applications.create`
- `delivery.application_environments.list`
- `delivery.application_services.list`
- `delivery.build_sources.list`
- `delivery.release_targets.list`
- `delivery.actions.trigger`
- `delivery.rollback.context`
- `delivery.release_bundles.list`
- `delivery.execution_tasks.list`
- `delivery.execution_logs.list`
- `delivery.approval_policies.list`
- `delivery.workflow_templates.list`
- `delivery.release_context.diff`
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

`delivery.actions.trigger` supports build, deploy, build_deploy, workflow, verify, and rollback. For rollback, read `delivery.rollback.context` first, then invoke `action=rollback` through the controlled workflow path.

k8s tools require `clusterId`; `namespace` follows the platform scope semantics and may be empty for an aggregated view. `k8s.pods.logs` also requires `podName`, with optional `container`, `tailLines`, `sinceSeconds`, and `previous`. Gateway applies basic sensitive-field redaction to log outputs.

## Skills

The repository ships the first Gateway Skills:

- `delivery-developer`
- `delivery-tester`
- `k8s-sre`
- `security-change`

List installable Skills:

```bash
soha-cli skill list --source skills/ai-gateway
```

Install one Skill:

```bash
soha-cli skill install \
  --source skills/ai-gateway \
  --dest ~/.soha/skills \
  delivery-developer
```

Install all Skills:

```bash
soha-cli skill install --all
```

The default source is `skills/ai-gateway`, overrideable with `SOHA_SKILLS_SOURCE=/abs/path`. The default destination is `~/.soha/skills`, overrideable with `SOHA_SKILLS_DIR=/abs/path`.

Skills are workflow instructions, not security boundaries. After a client installs a Skill, it still must work through the MCP tools visible to the current identity.

## Diagnose

```bash
soha-cli diagnose --profile local
soha-cli diagnose --profile local --tool k8s.pods.logs
soha-cli diagnose --profile local --resource soha://k8s/runtime
soha-cli diagnose --profile local --prompt soha.k8s.diagnose_workload
soha-cli diagnose --profile local --tool k8s.pods.logs --ai-client-id codex-local --skill-id k8s-sre --source codex-mcp
```

`diagnose` validates the profile, server, token, and Gateway capability path, then prints tool/resource/prompt/skill/permission-key counts. With `--tool`, it shows that tool's risk level, approval requirement, permission keys, required scopes, and `inputRequired` / `inputFields` / `outputRequired` / `outputFields` summarized from the manifest `inputSchema` and `outputSchema`, then hints to inspect MCP tool grants, AI access policies, skill bindings, AI client context, and resource scopes. With `--resource` or `--prompt`, it shows resource/prompt permission keys and points operators at Gateway `resources/read` / `prompts/get`, skill bindings, AI client context, and arguments/context. `--ai-client-id`, `--ai-client`, `--skill-id`, and `--source` override only this diagnostic request so operators can simulate the client context generated by `mcp start` or `mcp install`; they do not modify the profile. It does not print tokens.

## Completion

```bash
soha-cli completion bash
soha-cli completion zsh
```
