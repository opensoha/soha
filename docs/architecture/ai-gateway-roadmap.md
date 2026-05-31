# soha AI Gateway Roadmap

## 用途

本文档用于在新的开发会话中继续推进 soha AI Gateway。当前可运行基线已经具备后端 manifest、CLI/MCP、首批 delivery/k8s tools、token/service account/AI client、tool grants、access policies、skill bindings 和审计基础。

新会话可以直接把“继续目标”一节作为目标输入。

## 已完成基线

- 后端 API：
  - `GET /api/v1/ai-gateway/capabilities`
  - `POST /api/v1/ai-gateway/tools/:toolName/invoke`
  - `POST /api/v1/ai-gateway/resources/read`
  - `POST /api/v1/ai-gateway/prompts/get`
  - `GET /api/v1/ai-gateway/audit-logs`
  - `GET /api/v1/ai-gateway/governance/status`
  - personal access token、service account、service token、AI client、tool grant、access policy、skill binding 管理 API。
- CLI：
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
- MCP stdio server：
  - 从 Gateway manifest 动态暴露 tools/resources/prompts。
  - `tools/call` 只代理到后端 Gateway invoke API。
- 首批 tools：
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
- 首批 Skills：
  - `delivery-developer`
  - `delivery-tester`
  - `k8s-sre`
  - `security-change`
- 安全基线：
  - `ai.gateway.view`
  - `ai.gateway.invoke`
  - `ai.gateway.manage`
  - `personal_access_tokens`
  - `service_accounts`
  - `service_account_tokens`
  - `ai_clients`
  - `ai_access_policies`
  - `mcp_tool_grants`
  - `ai_gateway_skill_bindings`
  - `ai_gateway_audit_logs`
  - `mcp_tool_grants.resource_scopes` 和 `ai_access_policies.resource_scopes` 已参与 invocation 判定。
  - Gateway 专用审计查询支持 actor、AI client、skill、tool、risk level、result 和时间范围过滤。
  - `ai_access_policies.approval_policy.strategy` 已支持 `allow`、`deny`、`require_approval`、`require_human_confirm` 和 `dry_run_only`。命中审批、确认或 dry-run 策略时 Gateway 不会进入 owning application service，并会写入 Gateway audit 和 operation log。
  - `ai_gateway_approval_requests` 已支持 `pending`、`approved`、`executed`、`failed`、`rejected`、`canceled` 和 `timeout` 状态；审批通过后重新进入 Gateway 应用服务编排，再回到 owning application service 或 durable task。
  - `approval_policy.approvalPolicyRef` / `approvalPolicyId` 可衔接 delivery approval policy，复用其 SLA 作为 Gateway 审批请求过期时间，并把 delivery policy 的 `mode`、`requiredApprovals`、`approverRoles`、`changeWindow` 和 `metadata` routing 扩展合并进 `approvalRouting`。
  - `approval_policy.approverUsers` / `approverRoles` / `approverTeams`、`onCallRef`、change window、`requiredApprovals` / `minApprovals` 和 `requiredRoleApprovals` / `requiredTeamApprovals` 已写入 `approvalRouting` metadata；`onCallRef` 会通过 monitoring on-call resolver 解析当前值班人并追加到候选 `userId`，审批入口会限制候选操作者，批准会校验 change window，且多人审批总人数或分组 quorum 未满足前保持 `pending`。
- 审批闭环 API：
  - `GET /api/v1/ai-gateway/approval-requests`
  - `GET /api/v1/ai-gateway/approval-requests/:requestID/timeline`
  - `POST /api/v1/ai-gateway/approval-requests/:requestID/approve`
  - `POST /api/v1/ai-gateway/approval-requests/:requestID/reject`
  - `POST /api/v1/ai-gateway/approval-requests/:requestID/cancel`
