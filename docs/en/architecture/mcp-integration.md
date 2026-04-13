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

## Permission Boundary

MCP must not bypass platform authorization. Every MCP capability maps to a platform action and target scope. The platform owns:

- user identity
- authorization decision
- invocation audit
- response filtering
