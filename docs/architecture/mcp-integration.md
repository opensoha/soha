# MCP Integration

## Placement

MCP belongs behind the integration boundary.

Flow:

1. API layer receives request
2. access service and policy engine validate permission
3. integration service resolves adapter and capability metadata
4. infrastructure mcp registry locates the adapter
5. audit service records invocation metadata
6. event service can emit integration events when needed

## Directory Placement

```text
internal/domain/mcp
internal/application/integration
internal/infrastructure/mcp
```

## Registration Model

Each MCP adapter should register:

- adapter id
- display name
- capability list
- required scopes
- transport configuration
- timeout and isolation policy

当前内置 adapter 至少包括：

- `platform-native.v1`
- `logs.v1`
- `metrics.v1`
- `traces.v1`
- `delivery.v1`

## Permission Boundary

MCP must not bypass platform authorization. Every MCP capability maps to a platform action and target scope. The platform owns:

- user identity
- authorization decision
- invocation audit
- response filtering

`delivery.v1` 当前首批 Gateway 工具使用 canonical tool name 暴露给 MCP client：

- `delivery.applications.list`
- `delivery.applications.detail`
- `delivery.applications.create`
- `delivery.application_environments.list`
- `delivery.application_services.list`
- `delivery.build_sources.list`
- `delivery.release_targets.list`
- `delivery.release_bundles.list`
- `delivery.execution_tasks.list`
- `delivery.execution_logs.list`
- `delivery.release_context.diff`
- `delivery.rollback.context`
- `delivery.actions.trigger`

这些能力必须仍然经过平台权限、scope、审计和操作日志边界；Gin handler 不得直接执行 repo 写入或长时 shell。

## Module Contract

为了支持单二进制内置模块启停与未来外部模块协议预留，平台已引入模块注册契约。

后端模块描述符最小字段：

- `id`
- `name`
- `defaultPath`
- `enabledConfigKey`
- `dependencies`
- `visiblePermissions`
- `seedMenus`

当前模块状态通过 `GET /api/v1/modules` 暴露。
