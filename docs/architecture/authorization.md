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

## Access Management Surfaces

- the console exposes `users`, `roles`, `user groups`, `policies`, and `scope grants` as the access management surface
- user-facing `user groups` map to the persisted `teams` relation so existing policy matchers and scope grants remain stable
- user create and update operations persist base profile fields together with role bindings and user-group bindings in the same request, so permission changes become effective on the next principal load or token refresh

## Frontend Permission Projection

- the frontend now consumes a permission snapshot for authenticated sessions
- the snapshot contains role-derived `permissionKeys` and backend-filtered `visibleMenuIds`
- route visibility must not rely on static route metadata alone; route access is determined by both required permission keys and visible menu bindings when a route is bound to a managed menu
- page-level destructive or mutable buttons should progressively switch from unconditional rendering to either:
  - role-derived permission keys for delivery/management surfaces
  - backend-returned `allowedActions` for scoped platform resource rows

## Observability And AI

- observability APIs such as alert summary, alerts, acknowledgements, ownership assignment, notification channel management, routes, and silences must perform backend permission checks instead of relying on frontend button visibility
- copilot APIs are split into:
  - `observe.ai.*` for user-facing chat, root-cause runs, and inspection actions
  - `settings.ai.*` for control-plane configuration such as data sources, analysis profiles, and automation policies
- scheduled automation or inspection jobs may execute with a system principal internally, but interactive user requests must always be evaluated against the caller's permission keys

## Delivery Management

- delivery master-data APIs such as business lines, environments, application-environment bindings, workflow templates, and registry connections must enforce backend permission keys for write operations
- workflow and release triggering are separate permissions from page visibility; a user may view release records without being allowed to trigger new workflow or release runs

## Settings Center

- settings routes use `settings.<domain>.view` to control page access
- mutable operations such as saving OIDC, Prometheus, or AI provider/control-plane settings use `settings.<domain>.manage`
- frontend forms must hide submit actions and block submit handlers when the manage permission is absent, but backend services remain the final enforcement point

## System Management

- system-management routes such as online users, announcements, menus, audit logs, and operation logs use `system.*.view` permissions for route access
- mutable operations such as session revocation, announcement maintenance, and menu maintenance use dedicated `system.*.manage` permissions

## Console Navigation Notes

- access control remains a first-level console entry so administrators can discover permission configuration directly from the sidebar
- settings center is presented as a single first-level entry with tabbed identity, branding, and AI sections inside the page
- cluster monitoring connection details are expected to be managed with cluster configuration, not as a separate global settings-center submenu

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
