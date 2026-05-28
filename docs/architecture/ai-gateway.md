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
- `profile`
- `context`
- `capabilities`
- `mcp start`
- `mcp install`
- `skill install`
- `diagnose`

当前代码入口位于 `cmd/soha-cli`，核心实现位于 `internal/cli/sohacli`。第一版已落地 `login`、`profile`、`context`、`capabilities`、`mcp start`、`mcp install`、`skill list`、`skill install` 和 `diagnose`。

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
   - 继续复用应用、环境、业务线、集群、namespace 等 scope grants。
3. MCP tool grants
   - `mcp_tool_grants` 控制主体和 AI client 能调用哪些 tool。
   - tool grant 只能收窄能力，不能绕过已有 `permissionKeys`。
   - 未配置 grant 时按 `permissionKeys` 暴露能力；一旦配置 allow grant，就形成 allow-list；deny grant 永远优先。
4. risk policy
   - `ai_access_policies` 控制主体、角色和 AI client 在 Gateway 内的风险边界。
   - deny policy 永远优先；存在 allow policy 时形成 allow-list。
   - policy 可按 tool pattern、skill、risk level 和 resource scope 收窄能力，并可把命中的 tool 标记为需要审批。
   - `read`、`analyze`、`mutate`、`execute`、`high` 分级。
   - 高风险动作需要审批、二次确认、脱敏或直接禁止。
5. skill bindings
   - `ai_gateway_skill_bindings` 控制主体、角色和 AI client 可使用哪些 soha Skills 以及每个 skill 可引用的 capability refs。
   - skill binding 只能收窄 manifest 和 tool invocation，不能赋予新权限。

## 首批 API

```http
GET /api/v1/ai-gateway/capabilities
POST /api/v1/ai-gateway/tools/:toolName/invoke
```

`capabilities` 返回当前身份可见的能力清单。`tools/:toolName/invoke` 是 MCP、CLI 和外部 AI Agent 的统一工具调用入口。调用时必须重新校验 `ai.gateway.invoke`、tool 自身的业务权限、scope、AI grant 和风险策略，然后转入拥有该能力的 application service。

首版可直接调用的工具覆盖应用交付和只读 Kubernetes 诊断：

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

Kubernetes 工具通过 `internal/application/resource` 读取平台聚合视图，继续遵守 direct/agent 集群能力边界、cluster/namespace scope 和资源权限。发布失败诊断工具会聚合 delivery execution、release bundle、Pod、Deployment、Service、Event 和日志上下文；日志类输出在 Gateway 层做基础敏感字段脱敏。

凭证入口：

```http
GET  /api/v1/ai-gateway/personal-access-tokens
POST /api/v1/ai-gateway/personal-access-tokens
POST /api/v1/ai-gateway/personal-access-tokens/:tokenID/revoke
GET  /api/v1/ai-gateway/service-accounts
POST /api/v1/ai-gateway/service-accounts
POST /api/v1/ai-gateway/service-accounts/:serviceAccountID/tokens
POST /api/v1/ai-gateway/service-account-tokens/:tokenID/revoke
```

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
```

`tool-grants` 支持 `user`、`service_account`、`role` 和 `ai_client` 四类 subject。运行时会同时合并当前主体、角色和 AI client 的 grant：deny 优先，存在 allow grant 时形成 allow-list。

`access-policies` 和 `skill-bindings` 同样支持 `user`、`service_account`、`role` 和 `ai_client` subject。运行时 Gateway 会合并当前主体、角色和 AI client 的启用记录：access policy 先按 deny/allow 收窄 tools 和 skills，skill binding 再按绑定的 skill/capability refs 收窄 manifest 和 invocation。所有这些控制都发生在 `permissionKeys` 之后，因此不会扩大已有 RBAC 或 scope grant。

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

首版 manifest 已覆盖应用交付和只读 Kubernetes 诊断方向：

- 应用列表和创建
- 应用环境绑定查询
- build/deploy/build_deploy/workflow/verify 触发入口
- release bundle 和 execution task 查询
- Pod、Deployment、Service、Event、日志类只读诊断
- 发布失败诊断上下文生成

## 数据对象

AI Gateway 使用增量迁移 `0015_ai_gateway.sql` 建立以下控制面表：

- `personal_access_tokens`
- `service_accounts`
- `service_account_tokens`
- `ai_clients`
- `ai_access_policies`
- `mcp_tool_grants`
- `ai_gateway_skill_bindings`
- `ai_gateway_audit_logs`

这些表服务于 CLI/MCP/service-account/AI-client 的企业安全接入。现有 AI Workbench 的 `ai_agent_runs`、tool binding、skill binding 和 analysis artifact 仍归 Copilot/Agent Runtime 使用。

每次 Gateway tool invocation 会同时进入通用审计和 `ai_gateway_audit_logs` 专表。专表记录 actor 类型与 ID、AI client、skill、tool、risk level、resource scope、request/result 和脱敏后的关联 metadata；不写入 token、kubeconfig、环境变量、原始日志正文或完整 tool 输入。

## 工程规则

- Gateway handler 只解析请求和返回 DTO。
- `internal/application/aigateway` 负责 manifest、权限、审计和 tool invocation 编排。
- 真实动作必须进入拥有该能力的 application service。
- 构建、发布、Docker、虚拟化等长任务必须复用 durable execution/operation/task 模型。
- AI 分析必须复用 Copilot/Agent Runtime 的 `AgentRun` 和 `AnalysisArtifact`。
- token、secret、kubeconfig、环境变量不得写入日志、audit metadata 或 AI artifact。
