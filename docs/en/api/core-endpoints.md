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

## AI Gateway APIs

- `GET /api/v1/ai-gateway/capabilities`
- `POST /api/v1/ai-gateway/tools/:toolName/invoke`
- `GET /api/v1/ai-gateway/personal-access-tokens`
- `POST /api/v1/ai-gateway/personal-access-tokens`
- `POST /api/v1/ai-gateway/personal-access-tokens/:tokenID/revoke`
- `POST /api/v1/ai-gateway/personal-access-tokens/:tokenID/rotate`
- `GET /api/v1/ai-gateway/service-accounts`
- `POST /api/v1/ai-gateway/service-accounts`
- `POST /api/v1/ai-gateway/service-accounts/:serviceAccountID/tokens`
- `POST /api/v1/ai-gateway/service-account-tokens/:tokenID/revoke`
- `POST /api/v1/ai-gateway/service-account-tokens/:tokenID/rotate`
- `GET /api/v1/ai-gateway/ai-clients`
- `POST /api/v1/ai-gateway/ai-clients`
- `PUT /api/v1/ai-gateway/ai-clients/:clientID`
- `GET /api/v1/ai-gateway/tool-grants`
- `POST /api/v1/ai-gateway/tool-grants`
- `DELETE /api/v1/ai-gateway/tool-grants/:grantID`
- `GET /api/v1/ai-gateway/access-policies`
- `POST /api/v1/ai-gateway/access-policies`
- `PUT /api/v1/ai-gateway/access-policies/:policyID`
- `DELETE /api/v1/ai-gateway/access-policies/:policyID`
- `GET /api/v1/ai-gateway/skill-bindings`
- `POST /api/v1/ai-gateway/skill-bindings`
- `PUT /api/v1/ai-gateway/skill-bindings/:bindingID`
- `DELETE /api/v1/ai-gateway/skill-bindings/:bindingID`

`/ai-gateway/capabilities` returns the current caller's AI-native tools, resources, prompts, and skills after backend permission filtering. AI clients should send `X-Soha-AI-Client-ID`, `X-Soha-AI-Client`, and `X-Soha-Skill-ID` when available so audit records can distinguish human, service, client, skill, and tool context.

`/ai-gateway/tools/:toolName/invoke` is the shared MCP/CLI/AI-agent tool invocation entry point. It must re-check Gateway permission, domain permission, scope, grants, and risk policy, then call the owning application service instead of bypassing soha control-plane logic.

First-version tool names:

- `delivery.applications.list`
- `delivery.applications.create`
- `delivery.application_environments.list`
- `delivery.actions.trigger`
- `delivery.release_bundles.list`
- `delivery.execution_tasks.list`
- `k8s.pods.list`
- `k8s.pods.logs`
- `k8s.deployments.list`
- `k8s.services.list`
- `k8s.events.list`
- `diagnosis.release_failure.analyze`

Kubernetes tools are read-only and route through the platform resource application service. Gateway applies basic sensitive-field redaction to log outputs before returning them to MCP/CLI callers.

Personal and service-account token create and rotate responses return the opaque token value once. The database stores only the hash and prefix; callers must keep the returned value in their local CLI/MCP credential store. Rotation revokes the previous token after creating the replacement and revalidates the copied permission keys against the current subject.

AI client, tool-grant, access-policy, and skill-binding endpoints require `ai.gateway.manage`. Tool grants, access policies, and skill bindings support `user`, `service_account`, `role`, and `ai_client` subjects; runtime evaluation combines subject, role, and client records. Deny policies and grants take precedence, allow records form allow-lists, and skill bindings narrow exposed skill/capability refs without granting new permissions.

Tool invocations write both the standard audit log and `ai_gateway_audit_logs`. The dedicated AI Gateway row records actor, AI client, skill, tool, risk level, resource scope, request/result, and related IDs without storing full tokens, kubeconfigs, raw logs, or complete tool input.

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
