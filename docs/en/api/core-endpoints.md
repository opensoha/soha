# Core Endpoints

## Cluster APIs

- `GET /api/v1/clusters`
- `POST /api/v1/clusters`
- `GET /api/v1/clusters/:clusterID/detail`
- `GET /api/v1/clusters/:clusterID/namespaces`
- `GET /api/v1/clusters/:clusterID/infrastructure/nodes`
- `GET /api/v1/clusters/:clusterID/workloads/pods?namespace=default`
- `GET /api/v1/clusters/:clusterID/workloads/deployments?namespace=default`
- `GET /api/v1/clusters/:clusterID/workloads/statefulsets?namespace=<optional>`
- `GET /api/v1/clusters/:clusterID/network/services?namespace=<optional>`
- `GET /api/v1/clusters/:clusterID/network/ingresses?namespace=<optional>`
- `GET /api/v1/clusters/:clusterID/events?namespace=<optional>&limit=20`
- `POST /api/v1/clusters/:clusterID/workloads/deployments/restart`
- `POST /api/v1/clusters/:clusterID/workloads/deployments/scale`

## Audit APIs

- `GET /api/v1/audit/logs`

## Monitoring APIs

- `POST /api/v1/integrations/alerts/webhook`
- `GET /api/v1/monitoring/summary`
- `GET /api/v1/alerts?status=<optional>&clusterId=<optional>&limit=50`
- `GET /api/v1/notification-channels`
- `POST /api/v1/notification-channels`
- `PUT /api/v1/notification-channels/:channelID`

## Virtualization APIs

- `GET /api/v1/virtualization/overview`
- `GET /api/v1/virtualization/clusters?provider=<optional>&kubernetesClusterId=<optional>&limit=100`
- `POST /api/v1/virtualization/clusters`
- `PUT /api/v1/virtualization/clusters/:id`
- `DELETE /api/v1/virtualization/clusters/:id`
- `POST /api/v1/virtualization/clusters/:id/test`
- `POST /api/v1/virtualization/clusters/:id/sync`
- `GET /api/v1/virtualization/vms?provider=<optional>&connectionId=<optional>&namespace=<optional>&status=<optional>&limit=100`
- `POST /api/v1/virtualization/vms`
- `GET /api/v1/virtualization/vms/:id`
- `POST /api/v1/virtualization/vms/:id/actions`
- `GET /api/v1/virtualization/images?provider=<optional>&connectionId=<optional>&status=<optional>&limit=100`
- `GET /api/v1/virtualization/flavors?provider=<optional>&connectionId=<optional>&status=<optional>&limit=100`
- `POST /api/v1/virtualization/flavors`
- `PUT /api/v1/virtualization/flavors/:id`
- `DELETE /api/v1/virtualization/flavors/:id`
- `GET /api/v1/virtualization/operations?taskKind=<optional>&status=<optional>&connectionId=<optional>&vmId=<optional>&limit=100`
- `GET /api/v1/virtualization/operations/:taskID`
- `GET /api/v1/virtualization/operations/:taskID/logs`
- `POST /api/v1/virtualization/sync`

PVE credentials are accepted only on create or update payloads and are never returned by API responses. Responses expose only `credentialConfigured`.

## Application APIs

- `GET /api/v1/applications?search=<optional>&limit=100`
- `POST /api/v1/applications`
- `GET /api/v1/applications/:applicationID`
- `PUT /api/v1/applications/:applicationID`
- `DELETE /api/v1/applications/:applicationID`
- `GET /api/v1/builds?applicationId=<optional>&limit=50`
- `POST /api/v1/builds/trigger`
- `GET /api/v1/integrations/gitlab/projects?search=<optional>&limit=50`
- `GET /api/v1/integrations/gitlab/branches?projectId=<required>&search=<optional>&limit=50`
- `GET /api/v1/integrations/gitlab/tags?projectId=<required>&search=<optional>&limit=50`

## Copilot APIs

