# Agent Runtime

## Overview

kubecrux now includes a minimal standalone agent runtime inside the Go module.

Entry point:

- `cmd/agent`

Primary config file:

- `configs/agent.config.yaml`

The platform uses this runtime for the `agent` cluster connection mode.

## Config Shape

```yaml
app:
http:
logger:
auth:
kubernetes:
```

Important fields:

- `http.addr`: agent listen address, default `:18080`
- `auth.bearer_token`: bearer token expected from kubecrux
- `kubernetes.kubeconfig`: kubeconfig path used by the agent
- `kubernetes.kubeconfig_data`: optional inline kubeconfig content
- `kubernetes.context`: optional kubeconfig context override
- `kubernetes.id`, `name`, `region`, `environment`, `labels`: metadata returned by the summary endpoint

## Start Locally

```bash
go run ./cmd/agent
```

Optional config override:

```bash
KC_AGENT_CONFIG_FILE=/abs/path/to/agent.config.yaml go run ./cmd/agent
```

## Exposed Endpoints

- `GET /healthz`
- `GET /api/v1/healthz`
- `GET /api/v1/platform/summary`
- `GET /api/v1/platform/namespaces`
- `GET /api/v1/platform/workloads/pods?namespace=default`
- `GET /api/v1/platform/workloads/deployments?namespace=default`
- `POST /api/v1/platform/actions/deployments/restart`
- `POST /api/v1/platform/actions/deployments/scale`

## Register From kubecrux

Example platform payload:

```json
{
  "id": "edge-agent-01",
  "name": "Edge Agent 01",
  "region": "cn-shanghai",
  "environment": "staging",
  "labels": {
    "provider": "agent",
    "owner": "platform"
  },
  "connectionMode": "agent",
  "agentEndpoint": "http://127.0.0.1:18080",
  "agentToken": "demo-agent-token"
}
```

## Current Scope

This runtime is intentionally minimal.

Current responsibilities:

- return cluster summary
- list namespaces, pods, deployments
- restart deployments
- scale deployments

Not implemented yet:

- logs
- YAML fetch
- events
- exec
- rollout history
- outbound callbacks from agent to platform
