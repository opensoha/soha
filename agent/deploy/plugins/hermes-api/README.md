# Hermes Chat 与受控工具

此示例把已部署的 Hermes Runs API 接入 Soha 普通聊天。需要包含本次 Phase 0 改动的 Soha、contracts 和 soha-agent 构建；manifest 的版本范围不代表旧发行版已支持该适配器。

## 准备 Hermes

部署支持 `/v1/capabilities`、`/v1/runs`、任务状态、SSE events 和 stop 的 Hermes API server，配置实际模型及 `API_SERVER_KEY`。通过受信任的 HTTPS 地址提供服务，runner 必须信任其证书链。

参考 [Hermes API server 文档](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/api-server.md)。此适配器按能力探测兼容性；仅有 CLI 的 Hermes 安装不能用于普通聊天流式执行。

使用关闭原生工具和原生记忆的独立 API profile。就绪探测要求 `/health/detailed` 返回 `ok` 或 `healthy`；唯一例外是磁盘百分比预警：所有功能检查必须为 `ok` 且实际剩余空间至少 4 GiB。模型、数据库、未知检查异常或不足 4 GiB 都会拒绝运行。`/v1/toolsets` 必须返回明确状态，只允许 `soha_query` 工具；其他原生工具或原生审批请求会中止本次运行。

把本仓库 `deploy/hermes-tools/` 中的 `plugin.yaml` 和 `__init__.py` 放入该 Hermes profile 的 `plugins/soha-tools/`，并设置：

```yaml
plugins:
  enabled: [soha-tools]
platform_toolsets:
  api_server: [soha]
```

Hermes 环境变量 `SOHA_RUNNER_URL` 指向 runner 的 API 根地址，默认 `http://127.0.0.1:18642/api/v1`。远程地址必须为 HTTPS；私有证书可通过 `SOHA_RUNNER_CA_FILE` 指定。插件不使用代理、不跟随重定向。无需向 Hermes 提供 runner 管理令牌；每轮临时工具凭据作为独立 `session_id` 由 runner 提交，插件从可信的 `HERMES_SESSION_CHAT_ID` 上下文读取。结束或取消即失效，模型参数不能选择运行身份。`HERMES_SESSION_KEY` 属于 Hermes 自己的审批命名空间，不用于 Soha 授权。

工具请求最多 16 KiB，每轮最多 8 次串行调用，共享 6000 个估算证据 token 的上限。知识检索只允许本轮选择的知识库；资源读取、当前用户权限、Gateway 策略和审计在 Soha 内重新校验。模型与检索证据的实际 token 计量仍以 Provider 返回值为准，字符估算不伪装成真实用量。

## 配置 runner

在已有 `agent.config.yaml` 的 `control_plane` 中合并以下配置。保留该部署现有的安全、认证及网络设置。

```yaml
control_plane:
  enabled: true
  base_url: https://soha.example.com
  bearer_token_file: /run/secrets/soha-runner-token
  agent_id: hermes-agent-runner
  provider_kinds: [agent_runtime_only]
  agent_runtime:
    enabled: true
    worker_id: hermes-agent-runner
    provider_ids: [hermes-api]
    provider_kinds: [hermes-api]
    providers:
      hermes-api:
        endpoint: https://hermes.example.com
        bearer_token_file: /run/secrets/hermes-api-key
        # 自签或私有 CA 时显式配置；留空则使用系统根证书。
        ca_file: /run/secrets/hermes-ca.pem
```

`hermes-api-key` 包含 Hermes 的 `API_SERVER_KEY`，使用绝对路径的普通文件，权限不超过 `0600`，不可为符号链接。不要同时设置 `bearer_token`。令牌只在 runner 本地读取，不写入插件 metadata、命令参数或仓库。

私有 CA 文件必须为绝对路径、有效 PEM、大小不超过 1 MiB。信任只对该 Provider 生效，不关闭 TLS 校验、不修改系统根证书。

