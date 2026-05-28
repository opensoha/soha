# soha AI Gateway Roadmap

## 用途

本文档用于在新的开发会话中继续推进 soha AI Gateway。当前第一版已经具备后端 manifest、CLI/MCP、首批 delivery/k8s tools、token/service account/AI client、tool grants、access policies、skill bindings 和审计基础。

新会话可以直接把“继续目标”一节作为目标输入。

## 已完成基线

- 后端 API：
  - `GET /api/v1/ai-gateway/capabilities`
  - `POST /api/v1/ai-gateway/tools/:toolName/invoke`
  - personal access token、service account、service token、AI client、tool grant、access policy、skill binding 管理 API。
- CLI：
  - `login`
  - `profile list|show|use`
  - `context show|set`
  - `capabilities`
  - `mcp start`
  - `mcp install`
  - `skill list`
  - `skill install`
  - `diagnose`
- MCP stdio server：
  - 从 Gateway manifest 动态暴露 tools/resources/prompts。
  - `tools/call` 只代理到后端 Gateway invoke API。
- 首批 tools：
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
- 验证基线：
  - `go test ./...`
  - `go run ./cmd/soha-cli help`
  - `go run ./cmd/soha-cli skill list`

## 继续目标

继续在 `/Users/yamabuki/Downloads/soha` 中实现 soha AI Gateway 下一阶段，使它从“第一版可运行后端入口”进化为“可运营、可审批、可观测、可扩展的企业 AI 运维控制面”。

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

## P0 剩余目标

### 1. 前端管理面

为 AI Gateway 增加 console 管理页面，至少覆盖：

- AI clients 列表、创建、编辑、禁用。
- service accounts 列表、创建、token 创建和吊销。
- MCP tool grants 列表、创建、删除。
- access policies 列表、创建、编辑、删除。
- skill bindings 列表、创建、编辑、删除。
- personal access tokens 列表、创建、吊销。
- capability manifest 预览，支持选择 AI client、skill、subject 进行调试。

要求：

- 使用现有 `web` Vite/React/Ant Design 6 baseline。
- 路由、菜单、权限可见性必须和 `ai.gateway.*` 权限对齐。
- 不要把 token 明文存入浏览器持久状态；token 创建返回值只展示一次。
- 表单要用结构化控件编辑 policy/grant/binding，不要只给 JSON textarea。

### 2. 审批和风险策略闭环

当前 `requiresApproval` 已能进入 manifest 和 invocation result，但高风险 tool 还没有完整审批流。下一步需要：

- 将 `ai_access_policies.approval_policy` 和现有 delivery/workflow approval 体系衔接。
- 对 `mutate`、`execute`、`high` 风险 tool 支持以下策略：
  - allow
  - deny
  - require_approval
  - require_human_confirm
  - dry_run_only
- 当策略要求审批时，tool invocation 不应直接执行真实动作，而应创建审批/操作记录或返回可追踪的 pending 状态。
- 审批通过后必须仍回到 owning application service 或 durable task，而不是在 Gateway/handler 中执行逻辑。
- 审批、拒绝、超时、取消都要写入 audit 和 operation log。

### 3. resource scope 强制收敛

当前 Gateway 复用业务 service 的权限校验，但 AI Gateway 自身的 policy `resource_scopes` 还需要更强的运行时匹配。

需要实现：

- 从 tool input 和 related IDs 中提取标准 scope：
  - `businessLineId`
  - `applicationId`
  - `applicationEnvironmentId`
  - `environmentId`
  - `clusterId`
  - `namespace`
  - `releaseBundleId`
  - `executionTaskId`
- `mcp_tool_grants.resource_scopes` 和 `ai_access_policies.resource_scopes` 必须参与 invocation 判定。
- manifest 可以保守展示，但 invocation 必须强制检查 scope。
- scope 不匹配时返回 access denied，并记录 deny audit。
- 不要重复造 scope grant 体系；应复用现有 access/scopegrant/policy 语义。

