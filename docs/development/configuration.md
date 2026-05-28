# Configuration

All runtime config is environment-variable based in phase 1.

## Required Local Values

- `SOHA_POSTGRES_HOST=localhost`
- `SOHA_POSTGRES_PORT=5432`
- `SOHA_POSTGRES_DB=soha`
- `SOHA_POSTGRES_USER=pgsql`
- `SOHA_POSTGRES_PASSWORD=pgsql`
- `SOHA_REDIS_ADDR=localhost:6379`
- `SOHA_CLUSTER_LOCAL_KUBECONFIG=$HOME/.kube/config`

## MCP Flags

- `SOHA_ENABLE_MCP=true`
- future adapter configuration will be namespaced under `SOHA_MCP_*`