- Console 管理面：
  - `/ai-gateway` 已作为独立 AI Gateway 工作台提供 AI clients、personal access tokens、service accounts、service account token 明细、MCP tool grants、access policies、skill bindings、manifest preview、Gateway audit 查询和审批请求处理。
  - route、菜单和权限可见性已对齐 `ai.gateway.view` / `ai.gateway.invoke` / `ai.gateway.manage`。
  - backend seed、默认角色权限、菜单可见性以及 frontend route/layout 测试已守护 `/ai-gateway` 的 `ai-gateway` 菜单和 `ai.gateway.view` 入口；旧 Gateway workbench 路径只允许作为隐藏兼容跳转。
  - token 明文仅在创建后一次性展示，不写入浏览器持久状态。
  - policy、grant、binding 使用结构化控件编辑 resource scopes 和审批策略；access policy 抽屉已支持常用审批路由字段，包括 `approvalPolicyRef`、候选用户/角色/团队、`onCallRef`、`approvalMode=all|any`、`requiredApprovals` 和 change window；常用治理条件也已覆盖 fixed-window、sliding-window、GCRA rateLimit、每日调用/token/cost budget、input redactionPolicy，以及 outputRedactionPolicy 的字段、Secret classifier、正则值、replacement 和 preserveFormat 控件；Secret classifier 多选项已和 backend 内置分类器同步到 OpenRouter、Fireworks AI、Voyage AI、Brave Search、SerpAPI、Browserbase、Exa、Jina AI、Unstructured、LlamaCloud、Helicone、DashScope、Moonshot、智谱 AI、SiliconFlow、腾讯混元、百度千帆、火山方舟、Grafana、Sentry、New Relic、Azure OpenAI、Azure DevOps PAT、Datadog、PagerDuty、PostHog、Splunk、Elastic 和 Terraform Cloud。
  - Governance 标签页已接入 `GET /api/v1/ai-gateway/governance/status`，支持按 1h/6h/24h/48h/7d 窗口查看 health、recent calls、token/client/approval 摘要、policy coverage、redaction hit summary、token findings、approval queue、top tools / AI clients / actors、anomaly findings、recommendations 和结构化 `recommendationActions`；redaction hit summary 来自 Gateway audit metadata 的 `redaction` 摘要，展示 total matches、命中 audit 数、input/output target、match type、classifier、field path、policy 和 tool TopN，只保留计数和定位字段，不暴露原始敏感值；token finding 可定位到 PAT 过滤视图、service account token 明细过滤视图或预填 service token 吊销抽屉，redaction policy/tool 可定位到 policy 或 audit 过滤视图，approval 队列、anomaly finding 和 recommendation action 可直接定位到对应 approval、AI client、policy、grant 或 audit 过滤视图，coverage 缺口和高风险 finding/action 可打开预填 access policy 草案以补 budget、rate limit、redaction、resource scope 或审批 guardrail。
  - Gateway approval request 与 delivery workflow run 已能通过 Console drilldown 双向定位：审批列表展示 `workflowRunId` 并跳转 `/workflows?workflowRunId=...&gatewayApprovalRequestId=...`，工作流列表展示 Gateway approval metadata 和 manual approval node 摘要，并可回跳 `/ai-gateway?approvalRequestId=...`；审批请求和 Gateway audit 工具栏都已支持 created-time range 过滤，便于按事故窗口排查。
- 验证基线：
  - `go test ./...`
  - `go run ./cmd/soha-cli help`
  - `go run ./cmd/soha-cli skill list`
  - `cd web && npm run typecheck`
  - `cd web && npm run test -- src/routes/meta.test.ts src/layouts/app-layout.test.tsx`
  - `cd web && npm test`
  - `cd web && npm run build`
- 人工 Console 验证：
  - 2026-05-30 Chrome 登录态浏览器核对已完成：本地 backend `127.0.0.1:8080` health 正常，Vite console 运行在 `127.0.0.1:5174`，使用 `admin / soha` 登录后返回 `/ai-gateway`，独立 `AI Gateway` 工作台菜单项可见并处于选中状态，页面展示 Gateway 管理面、Manifest / AI Clients / Tokens / Service Accounts / Tool Grants / Access Policies / Skill Bindings / Governance / Approvals / Audit 标签以及可见 tool 清单。
  - 本次只记录登录态浏览器核对结果，未提交正式截图制品；如后续需要对外发布或视觉验收材料，可基于同一登录路径补充截图。

## 继续目标

继续在 `/Users/shanchui/Downloads/soha` 中实现 soha AI Gateway 下一阶段，使它从“可运行 Gateway 基线”进化为“可运营、可审批、可观测、可扩展的企业 AI 运维控制面”。

必须遵守：

- `AGENTS.md`
- `.agents/skills/soha-backend/SKILL.md`
- 如涉及前端，再使用 `.agents/skills/soha-frontend/SKILL.md`

继续工作时优先读取：

- `docs/architecture/ai-gateway.md`
- `docs/operations/soha-cli.md`
- `internal/application/aigateway/service.go`
- `internal/application/aigateway/catalog.go`
- `internal/api/handlers/aigateway.go`
- `internal/repository/aigateway/repository.go`
- `cmd/soha-cli`
- `internal/cli/sohacli`
- `skills/ai-gateway`

## P0 状态

### 1. 审批和风险策略闭环

当前已完成：

