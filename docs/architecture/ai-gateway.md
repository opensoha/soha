# soha AI Gateway

## 目标

`soha AI Gateway` 是 soha 面向外部 AI Coding、IDE Agent、CI 自动化和企业 Agent 平台的 AI 原生运维入口。

它不是把页面功能简单搬到 CLI，也不是让 MCP 直接操作数据库或 Kubernetes。Gateway 的职责是把外部 AI 请求收进 soha 的安全边界，再复用现有应用层能力完成查询、发布、诊断和证据回写。

## 与 AI 工作台的边界

现有 `AI Workbench`、MCP adapter、Agent Runtime 和 execution plane 是 AI Gateway 的能力底座，但不是同一个层次：

- `AI Workbench`：soha 内部使用 AI 的交互工作台，负责会话、工具装配、巡检、RCA 和分析工件。
- `Agent Runtime`：soha 调度内部或外部 agent provider 的执行运行时，负责 claim/callback、tool binding、skill binding 和 artifact 归一化。
- `MCP adapter`：soha 内部工具和外部数据源的能力目录。
- `execution plane`：构建、发布、Docker、虚拟化等长任务的 durable runner/callback 体系。
- `AI Gateway`：外部 AI 客户端进入 soha 的统一安全入口和能力 manifest。

标准流转：

```text
AI Client / soha-cli / MCP
  -> AI Gateway
  -> permissionKeys / scope grants / AI grants / risk policy / audit
  -> delivery, resource, copilot, docker, virtualization services
  -> execution plane or Agent Runtime when needed
```

## CLI、MCP 和 Skills

`soha-cli` 是统一本地入口：

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

当前代码入口位于 `cmd/soha-cli`，核心实现位于 `internal/cli/sohacli`。已落地的命令面覆盖 profile/context 管理、capability manifest 查看、人工 Gateway tool 调用、resource/prompt 排查、PAT 和 service account token 管理、Gateway audit 查询、governance status、approval timeline 和处理、MCP 配置生成、skill 安装、diagnose 以及 shell completion。

CLI 只负责认证、配置、MCP 启动和人工兜底命令。所有真实平台动作必须调用 soha API。本地 profile 默认存储在 `~/.soha/config.json`，文件权限为 `0600`，`profile show` 只能展示脱敏 token。

`soha MCP Server` 通过 `GET /api/v1/ai-gateway/capabilities` 动态获取当前身份可用 tools、resources、prompts 和 skills。MCP 可以隐藏无权工具，但隐藏工具不是安全边界；后端应用层必须对每次工具调用再次校验权限、scope、grant 和风险策略。

`soha Skills` 负责告诉 AI “如何按 soha 规范工作”，例如开发者应用接入、测试发布验证、SRE 只读排障和安全变更流程。首批文件位于 `skills/ai-gateway`。Skills 不直接赋权。

## 身份模型

AI Gateway 的身份分三层：

1. 个人身份
   - `soha login` 获取本地 CLI/MCP token。
   - 继承用户角色、权限键、团队和 scope grants。
   - Gateway 个人 token 使用 `soha_pat_` opaque token 前缀，数据库只保存 hash 和展示用 prefix。
2. 服务身份
   - `service_accounts` 和 `service_account_tokens` 用于 CI、Webhook、共享 runner 和自动化系统。
   - 服务身份必须有明确角色、scope grants、过期时间和吊销路径。
   - 服务账号 token 使用 `soha_sat_` opaque token 前缀，解析后映射为 `service_account:<id>` principal。
3. AI 客户端身份
   - `ai_clients` 记录 Cursor、Codex、Claude Code、CI Agent、企业 Agent 平台等调用来源。
   - 审计必须同时记录用户或服务账号、AI client、skill 和 tool。

## 授权模型

AI Gateway 采用四层授权：

1. `permissionKeys`
   - 复用现有角色权限体系。
   - `ai.gateway.view` 允许读取 Gateway manifest。
   - `ai.gateway.invoke` 允许通过 Gateway 代理调用已授权工具。
   - `ai.gateway.manage` 允许管理 AI client、service account、tool grants、skill bindings 和 access policy。
2. resource scopes
   - 继续复用应用、应用环境、环境 tag、范围 Key（兼容 `businessLineId`）、集群、namespace 等 scope grants。
3. MCP tool grants
   - `mcp_tool_grants` 控制主体和 AI client 能调用哪些 tool。
   - tool grant 只能收窄能力，不能绕过已有 `permissionKeys`。
   - 未配置 grant 时按 `permissionKeys` 暴露能力；一旦配置 allow grant，就形成 allow-list；deny grant 永远优先。
