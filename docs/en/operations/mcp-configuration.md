# MCP Configuration

Phase 1 reserves MCP configuration and adapter registration only.

## Planned Config Shape

- `KC_ENABLE_MCP=true`
- `KC_MCP_DEFAULT_TIMEOUT=10s`
- `KC_MCP_ADAPTERS=<adapter list>`
- `KC_MCP_<ADAPTER>_ENDPOINT=...`
- `KC_MCP_<ADAPTER>_TOKEN=...`

## Boundary Rules

- permissions remain inside kubecrux access-service
- adapters receive scoped calls, not raw user sessions
- all invocations must be auditable
