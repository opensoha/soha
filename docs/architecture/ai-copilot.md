# AI Copilot

## Goal

soha 的 AI 层已经从单一聊天页升级为面向运维中后台的 AIOps assistant workbench。

当前目标分成两个层面：

1. 一个总入口 `/ai-workbench`
2. 一组工作台型子页面，承载调查、巡检自动化、工具装配

AI 层需要帮助完成：

- 告警驱动的根因分析
- 性能抖动与容量异常分析
- 链路慢点和错误热点分析
- 日志、事件、审计、发布、构建的多源证据归并
- 巡检结果到调查会话的闭环
- 工具、skills、数据源的会话级装配

## Current Implemented Surface

当前前后端已经实现以下能力：

- 总入口:
  - `/ai-workbench`
- 调查工作台:
  - `/ai-workbench/chat`
  - `/ai-workbench/root-cause`
  - `/ai-workbench/performance`
- 巡检与自动化:
  - `/ai-workbench/inspection`
- 工具与技能:
  - `/ai-workbench/tool-settings`
  - `/ai-workbench/model-settings`

兼容旧入口仍保留跳转：

- `/ai-observe`
- `/ai-observe/workbench`
- `/ai-observe/operations`
- `/ai-observe/tools`
- `/ai-observe/root-cause`
- `/ai-observe/performance`
- `/ai-observe/chat`
- `/ai-observe/inspection`
- `/chat`
- `/ai-workbench/investigation`
- `/ai-workbench/automation`
- `/ai-workbench/tools`

## Session Model

AI 调查以会话为一等对象，而不是临时聊天记录。

持久化基础表仍然是：

- `ai_sessions`
- `ai_messages`

但 `ai_sessions.metadata` 现在承载工作台元数据：

- `mode`
- `status`
- `scope`
- `pinnedContext`
- `toolset`
- `agentProviderId`
- `analysisRunRefs`
- `summary`
- `tags`
- `archivedAt`

其中 `scope` 现在也是监控工作台向 AI 工作台 handoff 的标准载体，统一承接：

- `alertId`
- `clusterId`
- `namespace`
- `workload`
- `timeRangeMinutes`

其中 `toolset` 已经进入后端执行路径：

- `enabledAdapterIds` 用于限制本会话可用 MCP adapter
- `disabledToolNames` 用于屏蔽具体工具名或 `adapter.tool` 形式的工具
- `budgetOverrides.maxEvidenceItems` 用于限制会话分析证据数量
- `budgetOverrides.timeoutSeconds` 用于限制单次分析工具运行时长
- `scopeOverrides` 会叠加到会话 scope，再进入根因、性能和链路分析

会话工具装配读取 `/api/v1/copilot/workbench/catalog`，该接口面向 `observe.ai.chat` / `observe.ai.view` / `settings.ai.view` 用户返回安全目录摘要：adapter 元数据、数据源启用状态、分析模板摘要、skill 摘要、agent provider、capability、tool binding 和 skill binding。全量 AI Provider 配置和密钥仍只允许通过 `settings.ai.view` 访问。

当前会话模式：

- `general`
- `root_cause`
- `performance`
- `trace`
- `inspection_review`

## API Surface

当前已实现或扩展的会话接口：

- `GET /api/v1/copilot/sessions`
- `GET /api/v1/copilot/sessions/:sessionID`
- `POST /api/v1/copilot/sessions`
- `PATCH /api/v1/copilot/sessions/:sessionID`
- `DELETE /api/v1/copilot/sessions/:sessionID`
- `GET /api/v1/copilot/sessions/:sessionID/messages`
- `POST /api/v1/copilot/sessions/:sessionID/messages`

消息发送不再只返回纯消息列表，当前返回 envelope：

- `messages`
- `toolCalls`
- `analysisArtifacts`
- `sessionPatch`

分析运行接口当前基于统一工件方向扩展：

- `GET /api/v1/copilot/root-cause/runs`
- `POST /api/v1/copilot/root-cause/runs`
- `GET /api/v1/copilot/root-cause/runs/:runID`
- `POST /api/v1/copilot/sessions/:sessionID/analyze`
- `GET /api/v1/copilot/agent-providers`
- `GET /api/v1/copilot/agent-runs`
- `POST /api/v1/copilot/agent-runs/claim`
- `POST /api/v1/copilot/agent-runs/callback`

当前 `ai_root_cause_runs` 已承载：

- `kind`
- `session_id`
- `tool_executions`
- 原有 root-cause 证据、假设、建议和数据源快照字段

## Data Sources And MCP

当前 AIOps 工具能力继续通过 MCP adapter 抽象暴露。

已注册 adapter：

- `platform-native.v1`
- `logs.v1`
- `metrics.v1`
- `traces.v1`
- `delivery.v1`

当前状态：

- `platform-native.v1`
  - 已可读平台聚合证据