4. risk policy
   - `ai_access_policies` 控制主体、角色和 AI client 在 Gateway 内的风险边界。
   - deny policy 永远优先；存在 allow policy 时形成 allow-list。
   - policy 可按 tool pattern、skill、risk level 和 resource scope 收窄能力，并可把命中的 tool 标记为需要审批。
   - `read`、`analyze`、`mutate`、`execute`、`high` 分级。
   - `approval_policy.strategy` 支持 `allow`、`deny`、`require_approval`、`require_human_confirm` 和 `dry_run_only`。`mutate`、`execute`、`high` 风险 tool 命中审批、确认或 dry-run 策略时不会进入 owning application service。
   - Gateway 会为 `require_approval` 和 `require_human_confirm` 建立持久审批请求，返回 `pending_approval` / `pending_human_confirm` 与 request id，并写入 Gateway audit 与 operation log。
   - 审批通过后由 `internal/application/aigateway` 重新校验当前 grant、policy、skill binding 和业务权限，再回到 owning application service 或 durable task 执行；handler 和 repository 不执行业务动作。
   - `approval_policy.approvalPolicyRef` / `approvalPolicyId` 可引用 delivery approval policy，Gateway 会复用其 SLA 作为审批请求过期时间，并把 delivery policy 的 `mode`、`requiredApprovals`、`approverRoles`、`changeWindow` 和 `metadata` 中的 routing 扩展合并进 `approvalRouting`。
   - `approval_policy.approverUsers` / `approverRoles` / `approverTeams`、`onCallRef`、`approvalMode`、`approvalStages`、`requiredApprovals` / `minApprovals`、`requiredRoleApprovals` / `requiredTeamApprovals` 和 `changeWindow.startsAt/endsAt` 会写入 approval request 的 `relatedIds.approvalRouting`；`onCallRef` 会通过 monitoring on-call resolver 解析当前值班人，并把当前值班 `userId` 追加到 `candidateUserIds`，同时记录 `onCallCandidateUserIds` 和 `onCallResolution`。审批、拒绝、取消时会校验当前操作者是否命中候选人/角色/团队，批准时还会校验 change window。配置多人审批 quorum 时，Gateway 会记录唯一审批用户的 `decisions`、`approvedCount`、角色/团队计数、`pendingRequirements` 和 `satisfiedRequirements`；默认 `approvalMode=all` 表示会签，所有总人数和分组配额都满足后才 replay tool，`approvalMode=any` 表示或签，任一显式总人数、角色或团队配额满足即可 replay tool。配置 `approvalStages` 时，Gateway 只允许当前阶段候选人审批，阶段满足后推进 `currentStageIndex` 并写入 `stageHistory`，最后阶段满足后才 replay tool。
   - 审批 replay `delivery.actions.trigger` 时，Gateway 会把 `aiGatewayApprovalRequestId`、policy、tool、skill 和 AI client 关联写入 delivery workflow variables；workflow run metadata 和 `workflow_approvals.metadata` 会保存这些跨域关联，Gateway/delivery related IDs 会回填 `workflowRunId`，用于从 Gateway 审批追踪到 workflow manual approval 节点。Console 中 `/ai-gateway` 和 `/workflows` 已通过 `approvalRequestId`、`workflowRunId` 和 `gatewayApprovalRequestId` query 参数提供双向 drilldown；带 `workflowRunId` 定位到工作流时会自动展开行内详情，展示 Gateway 关联、manual approval 节点详情、workflow node timeline 和原始 trace。后端还提供 `GET /api/v1/ai-gateway/approval-requests/:requestID/timeline`，把 approval request、`approvalRouting` 派生的 decision/stage trace、workflow/task 关联和同一请求的 Gateway audit events 聚合为只读排障视图。
5. skill bindings
   - `ai_gateway_skill_bindings` 控制主体、角色和 AI client 可使用哪些 soha Skills 以及每个 skill 可引用的 capability refs。
   - skill binding 只能收窄 manifest 和 tool invocation，不能赋予新权限。

## 首批 API

```http
GET /api/v1/ai-gateway/capabilities
POST /api/v1/ai-gateway/tools/:toolName/invoke
POST /api/v1/ai-gateway/resources/read
POST /api/v1/ai-gateway/prompts/get
GET /api/v1/ai-gateway/governance/status
```

`capabilities` 返回当前身份可见的能力清单。`tools/:toolName/invoke` 是 MCP、CLI 和外部 AI Agent 的统一工具调用入口。调用时必须重新校验 `ai.gateway.invoke`、tool 自身的业务权限、scope、AI grant 和风险策略，然后转入拥有该能力的 application service。

