# Agent Runtime

## 定位

soha 的 agent 进程同时承担两类运行时职责：

- 远程集群 agent：为 `connectionMode=agent` 的 Kubernetes 集群提供资源读取和有限操作入口。
- 控制平面 runner：从 soha 控制面领取 delivery、Docker 和 AI Agent Runtime 任务，通过回调写回状态和结果。

AI Agent Runtime 是 soha 面向 AI 工作台、自动化策略和业务诊断场景的统一执行抽象。页面和业务模块只面向 soha 的能力契约，不直接依赖 Hermes、OpenClaw 或任何具体 agent。

核心抽象：

- `AgentProvider`：一个可插拔执行器，例如 `internal`、`hermes`，后续可以扩展 OpenClaw 或其它 provider。
- `AgentCapability`：平台能力，例如 `root_cause`、`performance`、`trace`、`inspection_review`、`delivery_failure`、`docker_diagnosis`。
- `AgentToolBinding`：capability 到 MCP adapter、平台只读工具或 provider 原生工具的绑定。
- `AgentSkillBinding`：soha skill 到 Hermes skill、prompt template 或未来 provider skill 机制的映射。
- `AgentRun`：一次 durable agent 执行，包含 provider、capability、skills、toolset、scope、状态、回调 token、工具记录和输出工件。

Hermes Agent 是当前第一个外部 provider。它通过 soha agent runner 领取 `ai_agent_runs`，执行 Hermes CLI 或 Hermes Agent 能力，然后把结果统一回写为 `AnalysisArtifact`。

`AgentRun` 会保存创建时的 `toolBindings` 和 `skillBindings` 快照。runner claim 拿到的是一次完整的 soha capability contract：scope、toolset、预算、平台 playbook、provider skill 映射、MCP/internal tool 映射都会随任务一起下发。后续接 OpenClaw、internal agent 或其它 provider 时，应读取这份快照来实现 provider adapter，而不是在页面或业务模块里重新推导 Hermes/OpenClaw 私有参数。

## 控制面合约

AI 工作台的安全目录接口会返回 Agent Runtime 目录摘要：

- `GET /api/v1/copilot/workbench/catalog`
- `GET /api/v1/copilot/agent-providers`
- `GET /api/v1/copilot/agent-runs`

这些接口面向已授权用户，返回 provider、capability、tool binding、skill binding、MCP adapter、data source、analysis profile 和 skill registry 摘要。全量 provider 密钥和数据源凭据仍由 Settings > AI 管理。

Agent 执行相关的 runner 接口使用 `runtime.execution_runner_token` 鉴权：

- `POST /api/v1/copilot/agent-runs/claim`
- `POST /api/v1/copilot/agent-runs/callback`
- `POST /api/v1/copilot/agent-runs/tool-call`

`tool-call` 是外部 provider 调用 soha 工具的受控网关。请求必须同时携带 runner bearer token 和当前 `AgentRun.callbackToken`，并且只能调用该 run 创建时快照到 `toolBindings` 的只读工具。控制面当前可执行日志、指标、链路、平台事件、交付发布、交付构建和告警查询，并把每次调用写入 `ToolExecution`，供最终 `AnalysisArtifact` 复用。Provider adapter 不应该绕过这个网关直接访问 soha 数据源或凭据。

当前 runner 的 Hermes/CLI provider POC 会在执行 provider 命令前预取最多 3 个可预取只读工具结果，例如 `events.query`、`logs.query`、`metrics.query`、`traces.query`、`delivery.releases.list`、`delivery.builds.list`、`alerts.list`，并把结果作为 `prefetchedToolResults` 注入 provider prompt 和最终 output。这样即使 provider 还没有接入私有 tool-call 协议，也能消费 soha 受控工具上下文。后续如果 Hermes 或其它 agent 使用 MCP client 方式接入，应在 runner adapter 层把同一个 `tool-call` 网关包装成 provider 可识别的 MCP/tool server，仍然不暴露 soha 数据源凭据。

当前目录中还声明了 `delivery.execution_tasks.list`、`platform.resources.snapshot`、`docker.operations.list`、`docker.services.list`、`virtualization.operations.list`、`oncall.routes.resolve` 等能力绑定，它们是 Agent Runtime 的稳定 catalog contract。只有在对应 reader 或 adapter 接入 `executeAgentToolBindingOutput` 后才会成为可执行工具；在此之前 runner 不会把这些绑定加入默认预取集合，provider 直接调用时会收到显式 unsupported 错误。

`ai_agent_runs` 是 AI Agent Runtime 的 durable 队列表。状态包括：

- `queued`
- `running`
- `completed`
- `failed`
- `canceled`
- `callback_timeout`

## 分析链路

AI 工作台显式分析链路：

1. 用户在 `/ai-workbench` 会话中选择 provider、analysis profile、toolset 和分析模式。
2. 前端调用 `POST /api/v1/copilot/sessions/:sessionID/analyze`。
3. `internal` provider 继续走 soha 内置同步分析路径。
4. 外部 provider 通过 `AgentRun` 入队，assistant 消息先写入 queued 状态。
5. agent runner 使用 claim 接口领取任务。
6. Hermes provider runner 将 scope、toolset、skills、budget 和 prompt 转为 Hermes 调用。
7. provider 如需查询日志、指标、链路、事件、交付发布/构建或告警，应由 runner adapter 通过 `agent-runs/tool-call` 调用 soha 受控工具。
8. runner 通过 callback 回写 `running`、`completed` 或 `failed`。
9. 控制面把 provider 输出转换为 soha `AnalysisArtifact`，继续沉淀证据、假设、建议、关系图、工具调用和数据源快照。

