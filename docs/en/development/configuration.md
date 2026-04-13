# Configuration

All runtime config is environment-variable based in phase 1.

## Required Local Values

- `KC_POSTGRES_HOST=localhost`
- `KC_POSTGRES_PORT=5432`
- `KC_POSTGRES_DB=kubecrux`
- `KC_POSTGRES_USER=pgsql`
- `KC_POSTGRES_PASSWORD=pgsql`
- `KC_REDIS_ADDR=localhost:6379`
- `KC_CLUSTER_LOCAL_KUBECONFIG=$HOME/.kube/config`

## MCP Flags

- `KC_ENABLE_MCP=true`
- future adapter configuration will be namespaced under `KC_MCP_*`
