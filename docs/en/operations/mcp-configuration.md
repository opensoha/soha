# MCP Configuration

Phase 1 reserves MCP configuration and adapter registration only.

## Planned Config Shape

- `SOHA_ENABLE_MCP=true`
- `SOHA_MCP_DEFAULT_TIMEOUT=10s`
- `SOHA_MCP_ADAPTERS=<adapter list>`
- `SOHA_MCP_<ADAPTER>_ENDPOINT=...`
- `SOHA_MCP_<ADAPTER>_TOKEN=...`

## Boundary Rules

- permissions remain inside soha access-service
- adapters receive scoped calls, not raw user sessions
- all invocations must be auditable