Gateway manifest 由 `internal/application/aigateway` 的 capability registry/provider 生成。当前默认 delivery、k8s 和 diagnosis 能力作为 static provider 注册；服务运行路径中的 manifest 过滤、tool/resource/prompt/skill lookup、resource 到 tool/prompt/skill 的关联关系、可选 provider tool invoker、grant 和 binding 构建、access policy skill 匹配、governance 高风险扫描，以及 resource 文档的 related metadata 都使用同一 registry。后续新增模块能力应注册 provider；自定义 tool 执行也要通过 provider invoker 回到 owning application service，并继续复用 Gateway 鉴权、risk policy、redaction 和 audit 主流程，而不是在 handler、repository 或本地 MCP 进程里复制静态目录。

`resources/read` 读取 manifest 中的 `soha://...` resource 文档，返回 JSON 资源说明、关联 tools/prompts/skills、required scopes 和经过 Gateway 脱敏的请求 context。`prompts/get` 返回 soha prompt template messages，并可结合 `X-Soha-Skill-ID` 或请求中的 `skillId` 与当前 arguments/context 生成提示词。skill binding 会同时约束 tools、resources 和 prompts：资源/提示词可见性由其关联 tools 是否落在绑定 capability refs 内决定，读取时也会重新校验，返回的 resource 文档和 prompt skill context 只展示已绑定能力。MCP `initialize` 会返回稳定 `instructions`，声明本地 MCP 进程只是 Gateway proxy，不直接访问 PostgreSQL、Kubernetes、runner workspace、kubeconfig、Docker 或特权 prompt/resource 内容。MCP `tools/list` 会输出 manifest `inputSchema`，并在 manifest 定义输出契约时透出 `outputSchema`；`resources/list` 会透出 resource `contextSchema`，`prompts/list` 会透出 prompt `argumentSchema`、`contextSchema` 和 MCP `arguments[]` 摘要；同时输出标准 `annotations`：`read` tool 标记只读/幂等，`mutate` / `execute` / `high` 标记 destructive，且 soha tool 默认 `openWorldHint=true`；`tools/list` / `resources/list` / `prompts/list` 还会在 `_meta.soha` 中透出 manifest 的 `permissionKeys` 和 `requiredScopes`，tool 元数据还会透出 `domain`、`action`、`mcpAdapterId`、`mcpToolName`、`riskLevel` 和 `requiresApproval`。这些字段便于客户端在调用或读取前展示预期权限、scope、adapter 映射、输入/输出契约和风险；真实 tool 调用、resource 读取和 prompt 读取仍由后端应用层校验 `ai.gateway.invoke`、业务权限、skill binding、AI grant 和风险策略，并写入 Gateway audit；空 `tools/call.name`、`resources/read.uri/name` 或 `prompts/get.name` 会直接返回 JSON-RPC 参数错误，不会对后端发起无效请求。

`governance/status` 是 AI Gateway 的只读运营状态入口，需要 `ai.gateway.manage`。它从 token、AI client、access policy、tool grant、skill binding、Gateway audit 和 pending approval request 汇总健康状态、近窗口调用指标、异常调用线索、token 过期/未使用提醒、last-used 跟踪状态、AI client 注册审批状态、pending approval SLA，以及 budget、rate limit、redaction policy 和 resource scope 的配置覆盖度。coverage 会同时返回 total 与 active 数量，`budgetState` / `rateLimitState` / `redactionPolicyState` / `resourceScopeState` 只由启用中的 access policy、未过期 tool grant 和启用中的 skill binding 贡献；`redactionPolicyState` 会把输入 `redactionPolicy`、`sensitiveDataRedaction`、输出 `outputRedactionPolicy` 以及 response/output redaction aliases 都计为 active coverage，disabled 或 expired 记录不会把状态伪装为 `configured`；当没有任何 active access policy 或 tool grant 配置具体 `resourceScopes` 时，`resource_scope_coverage` health check 会降级并给出跨环境 Gateway 访问前补齐 scope 的 recommendation。它还会基于当前 capability registry 识别 active allow access policy 和未过期 allow tool grant 中缺少 `require_approval`、`require_human_confirm` 或 `dry_run_only` 防护的高风险 allow，并作为 warning finding、`high_risk_guardrails` health check 和 recommendation 暴露；同类高风险 allow 如果缺少具体 `resourceScopes` 约束，也会作为 `high_risk_allow_without_resource_scope` / `high_risk_grant_without_resource_scope` finding 和 `high_risk_resource_scopes` health check 暴露。pending 审批会汇总 `dueSoon`、`stalePending`、`oldestPendingHours` 和 `nextDueAt`，并通过 `approval_sla_due_soon`、`stale_gateway_approvals` 或 `overdue_gateway_approvals` finding 暴露审批队列压力。`pending` / `pending_approval` AI client 会创建 `ai_gateway_approval_requests` 持久审批请求，批准后转为 `active`，拒绝后转为 `rejected`，取消后转为 `disabled`。该接口只返回脱敏摘要、请求 ID 和 token prefix，不返回 token hash、明文 token、raw tool input 或原始日志正文。