- `logs.v1`
  - 已有真实后端执行层
  - 支持 `es` / `loki` / `clickhouse`
- `metrics.v1`
  - 已补齐 Prometheus-backed 执行层
- `traces.v1`
  - 已补齐 Jaeger-backed 执行层

控制平面采用双入口：

1. Settings > AI
   - 全局 provider、data source、analysis profile、automation policy 配置
2. `/ai-workbench/tools`
   - 兼容旧入口，实际跳转到 `/ai-workbench/tool-settings`
3. `/ai-workbench/tool-settings`
   - 会话级临时 toolset 和 skill 装配入口

全局 skill registry 现在采用企业 skill definition，而不是仅保留简单展示项：

- `id`
- `name`
- `category`
- `ownerModule`
- `capabilityRefs`
- `blueprintRefs`
- `inputSchema`
- `outputSchema`
- `scopeRules`
- `enabled`

## Agent Runtime

soha 现在把 AI 执行器抽象为 Agent Runtime，而不是把页面或分析流程直接绑定到 Hermes。

核心对象：

- `AgentProvider`: 执行器目录，当前内置 `internal` 和 `hermes`
- `AgentCapability`: soha 平台能力，包括 `root_cause`、`performance`、`trace`、`inspection_review`、`delivery_failure`、`post_deploy_observation`、`platform_resource_diagnosis`、`docker_diagnosis`、`virtualization_diagnosis`、`oncall_brief`
- `AgentToolBinding`: capability 到 MCP adapter、平台只读工具或 provider 原生工具的映射
- `AgentSkillBinding`: soha skill 到 Hermes skill、prompt template 或后续 provider skill 的映射
- `AgentRun`: 外部 provider 的 durable 异步执行记录，统一承载状态、scope、toolset、skills、回调 token、工具执行和输出工件

Hermes Agent 是首个外部 provider。AI 工作台和 automation policy 只选择 `agentProviderId` 和 capability；真正的 Hermes 调用由独立 agent runner 通过 claim/callback 完成。后续接入 OpenClaw、internal agent 或其它 agent 时，只扩 provider adapter 和 runner 执行器，不重写会话页、自动化策略和业务分析流程。

Agent Runtime 输出统一回写为 soha `AnalysisArtifact`，继续复用证据、假设、建议、关系图、工具调用记录和数据源快照。权限、菜单、预算、数据源脱敏、审计和高风险操作边界仍由 soha 控制。

## Frontend Shape

### `/ai-workbench`

总入口负责：

- 助手欢迎
- 最近调查
- 最近分析
- 风险雷达
- 快捷跳转到工作台、巡检自动化、工具技能

### `/ai-workbench/chat`

调查工作台使用 Ant Design X + antd 组合：

- 左侧 `Conversations`
- 中间 `Bubble.List` + `Sender` + `Prompts`
- 右侧上下文 / 证据 / 假设 / 建议面板
- `ThoughtChain` 抽屉显示工具链与分析步骤
- 当当前会话 scope 携带 `alertId` 时，工作台支持回跳原始监控告警详情
- 支持通过 `mode=trace` 和 `mode=inspection_review` 在同一会话画布内切换链路分析和巡检复盘
- `会话级工具集` 抽屉现在直接展示有效执行策略，并可编辑 adapter 选择、`adapter.tool` 级禁用清单、预算覆盖和 scope override
- `显式分析` 会先打开确认弹窗，让用户选择 provider、analysis profile、可运行分析模式、编辑本轮分析目标，并预览会话 scope 与工具集来源，然后再调用会话分析接口；当前可运行模式为 `root_cause`、`performance`、`trace` 和 `inspection_review`，后端会拒绝 `general`，成功后会把所选 mode 与 `agentProviderId` 写回会话 metadata
- 选择 `internal` provider 时继续使用 soha 内置同步分析；选择 `hermes` 或其它外部 provider 时创建 `AgentRun` 并由 runner 异步回写分析工件
- 会话内所有 assistant 消息携带的 `analysisArtifacts` 会汇总成“分析工件历史”，用户可以在根因、性能、链路和巡检复盘工件之间切换图谱、证据和建议

### `/ai-workbench/root-cause` 与 `/ai-workbench/performance`

根因和性能模式复用同一个会话画布，但入口路径直接表达当前分析类型，避免通过旧 `investigation?mode=*` 私有跳转协议承载主要 IA。

### `/ai-workbench/inspection`

当前是巡检任务、运行记录和自动化策略的统一入口。

`/ai-workbench/inspection` 现在不是只读列表：

