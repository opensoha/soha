# Agent Protocol

## Goal

The soha agent protocol gives the platform a stable way to talk to remote clusters when direct kubeconfig connectivity is not desired or is not possible.

The backend stores agent connection metadata in PostgreSQL, then the Gin API calls the agent over HTTP for summary, resource list, and controlled execution requests.

## Registration Model

Platform registration payload:

```json
{
  "id": "edge-prod-01",
  "name": "Edge Production 01",
  "region": "cn-shanghai",
  "environment": "production",
  "connectionMode": "agent",
  "agentEndpoint": "https://agent.example.internal",
  "agentToken": "optional-bearer-token"
}
```

Persisted storage:

- `clusters.connection_mode = agent`
- `cluster_credentials_meta.credential_type = bearer`
- `cluster_credentials_meta.source_type = agent`
- `cluster_credentials_meta.metadata.endpoint`
- `cluster_credentials_meta.metadata.token`

## Required Agent Endpoints

### Summary

`GET /api/v1/platform/summary`

Response:

```json
{
  "data": {
    "id": "edge-prod-01",
    "name": "Edge Production 01",
    "region": "cn-shanghai",
    "environment": "production",
    "labels": { "provider": "agent" },
    "connectionMode": "agent",
    "version": "v1.31.2",
    "capabilities": ["apps", "batch", "networking.k8s.io"],
    "health": {
      "status": "healthy",
      "message": "ok",
      "lastChecked": "2026-03-22T10:00:00Z"
    }
  }
}
```

### Namespaces

`GET /api/v1/platform/namespaces`

Response:

```json
{
  "items": [
    {
      "name": "default",
      "status": "Active",
      "labels": {},
      "ageSeconds": 3600,
      "allowedActions": ["view", "list"]
    }
  ]
}
```

### Pods

`GET /api/v1/platform/workloads/pods?namespace=default`

### Deployments

`GET /api/v1/platform/workloads/deployments?namespace=default`

## Controlled Action Endpoints

### Restart Deployment

`POST /api/v1/platform/actions/deployments/restart`

Request:

```json
{
  "namespace": "default",
  "name": "api"
}
```

### Scale Deployment

`POST /api/v1/platform/actions/deployments/scale`

Request:

```json
{
  "namespace": "default",
  "name": "api",
  "replicas": 3
}
```

## Authentication

When `agentToken` is configured, soha sends:

```http
Authorization: Bearer <token>
```

Recommended agent behavior:

- verify bearer token before any platform call
- separate read endpoints from execution endpoints
- keep execution endpoints explicitly allow-listed

## Error Contract

Agent errors should return normal HTTP error codes. soha maps downstream connection or execution failures into `cluster_unavailable` when the remote agent cannot be reached or rejects the operation.

## Authorization Boundary

The platform remains the primary authorization decision point.

That means:

- RBAC + ABAC is evaluated in soha before the agent call
- the agent should still validate its own bearer token
- the agent should not assume the caller is trusted just because the request comes from soha
- high-risk actions should stay narrow and explicit

## Current Platform Usage

Current backend usage of the agent protocol:

- cluster health snapshot sync via summary endpoint
- resource list reads for namespaces, pods, deployments
- deployment restart and scale actions

Future protocol expansion can add:

- logs
- YAML fetch
- events
- exec
- rollout history