Console 的 `/ai-gateway` Governance 标签页直接消费该接口，支持按 1h、6h、24h、48h 或 7d 时间窗口查看 health checks、recent calls、token/client/approval 摘要、policy coverage、top tools / AI clients / actors、anomaly findings 和 recommendations。

当前可直接调用的工具覆盖应用交付、交付上下文分析和只读 Kubernetes 诊断：

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

Kubernetes 工具通过 `internal/application/resource` 读取平台聚合视图，继续遵守 direct/agent 集群能力边界、cluster/namespace scope 和资源权限。发布失败诊断工具会聚合 delivery execution、release bundle、Pod、Deployment、Service、Event 和日志上下文；日志类输出在 Gateway 层做基础敏感字段脱敏，并把诊断结果转换成 Copilot/Agent Runtime 的 `AnalysisArtifact`。

增强后的 k8s 诊断工具仍只返回 soha 平台 view model，不返回 raw Kubernetes object。Pod describe 聚合 containers、conditions、volumes 和 related resources；Deployment 工具读取 rollout status 和有界事件上下文；Service 工具聚合 selector、backend pods 和 ingress hints；Route context 聚合 Ingress 与 Gateway API typed route 视图，并在 Gateway API 可选资源不可用时返回 `capabilityWarnings`；Storage context 聚合 PVC、PV、StorageClass 与 unbound PVC；Node detail 返回 conditions、taints、resource summary 和 scheduled pods。

交付工具只通过 owning application service 读取或触发能力：应用和 service/container 信息来自 application/delivery service，workflow template 来自 catalog service，approval policy、release bundle、execution task 和 execution logs 来自 delivery service。`delivery.release_context.diff` 与 `delivery.rollback.context` 只生成候选发布、promotion 和 rollback 上下文，不执行真实 rollback；实际 build/deploy/rollback 必须使用 `delivery.actions.trigger` 并通过风险策略、审批和 execution/workflow plane。`action=rollback` 会进入 delivery workflow rollback 模式，只执行绑定 workflow template 中的 `rollback_to_previous` DAG 节点，并把 `releaseBundleId` 写入 workflow variables 和 run metadata。

`diagnosis.release_failure.analyze` 的 artifact 落库通过 Copilot application service 完成，最终写入 `ai_agent_runs.analysis_artifacts`。Gateway 只负责收集、脱敏、归纳证据和调用应用服务；artifact 中保存 application、environment、release bundle、execution task、cluster、namespace、workload、pod 等关联 ID，以及证据摘要、假设、建议、下一步检查、tool execution 和 data source snapshot，不保存 raw log 正文、secret、kubeconfig 或环境变量。需要外部 provider 深度推理时，调用方可传 `agentProviderId` / `providerId` 或 `deepAnalysis` / `externalAnalysis`，Gateway 会把同一份脱敏上下文交给 Copilot application service 创建 `queued` AgentRun，并返回 `agentRunId`、provider 和 `agent_runtime_claim_callback` 运行时标记；后续由 Agent Runtime runner claim/callback 写回 artifact，Gateway 不同步调用外部 provider。Agent Runtime callback 分批回写 artifact 时会按 `runId + kind + title` 稳定键增量合并，保留既有 artifact 顺序并替换同键产物，避免后续 provider callback 覆盖先前分析产物。Agent Runtime runner 在 provider 命令失败或超时时也会随 failed/callback_timeout callback 主动回传合成 artifact，保留 run、provider、capability、scope、tool execution、错误状态和排查建议，并在回传前对失败 error、logs、tool output 和 artifact snapshot 做基础敏感文本脱敏，控制面无需只依赖服务端兜底生成失败产物。Copilot application service 会为 root-cause、performance、trace、inspection-review 和 Agent Runtime artifact 的 data source snapshot 写入 `sessionId`、`rootCauseRunId`、`inspectionRunId`、`agentRunId` / `agentRuntimeId` 等稳定关联；Console 的 AI Workbench artifact 历史会把这些关联展示成会话、根因运行、巡检运行和 Agent Run 入口，其中巡检复盘入口可定位到 `/ai-workbench/inspection?view=runs&inspectionRunId=...`。外部 Agent Runtime provider 的 callback payload 如果携带 `usage`、`tokenUsage`、`aiUsage`、`providerUsage`、`llmUsage`、`metering`、`billing` 或 `costUsage`，Copilot application service 会把其中的数值 token/cost 摘要标准化为 `providerUsage` / `usage`，并写入 agent run output 与 artifact data source snapshot；同时显式识别 Gemini/Vertex `usageMetadata.promptTokenCount/candidatesTokenCount/totalTokenCount`、Ollama `prompt_eval_count/eval_count`、Anthropic cache input token 字段、Cohere `billed_units` 优先语义、search/classification/embedding/rerank/request/response usage units、Brave Search / SerpAPI 查询与搜索 credit、Browserbase session/minute/page-load、Exa/Jina/Unstructured/LlamaCloud 文档页/解析页/字符/chunk、Helicone request 计量别名、通用 `promptCost` / `inputCost` / `completionCost` / `outputCost` 以及常见 cost 字段。provider 原始 payload、模型字符串和非数值字段不进入用量摘要。