- 支持在 AI 工作台内直接新建和编辑巡检任务
- 巡检任务表单会按平台、集群、命名空间三种 scope 收敛输入
- 巡检任务可选择 `mode=inspection` 的分析模板，写入 task metadata 的 `analysisProfileId`，后端执行时可据此覆盖巡检 playbooks 和数据源约束
- 巡检任务现在支持删除；删除入口在工作台内受 `observe.ai.inspection.manage` 控制，后端按创建者隔离并依赖数据库级联清理关联巡检运行
- 任务创建、编辑、立即执行和巡检运行转调查会话都在同一工作区完成，并继续受 `observe.ai.inspection.manage`、`observe.ai.inspection.run` 和 `observe.ai.chat` 权限控制；巡检运行转调查会话还必须具备 `observe.ai.view`，调查会话生成巡检任务还必须具备 `observe.ai.chat`
- 从调查会话生成巡检任务时，工作台会优先附带当前可用的 inspection profile，避免会话转巡检后丢失 profile 驱动的检查契约
- 自动化策略页签支持直接新建和编辑触发类型、分析类型、分析模板、修复策略、去重窗口、冷却时间和启用状态
- 自动化策略读取、创建、编辑和删除继续使用后端 `/api/v1/copilot/automation-policies` 合约，并受 `settings.ai.manage` 权限控制；缺少该权限时工作台不得主动拉取策略列表，只展示明确的权限边界
- 自动化策略表单必须从工作台安全目录里的分析模板摘要选择 `analysisProfileId`，并从 agent provider 目录选择 `agentProviderId`；当前执行器只支持 `alert_webhook` 触发类型，未接入执行器的触发类型不得作为可运行策略选项暴露
- 自动化策略的可运行分析类型包括 `root_cause`、`performance`、`trace` 和 `inspection_review`；外部 provider 还可以通过 Agent Runtime 扩展到交付失败、发布后观察、平台资源诊断、Docker/虚拟化诊断和值班摘要等 capability
- 告警级别、状态、最小持续时间、标签匹配、分析时间范围和审批角色会写入结构化 `triggerConditions` / `approvalPolicy`，避免在工作台里只创建不可触发的空策略
- 自动化执行器会把内置根因、性能和链路分析运行写入带 policy 前缀的 `dedupKey`；外部 provider 会把相同去重上下文写入 `AgentRun.input`。`dedupWindowSeconds` 负责同一告警指纹去重，`cooldownSeconds > 0` 负责策略级冷却，避免不同告警在冷却期内重复触发

巡检结果进入会话时不再只创建一个空调查：

- 后端会把巡检 task 的 `clusterId` / `namespace` 写入会话 scope
- 会话 `pinnedContext` 保留 `inspectionRunId`、`inspectionTaskId`、严重度和状态
- 首条 assistant 消息会携带 `inspection_review` 分析工件
- 该工件把 findings 转成 evidence、recommendations 和左到右关系图，供会话右侧证据面板直接渲染

### `/ai-workbench/tool-settings`

当前展示：

- MCP adapters
- 全局数据源镜像
- 会话级 toolset 装配入口
- 企业 skill registry
- Agent Provider / Capability / Tool Binding / Skill Binding 安全目录

工具装配入口必须与后端执行契约保持一致：

- 禁用工具保存为 `adapter.tool` 形式，避免同名工具误伤其它 adapter
- 空 adapter 列表表示自动选择；推荐预设会优先启用已注册且有数据源支撑的 adapter
- 预算覆盖只保存正数，避免 `0` 被误解释成有效限制
- scope override 写入结构化 `clusterId`、`namespace`、`workload`、`service`、`alertId` 和 `timeRangeMinutes`

### `/ai-workbench/model-settings`

在 AI 工作台内嵌全局 AI 设置，继续使用 `settings.ai.view` / `settings.ai.manage` 权限键。

## Safety And Execution Model

AI 层仍保持“分析与建议优先”的安全方向。

当前重点是：

- 聚合上下文
- 调用读型工具
- 生成证据、假设、建议
- 把工具调用和分析工件沉淀进会话
- 将外部 agent 输出转换成 soha `AnalysisArtifact`

当前没有把高风险执行动作直接挂入聊天自动执行链。

应用接入规范生成与平台交付编排不归 AI 工作台，而归应用交付工作台；AI 工作台只负责暴露能被企业 AI coding 客户端和 Agent Runtime 发现和调用的 MCP/skills 能力。

## Near-Term Expectations

本阶段之后，AI 相关功能应默认遵守以下规则：

- 新的 AI 页面优先接入 AI 工作台，而不是继续新增独立传统表格页
- 会话相关增强优先扩 `metadata`，避免过早拆分调查实体模型
- 新的数据源或工具能力需要同时考虑：
  - 全局配置
  - 会话级装配
  - 分析工件落盘
- root cause / performance / trace / inspection review 以及外部 agent 分析应共用统一 artifact 模型，而不是重复造页面协议
- 新增 agent provider 时只能扩 provider adapter、tool binding、skill binding 和 runner 执行器，不能让页面或业务分析流程直接依赖 provider SDK/CLI
- 监控工作台到 AI 工作台的 handoff 应保持标准 scope 契约，不再由页面各自定义私有跳转协议