- Gateway 可基于 policy strategy 在执行前拦截高风险 tool，并返回 `pending_approval`、`pending_human_confirm` 或 `dry_run`。
- `require_approval` 和 `require_human_confirm` 会建立可查询、可审批、可取消、可超时的持久审批请求。
- 审批通过后仍回到 Gateway application service，再进入 owning application service 或 durable task；handler/repository 不执行业务动作。
- 审批、拒绝、超时、取消、审批后执行成功和失败都会写入 audit 和 operation log。
- Gateway approval request 可引用 delivery approval policy，并复用其 SLA、审批模式、审批人数、审批角色、变更窗口和 metadata routing 扩展。
- Gateway approval request 已能保存 `approvalRouting` metadata：候选审批人、候选角色、候选团队、`onCallRef`、当前值班候选人、change window、`approvalMode`、`approvalStages`、`requiredApprovals` / `minApprovals` 总人数 quorum，以及 `requiredRoleApprovals` / `requiredTeamApprovals` 分组配额；审批创建时会解析 monitoring on-call 当前值班人并写入 `onCallCandidateUserIds` / `onCallResolution`，审批、拒绝、取消会限制为候选操作者，批准还会校验当前时间是否在 change window 内。
- 多人审批已支持唯一审批用户计数、角色/团队分组配额、会签/或签模式和分阶段审批：每次批准写入 `decisions`、`approvedCount`、`roleApprovedCounts`、`teamApprovedCounts`、`pendingRequirements` 和 `satisfiedRequirements`，同一审批人重复批准只替换自己的 decision；默认 `approvalMode=all` 要求总人数和所有分组配额都满足，`approvalMode=any` 满足任一显式 quota 即可进入下一阶段或 replay tool；配置 `approvalStages` 时会按 `currentStageIndex` 限制当前阶段候选人，阶段满足后写入 `stageHistory` 并推进下一阶段，最后阶段满足后才 replay tool。
- Gateway approval replay 触发 `delivery.actions.trigger` 时会把 `aiGatewayApprovalRequestId`、policy、tool、skill 和 AI client 关联写入 delivery workflow variables；workflow run metadata 会保留这些字段，workflow `manual_approval` 节点的 `workflow_approvals.metadata` 会持久化同一组跨域关联，delivery/Gateway related IDs 也会回填 `workflowRunId`。
- Approval request 列表响应已包含由 `approvalRouting` 派生的 `approvalTrace`；`GET /api/v1/ai-gateway/approval-requests/:requestID/timeline` 会聚合 request、decision、stage history、workflow/task 关联和同一 request 的 Gateway audit events；`GET /api/v1/ai-gateway/audit-logs` 支持 `approvalRequestId` 过滤。

后续增强：

- P0 审批闭环当前无剩余必做项；Console 已提供 Gateway approval request、workflow run 和 manual approval 节点的双向 drilldown，带 `workflowRunId` 的 `/workflows` 定位会自动展开行内详情，展示 Gateway 关联、manual approval 节点详情、workflow node timeline 和原始 trace。Backend 已先提供 Gateway approval timeline 聚合视图；后续若 delivery/backend 暴露更细的 workflow 节点审计事件，可继续把执行日志按 workflow/manual-approval 节点归并展示。

## P1 状态

### 5. tools 扩展到更完整的交付闭环

当前已完成：

- `delivery.applications.detail` 通过 delivery application service 读取应用、环境绑定、release target、latest bundle 和 execution task 摘要。
- `delivery.application_services.list` 通过 application service 读取 service/container 配置摘要。
- `delivery.build_sources.list` 读取应用 build sources，并可返回绑定使用关系；build policy 变量和 build args 会走 Gateway 脱敏。
- `delivery.release_targets.list` 读取 application environment binding 的 release targets。
- `delivery.execution_logs.list` 作为独立 tool 读取 execution task logs，并在 Gateway 层脱敏。
- `delivery.approval_policies.list` 读取 delivery approval policies。
- `delivery.workflow_templates.list` 通过 catalog service 读取 workflow templates。
- `delivery.release_context.diff` 生成候选发布/promotion 只读上下文，比较 source/target release bundle。
- `delivery.rollback.context` 生成 rollback 建议上下文，聚合 bundle、task、redacted logs 和 target binding 信息。
- `delivery.actions.trigger` 已支持 `action=rollback`，由 delivery application service 调用 workflow rollback 模式，只执行绑定 workflow template 中的 `rollback_to_previous` DAG 节点，并把 `releaseBundleId` 作为 workflow variables/metadata 传递。

注意：

- `delivery.rollback.context` 仍是只读规划工具，不执行真实 rollback。
- 真实 build/deploy/rollback 必须走 delivery application service 和 execution/workflow plane。
- rollback 实际变更必须通过 `delivery.actions.trigger action=rollback`，并受 Gateway risk policy、approval replay 和 owning service 权限控制。
- 交付类新增 tools 仍通过 `internal/application/aigateway` 调用 owning application service，不由 handler 或 repository 直接拼业务数据。

### 6. k8s 诊断增强

当前已完成：

- `k8s.pods.describe` 通过 resource application service 返回 pod describe-style 聚合视图，包含 containers、conditions、volumes、related resources 和重启摘要。
- `k8s.deployments.rollout_status` 读取 deployment rollout status、replica progress、revision 和 conditions。
- `k8s.deployments.events` 使用有界 event 查询并按 deployment 过滤。
- `k8s.services.backends` 聚合 service selector、匹配 pod、ready backend 摘要和相关 ingress route hints。
- `k8s.routes.context` 聚合 ingress 与 Gateway API typed route 视图；Gateway API 可选资源不可用时返回 `capabilityWarnings`，不伪装成能力完整。
- `k8s.storage.context` 聚合 PVC、PV 和 storage class 只读诊断，并标出 unbound PVC。
- `k8s.nodes.detail` 读取 node conditions、resource summary、taints 和 scheduled pods。

注意：

- 不返回 raw Kubernetes object。
- 所有新增能力都通过 `internal/application/resource` 的平台 view model 进入 Gateway，不在 Gateway 中直接调用 Kubernetes client。
- direct/agent 能力差异继续由 resource service 暴露为 unsupported、degraded 或 capability warning。
- 大集群读取使用已有聚合 API 或有界查询，不在 Gateway 中做无界 namespace fan-out。

