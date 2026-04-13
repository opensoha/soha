# Authorization

## Model

kubecrux uses RBAC + ABAC.

### RBAC Roles

- `admin`
- `ops`
- `developer`
- `readonly`
- `auditor`

RBAC answers one question first: does the principal's role set ever permit this action type?

### ABAC Attributes

- user: `user_id`, `roles`, `teams`, `projects`, `tags`
- cluster: `cluster_id`, `region`, `environment`, `labels`
- namespace: `namespace`, `labels`, `owner_team`
- resource: `kind`, `name`, `labels`, `annotations`, `owner`
- action: `view`, `list`, `watch`, `update`, `delete`, `restart`, `scale`, `logs`, `exec`
- context: `request time`, `source ip`, `approval state`

## Authorization Flow

1. API middleware validates bearer JWT or applies local bootstrap principal when development auth is explicitly enabled
2. Handler builds service call parameters only
3. Application service builds a normalized access request
4. RBAC evaluator derives baseline allowed actions from persisted role capabilities
5. ABAC evaluator matches persisted policies against attributes and conditions
6. Scope filter builder calculates allowed clusters, namespaces, and selectors
7. Final decision is returned to application service
8. Resource service either performs the operation or returns deny
9. Audit service records the allow or deny result

## Result Structure

```json
{
  "allowed": true,
  "reason": "role ops matched non-production scope",
  "allowedActions": ["view", "list", "watch", "logs"],
  "resourceScope": {
    "clusters": ["local"],
    "namespaces": ["default"],
    "labelSelector": "owner=team-a"
  }
}
```

## Storage Model

### PostgreSQL

- `roles`
- `policies`
- `policy_bindings`
- `user_role_bindings`
- durable user, team, and project attributes
- `sessions`
- audit trail of allow, deny, and operation outcomes

### Redis

- session cache and token blacklist
- OIDC state and one-time frontend exchange payloads
- optional short-lived policy evaluation cache for repeated read operations
- lock state for mutable operations and future approvals

## Recommended Policy Schema

### `policies`

- `id`
- `name`
- `effect`
- `priority`
- `subjects` JSONB
- `targets` JSONB
- `actions` JSONB
- `conditions` JSONB
- `reason`
- `created_at`
- `updated_at`

### `policy_bindings`

- `id`
- `policy_id`
- `subject_type`
- `subject_id`
- `scope` JSONB
- `created_at`
- `updated_at`

## Responsibility Split

### Middleware

- request ID
- bearer token parsing and principal extraction
- source and request context capture
- no policy evaluation

### Service Layer

- build access request
- call access service and policy engine
- apply effective scope to downstream resource queries
- emit audit entries for allow and deny paths
