# AI Copilot

## Goal

kubecrux 的 AI 层已经从单一聊天页升级为面向运维中后台的 AIOps assistant workbench。

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
  - `/ai-workbench/investigation`
- 巡检与自动化:
  - `/ai-workbench/automation`
- 工具与技能:
  - `/ai-workbench/tools`

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

## Frontend Shape

### `/ai-workbench`

总入口负责：

- 助手欢迎
- 最近调查
- 最近分析
- 风险雷达
- 快捷跳转到工作台、巡检自动化、工具技能

### `/ai-workbench/investigation`

调查工作台使用 Ant Design X + antd 组合：

- 左侧 `Conversations`
- 中间 `Bubble.List` + `Sender` + `Prompts`
- 右侧上下文 / 证据 / 假设 / 建议面板
- `ThoughtChain` 抽屉显示工具链与分析步骤
- 当当前会话 scope 携带 `alertId` 时，工作台支持回跳原始监控告警详情

### `/ai-workbench/automation`

当前基于原 `InspectionCenterPage` 扩展，为后续整合自动化策略预留统一入口。

### `/ai-workbench/tools`

当前展示：

- MCP adapters
- 全局数据源镜像
- 会话级 toolset 装配入口
- 企业 skill registry

## Safety And Execution Model

AI 层仍保持“分析与建议优先”的安全方向。

当前重点是：

- 聚合上下文
- 调用读型工具
- 生成证据、假设、建议
- 把工具调用和分析工件沉淀进会话

当前没有把高风险执行动作直接挂入聊天自动执行链。

应用接入规范生成与平台交付编排不归 AI 工作台，而归应用交付工作台；AI 工作台只负责暴露能被企业 AI coding 客户端发现和调用的 MCP/skills 能力。

## Near-Term Expectations

本阶段之后，AI 相关功能应默认遵守以下规则：

- 新的 AI 页面优先接入 AI 工作台，而不是继续新增独立传统表格页
- 会话相关增强优先扩 `metadata`，避免过早拆分调查实体模型
- 新的数据源或工具能力需要同时考虑：
  - 全局配置
  - 会话级装配
  - 分析工件落盘
- root cause / performance / trace 三类分析应尽量共用统一 artifact 模型，而不是重复造页面协议
- 监控工作台到 AI 工作台的 handoff 应保持标准 scope 契约，不再由页面各自定义私有跳转协议