### 7. AI 分析产物落库

当前已完成：

- `diagnosis.release_failure.analyze` 会将 Gateway 采集到的交付和运行态上下文转换成 Copilot/Agent Runtime 的 `AnalysisArtifact`。
- artifact 通过 Copilot application service 写入 `ai_agent_runs.analysis_artifacts`，Gateway 不直接写 Copilot repository。
- artifact scope 支持关联 application、application environment、release bundle、execution task、cluster、namespace、workload 和 pod。
- artifact 保存证据摘要、假设、建议、下一步检查、工具执行快照和数据源快照。
- artifact 只保存 redacted summary、计数和关联 ID；不持久化 raw secret、raw log 正文、kubeconfig 或环境变量。
- Agent Runtime callback 分批回写 artifact 时会按 `runId + kind + title` 稳定键增量合并，保留既有 artifact 顺序并替换同键产物，避免后续 provider callback 覆盖先前分析产物。
- Agent Runtime runner 在 provider 命令失败或超时时也会主动随 failed/callback_timeout callback 回传合成 `analysisArtifacts`，保留 run、provider、capability、scope、tool execution、错误状态和排查建议，并在 runner 侧先对失败 error、logs、tool output 和 artifact snapshot 做基础敏感文本脱敏，避免控制面只能依赖服务端兜底合成失败产物。
- 外部 Agent Runtime provider callback payload 中的 `usage`、`tokenUsage`、`aiUsage`、`providerUsage`、`llmUsage`、`metering`、`billing`、`costUsage` 会被归一化为 `providerUsage` / `usage` 数值摘要，并写入 agent run output 与 artifact data source snapshot；同时显式识别 Gemini/Vertex `usageMetadata.promptTokenCount/candidatesTokenCount/totalTokenCount`、Ollama `prompt_eval_count/eval_count`、Anthropic cache input token 字段、Cohere `billed_units` 优先语义、search/classification/embedding/rerank/request/response usage units、Brave Search / SerpAPI 查询与搜索 credit、Browserbase session/minute/page-load、Exa/Jina/Unstructured/LlamaCloud 文档页/解析页/字符/chunk，以及 Helicone request 计量别名、通用 `promptCost` / `inputCost` / `completionCost` / `outputCost` 和常见 cost 字段。原始 provider payload、模型字符串和其它非数值字段不进入用量摘要。
- AI Workbench artifact 历史已把 `sessionId`、`rootCauseRunId`、`inspectionRunId`、`agentRunId` / `agentRuntimeId` 等 data source snapshot 关联展示成可点击入口；根因/Agent 关联会留在调查工作台，巡检复盘关联会跳到 `/ai-workbench/inspection?view=runs&inspectionRunId=...` 并自动定位运行记录。Copilot application service 也会为新生成的 root-cause、performance、trace、inspection-review 和 queued Agent Runtime artifact 写入这些稳定关联字段。
- `diagnosis.release_failure.analyze` 已支持通过 `agentProviderId` / `providerId` 或 `deepAnalysis` / `externalAnalysis` 把深度推理排入外部 Agent Runtime provider；Gateway 仍只收集和脱敏上下文，随后调用 Copilot application service 创建 `queued` AgentRun，后续由 runner claim/callback 回写 artifact，不在 Gateway 中同步调用外部 provider。

后续增强：

- 如果后续新增独立 root-cause run 详情页或 Agent Run 运维页，可继续把这些 artifact 关联入口从当前工作台内锚点升级为专用详情页跳转。

### 8. CLI 体验增强

当前已完成：

- `capabilities --json` 作为显式 JSON manifest 输出入口，`--output names` 保留人读列表，`--output inputs` 按 tool 汇总 manifest `inputSchema` 与 `outputSchema` 的 required 字段和 properties 字段。
- `tool call <name> --input file.json` 和 `--input -` / `--input-json` 作为人工兜底 Gateway tool 调用命令。
- `token list|create|revoke` 支持个人 access token 管理。
- `service-account list|create|token-list|token-create|token-revoke` 支持 service account 与 service token 管理；`token-list` 只输出 token metadata、prefix、权限、scope、过期、最近使用和吊销状态，不暴露明文 token 或 hash。
- `audit list` 支持查询 Gateway audit，并可按 actor、AI client、skill、tool、approval request、risk level、result、action 和时间范围过滤。
- `approval list|timeline|approve|reject|cancel` 支持从终端查询、追踪和处理 Gateway approval request；命令只代理后端审批 API，候选审批人、change window、多阶段 quorum、AI client 激活和 tool replay 仍由 Gateway application service 执行，输出默认脱敏 tool input、decision comment、执行输出和关联 metadata。
- `diagnose --tool <name>` 会输出可见 tool 的 domain、action、MCP adapter/tool 映射、permission keys、scope、risk、approval 状态，以及 manifest `inputSchema` / `outputSchema` 汇总出的 `inputRequired` / `inputFields` / `outputRequired` / `outputFields`，并提示检查 MCP tool grants、AI access policies、skill bindings、AI client context 和 resource scopes；`--resource` / `--prompt` 会输出可见 resource/prompt 的 permission keys，并提示检查 Gateway resources/prompts API、skill binding、AI client context 和 arguments/context。
- `diagnose` 支持 `--ai-client-id`、`--ai-client`、`--skill-id` 和 `--source` 临时覆盖本次 capability 请求的 AI client / skill / source headers，便于模拟 `mcp start` 或 `mcp install` 生成的客户端上下文；这些参数不会写回 profile。
- `completion bash|zsh` 输出 shell completion 脚本。
- `governance status` 普通输出会打印 health checks、治理 finding 的 actor、subject、AI client、policy、grant、tool 和 risk level 定位字段，并展示 redaction hit summary；`--json` 保留完整脱敏 DTO，包括 riskCounts、pendingApprovalClientIds、redaction summary 和 token due/stale day 字段。

