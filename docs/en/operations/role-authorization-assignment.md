# Role Authorization Assignment Runbook

## Purpose

This runbook standardizes how operators assign role-based access in soha so that "the role is assigned but the page is still missing" and "the page is visible but the API still returns 403" are handled as expected outcomes of an incomplete authorization flow rather than as ambiguous bugs.

Console authorization has now converged on a permission-derived default path:

- the role's `permissionKeys` control page access, control-plane API access, and most menu visibility
- explicit menu-to-role bindings are now reserved for exception cases or still-unmapped menus

For platform resource pages, a third gate may also apply:

- ABAC policy or `scope grants` that decide cluster, namespace, and resource scope

Because of that, assigning a role to a user still does not automatically mean the user has a fully usable page and API experience, but common route-backed menus no longer require a second manual binding step.

## When To Use This

Use this runbook when operators or administrators need to:

- create a new role and expose console pages
- add read or write access to an existing role
- troubleshoot why a user cannot see a menu, open a page, save a change, or load platform resource data

## Effective Authorization Model

Treat authorization as complete only when all of the following are true:

1. the user is bound to the target role
2. the role contains the required `permissionKeys`
3. the target menu is either covered by the default derivation path or explicitly handled as an exception
4. if the page is scope-aware, the user or its role/group also matches the required policies and `scope grants`
5. the user's current session has refreshed its permission snapshot

Any missing layer can produce partial or fully denied access.

## Prerequisites

Before changing access, confirm:

- who the target user is
- which `user groups` the user belongs to
- whether the target role is new or existing
- whether the requested access is page visibility, read-only, or manage/write access
- which menus are involved
- which `permissionKeys` are involved
- whether the target menu currently shows as `自动派生`, `显式覆盖`, or `未映射` in menu management
- whether platform scope authorization is also required

Record these three mappings before you apply the change:

- page or feature -> menu
- page or feature -> `permissionKeys`
- page or feature -> whether `scope grants` or ABAC policy is required
- page or feature -> default-derived path or explicit menu exception

## Formal Assignment Checklist

### 1. Define the exact capability

Translate the request into explicit authorization objects instead of using a vague request such as "give this user access."

At minimum, define:

- the target page or module, for example `Access Control > Users` or `Settings > AI`
- whether the user needs read-only or manage access
- whether backend write APIs are required
- whether platform resource scope is required

Recommended split:

- page access: `*.view`
- change or save: `*.manage`
- platform runtime actions: controlled by resource `capabilities`, `allowedActions`, policies, and scope

### 2. Verify or update the role's `permissionKeys`

Confirm that the target role contains the required `permissionKeys`.

Typical examples:

- open the users list: `access.users.view`
- create or update users: `access.users.manage`
- open AI settings: `settings.ai.view`
- modify AI settings: `settings.ai.manage`

Checks:

- do not configure a read-only flow with only `*.manage` and no `*.view`
- manage flows usually need both `*.view` and `*.manage`
- do not confuse platform resource actions with console `permissionKeys`
- do not assume built-in role defaults are identical to the currently persisted role definition

### 3. Verify the menu visibility mode

Most console menus no longer require manual menu-role bindings. In `System Management > Menu Management`, first identify which mode the target menu is using:

- `自动派生`
  - this is the default path
  - once the role has the mapped `permissionKeys`, the menu should automatically appear in `visibleMenuIds`
  - no manual `roleIds` maintenance is required
- `显式覆盖`
  - reserve this for a small number of exception cases or intentionally curated entries
  - in the current implementation, `roleIds` are an additive fallback path, not a stricter deny-style override over derived permissions
- `未映射`
  - this means the route and the current backend derivation table do not yet provide a stable mapping
  - if visibility still needs to be controlled, configure explicit `roleIds`

Decision rule:

- `permissionKeys` answer "is this page or API allowed"
- menu derivation or explicit bindings answer "does this entry appear in the visible menu set"

For mapped menus, the common path is `permissionKeys` only. Maintain `roleIds` only for exception paths.

### 4. Verify platform scope authorization

For platform resource pages or other scope-aware data, visible menus and openable routes still do not guarantee resource access.

Confirm:

- the user role or user group matches the correct policy
- the required `scope grants` exist
- cluster, namespace, and label conditions cover the intended resources

Typical symptoms:

- the page opens but the list is empty
- the detail page loads but actions are missing
- login succeeds but resource requests return 403 or empty results

### 5. Assign the role to the user

After the role definition is correct, verify that the user is actually bound to the role and that group membership is also correct.

Checks:

- the user-role binding saved successfully
- the user-group binding saved successfully
- if policy depends on `user groups`, do not stop at role binding alone

For users logging in through OIDC, OAuth2, Feishu, DingTalk, or WeCom providers with login-time role or organization enrichment enabled, also confirm:

- `roleField` and `organizationField` match the provider's claim/profile payload
- external role references resolve to local `roles.id` or `roles.name`
- external organization references resolve to local organization `id`, `slug`, `org_path`, or `source + externalId`
- `replace_external` only replaces bindings written by the same login provider and does not remove locally managed bindings from the user page

### 6. Refresh the permission snapshot

After role, menu, policy, or scope changes, the current session may still hold an outdated permission snapshot.

Complete at least one refresh action:

