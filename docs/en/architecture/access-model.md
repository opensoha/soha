# Access Model

## Goals

soha uses RBAC plus ABAC.

- RBAC answers whether a role can perform a class of operation.
- ABAC answers whether the operation is allowed for this principal, target, and runtime context.

## Base Roles

- `admin`
- `ops`
- `developer`
- `readonly`
- `auditor`

## RBAC Capability Matrix

| Role | Menu Scope | API Scope | Resource Actions |
| --- | --- | --- | --- |
| admin | all | all | all |
| ops | operations and delivery | most platform APIs | list, watch, logs, restart, scale, update within assigned scope |
| developer | workloads and delivery | namespace-scoped APIs | list, logs, restart, scale within owned projects |
| readonly | read-only menus | read-only APIs | view, list, watch, logs |
| auditor | audit and events | audit and read APIs | view, list, export audit and event data |

## ABAC Inputs

### Subject attributes

- `user_id`
- `roles`
- `teams`
- `projects`
- `tags`

### Cluster attributes

- `cluster_id`
- `region`
- `environment`
- `labels`

### Namespace attributes

- `namespace`
- `labels`
- `owner_team`

### Resource attributes

- `kind`
- `name`
- `labels`
- `annotations`
- `owner`

### Action attributes

- `view`
- `list`
- `watch`
- `update`
- `delete`
- `restart`
- `scale`
- `logs`
- `exec`

### Context attributes

- `time`
- `source`
- `approval_state`

## Policy Data Structure

```json
{
  "effect": "allow",
  "priority": 100,
  "subjects": {
    "roles": ["ops"],
    "teams": ["platform"]
  },
  "targets": {
    "clusters": {"environment": ["prod"]},
    "namespaces": {"owner_team": ["platform"]},
    "resources": {"kind": ["Deployment"], "labels": {"tier": ["core"]}}
  },
  "actions": ["list", "view", "scale", "restart"],
  "conditions": {
    "approval_state": ["approved"],
    "source": ["console", "api"]
  }
}
```

## Decision Pipeline

1. Resolve principal from identity-service.
2. Compute RBAC capability baseline from roles.
3. Build authorization request envelope with subject, cluster, namespace, resource, action, and context attributes.
4. Evaluate deny policies first.
5. Evaluate allow policies by priority.
6. Compute filtered action set and resource scope.
7. Return decision:
   - `allowed`
   - `reason`
   - `allowed_actions`
   - `resource_scope`
8. Persist audit record for allow, deny, or high-risk action.

## Response Shape

```json
{
  "allowed": true,
  "reason": "role ops matched and ABAC scope platform-prod",
  "allowed_actions": ["view", "list", "logs", "restart", "scale"],
  "resource_scope": {
    "clusters": ["prod-cn-1"],
    "namespaces": ["platform-system", "payments"],
    "label_selector": "owner-team in (platform)"
  }
}
```