约束：

- CLI 仍然不能绕过后端权限和应用服务。
- CLI 输出必须默认脱敏。

## P2 状态

### 9. MCP resources/prompts 完整实现

当前已完成：

- resources/read 支持读取 Gateway manifest 中的 `soha://...` 资源。
- prompts/get 支持 soha prompt 模板。
- prompts 能结合 skill 和当前 arguments/context。
- 所有 resources/prompts 读取都会进入后端 `ai.gateway.invoke`、resource/prompt 业务权限、skill binding 和 Gateway audit 边界。
- skill binding 不再只收窄 tools：`capabilities` 会按绑定 capability refs 过滤 resources/prompts，可见 resource/prompt 由其关联 tools 决定；`resources/read` 和 `prompts/get` 也会在读取前重新校验当前 skill binding，resource 文档和 prompt skill context 只展示已绑定 capability refs。
- resources/prompts 已补充可发现参数契约：resource capability 透出 `contextSchema`，prompt capability 透出 `argumentSchema` 和 `contextSchema`；MCP `prompts/list` 还会把 prompt arguments 转成标准 `arguments[]` 摘要，CLI `diagnose --resource/--prompt` 会显示 context/argument required 与 fields。
- Gateway 能力目录已先收敛到 application-layer capability registry/provider：默认 delivery、k8s 和 diagnosis 清单作为 static provider 注册，`capabilities`、tool/resource/prompt/skill lookup、resource 到 tool/prompt/skill 的关联关系、可选 provider tool invoker、tool grant 和 skill binding 构建、access policy 的 skill 匹配、governance 高风险扫描，以及 resource 文档中的 related capability metadata 都使用同一 registry。后续新增模块能力时应注册 provider；自定义 tool 的执行也必须通过 provider invoker 回到 application service，并继续复用 Gateway 鉴权、risk policy、redaction 和 audit 主流程，而不是继续扩散运行路径里的 `default*` 静态查找。
- soha MCP stdio server 的 `resources/read` 和 `prompts/get` 只代理到后端 Gateway API，不在本地绕过权限拼接内容。
- `soha-cli resource read` 和 `soha-cli prompt get` 已作为人工排查入口，只代理到后端 Gateway resources/prompts API，并复用 profile、AI client、skill 和 source headers。
- `tools/list` 已对当前默认 Gateway tools 透出结构化 `inputSchema`，并会在 manifest 定义了输出契约时同步透出 `outputSchema`；默认工具覆盖 delivery 应用/环境/build source/release target/bundle/task/log/action/context tools、k8s pod/deployment/service/route/storage/node/event tools 以及 `diagnosis.release_failure.analyze`，帮助 MCP client 识别 `applicationId`、`action`、`clusterId`、`namespace`、`podName`、`deploymentName`、`serviceName`、`nodeName`、`taskId` 等入参和输出字段；CLI `capabilities --output inputs` 与 `diagnose --tool` 也可批量查看每个 tool 的 input/output required/properties 汇总。
- `tools/list` 会输出标准 MCP `annotations`：`read` tool 标记 `readOnlyHint=true` / `idempotentHint=true`，`mutate` / `execute` / `high` 标记 `destructiveHint=true`，所有 soha tool 默认 `openWorldHint=true`；`tools/list`、`resources/list` 和 `prompts/list` 也会把 manifest 中的 `permissionKeys` 与 `requiredScopes` 放入 MCP `_meta.soha`，tool 元数据还包含 `domain`、`action`、`mcpAdapterId`、`mcpToolName`、`riskLevel` 和 `requiresApproval`。这些字段只帮助 MCP client 在调用/读取前展示权限、scope、adapter 映射和风险预期；真实调用/读取仍必须通过后端 Gateway API 重新鉴权、脱敏和审计。
- MCP `initialize` 会返回稳定 `instructions`，明确本地 MCP 进程只是 AI Gateway 代理，所有调用、resource read 和 prompt get 都必须回到后端权限、skill binding、AI client context、脱敏、风险策略、审批和审计边界；空 `tools/call.name`、`resources/read.uri/name` 或 `prompts/get.name` 会在本地返回 JSON-RPC `-32602` 参数错误，不会对后端发起无效请求。
- `mcp install` 生成的 MCP client 配置可写入 `--ai-client-id`、`--ai-client` 和 `--skill-id` 到 `mcp start` 参数，便于客户端固定审计来源、AI client context 和 skill binding 上下文。

### 10. 企业化治理