自动化策略链路：

1. soha automation policy 仍是持续分析的主调度源。
2. `alert_webhook` 告警进入后，由 automation policy 做匹配、去重窗口和冷却控制。
3. `agentProviderId=internal` 时沿用内置分析。
4. `agentProviderId=hermes` 或其它外部 provider 时创建 `AgentRun`，由 runner 异步执行。
5. Hermes cron 只作为实验性 provider 原生能力，不作为 soha 平台主调度源。

## Hermes Runner 配置

服务端需要配置 runner token：

```yaml
runtime:
  execution_runner_token: demo-execution-runner-token
```

agent 侧通过 `configs/agent.config.yaml` 启用 AI Agent Runtime：

```yaml
control_plane:
  enabled: true
  base_url: http://127.0.0.1:8080
  bearer_token: demo-execution-runner-token
  agent_id: local-agent
  poll_interval: 5s
  agent_runtime:
    enabled: true
    worker_id: local-hermes-runner
    provider_ids:
      - hermes
    provider_kinds:
      - hermes
    hermes_command: hermes
    providers:
      hermes:
        command: hermes
        args:
          - chat
        prompt_arg: -q
        skill_arg: -s
        provider_skill_arg: ""
    workspace_root: ./.soha/agent-runtime
    poll_interval: 5s
```

启动方式：

```bash
go run ./cmd/agent
```

使用自定义配置文件：

```bash
SOHA_AGENT_CONFIG_FILE=/abs/path/to/agent.config.yaml go run ./cmd/agent
```

当前 Hermes provider runner 的最小 POC 约定：

- runner 只领取 provider id 或 kind 匹配 `hermes` 的 `AgentRun`。
- 默认命令为 `hermes`，可通过 `control_plane.agent_runtime.hermes_command` 覆盖。
- runner 会把 soha run 输入、tool binding 快照、skill binding 快照转换为 prompt，并把 skill id 传给 Hermes。
- runner 提供 `agent-runs/tool-call` 客户端 helper，provider adapter 可以用它执行 run 快照中授权的只读工具；默认 Hermes CLI POC 会预取一小批只读工具结果并注入 prompt，包括日志、指标、链路、事件、交付发布/构建和告警上下文，但不假设某个 Hermes 私有 tool-call 协议。
- runner 回调中的 `analysisArtifacts` 优先作为最终工件；如果缺失，控制面会用输出摘要合成一个基础 `AnalysisArtifact`。

runner 侧已经按 provider 分发执行器。Hermes 是内置默认 executor；其它 CLI 型 provider 可以先通过 `control_plane.agent_runtime.providers.<providerKind>` 配置接入，例如：

```yaml
control_plane:
  agent_runtime:
    provider_ids:
      - openclaw
    provider_kinds:
      - openclaw
    providers:
      openclaw:
        command: openclaw
        args:
          - run
        prompt_arg: --prompt
        skill_arg: ""
        provider_skill_arg: --skill
```

这种 provider executor 仍然只接收 soha `AgentRun` 合约：prompt 内包含 scope、toolset、toolBindings、skillBindings 和 output schema；provider 原生 skill 参数来自 `AgentSkillBinding.providerSkillRef`。如果没有配置对应 provider executor，runner 会显式回调 failed，而不是把未知 provider 伪装成 Hermes 执行。

## 扩展新 Provider

新增 provider 时只扩 adapter，不重写 AI 工作台和业务分析流程：

1. 在 copilot service 的 provider catalog 中注册 `AgentProvider`。
2. 声明 provider 支持的 `AgentCapability`。
3. 为 capability 增加 `AgentToolBinding` 和 `AgentSkillBinding`。
4. 在 runner 中实现 provider-specific 执行器，将 `AgentRun` 输入转换为该 provider 的调用。
5. 保持 callback 输出为 soha `AnalysisArtifact` 或可被合成为 `AnalysisArtifact` 的结构。

硬性边界：

- 页面、handler 和业务模块不得直接调用 Hermes。
- 预算、权限、菜单、审计、数据源脱敏和操作边界由 soha 控制。
- Agent 只作为可插拔执行器，不能绕过 soha 的 MCP adapter、scope、toolset、tool-call 网关和 analysis profile 合约。
- 高风险写操作不能自动挂到聊天链路，必须先落到对应业务模块的 durable operation 或 approval 流程。

## 远程集群 Agent 范围

同一个 `cmd/agent` 仍保留远程集群 agent 能力：

- `GET /healthz`
- `GET /api/v1/healthz`
- `GET /api/v1/platform/summary`
- `GET /api/v1/platform/namespaces`
- `GET /api/v1/platform/workloads/pods?namespace=default`
- `GET /api/v1/platform/workloads/deployments?namespace=default`
- `POST /api/v1/platform/actions/deployments/restart`
- `POST /api/v1/platform/actions/deployments/scale`

当前远程集群 agent 仍是较小范围能力，日志、YAML、事件、exec 和 rollout history 需要继续按平台 agent 语义补齐，不能和 AI Agent Runtime 的 provider 执行链路混用。
