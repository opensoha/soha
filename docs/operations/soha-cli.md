# soha-cli

`soha-cli` 是 soha AI Gateway 的本地入口。它负责登录、保存本地 profile、声明 AI client 上下文、启动 MCP stdio server，以及检查当前身份能看到的 Gateway capability。

CLI 不执行真实平台动作。所有 MCP tool 调用都会代理到 soha 后端：

```text
soha-cli mcp start
  -> GET /api/v1/ai-gateway/capabilities
  -> POST /api/v1/ai-gateway/tools/:toolName/invoke
  -> POST /api/v1/ai-gateway/resources/read
  -> POST /api/v1/ai-gateway/prompts/get
  -> backend permissionKeys / scope grants / MCP tool grants / access policies / skill bindings / audit
  -> owning application service
```

## 构建和帮助

```bash
go run ./cmd/soha-cli help
go build -o ./bin/soha-cli ./cmd/soha-cli
```

当前 Gateway 命令面：

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

`skill install` 只安装 AI 可读流程和方法论，不赋予额外权限。真实授权仍由 Gateway manifest、permission keys、scope grants 和 MCP tool grants 决定。

端到端 MCP client、CI service account、delivery/k8s workflow、access policy 和 skill binding 示例见 [AI Gateway Examples](./ai-gateway-examples.md)。

## 登录

```bash
soha-cli login \
  --server http://localhost:8080 \
  --login admin \
  --profile local \
  --ai-client codex \
  --ai-client-id codex-local
```

如果没有传 `--password`，CLI 会从标准输入读取密码。

本地配置默认写入：

```text
~/.soha/config.json
```

也可以通过 `SOHA_CONFIG=/abs/path/config.json` 指定路径。配置文件使用 `0600` 权限写入，父目录使用 `0700` 权限创建。

`profile show` 只展示脱敏后的 token；不要把完整 token、kubeconfig、环境变量或服务账号密钥写入日志、issue、AI 对话或诊断附件。

## Profile 和上下文

```bash
soha-cli profile list
soha-cli profile show local
soha-cli profile use local
```

AI client 上下文会作为请求头发送到 Gateway，用于审计和 tool grant 过滤：

```bash
soha-cli context set \
  --profile local \
  --ai-client-id codex-local \
  --ai-client Codex \
  --skill-id delivery-developer \
  --source soha-cli
```

对应请求头：

- `X-Soha-AI-Client-ID`
- `X-Soha-AI-Client`
- `X-Soha-Skill-ID`
- `X-Soha-Source`

## Capability 检查

```bash
soha-cli capabilities --profile local
soha-cli capabilities --profile local --json
soha-cli capabilities --profile local --output names
soha-cli capabilities --profile local --output inputs
```

`capabilities` 会调用 `GET /api/v1/ai-gateway/capabilities`，输出当前身份可用的 tools、resources、prompts、skills、permission keys 和 manifest summary。`--output inputs` 会按 tool 汇总 manifest `inputSchema` 与 `outputSchema` 中的 required 字段和 properties 字段，方便在不展开完整 JSON manifest 的情况下核对 MCP client 需要传入的参数和可预期输出字段。

如果 capability 不存在，优先检查：

- 当前 profile 是否登录到正确 server。
- 用户或 token 是否拥有 `ai.gateway.view`。
- 业务工具是否还需要对应 domain permission，例如 `delivery.*`。
- `mcp_tool_grants` 是否把 tool 收窄掉。
- `ai_access_policies` 或 `ai_gateway_skill_bindings` 是否把当前 AI client、角色或主体收窄掉。

## Tool Call

人工兜底调用 Gateway tool：

```bash
soha-cli tool call k8s.pods.list \
  --profile local \
  --input ./pod-list-input.json
```

`--input -` 从标准输入读取 JSON；也可以用 `--input-json '{"clusterId":"cluster-a","namespace":"prod"}'`。CLI 只调用 `POST /api/v1/ai-gateway/tools/:toolName/invoke`，不直接访问数据库、Kubernetes 或 runner。