凭证入口：

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

PAT/SAT 只保存 hash 和展示 prefix，创建和轮换只返回一次明文。撤销和过期会在 identity token 解析层生效；轮换会复制旧 token 的 scopes、permissionKeys 和 metadata，重新按当前用户或服务账号角色校验 permissionKeys，生成新 token 后吊销旧 token。请求体可选传 `expiresAt` 覆盖新过期时间；未传时沿用旧 token 的未来过期时间，旧 token 已过期则默认给新 token 90 天窗口。

AI 客户端和工具授权管理入口：

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

`tool-grants` 支持 `user`、`service_account`、`role` 和 `ai_client` 四类 subject。运行时会同时合并当前主体、角色和 AI client 的 grant：deny 优先，存在 allow grant 时形成 allow-list。

`access-policies` 和 `skill-bindings` 同样支持 `user`、`service_account`、`role` 和 `ai_client` subject。运行时 Gateway 会合并当前主体、角色和 AI client 的启用记录：access policy 先按 deny/allow 收窄 tools 和 skills，skill binding 再按绑定的 skill/capability refs 收窄 manifest 和 invocation。所有这些控制都发生在 `permissionKeys` 之后，因此不会扩大已有 RBAC 或 scope grant。

tool invocation 会从输入提取标准资源 scope，包括兼容范围字段 `businessLineId`、应用与交付字段 `applicationId`、`applicationEnvironmentId`、`environmentId`，以及运行时字段 `clusterId`、`namespace`、`releaseBundleId` 和 `executionTaskId`。其中 `businessLineId` 与全局 `environmentId` 在当前模型下主要作为历史 scope / 兼容字段存在，新的控制面优先使用应用分组、应用环境绑定和环境 tag。`mcp_tool_grants.resource_scopes` 与 `ai_access_policies.resource_scopes` 在 invocation 前强制匹配；manifest 可以保守展示潜在能力，但实际调用必须通过 scoped grant/policy 与拥有该能力的业务 service 双重校验。

高风险 tool 的 risk policy 会在进入业务 service 前执行。策略 `deny` 直接拒绝；`require_approval` 返回 `pending_approval` 和 `approvalRequestId`；`require_human_confirm` 返回 `pending_human_confirm` 和 `confirmationRequestId`；`dry_run_only` 返回 `dry_run` 和 `dryRunId`，且不会执行真实变更。策略 `allow` 可用于显式允许某个命中的风险范围继续进入 owning application service，但后续仍要通过业务 service 自身权限、scope 和 durable task 边界。需要持久审批的策略可以配置候选审批人、候选角色、候选团队、`onCallRef`、change window、`approvalMode=all|any`、`approvalStages`、`requiredApprovals` / `minApprovals` 总人数 quorum，以及 `requiredRoleApprovals` / `requiredTeamApprovals` 分组配额；Gateway 会把这些路由信息作为脱敏 metadata 持久化，并在决策入口拦截非候选操作者或不在变更窗口内的批准动作。配置 `approvalPolicyRef` 时，Gateway 会读取 delivery approval policy，把其 `mode` 映射为 Gateway 会签/或签模式，把 `requiredApprovals`、`approverRoles`、`changeWindow` 和 `metadata` 中的 routing 扩展合并进请求；AI access policy 中的显式候选人和更高 quorum 会继续叠加，但不会把 delivery policy 的人数配额降小。配置 `onCallRef` 时，Gateway 会先调用 monitoring on-call resolver 获取当前值班 `currentParticipant`，将其追加为审批候选 `userId`，并保存排班、轮值、route、窗口或 unresolved 状态摘要。多人审批只按唯一 `userId` 计数，同一审批人重复批准会替换自己的 decision，不会推进 quorum；默认会签要求总人数、角色配额和团队配额全部满足，或签模式下任一显式 quota 满足即可转入 approved/executed replay。分阶段审批会把每个阶段的候选人、角色/团队配额、审批模式、`onCallRef` 和 change window 作为当前 active routing，当前阶段满足后仍保持 `pending` 并推进下一阶段，直到最后阶段满足才执行。

