# soha-cli

`soha-cli` 是 soha AI Gateway 的本地入口。它负责登录、保存本地 profile、声明 AI client 上下文、启动 MCP stdio server，以及检查当前身份能看到的 Gateway capability。

CLI 不执行真实平台动作。所有 MCP tool 调用都会代理到 soha 后端：

```text
soha-cli mcp start
  -> GET /api/v1/ai-gateway/capabilities
  -> POST /api/v1/ai-gateway/tools/:toolName/invoke
  -> backend permissionKeys / scope grants / MCP tool grants / access policies / skill bindings / audit
  -> owning application service
```

## 构建和帮助

```bash
go run ./cmd/soha-cli help
go build -o ./bin/soha-cli ./cmd/soha-cli
```

当前首版命令面：

- `login`
- `profile list|show|use`
- `context show|set`
- `capabilities`
- `mcp start`
- `mcp install`
- `skill list`
- `skill install`
- `diagnose`

`skill install` 只安装 AI 可读流程和方法论，不赋予额外权限。真实授权仍由 Gateway manifest、permission keys、scope grants 和 MCP tool grants 决定。

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
soha-cli capabilities --profile local --output names
```

`capabilities` 会调用 `GET /api/v1/ai-gateway/capabilities`，输出当前身份可用的 tools、resources、prompts、skills、permission keys 和 manifest summary。

如果 capability 不存在，优先检查：

- 当前 profile 是否登录到正确 server。
- 用户或 token 是否拥有 `ai.gateway.view`。
- 业务工具是否还需要对应 domain permission，例如 `delivery.*`。
- `mcp_tool_grants` 是否把 tool 收窄掉。
- `ai_access_policies` 或 `ai_gateway_skill_bindings` 是否把当前 AI client、角色或主体收窄掉。

## MCP stdio server

本地启动：

```bash
soha-cli mcp start --profile local
```

生成 MCP client 配置：

```bash
soha-cli mcp install --profile local --command /usr/local/bin/soha-cli
```

输出形态：

```json
{
  "mcpServers": {
    "soha": {
      "command": "/usr/local/bin/soha-cli",
      "args": ["mcp", "start", "--profile", "local"]
    }
  }
}
```

如果没有传 `--profile`，`mcp install` 会使用当前 profile。

MCP server 支持：

- `initialize`
- `tools/list`
- `tools/call`
- `resources/list`
- `prompts/list`

`tools/list` 从 Gateway manifest 动态生成 MCP tool 列表。`tools/call` 只代理到 `POST /api/v1/ai-gateway/tools/:toolName/invoke`，不会直接访问 PostgreSQL、Kubernetes、Docker、runner 工作目录或本地 kubeconfig。

首版可调用工具包括：

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

k8s 工具需要 `clusterId`，`namespace` 可按平台 scope 语义为空表示聚合视图。`k8s.pods.logs` 还需要 `podName`，可选 `container`、`tailLines`、`sinceSeconds` 和 `previous`。日志输出会在 Gateway 层做基础敏感字段脱敏。

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
```

`diagnose` 会验证 profile、server、token 和 Gateway capability 读取链路，并输出 tool/resource/prompt/skill 数量。它不打印 token。