## Resource 和 Prompt

人工读取 Gateway MCP resource 或 prompt，用于排查 MCP client 能看到的上下文：

```bash
soha-cli resource read soha://delivery/applications \
  --profile local \
  --context-json '{"applicationId":"app-1"}'

soha-cli prompt get soha.delivery.plan_release \
  --profile local \
  --arguments-json '{"applicationId":"app-1"}' \
  --context-json '{"environmentId":"dev"}'
```

`resource read` 只调用 `POST /api/v1/ai-gateway/resources/read`，`prompt get` 只调用 `POST /api/v1/ai-gateway/prompts/get`。`--context -`、`--arguments -` 可从标准输入读取 JSON；同一命令一次只允许一个参数源读取标准输入。输出会按 CLI 脱敏规则处理。

## Token 和 Service Account

个人 token：

```bash
soha-cli token list --profile local
soha-cli token create --profile local --name codex-local --permission-keys ai.gateway.view,ai.gateway.invoke
soha-cli token revoke --profile local pat-123
```

Service account：

```bash
soha-cli service-account list --profile local
soha-cli service-account create --profile local --name ci-runner --role-ids delivery-operator
soha-cli service-account token-list --profile local
soha-cli service-account token-create --profile local --service-account-id sa-123 --name ci-token
soha-cli service-account token-revoke --profile local sat-123
```

创建 token 时后端返回的明文 token 只会在本次命令输出中出现一次。`token-list` 只返回服务账号 token 的 id、所属服务账号、prefix、权限、scope、过期、最近使用和吊销状态。列表、审计和诊断输出默认脱敏，不显示 token hash、kubeconfig、环境变量或 secret-looking 字段。

## Audit

查询 Gateway audit：

```bash
soha-cli audit list --profile local --tool-name diagnosis.release_failure.analyze --limit 20
soha-cli audit list --profile local --actor user-1 --from 2026-05-29T00:00:00Z
```

支持 actor、actor type、AI client、skill、tool、risk level、result、action、from/to 和 limit 过滤。时间使用 RFC3339。

## Governance

查询 Gateway 治理健康和运营指标：

```bash
soha-cli governance status --profile local
soha-cli governance status --profile local --window-hours 48 --json
```

`governance status` 调用 `GET /api/v1/ai-gateway/governance/status`，默认统计最近 24 小时审计；显式 `--window-hours` 只接受 1 到 168，避免直接 API 请求触发异常大窗口扫描。输出包括健康状态、health checks、调用成功/拒绝/失败数、pending approval 数、approval SLA 摘要、PAT/service token 的 last-used/过期/未使用提醒、AI client 注册审批状态、budget/rate-limit/redaction/resource-scope 覆盖度和异常调用 finding。普通文本输出会把每个 health check 的状态、名称、count 和脱敏消息打印出来，也会把 finding 的 actor、subject、AI client、approval request、policy、grant、tool 和 risk level 一并打印，并展示结构化 `recommendationAction` 的 action、target、refs 和 count，便于定位 `approval_sla_due_soon`、`stale_gateway_approvals`、`high_risk_allow_without_approval` / `high_risk_grant_without_approval` 以及 `high_risk_allow_without_resource_scope` / `high_risk_grant_without_resource_scope` 这类治理告警；普通文本输出还会展示 redaction hit summary，包括 total matches、命中 audit 数、input/output target、主要 match type/classifier/field path/policy/tool。`policyCoverage` 会同时展示 total 与 active policy/grant/binding 数量，`redactionPolicyState` 会把 input `redactionPolicy`、`outputRedactionPolicy` 和 response/output redaction aliases 计为 configured，disabled policy、expired grant 和 disabled binding 不会把 coverage state 标记为 `configured`；没有任何 active `resourceScopes` 覆盖时，后端会通过 `resource_scope_coverage` health check 和 recommendation action 提醒补齐跨环境访问约束；`--json` 保留完整脱敏 DTO，包括 approvals、riskCounts、pendingApprovalClientIds、redaction summary、activeAccessPolicies、activeToolGrants、resourceScopeState、resourceScopedAccessPolicies、`recommendationActions` 和 token due/stale day 字段。已配置的 `rateLimit`、调用次数型 `budget`、token/cost budget、strict/sanitize input redaction、output redaction 和 `resourceScopes` 会在后端 invocation 前强制执行；CLI 只展示脱敏治理摘要。

