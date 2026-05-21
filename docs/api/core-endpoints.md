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
- `GET /api/v1/applications/:applicationID/detail`
- `PUT /api/v1/applications/:applicationID`
- `DELETE /api/v1/applications/:applicationID`
- `GET /api/v1/build-templates`
- `POST /api/v1/build-templates`
- `PUT /api/v1/build-templates/:buildTemplateID`
- `DELETE /api/v1/build-templates/:buildTemplateID`
- `GET /api/v1/application-environments`
- `GET /api/v1/application-environments/:applicationEnvironmentID`
- `GET /api/v1/application-environments/:applicationEnvironmentID/detail`
- `GET /api/v1/application-environments/target-candidates?clusterId=<required>&namespace=<required>&search=<optional>`
- `GET /api/v1/workflow-templates`
- `GET /api/v1/delivery/release-board`
- `GET /api/v1/delivery/release-bundles?applicationId=<optional>&applicationEnvironmentId=<optional>&limit=50`
- `GET /api/v1/delivery/release-bundles/:bundleID`
- `GET /api/v1/delivery/execution-tasks?applicationId=<optional>&applicationEnvironmentId=<optional>&releaseBundleId=<optional>&status=<optional>&providerKind=<optional>&limit=50`
- `GET /api/v1/delivery/execution-tasks/:taskID`
- `POST /api/v1/delivery/execution-tasks/:taskID/cancel`
- `POST /api/v1/delivery/execution-tasks/:taskID/retry`
- `GET /api/v1/delivery/approval-policies`
- `POST /api/v1/delivery/approval-policies`
- `PUT /api/v1/delivery/approval-policies/:approvalPolicyID`
- `DELETE /api/v1/delivery/approval-policies/:approvalPolicyID`
- `POST /api/v1/delivery/execution-callbacks`
- `GET /api/v1/delivery/execution-tasks/:taskID/runner-status`
- `GET /api/v1/builds?applicationId=<optional>&limit=50`
- `POST /api/v1/builds/trigger`
- `GET /api/v1/workflows?applicationId=<optional>&limit=50`
- `POST /api/v1/workflows/trigger`
- `POST /api/v1/workflows/:workflowRunID/approve`
- `POST /api/v1/workflows/:workflowRunID/reject`
- `GET /api/v1/releases?applicationId=<optional>&clusterId=<optional>&limit=50`
- `POST /api/v1/releases/trigger`
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
- `GET /api/v1/copilot/root-cause/runs`
- `POST /api/v1/copilot/root-cause/runs`
- `GET /api/v1/copilot/root-cause/runs/:runID`
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
  "buildSources": [
    {
      "id": "default:billing-api",
      "name": "Repository Dockerfile",
      "type": "repo_dockerfile",
      "enabled": true,
      "isDefault": true,
      "buildImage": "registry.example.com/platform/billing-api",
      "defaultTag": "v1.0.0",
      "config": {
        "contextDir": ".",
        "dockerfilePath": "Dockerfile",
        "builderKind": "docker"
      }
    }
  ],
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
  "applicationEnvironmentId": "binding-prod",
  "buildSourceId": "default:billing-api",
  "refType": "branch",
  "refName": "main",
  "imageTag": "billing-api:manual-20260322",
  "buildArgs": {
    "profile": "default"
  },
  "variables": {
    "GO_VERSION": "1.24"
  }
}
```

## Execution Callback Payload

```json
{
  "callbackToken": "task-callback-token",
  "status": "completed",
  "payload": {
    "logs": [
      "checkout source",
      "build image completed"
    ],
    "image": "registry.example.com/platform/billing-api:20260505",
    "imageDigest": "sha256:..."
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

- `X-Kubecrux-Webhook-Token: <monitoring.webhook_token>`
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