当前已完成：

- token `last_used_at` 已由 PAT/service account token 认证路径更新，并通过治理状态接口聚合为 stale、never-used、expired active 和 expiring-soon 提醒。
- `GET /api/v1/ai-gateway/governance/status` 已提供 Gateway health/metrics 汇总，覆盖近窗口调用量、success/deny/failure/pending/dry-run 计数、top tools、top AI clients、top actors、pending approvals、approval SLA 和 health checks；默认窗口为 24h，显式 `windowHours` 只接受 1..168，避免直接 API 请求触发异常大窗口扫描。
- 治理状态接口会从 `ai_gateway_audit_logs` 中识别 repeated deny/failure 的 actor、AI client 和 tool，作为异常调用 finding 暴露。
- 治理状态接口会对 pending approval request 生成 SLA 摘要，暴露 pending、dueSoon、stalePending、overdue、oldestPendingHours、nextDueAt 和相关 request ID；1 小时内到期、pending 超过 24 小时或异常 overdue 的请求会分别产生 `approval_sla_due_soon`、`stale_gateway_approvals` 或 `overdue_gateway_approvals` finding，并降级 `approval_sla` health check。
- 治理状态接口会扫描 active allow access policy 和未过期 allow tool grant；当它们覆盖当前 Gateway capability registry 中可无审批执行的 `mutate` / `execute` / `high` 风险 tool，且缺少 `require_approval`、`require_human_confirm` 或 `dry_run_only` 防护时，会暴露 warning finding、`high_risk_guardrails` health check 和修复 recommendation；当高风险 allow 缺少具体 `resourceScopes` 约束或只配置通配 `*` 时，也会暴露 `high_risk_allow_without_resource_scope` / `high_risk_grant_without_resource_scope` finding、`high_risk_resource_scopes` health check 和修复 recommendation。
- AI client 注册审批已落成持久 approval request：创建或更新 `pending` / `pending_approval` client 会生成 `ai_gateway_approval_requests` 记录，批准后 client 转为 `active`，拒绝后转为 `rejected`，取消后转为 `disabled`；该流程复用 Gateway 审批 audit/operation log，但不会 replay tool。
- budget、rate limit、input/output redaction policy 和 resource scope 当前通过 active controls 统计配置覆盖度，并作为 health/recommendation 暴露；coverage DTO 同时保留 total 与 active policy/grant/binding 数量，`redactionPolicyState` 会把启用中的 `redactionPolicy`、`outputRedactionPolicy` 和 response/output redaction aliases 都计为已配置，disabled access policy、expired tool grant 和 disabled skill binding 不会把 coverage state 伪装成 `configured`；没有任何 active `resourceScopes` 时会降级 `resource_scope_coverage` health check，并提示在扩大跨环境 Gateway 访问前补齐具体 scope。
- 命中 allow access policy 的 invocation 会在进入 owning application service 前强制执行 `rateLimit`、调用次数型 `budget`、token/cost 用量型 `budget` 和 `redactionPolicy`。rate limit 默认写入 `ai_gateway_rate_limit_counters` 独立 fixed-window counter，并通过 PostgreSQL upsert 原子递增拒绝超额请求；当 conditions 设置 `mode=sliding_window|rolling_window|audit_window` 时，会直接按 Gateway audit 做真实滚动窗口；当 conditions 设置 `mode=gcra|token_bucket|leaky_bucket|strict` 时，会改用 GCRA/TAT 语义，支持 `burst` / `burstSize` / `capacity` 突发容量，拒绝请求只返回 retry-after 不推进未来可用额度。默认 backend 使用 PostgreSQL `ai_gateway_rate_limit_counters` / `ai_gateway_rate_limit_states`，也可以通过 `ai_gateway.rate_limit.backend=redis` 接入 Redis 高吞吐共享限流；Redis 后端使用原子 INCR/EXPIRE 和 Lua GCRA 脚本，故障时默认回退 PostgreSQL，再在旧库缺对应限流表时 fallback 到 `ai_gateway_audit_logs` 近窗口统计。调用次数预算仍基于 `ai_gateway_audit_logs` 统计 success/failure/dry_run 调用，支持 actor/client/tool 维度 scope；token/cost 预算读取 audit metadata 中的 `usage`、`tokenUsage`、`providerUsage` 等脱敏用量摘要。Gateway 已能从普通 invocation 和 approval replay 的 owning service 输出或 related IDs 中抽取 `usage`、`tokenUsage`、`aiUsage`、`providerUsage`、`llmUsage`、`metering`、`billing`、`costUsage` 容器里的数值型 token/cost 摘要，并显式识别 Gemini/Vertex `usageMetadata.promptTokenCount/candidatesTokenCount/totalTokenCount`、cached content、tool-use prompt 和 thoughts token 字段，Ollama `prompt_eval_count/eval_count`、Anthropic cache input token 字段、OpenAI cached/audio/reasoning/prediction token details、Bedrock text/image/audio token fields、Cohere billed units（当同一 usage 容器同时有 `billed_units` 和 `tokens` 时优先使用 billed units，避免双计）、search/classification/embedding/rerank/request/response usage units、Brave Search / SerpAPI 查询与搜索 credit、Browserbase session/minute/page-load、Exa/Jina/Unstructured/LlamaCloud 文档页/解析页/字符/chunk，以及 Helicone request 计量别名、billable/billed/usage token aliases、多模态 text/image/audio/video 输入输出别名、cache read/write/hit/miss input token 变体、DashScope/Moonshot/智谱/千帆常见 `*_tokens_count` / `*_token_usage` 变体、通用 `promptCost` / `inputCost` / `completionCost` / `outputCost` 和常见 cost / costMicros / costCents 字段，再以 `providerUsage` / `usage` 写回 Gateway audit metadata；不会保存 provider 原始 payload、模型字符串或其它非数值详情。redaction strict 模式会拒绝携带 sensitive key 或 secret-like 文本的输入并写入 deny audit，sanitize/mask/redact 模式会在调用前把敏感输入改写为 `[REDACTED]`。
- redaction policy 已支持字段级 `fields` / `redactFields`、`allowFields` 例外、自定义 `replacement`、`preserveFormat` 尾部保留 mask、`valuePatterns` 正则值规则、`secretTypes` 结构化 secret 分类器，以及 `rules[]` + `toolPatterns` 的按 tool 定制替换规则。
- 内置 `secretTypes` 已覆盖 GitHub、GitLab、OpenAI、Anthropic、Google API key、Hugging Face、Cohere、Mistral、DeepSeek、Groq、Together AI、Replicate、LangSmith、Pinecone、xAI、Perplexity、Tavily、Langfuse、Qdrant、Weights & Biases、Linear、OpenRouter、Fireworks AI、Voyage AI、Brave Search、SerpAPI、Browserbase、Exa、Jina AI、Unstructured、LlamaCloud、Helicone、DashScope、Moonshot、智谱 AI、SiliconFlow、腾讯混元、百度千帆、火山方舟、Grafana、Sentry、New Relic、Azure OpenAI、Azure DevOps PAT、Datadog、PagerDuty、PostHog、Splunk、Elastic、Terraform Cloud、npm、Stripe、Slack、JWT、AWS access key、PEM private key、Kubernetes Secret、kubeconfig、Docker config、GCP service account JSON 和 AWS credentials 结构。
- 字段、正则值、secret classifier 和结构化 Secret 脱敏命中会写入 Gateway audit metadata 的 `redaction` 摘要；摘要只记录 target、字段路径、命中类型、分类器名称和计数，不保存原始敏感值。
- 治理状态接口会把近窗口 audit metadata 的 `redaction` 摘要聚合为 redaction summary，供 API、CLI 和 Console 查看 total matches、命中 audit 数、input/output target、match type、classifier、field path、policy 和 tool TopN。
- `outputRedactionPolicy` 和 `redactionPolicy.rules[].target=output|both` 已支持 per-tool 输出脱敏；普通 tool invocation 和 approval replay 都会在 owning service 返回后、响应或 approval request 输出持久化前应用输出规则。
- CLI 已提供 `soha-cli governance status` 和 `--json` 输出，默认脱敏；普通输出会展示 approvals SLA 摘要、finding 定位字段、recommendation action、resource-scope 覆盖状态、redaction hit summary 以及 total/active policy、grant、binding 数量，JSON 输出保留 backend governance DTO 的 approvals、riskCounts、pendingApprovalClientIds、redaction summary、activeAccessPolicies、activeToolGrants、resourceScopeState、resourceScopedAccessPolicies、resourceScopedToolGrants、`recommendationActions` 和 token due/stale day 等运营字段。

