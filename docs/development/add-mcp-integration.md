# Add MCP Integration

1. Define adapter capability metadata in `internal/domain/mcp`.
2. Implement adapter contract under the future MCP integration package.
3. Register the adapter through `integration-service`.
4. Map platform permissions to adapter capabilities.
5. Emit audit and event records for each invocation.
6. Document the adapter under operations and architecture docs.
