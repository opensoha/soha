# MCP

## Positioning

MCP is not placed directly inside handlers or Kubernetes resource services. soha reserves MCP at the infrastructure and integration boundary so external tools can be connected without bypassing authorization, audit, or request tracing.

## Current Extension Point

- infrastructure: `internal/infrastructure/mcp`
- application facade: `internal/application/integration`
- API surface: `GET /api/v1/mcp/capabilities`

## Registration Model

1. Define adapter metadata in the MCP registry
2. Expose adapter capabilities through the integration service
3. Route calls through access checks and audit recording
4. Keep each adapter's credential and permission boundary isolated from cluster credentials

## Isolation Rules

- MCP adapters never read raw request headers directly
- API middleware provides principal and request context
- access service must evaluate adapter scope before invocation
- audit service records adapter usage and deny decisions
- adapter configuration must stay separate from Kubernetes kubeconfig metadata

## Documentation Expectation

Every new MCP adapter should document:

- capability name and scope
- required credentials or external endpoints
- authorization boundary
- audit fields emitted
- failure and timeout behavior