- token/cost 预算已具备 Gateway 侧拦截能力、Gateway invocation 通用数值用量摘要回写，以及 Agent Runtime callback 用量摘要标准化；当前已显式映射 Gemini/Vertex cached content、tool-use prompt 和 thoughts token，Ollama、Anthropic cache token、OpenAI cached/audio/reasoning/prediction token details、Bedrock text/image/audio token、Cohere billed units 优先去重、search/classification/embedding/rerank/request/response usage units、Brave Search / SerpAPI 查询与搜索 credit、Browserbase session/minute/page-load、Exa/Jina/Unstructured/LlamaCloud 文档页/解析页/字符/chunk、Helicone request 计量别名、billable/billed/usage token aliases、多模态 text/image/audio/video 输入输出别名、cache read/write/hit/miss input token 变体、DashScope/Moonshot/智谱/千帆常见 `*_tokens_count` / `*_token_usage` 变体、通用 `promptCost` / `inputCost` / `completionCost` / `outputCost` 和 costMicros / costCents 字段，后续按新增 provider adapter 继续补字段别名。
- rate limit 已从单纯审计窗口计数升级为 PostgreSQL-backed fixed-window counter、audit-backed sliding-window、PostgreSQL-backed GCRA/token-bucket 状态表，并新增可选 Redis 高吞吐共享限流后端；Redis 后端支持 fixed-window counter 与 Lua GCRA/token-bucket，未配置或不可用时保持 PostgreSQL/audit fallback。
- redaction policy 已扩展多轮内置 secret provider 分类器，当前新增 xAI、Perplexity、Tavily、Langfuse、Qdrant、Weights & Biases、Linear、OpenRouter、Fireworks AI、Voyage AI、Brave Search、SerpAPI、Browserbase、Exa、Jina AI、Unstructured、LlamaCloud、Helicone、DashScope、Moonshot、智谱 AI、SiliconFlow、腾讯混元、百度千帆、火山方舟，以及观测/DevOps 集成侧的 Grafana、Sentry、New Relic、Azure OpenAI、Azure DevOps PAT、Datadog、PagerDuty、PostHog、Splunk、Elastic 和 Terraform Cloud；后续可按新 provider token 格式继续增补。