插件 endpoint 与 runner endpoint 必须一致（忽略末尾 `/`）；重定向、HTTP、URL 中的用户名、密码、查询参数或 fragment 都不接受。可使用通用 Soha Agent 镜像作为执行桥接进程；它不提供内置聊天 Agent。已有 Hermes CLI runner 配置的 `hermes` 与此远程 API provider `hermes-api` 不同。

## 安装和启用

从 OpenSoha 工作区根目录运行，使用已有本地管理员登录：

```sh
soha plugin install --profile local   --manifest ./soha-agent/deploy/plugins/hermes-api/manifest.json --json

soha plugin config opensoha.hermes-api --profile local   --metadata-json '{"endpoint":"https://hermes.example.com"}' --enable --json
```

只安装或未配置 endpoint 时，不会得到就绪助手。runner 拉取目录、完成能力和健康探测并确认当前目录版本后，工作台才允许选择该助手。移除 endpoint、禁用插件或 runner 健康确认过期后，不应继续发起新聊天。

## 验收

1. 在工作台创建会话，确认助手为 Hermes Chat，发送普通问题并验证真实回复和多轮上下文。
2. 在长回复中确认正文逐步出现；刷新页面后通过任务记录恢复进度。
3. 点击取消，核对 Soha 任务及 Hermes 对应 run 均停止。
4. 停止 Hermes 或撤销其凭据，确认助手未就绪或请求明确失败，无模型回复伪装成成功。
5. 启用原生工具后确认就绪检查拒绝该 profile；重新禁用后才恢复。

自动回归使用 HTTPS 测试服务覆盖协议、版本固定、完整流式输出、取消和错误路径；真实模型与实际部署仍须逐项验收。

## 复用 Soha AI Gateway 中的模型

模型厂商地址与上游 Key 继续由 Soha AI Gateway 管理。Hermes 作为 OpenAI-compatible 客户端，连接：

```text
http://127.0.0.1:8080/api/v1/ai-gateway/llm/openai/v1
```

本机地址只适用于原生运行的 Hermes；容器或其他主机应使用其可达的 Soha 地址。Hermes 使用命名 Provider 显式绑定 Gateway 凭据，避免通用 custom 配置将 loopback 端点误判为免密服务：

```yaml
model:
  provider: soha_gateway
  default: gpt-5.6-sol
providers:
  soha_gateway:
    api: http://127.0.0.1:8080/api/v1/ai-gateway/llm/openai/v1
    key_env: SOHA_GATEWAY_API_KEY
    transport: chat_completions
    default_model: gpt-5.6-sol
auxiliary:
  title_generation:
    enabled: false
compression:
  provider: main
```

将 `SOHA_GATEWAY_API_KEY` 写入 Hermes 独立 profile 的受限 `.env` 文件；模型名必须同时在上游 `supportedModels` 和已启用公开路由中存在。这里的模型名是本机已验证示例，应使用各部署实际启用的模型。Gateway 凭据应仅具有 `ai.gateway.relay.invoke` 权限与 `llm-relay` scope，按需要进一步限制 `allowedModels`。

这与 runner 的控制平面令牌、Hermes 的 `API_SERVER_KEY` 是三种不同凭据。普通浏览器登录 token 和 runner token 不能直接用于模型 relay。安装 Agent 插件目前不会自动下发 Gateway 模型选择和凭据；不需要重复配置模型厂商 Key，但需要完成这一次 Agent → Gateway 连接配置。

Hermes 0.21.1 的 `/v1/toolsets` 返回带 `data` 数组的对象；runner 按此结构校验原生工具关闭状态。
Hermes 按需加载工具说明时使用的 `tool_describe`、`tool_search` 仅检索当前 profile 的工具目录，允许这两个元数据事件；它们不授予新的 Soha 数据权限，也不作为已执行的业务工具展示。