- sign out and sign back in
- refresh the session's authentication context
- confirm that the latest session payload now includes the updated `permissionKeys` and `visibleMenuIds`

If you skip this step, the backend state may be correct while the frontend still behaves as if the old permissions are active.

## Validation Steps

### Basic validation

1. Sign in again as the target user.
2. Confirm that the target menu appears in the sidebar.
3. Open the target page and confirm it is not blocked by route guards or redirects.
4. For read-only pages, confirm that lists or details load successfully.
5. For manage pages, confirm that create, save, delete, or other manage actions are shown as expected.

### Permission snapshot validation

In browser developer tools, inspect the post-login session bootstrap or permission snapshot response and confirm:

- `permissionKeys` contains the expected keys
- `visibleMenuIds` contains the expected menu entries

For `自动派生` menus, both should usually be present together. For `显式覆盖` or `未映射` menus, continue by checking the menu configuration itself.

### API validation

Execute one real read request and one real write request for the target feature.

Expected outcomes:

- read-only role: read succeeds and write is denied
- manage role: both read and write succeed
- scoped platform role: responses are limited to the allowed clusters, namespaces, or resources

### Scope validation

If the feature is scope-aware, also confirm:

- allowed cluster or namespace selections show data
- unauthorized scopes return empty or denied results
- row-level actions appear only inside the allowed scope

## Common Failure Modes

### `permissionKeys` exist, but the menu is still hidden

Symptoms:

- the sidebar entry is missing
- direct navigation may still fail or remain undiscoverable

Action:

- first confirm that the menu is supposed to be `自动派生`
- if it is `自动派生`, check whether the menu is enabled, whether the menu ID is covered by the backend derivation table, and whether the session has refreshed
- if it is `未映射`, switch to explicit `roleIds` or add the missing backend/frontend mapping before validating again

### Explicit menu binding exists, but `permissionKeys` are missing

Symptoms:

- the menu may still be visible, but the route opens and then fails authorization
- save operations return 403

Action:

- add the required `*.view` or `*.manage` keys to the role

### Explicit `roleIds` are configured, but a user with permissions still sees the menu

Symptoms:

- the menu is marked `显式覆盖`
- a user is not listed in the explicit `roleIds`, but still sees the menu because the mapped permission key exists

Action:

- this is the current implementation, not an isolated failure
- the backend treats explicit `roleIds` as an additive fallback path, not as a stronger rule that can suppress a derived permission result
- if the product later needs a true "has permission but still hide this menu" behavior, that will require a separate model change

### An unmapped menu has no `roleIds` and remains visible

Symptoms:

- the menu is marked `未映射`
- it is enabled and has no explicit `roleIds`
- users can still see the sidebar entry even without a specific page permission

Action:

- this is the current compatibility path so older menus do not disappear before they are mapped
- if the entry needs access control now, add explicit `roleIds`
- if the entry should move onto the default path, add the backend menu-ID-to-permission mapping

### Only half of the `view` and `manage` split was assigned

Symptoms:

- the page is visible but save buttons are missing
- a button appears but backend submission is denied

Action:

- assign both `*.view` and `*.manage` according to the page contract

### The role changed, but the session still uses an old snapshot

Symptoms:

- database or admin configuration is already correct
- the frontend still renders the old access state

Action:

- re-authenticate or refresh the permission snapshot

### The page is visible, but platform data is empty

Symptoms:

- menu and route access look correct
- resource lists are empty or resource operations are denied

Action:

- inspect ABAC policy and `scope grants`
- verify the actual user group, role, cluster, and namespace matchers

### Runtime resource actions were mistaken for console page permissions

Symptoms:

- `permissionKeys` were assigned, but restart, scale, logs, exec, or similar actions still fail

Action:

- inspect resource `capabilities`
- inspect backend `allowedActions`
- inspect the relevant scope policy

## Minimum Acceptance Criteria

Do not treat an authorization change as complete unless all of the following are true:

- the target user sees the correct menu entry
- the target page opens
- read and manage behavior match the intended assignment
- platform data scope matches the intended assignment
- unauthorized pages, buttons, and scopes remain denied

If any item fails, the access change is still incomplete.

## Operating Guidance

- default to maintaining `permissionKeys`; do not treat explicit menu bindings as a required step for normal route-backed pages
- prefer keeping route-backed menus in `自动派生`
- when a new page or permission is added, update the three-column mapping for permission keys, menu mapping or exception mode, and scope requirements in the same change
- for a new custom menu, make the mode explicit:
  - a fully mapped menu on the backend derivation table
  - an intentional exception menu controlled by explicit `roleIds`
  - a temporarily unmapped menu that is still using compatibility visibility
- do not stop at page visibility checks; validate real APIs and real scope behavior
- before rolling out a custom role, test the full login, menu, page, write-action, and scope-filter flow with a non-admin user

## Unresolved Exception Paths

- explicit `roleIds` on mapped menus currently add visibility fallback; they do not override or narrow a successful derived permission result
- enabled unmapped menus without explicit `roleIds` remain visible for compatibility with older menu records
- the menu-management UI labels `自动派生` / `显式覆盖` / `未映射` partly from route metadata today; the persisted backend payload still only stores `roleIds`

## Related Documents

- [Authorization](../architecture/authorization.md)
- [Configuration](./configuration.md)