### 4. 查询审计和运营视图 API

补齐 Gateway 专用审计查询能力：

- `GET /api/v1/ai-gateway/audit-logs`
- 支持按 actor、AI client、skill、tool、risk level、result、时间范围过滤。
- 返回 DTO 不包含 raw tool input、token、kubeconfig、环境变量、原始日志正文。
- 前端后续可用于 AI Gateway 运维看板。

## P1 剩余目标

### 5. tools 扩展到更完整的交付闭环

补齐 delivery AI tools：

- application detail 查询。
- service/container 列表和配置摘要。
- build source 查询。
- release target 查询。
- execution logs 单独 tool。
- approval policies 查询。
- workflow template 查询。
- release diff / candidate promotion context。
- rollback suggestion context。

注意：

- 真实 build/deploy/rollback 仍必须走 delivery application service 和 execution plane。
- 对 rollback 类能力先做建议和上下文生成；实际变更必须受 risk policy/approval 控制。

### 6. k8s 诊断增强

扩展只读 k8s tools：

- deployment rollout status。
- deployment events。
- pod describe-style 聚合视图。
- service backend linkage。
- ingress/gateway route context。
- PVC/PV/storage class 只读诊断。
- node condition 和 scheduled pods。

注意：

- 不返回 raw Kubernetes object。
- direct/agent 能力差异必须显式暴露。
- 大集群读取要使用已有聚合 API 或有界查询，不要在 Gateway 中做无界 namespace fan-out。

### 7. AI 分析产物落库

当前 `diagnosis.release_failure.analyze` 返回上下文，但还没有形成 AI Workbench/Agent Runtime 的标准 analysis artifact。

需要：

- 将诊断上下文转成 `AnalysisArtifact` 或对应 copilot/domain 模型。
- 支持关联 application、environment、release bundle、execution task、cluster、namespace。
- artifact 中保存证据摘要、假设、建议、下一步检查，不保存 raw secret/log。
- 如果调用外部 agent provider，必须走 Agent Runtime claim/callback。

### 8. CLI 体验增强

改进 `soha-cli`：

- `capabilities --json` 和更稳定的 machine-readable 输出。
- `tool call <name> --input file.json` 人工兜底命令。
- `token create/revoke` 和 service account 管理命令。
- `audit list` 查询 Gateway audit。
- `diagnose` 输出 policy/grant/binding 命中线索。
- shell completion。

约束：

- CLI 仍然不能绕过后端权限和应用服务。
- CLI 输出必须默认脱敏。

## P2 剩余目标

### 9. MCP resources/prompts 完整实现

当前 MCP 主要围绕 tools。后续应让 resources/prompts 成为可用能力：

- resources/read 支持读取 Gateway manifest 中的 `soha://...` 资源。
- prompts/get 支持 soha prompt 模板。
- prompts 要能结合 skill 和当前 context。
- 所有 resources/prompts 读取也要进入后端权限和审计边界。

### 10. 企业化治理

补充：

- AI client 注册审批。
- token 过期提醒和 last used 管理。
- 异常调用检测。
- per-client/per-user budget。
- tool invocation rate limit。
- sensitive data redaction policy 配置化。
- Gateway health/metrics。

### 11. 文档和示例

继续完善：

- Cursor/Codex/Claude Code MCP 配置示例。
- CI service account 示例。
- delivery-developer 端到端示例。
- k8s-sre 发布失败诊断示例。
- access policy cookbook。
- skill binding cookbook。

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

建议新会话先做 P0-1 前端管理面，原因：

- 后端控制面 API 已经存在。
- 企业用户需要可视化管理 AI clients、service accounts、tool grants、access policies 和 skill bindings。
- 前端管理面能暴露并验证后端 policy/grant/binding 设计是否足够可运营。

若新会话偏后端，建议先做 P0-3 resource scope 强制收敛，因为它直接提升安全边界。