`ai_access_policies.conditions` 已参与 invocation 前强制执行。`rateLimit.maxCallsPerMinute`、`rateLimit.maxCallsPerHour` 和通用 `rateLimit.maxCalls + windowSeconds/windowMinutes/windowHours` 默认写入 `ai_gateway_rate_limit_counters` 独立 fixed-window counter，并用 PostgreSQL upsert 原子递增来拒绝超额请求；当 `rateLimit.mode` / `algorithm` / `strategy` 设置为 `sliding_window`、`rolling_window` 或 `audit_window` 时，会直接使用 Gateway audit 作为真实滚动窗口；设置为 `gcra`、`token_bucket`、`leaky_bucket` 或 `strict` 时，会改用 GCRA/TAT 语义，`burst` / `burstSize` / `capacity` 控制突发容量，拒绝请求只返回 retry-after 不推进未来可用额度。默认 backend 使用 PostgreSQL `ai_gateway_rate_limit_counters` / `ai_gateway_rate_limit_states`；配置 `ai_gateway.rate_limit.backend=redis` 和 `ai_gateway.rate_limit.redis.addr` 后，Gateway 会优先使用 Redis 原子 INCR/EXPIRE 与 Lua GCRA 脚本作为高吞吐共享限流后端，Redis 不可用时回退 PostgreSQL，旧库缺少对应限流表时再暂时 fallback 到近窗口 Gateway audit 统计。`budget.maxCallsPerHour`、`budget.maxCallsPerDay`、`budget.maxCallsPerMonth` 和通用 `budget.maxBudgetCalls + window...` 作为调用次数预算执行，仍基于 Gateway audit 对 success/failure/dry_run 调用做配额核算；`budget.maxTokens*` / `maxBudgetTokens` 会统计 audit metadata 中的 `usage`、`tokenUsage` 或 `providerUsage` token 用量，`budget.maxCost*` / `maxBudgetCost` 会统计同类 metadata 中的 cost 用量。Gateway 在普通 tool invocation 和 approval replay 成功后，会从 owning service 输出或 related IDs 中的 `usage`、`tokenUsage`、`aiUsage`、`providerUsage`、`llmUsage`、`metering`、`billing`、`costUsage` 容器抽取数值型 token/cost 摘要，也会显式识别 Gemini/Vertex `usageMetadata.promptTokenCount/candidatesTokenCount/totalTokenCount`、cached content、tool-use prompt 和 thoughts token 字段，Ollama `prompt_eval_count/eval_count`、Anthropic cache input token 字段、OpenAI cached/audio/reasoning/prediction token details、Bedrock text/image/audio token fields、Cohere billed units（同一 usage 容器同时有 `billed_units` 和 `tokens` 时优先使用 billed units，避免双计）、search/classification/embedding/rerank/request/response usage units、Brave Search / SerpAPI 查询与搜索 credit、Browserbase session/minute/page-load、Exa/Jina/Unstructured/LlamaCloud 文档页/解析页/字符/chunk、Helicone request 计量别名、billable/billed/usage token aliases、多模态 text/image/audio/video 输入输出别名、cache read/write/hit/miss input token 变体、DashScope/Moonshot/智谱/千帆常见 `*_tokens_count` / `*_token_usage` 变体、通用 `promptCost` / `inputCost` / `completionCost` / `outputCost` 和常见 cost / costMicros / costCents 字段，并回写为 audit metadata 的 `providerUsage` 和 `usage`；只保存 `total/input/output tokens` 与 `total/input/output cost` 等数值字段，不保存 provider 原始 payload 或字符串模型信息。缺少用量 metadata 时不会伪装成已消费。`redactionPolicy.mode=strict|deny_sensitive|block_sensitive` 或 `denySensitiveInput=true` 会拒绝携带 sensitive key、token/password/secret-like 文本的输入，`redactionPolicy.mode=sanitize|mask|redact` 或 `sanitizeInput=true` 会在进入 owning service 前改写 tool input，把敏感键和值替换为 `[REDACTED]`。redaction policy 还支持 `fields` / `redactFields` 指定字段路径、`allowFields` 字段级例外、`replacement` 自定义替换值、`preserveFormat` 尾部保留 mask、`valuePatterns` 自定义正则值规则、`secretTypes` 结构化 secret 分类器，以及 `rules[]` 按 `toolPatterns` 定制字段级替换规则。内置 `secretTypes` 覆盖 GitHub、GitLab、OpenAI、Anthropic、Google API key、Hugging Face、Cohere、Mistral、DeepSeek、Groq、Together AI、Replicate、LangSmith、Pinecone、xAI、Perplexity、Tavily、Langfuse、Qdrant、Weights & Biases、Linear、OpenRouter、Fireworks AI、Voyage AI、Brave Search、SerpAPI、Browserbase、Exa、Jina AI、Unstructured、LlamaCloud、Helicone、DashScope、Moonshot、智谱 AI、SiliconFlow、腾讯混元、百度千帆、火山方舟、Grafana、Sentry、New Relic、Azure OpenAI、Azure DevOps PAT、Datadog、PagerDuty、PostHog、Splunk、Elastic、Terraform Cloud、npm、Stripe、Slack、JWT、AWS access key、PEM private key、Kubernetes Secret、kubeconfig、Docker config、GCP service account JSON 和 AWS credentials 结构。默认 redaction rule 只处理 tool input；需要处理返回结果时使用 `outputRedactionPolicy`，或在规则中设置 `target=output|both`。输出脱敏同样在 owning service 返回后、Gateway 响应和审批执行结果持久化前完成。字段、正则值、secret classifier 和结构化 Secret 命中会写入 Gateway audit metadata 的 `redaction` 摘要，只记录 target、字段路径、命中类型、分类器名称和计数，不记录原始敏感值。条件默认按 actor+AI client+tool 计数，policy 可用 `scope` / `limitScope` 调整为 actor、client、actor_client、client_tool 或 global；所有拦截都会写 deny audit，且错误摘要不包含原始敏感输入。