### 11. 文档和示例

当前已完成：

- `docs/operations/ai-gateway-examples.md` 已提供 Cursor/Codex/Claude Code MCP 配置示例。
- 同一示例文档已覆盖 CI service account、临时 token、独立 CI profile 和 MCP 启动方式。
- delivery-developer 端到端示例已覆盖应用详情、service/container、build source、release target、release diff 和受控 build/deploy action。
- k8s-sre 发布失败诊断示例已覆盖 rollout status、deployment events、pod describe、service backends 和 `diagnosis.release_failure.analyze` artifact 落库路径。
- access policy cookbook 已覆盖 read-only、mutating approval、approval routing/change window、high-risk deny、cluster/namespace scope、fixed-window、sliding-window、GCRA/token-bucket 与 Redis rateLimit、调用次数型 budget、token/cost budget、strict redaction、sanitize/mask redaction policy、字段级 allow-list、格式保留 mask、正则值规则、结构化 secret 分类器、按 tool 定制的 redaction rule 和 output redaction policy。
- skill binding cookbook 已覆盖 AI client 绑定 delivery-developer 与 role 绑定 k8s-sre。
- `docs/en/operations/ai-gateway-examples.md` 已同步英文版核心示例，`docs/operations/soha-cli.md` 和英文 CLI 文档已链接到示例文档。
- Console 字段映射已补入 `docs/operations/ai-gateway-examples.md` 和英文示例文档，覆盖 `/ai-gateway` 的 manifest、AI clients、PAT、service accounts、MCP tool grants、access policies、常用治理 conditions、skill bindings、governance status、approval requests、audit 和 Redis rate-limit runtime config 边界。
- resource/prompt 人工排查示例已补入中英文示例文档：`soha-cli resource read`、`soha-cli prompt get`、`diagnose --resource` 和 `diagnose --prompt` 都明确只通过 Gateway `resources/read` / `prompts/get` 边界，并提示按 runtime permission、skill binding、AI client context 和 resource scope 排查。
- `docs/ai_gateway_examples_test.go` 和 AI Gateway application tests 已守护操作文档、架构文档、roadmap 与默认 capability registry 的当前 CLI/tool 清单一致性，防止回退到旧命令面或漏写新增默认 tool。

后续补充：

- 如后续需要对外发布或视觉验收材料，可基于已验证的登录态 Console 路径补正式截图。
- 等后续新增 provider adapter/token 格式或更多限流策略落地后，继续扩展 policy cookbook。

## 验收标准

每个阶段完成前至少验证：

```bash
go test ./...
go run ./cmd/soha-cli help
go run ./cmd/soha-cli skill list
```

如涉及前端：

```bash
cd web
npm test
npm run build
```

还需要人工核对：

- handler 是否保持 thin。
- 业务动作是否只进入 owning application service。
- 高风险动作是否进入审批或 durable task。
- Gateway、MCP、CLI 是否没有直接查库、直接操作 Kubernetes、直接执行 runner 逻辑。
- audit/operation log 是否没有 token、kubeconfig、环境变量、原始日志正文。
- docs、权限种子、菜单、测试是否同步更新。

## 推荐下一步

建议新会话继续收敛 AI Gateway 的剩余企业化增强，原因：

- P0 和 P1 后端、Console、CLI、delivery/k8s tools、artifact 落库已经具备主要入口。
- MCP tools/resources/prompts 已都能通过后端 Gateway API 进入权限和审计边界。
- 治理状态面已经能在 API、CLI 和 Console 暴露 token last-used/过期、pending approval SLA、异常调用、高风险 allow 缺少审批/确认/dry-run 防护、高风险 allow 缺少具体 resource scope 约束、health/metrics、top callers 和 policy 覆盖度，且 access policy conditions 已能在 invocation 前强制拦截调用次数预算、token/cost 用量预算、PostgreSQL-backed fixed-window、audit-backed sliding-window、GCRA/token-bucket 限流、可选 Redis 高吞吐限流、strict/sanitize redaction、字段级 redaction、正则/分类器 redaction、output redaction、resource scope 匹配和脱敏命中摘要。
- Gateway 审批与 delivery workflow manual approval 已具备跨域关联，Console 也已能把 Gateway approval request、workflow run 和 manual approval 节点串成可点击排障链路。
- 文档示例已经覆盖 MCP client、CI service account、Console 字段映射、delivery/k8s 端到端流程、resource/prompt 人工排查和 policy/binding cookbook；下一步可在需要发布材料时补 Console 截图，或在新增 capability provider、provider adapter/token 格式进入代码库时继续补 usage alias 与 secret classifier。