## Approval Requests

治理输出或 Console 中看到 approval request id 后，可以从 CLI 继续查询和处理：

```bash
soha-cli approval list --profile local --status pending --tool-name delivery.actions.trigger
soha-cli approval timeline approval-123 --profile local
soha-cli approval approve approval-123 --profile local --comment "approved in change window"
soha-cli approval reject approval-123 --profile local --comment "missing rollback plan"
soha-cli approval cancel approval-123 --profile local --comment "duplicate request"
```

`approval list` 代理 `GET /api/v1/ai-gateway/approval-requests`，支持 `id`、`status`、`actor`、`actor-type`、`ai-client-id`、`skill-id`、`tool-name`、`risk-level`、`strategy`、`from`、`to` 和 `limit` 过滤。`approval timeline` 代理 `GET /api/v1/ai-gateway/approval-requests/:requestID/timeline`，返回后端从 `approvalRouting` 派生的 `approvalTrace`、decision、stage history、workflow/task 关联和同一 approval request 的 Gateway audit events。`audit list` 也支持 `--approval-request-id`，便于只查看某个审批请求关联的审计事件。`approve`、`reject` 和 `cancel` 只代理已有审批 API，并把 `--comment` 作为 decision comment 发送；实际候选审批人、change window、多阶段 quorum、AI client 激活或 tool replay 都仍由后端 Gateway application service 执行。CLI 输出会脱敏 approval tool input、decision comment、执行输出和关联 metadata。

## MCP stdio server

本地启动：

```bash
soha-cli mcp start --profile local
```

生成 MCP client 配置：

```bash
soha-cli mcp install \
  --profile local \
  --command /usr/local/bin/soha-cli \
  --ai-client-id codex-local \
  --ai-client Codex \
  --skill-id k8s-sre
```

输出形态：

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

如果没有传 `--profile`，`mcp install` 会使用当前 profile。`--ai-client-id`、`--ai-client` 和 `--skill-id` 会写入生成的 `mcp start` 参数，用于固定审计来源、AI client context 和 skill binding 上下文。

MCP server 支持：

- `initialize`
- `tools/list`
- `tools/call`
- `resources/list`
- `resources/read`
- `prompts/list`
- `prompts/get`

`initialize` 会返回 soha MCP 的安全边界说明：本地进程只是 AI Gateway proxy，不直接访问 PostgreSQL、Kubernetes、Docker、runner 工作目录、本地 kubeconfig，也不会绕过后端权限拼接 prompt 或 resource 内容。`tools/list`、`resources/list` 和 `prompts/list` 从 Gateway manifest 动态生成 MCP 能力列表；当前默认 Gateway tools 都会透出结构化 `inputSchema`，并在 manifest 定义输出契约时透出 `outputSchema`，让 MCP client 可以识别 `applicationId`、`action`、`clusterId`、`namespace`、`podName`、`deploymentName`、`serviceName`、`nodeName`、`taskId` 等输入字段以及可预期输出字段。`resources/list` 会透出 resource `contextSchema`，`prompts/list` 会透出 prompt `argumentSchema`、`contextSchema` 和标准 MCP `arguments[]` 摘要。`tools/list` 会输出标准 MCP `annotations`：`read` tool 标记只读/幂等，`mutate`、`execute`、`high` tool 标记 destructive，且 soha tool 默认 `openWorldHint=true`。三类列表都会在 `_meta.soha` 中带上 `permissionKeys` 和 `requiredScopes`；tool 列表还会带上 `domain`、`action`、`mcpAdapterId`、`mcpToolName`、`riskLevel` 和 `requiresApproval`，用于客户端展示调用/读取前的权限、scope、adapter 映射与风险预期。`tools/call` 只代理到 `POST /api/v1/ai-gateway/tools/:toolName/invoke`，`resources/read` 只代理到 `POST /api/v1/ai-gateway/resources/read`，`prompts/get` 只代理到 `POST /api/v1/ai-gateway/prompts/get`；缺少 tool name、resource URI/name 或 prompt name 时会直接返回 JSON-RPC invalid params，不会把无效请求发给后端。