审批请求入口 `GET /api/v1/ai-gateway/approval-requests` 支持按 id、status、actor、AI client、skill、tool、risk level、strategy 和时间范围过滤，并在响应中附带后端从 `relatedIds.approvalRouting` 派生的 `approvalTrace` 摘要。批准、拒绝、取消入口只接受 request id 和备注。Tool invocation 审批批准后会先记录当前审批人的 vote；如果当前阶段或全局 `approvalRouting.requiredApprovals`、`requiredRoleApprovals`、`requiredTeamApprovals` 按当前 `approvalMode` 尚未满足，请求继续保持 `pending`，并在 routing metadata 中写入 `pendingRequirements` 和已满足的 `satisfiedRequirements`。配置 `approvalStages` 时，当前阶段满足会写入 `stageHistory` 并推进到下一阶段；最后阶段或无阶段请求满足 quorum 后，请求从 `pending` 转为 `approved`，再按原始 actor 重新进入 Gateway tool 调度；执行成功后状态为 `executed`，执行失败后状态为 `failed`。`GET /api/v1/ai-gateway/approval-requests/:requestID/timeline` 会返回 request、trace、decision/stage 节点和同一 approval id 的 Gateway audit events；`GET /api/v1/ai-gateway/audit-logs` 也支持 `approvalRequestId` 过滤。当 replay 触发 delivery workflow 时，Gateway approval id 会进入 workflow run metadata，后续 workflow `manual_approval` approve/reject 会把相同关联写入 `workflow_approvals.metadata`。AI client 注册审批使用同一张 approval request 表，但不会 replay tool；批准只激活对应 client。拒绝、取消和超时分别进入 `rejected`、`canceled`、`timeout`。所有状态流转都写 audit 和 operation log。

审计查询入口 `GET /api/v1/ai-gateway/audit-logs` 面向 Gateway 运维视图，支持按 actor、AI client、skill、tool、approval request、risk level、result 和时间范围过滤。返回 DTO 使用专表里的脱敏审计字段，不返回 raw tool input、token、kubeconfig、环境变量或原始日志正文。