- `GET /api/v1/copilot/insights`
- `GET /api/v1/copilot/sessions`
- `GET /api/v1/copilot/sessions/:sessionID`
- `POST /api/v1/copilot/sessions`
- `PATCH /api/v1/copilot/sessions/:sessionID`
- `DELETE /api/v1/copilot/sessions/:sessionID`
- `GET /api/v1/copilot/sessions/:sessionID/messages`
- `POST /api/v1/copilot/sessions/:sessionID/messages`
- `POST /api/v1/copilot/sessions/:sessionID/analyze`
- `GET /api/v1/copilot/root-cause/runs`
- `POST /api/v1/copilot/root-cause/runs`
- `GET /api/v1/copilot/root-cause/runs/:runID`
- `GET /api/v1/copilot/agent-providers`
- `GET /api/v1/copilot/agent-runs`
- `GET /api/v1/copilot/data-source-capabilities`
- `GET /api/v1/copilot/data-sources`
- `POST /api/v1/copilot/data-sources`
- `PUT /api/v1/copilot/data-sources/:dataSourceID`
- `POST /api/v1/copilot/data-sources/:dataSourceID/validate`
- `GET /api/v1/copilot/analysis-profiles`
- `POST /api/v1/copilot/analysis-profiles`
- `PUT /api/v1/copilot/analysis-profiles/:profileID`
- `GET /api/v1/copilot/automation-policies`
- `POST /api/v1/copilot/automation-policies`
- `PUT /api/v1/copilot/automation-policies/:policyID`
- `GET /api/v1/copilot/inspection-tasks`
- `POST /api/v1/copilot/inspection-tasks`
- `PUT /api/v1/copilot/inspection-tasks/:taskID`
- `GET /api/v1/copilot/inspection-runs`
- `POST /api/v1/copilot/inspection-tasks/:taskID/execute`
- `POST /api/v1/copilot/agent-runs/claim`
- `POST /api/v1/copilot/agent-runs/callback`
- `POST /api/v1/copilot/agent-runs/tool-call`

`/copilot/agent-runs/claim`, `/copilot/agent-runs/callback`, and `/copilot/agent-runs/tool-call` are runner-facing APIs and require `Authorization: Bearer <runtime.execution_runner_token>`. Tool calls also require the per-run `callbackToken`; the control plane only executes tools present in the `AgentRun.toolBindings` snapshot and records the result as `ToolExecution`.

`POST /copilot/root-cause/runs` accepts `agentProviderId`, `analysisProfileId`, and `triggerType`. `agentProviderId=internal` runs the built-in analyzer synchronously; external providers such as `hermes` create a queued root-cause business run plus a linked `AgentRun`, then backfill the business run from the runner callback.

## Application Payload

```json
{
  "id": "billing-api",
  "name": "Billing API",
  "key": "billing-api",
  "group": "commerce",
  "language": "Go",
  "description": "Core billing service",
  "ownerTeam": "platform",
  "repositoryProvider": "gitlab",
  "repositoryProjectId": "12345",
  "repositoryPath": "platform/billing-api",
  "defaultBranch": "main",
  "defaultTag": "v1.0.0",
  "buildImage": "registry.example.com/platform/billing-api",
  "buildContextDir": ".",
  "dockerfilePath": "Dockerfile",
  "enabled": true,
  "metadata": {
    "tier": "core"
  }
}
```

## Build Trigger Payload

```json
{
  "applicationId": "billing-api",
  "refType": "branch",
  "refName": "main",
  "imageTag": "billing-api:manual-20260322",
  "buildArgs": {
    "profile": "default"
  }
}
```

## Alert Webhook Payload

```json
{
  "source": "alertmanager",
  "alerts": [
    {
      "fingerprint": "cpu-high-prod-1",
      "title": "CPUHigh",
      "summary": "CPU usage exceeded threshold",
      "severity": "critical",
      "status": "firing",
      "clusterId": "local",
      "namespace": "default",
      "labels": {
        "alertname": "CPUHigh",
        "team": "platform"
      },
      "annotations": {
        "runbook": "https://example.internal/runbooks/cpu-high"
      },
      "receiver": "platform-default",
      "generatorUrl": "http://prometheus.local/graph"
    }
  ]
}
```

Headers:

- `X-Soha-Webhook-Token: <monitoring.webhook_token>`
  or
- `Authorization: Bearer <monitoring.webhook_token>`

## Notification Channel Payload

```json
{
  "id": "platform-default-webhook",
  "name": "Platform Default Webhook",
  "channelType": "webhook",
  "enabled": true,
  "config": {
    "url": "https://notify.example.internal/hooks/platform-alerts",
    "method": "POST"
  }
}
```

## Cluster Registration Payload

### Direct kubeconfig

```json
{
  "id": "direct-prod-01",
  "name": "Direct Production 01",
  "region": "cn-shanghai",
  "environment": "production",
  "labels": {
    "provider": "kubeconfig",
    "owner": "platform"
  },
  "connectionMode": "direct_kubeconfig",
  "kubeconfig": "apiVersion: v1\n..."
}
```

### Agent

```json
{
  "id": "edge-prod-01",
  "name": "Edge Production 01",
  "region": "cn-shanghai",
  "environment": "production",
  "labels": {
    "provider": "agent",
    "owner": "platform"
  },
  "connectionMode": "agent",
  "agentEndpoint": "https://agent.example.internal",
  "agentToken": "optional-bearer-token"
}
```