`resources/read` 使用 manifest 暴露的 `soha://...` URI，例如 `soha://delivery/applications`、`soha://delivery/execution-tasks` 和 `soha://k8s/runtime`。`prompts/get` 支持 `soha.delivery.plan_release` 和 `soha.k8s.diagnose_workload`，并会把 MCP arguments 和 context 交给后端 Gateway 脱敏后合成 prompt messages。resource 和 prompt 也受 skill binding 约束：当 `--skill-id` 指向某个绑定 skill 时，后端只允许读取与该绑定 capability refs 相关的 resource/prompt。

当前可调用 Gateway tools 包括：

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

`delivery.actions.trigger` 支持 build、deploy、build_deploy、workflow、verify 和 rollback；rollback 应先用 `delivery.rollback.context` 读取建议上下文，再以 `action=rollback` 触发受控 workflow。k8s 工具需要 `clusterId`，`namespace` 可按平台 scope 语义为空表示聚合视图。`k8s.pods.logs` 还需要 `podName`，可选 `container`、`tailLines`、`sinceSeconds` 和 `previous`。日志输出会在 Gateway 层做基础敏感字段脱敏。

## Skills

仓库内置首批 Skills：

- `delivery-developer`
- `delivery-tester`
- `k8s-sre`
- `security-change`

查看可安装 Skills：

```bash
soha-cli skill list --source skills/ai-gateway
```

安装单个 Skill：

```bash
soha-cli skill install \
  --source skills/ai-gateway \
  --dest ~/.soha/skills \
  delivery-developer
```

安装全部：

```bash
soha-cli skill install --all
```

默认来源为 `skills/ai-gateway`，可用 `SOHA_SKILLS_SOURCE=/abs/path` 覆盖。默认安装目录为 `~/.soha/skills`，可用 `SOHA_SKILLS_DIR=/abs/path` 覆盖。

Skills 是工作流说明，不是安全边界。AI 客户端安装 Skill 后仍必须通过当前身份可见的 MCP tools 工作。

## 诊断

```bash
soha-cli diagnose --profile local
soha-cli diagnose --profile local --tool k8s.pods.logs
soha-cli diagnose --profile local --resource soha://k8s/runtime
soha-cli diagnose --profile local --prompt soha.k8s.diagnose_workload
soha-cli diagnose --profile local --tool k8s.pods.logs --ai-client-id codex-local --skill-id k8s-sre --source codex-mcp
```

`diagnose` 会验证 profile、server、token 和 Gateway capability 读取链路，并输出 tool/resource/prompt/skill/permission key 数量。传入 `--tool` 时会展示该 tool 的 risk level、审批要求、permission keys、required scopes，以及从 manifest `inputSchema` / `outputSchema` 汇总出的 `inputRequired`、`inputFields`、`outputRequired` 和 `outputFields`，并提示检查 MCP tool grants、AI access policies、skill bindings、AI client context 和 resource scopes。传入 `--resource` 或 `--prompt` 时会展示 resource/prompt 的 permission keys，并提示检查 Gateway `resources/read` / `prompts/get`、skill binding、AI client context 和 arguments/context。`--ai-client-id`、`--ai-client`、`--skill-id` 和 `--source` 只覆盖本次诊断请求，用于模拟 `mcp start` 或 `mcp install` 生成的客户端上下文，不会修改 profile。它不打印 token。

## Completion

```bash
soha-cli completion bash
soha-cli completion zsh
```