治理状态入口 `GET /api/v1/ai-gateway/governance/status?windowHours=24` 面向 Gateway 管理员，默认统计最近 24 小时审计，最大 168 小时。它会把认证路径维护的 `last_used_at` 转成 stale/never-used token 提醒，把未吊销但已过期或即将过期的 PAT/service token 标成治理 finding，并从近窗口 audit 中识别 repeated deny/failure 的 actor、AI client 和 tool。它也会扫描 pending approval request：1 小时内到期的请求写入 `approval_sla_due_soon`，pending 超过 24 小时的请求写入 `stale_gateway_approvals`，异常仍处于 pending 但已过期的请求写入 `overdue_gateway_approvals`；`approvals` DTO 会给出 pending、dueSoon、stalePending、overdue、oldestPendingHours 和 nextDueAt。它也会扫描 active allow access policy 和未过期 allow tool grant；当它们覆盖当前 Gateway capability registry 中可无审批执行的 `mutate`、`execute` 或 `high` 风险 tool，且缺少审批、人工确认或 dry-run 防护时，会降级 `high_risk_guardrails` health check 并给出修复建议；当高风险 allow 缺少具体 `resourceScopes` 约束或只配置通配 `*` 时，会降级 `high_risk_resource_scopes` health check，并把 policy/grant、subject、AI client 和最高风险级别写入 finding。budget、rate limit、input/output redaction policy 和 resource scope 按 active controls 统计配置覆盖度：disabled access policy、expired tool grant 和 disabled skill binding 会保留在总数中用于清点，但不会增加 active 数量、不会把状态标记为 `configured`；`redactionPolicyState` 会把输入脱敏条件和 `outputRedactionPolicy` / response redaction aliases 都视为已配置；没有任何 active `resourceScopes` 覆盖时会降级 `resource_scope_coverage` health check，并输出通用 recommendation。配置存在时会在 invocation 前按当前支持的调用次数预算、token/cost 用量预算、独立 fixed-window counter 限流、GCRA/token-bucket 限流、可选 Redis 高吞吐限流、strict/sanitize input redaction、字段级 redaction、正则/分类器 redaction、output redaction、resource scope 匹配和脱敏命中摘要语义强制执行，未配置则作为 recommendation 暴露。

Console 的 `/ai-gateway` access policy 抽屉已提供常用审批路由和 `conditions` 的结构化编辑入口：`approvalPolicyRef`、候选用户/角色/团队、`onCallRef`、`approvalMode=all|any`、`requiredApprovals`、change window、fixed-window、sliding-window 或 GCRA/token-bucket `rateLimit`、每日调用/token/cost `budget`、input `redactionPolicy` 和 `outputRedactionPolicy` 字段列表。更复杂的 staged approval routing、delivery approval policy metadata 扩展和 per-tool redaction `rules[]` 仍通过 API 或 cookbook JSON 配置。

请求头建议：

- `Authorization: Bearer <token>`
- `X-Soha-AI-Client-ID`
- `X-Soha-AI-Client`
- `X-Soha-Skill-ID`
- `X-Soha-Source`

返回当前身份可用的：

- `tools`
- `resources`
- `prompts`
- `skills`
- `permissionKeys`
- caller 上下文
- manifest summary

当前 manifest 基线覆盖应用交付和只读 Kubernetes 诊断方向：

- 应用列表和创建
- 应用环境绑定查询
- build/deploy/build_deploy/workflow/verify/rollback 触发入口
- release bundle 和 execution task 查询
- Pod、Deployment、Service、Ingress/Gateway route、Storage、Node、Event 和日志类只读诊断
- 发布失败诊断上下文生成

## 数据对象

AI Gateway 使用增量迁移 `0015_ai_gateway.sql`、`0016_ai_gateway_approval_requests.sql`、`0017_ai_gateway_rate_limit_counters.sql` 和 `0018_ai_gateway_rate_limit_states.sql` 建立以下控制面表，`0019_workflow_approval_metadata.sql` 则为 workflow manual approval 记录补充 Gateway 跨域关联 metadata：

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

这些表服务于 CLI/MCP/service-account/AI-client 的企业安全接入。Redis rate-limit backend 只作为高吞吐运行时 limiter，不替代审计表或审批表，也不会持久化 tool input。现有 AI Workbench 的 `ai_agent_runs`、tool binding、skill binding 和 analysis artifact 仍归 Copilot/Agent Runtime 使用。

每次 Gateway tool invocation 会同时进入通用审计和 `ai_gateway_audit_logs` 专表。专表记录 actor 类型与 ID、AI client、skill、tool、risk level、resource scope、request/result 和脱敏后的关联 metadata；不写入 token、kubeconfig、环境变量、原始日志正文或完整 tool 输入。审批请求会持久化脱敏后的 tool input 以支持审批后 replay；后续包含敏感 payload 的 tool 必须改为持久业务任务引用，而不是把 secret 放入 Gateway 审批请求。

## 工程规则

- Gateway handler 只解析请求和返回 DTO。
- `internal/application/aigateway` 负责 manifest、权限、审计和 tool invocation 编排。
- 真实动作必须进入拥有该能力的 application service。
- 构建、发布、Docker、虚拟化等长任务必须复用 durable execution/operation/task 模型。
- AI 分析必须复用 Copilot/Agent Runtime 的 `AgentRun` 和 `AnalysisArtifact`。
- token、secret、kubeconfig、环境变量不得写入日志、audit metadata 或 AI artifact。
