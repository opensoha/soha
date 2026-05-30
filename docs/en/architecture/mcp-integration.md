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

The built-in registry now includes at least:

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

The first delivery-facing Gateway tools are exposed to MCP clients with canonical tool names:

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

These tools still must flow through platform permission, scope, audit, and response filtering. Gin handlers must not turn MCP invocations into direct repo writes or long-running shell execution.

## Module Contract

To support single-binary modular startup with a future external-module protocol, the platform now exposes a module contract.

The backend module descriptor currently includes:

- `id`
- `name`
- `defaultPath`
- `enabledConfigKey`
- `dependencies`
- `visiblePermissions`
- `seedMenus`

Current module state is exposed through `GET /api/v1/modules`.
