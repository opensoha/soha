# Authorization

## Model

soha uses RBAC + ABAC.

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
- roles persist two distinct authorization payloads:
  - `capabilities`: RBAC resource actions such as `view`, `list`, `update`, `trigger`
  - `permissionKeys`: console menu and backend API permissions such as `access.users.manage` or `settings.ai.view`
- built-in role permission maps remain bootstrap defaults for seed replay, but runtime backend authorization and permission snapshots must resolve effective `permissionKeys` from persisted role assignments so custom roles can formally delegate console/API access

## Frontend Permission Projection

- the frontend now consumes a permission snapshot for authenticated sessions
- the snapshot contains persisted role-derived `permissionKeys` and backend-filtered `visibleMenuIds`
- route visibility must not rely on static route metadata alone; route access is determined by both required permission keys and backend-filtered visible menus
- page-level destructive or mutable buttons should progressively switch from unconditional rendering to either:
  - role-derived permission keys for delivery/management surfaces
  - backend-returned `allowedActions` for scoped platform resource rows

## Menu Visibility Derivation

- the common path now derives visible menus from runtime `permissionKeys` instead of requiring a second manual menu-role binding step
- backend menu visibility evaluation follows this order:
  - disabled menus are always hidden
  - menus with a known backend derivation rule become visible when any mapped `*.view` permission key is present
  - explicit `menu_role_bindings` remain as a fallback path for menus that still need operator-curated visibility
  - unmapped menus without explicit bindings remain visible for compatibility until they are either mapped or explicitly bound
- visible child menus automatically pull their enabled ancestors into the returned tree so sidebar structure stays navigable
- the permission snapshot's `visibleMenuIds` is produced from this backend-filtered tree, so sidebar ordering and visibility stay aligned with backend menu state
- frontend menu management now exposes three operator-facing states:
  - `自动派生`: visibility comes from the mapped permission keys
  - `显式覆盖`: menu role IDs are stored and used as the explicit path
  - `未映射`: no known permission mapping exists yet, so operators must either keep explicit role IDs or accept the current compatibility fallback

## Known Exception Paths

- explicit menu role IDs are additive fallback in the current backend implementation; they do not narrow visibility for a principal that already satisfies the derived permission rule
- the backend derivation table is intentionally bounded to the current menu contract, so newly added or custom menus do not auto-derive until their menu ID is mapped in the backend
- the frontend can infer derived permission keys from route metadata for the menu-management UI, but the persisted menu payload still stores only `roleIds`; `visibilityMode` and `derivedPermissionKeys` are currently UI-level interpretation fields rather than durable backend columns

## Access Surface Permissions

- access-management read/list surfaces use:
  - `access.users.view`
  - `access.roles.view`
  - `access.groups.view`
  - `access.policies.view`
  - `access.scope-grants.view`
- mutable access-management operations use:
  - `access.users.manage`
  - `access.roles.manage`
  - `access.groups.manage`
  - `access.policies.manage`
  - `access.scope-grants.manage`
- these permission keys are separate from RBAC resource `capabilities`; they gate console menus, permission snapshots, and backend API writes rather than Kubernetes or delivery runtime actions directly

## Observability And AI

- observability APIs such as alert summary, alerts, acknowledgements, ownership assignment, notification channel management, routes, and silences must perform backend permission checks instead of relying on frontend button visibility
- copilot APIs are split into:
  - `observe.ai.*` for user-facing chat, root-cause runs, and inspection actions
  - `settings.ai.*` for control-plane configuration such as data sources, analysis profiles, and automation policies
- AI Gateway APIs add a separate external-agent boundary:
  - `ai.gateway.view` for reading the caller-specific capability manifest
  - `ai.gateway.invoke` for invoking already-authorized MCP tools through Gateway
  - `ai.gateway.manage` for managing AI clients, service accounts, access policies, tool grants, and skill bindings
- AI Gateway permissions do not replace business permissions. A delivery tool still needs the relevant `delivery.*` key, and a Kubernetes diagnosis tool still needs the relevant workspace/platform key.
- MCP tool grants are evaluated across the current subject, its roles, and the declared AI client. They only narrow access; deny grants take precedence over allow grants.
- scheduled automation or inspection jobs may execute with a system principal internally, but interactive user requests must always be evaluated against the caller's permission keys

## Delivery Management

- delivery configuration APIs such as application-environment bindings, workflow templates, build templates, registries, and application delivery actions must enforce backend permission keys for write operations
- workflow and release triggering are separate permissions from page visibility; a user may view release records without being allowed to trigger new workflow or release runs

## Settings Center

- settings routes use `settings.<domain>.view` to control page access
- mutable operations such as saving login-provider, Prometheus, or AI provider/control-plane settings use `settings.<domain>.manage`
- frontend forms must hide submit actions and block submit handlers when the manage permission is absent, but backend services remain the final enforcement point

## System Management

- system-management routes such as online users, announcements, menus, audit logs, and operation logs use `system.*.view` permissions for route access
- mutable operations such as session revocation, announcement maintenance, and menu maintenance use dedicated `system.*.manage` permissions

## Console Navigation Notes

- access control remains a first-level console entry so administrators can discover permission configuration directly from the sidebar
- settings center remains a first-level system-workspace entry, but login settings and branding settings now resolve as dedicated child routes under `/settings/login` and `/settings/branding`
- AI settings no longer live inside the settings-center tab surface; they belong to the AI workbench settings routes
- cluster monitoring connection details are expected to be managed with cluster configuration, not as a separate global settings-center submenu

## Operator Runbook

- formal role assignment and validation steps now live in the operations runbook: [角色授权分配运行手册](../operations/role-authorization-assignment.md)

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
- `user_role_bindings`
- `role_permission_bindings`
- `scope_grants`
- durable user, team, and project attributes
- `sessions`
- audit trail of allow, deny, and operation outcomes

Short-lived login and policy-evaluation state should use signed tokens, request context, process-local cache, or explicit PostgreSQL-backed records. No separate cache service is part of the current runtime baseline.

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

Role permission keys live in `role_permission_bindings`; scoped access grants live in `scope_grants`.

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
