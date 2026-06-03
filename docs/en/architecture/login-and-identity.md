# Login And Identity Flow

## Goal

`soha` now treats login as two coordinated layers:

- local username/password login
- aggregated third-party login providers

The provider model currently supports:

- `oidc`
- `feishu`
- `dingtalk`
- `wecom`
- `oauth2`
- `saml`

The design goal is not "support one enterprise SSO mode". The goal is to let the console hold multiple enterprise login entries at the same time and render all enabled providers on the login page.

## Configuration Model

The backend settings service stores the active provider document under `identity.login_providers`.

It contains:

- `providers[]`
- `defaultProviderId`

Each provider includes at least:

- `id`
- `name`
- `type`
- `enabled`
- `redirectUrl`
- `frontendRedirectUrl`

OAuth and OIDC style providers may also include:

- `clientId`
- `clientSecret`
- `issuer` or `authorizeUrl`
- `tokenUrl`
- `userInfoUrl`
- `scopes`
- `defaultRoles`
- `userIdField`
- `userNameField`
- `emailField`
- `roleField`
- `organizationField`
- `syncRolesOnLogin`
- `syncOrgsOnLogin`
- `roleSyncMode`
- `orgSyncMode`

## Login-Time Role And Organization Mapping

Runnable providers in the OIDC, OAuth2, Feishu, DingTalk, and WeCom families now support login-time binding enrichment:

- `roleField` reads external role references from ID token claims or the userinfo/profile response
- `organizationField` reads external organization references from ID token claims or the userinfo/profile response
- `syncRolesOnLogin` controls whether successful login supplements local role bindings
- `syncOrgsOnLogin` controls whether successful login supplements local organization bindings
- `roleSyncMode` and `orgSyncMode` support `append` and `replace_external`

`append` only adds resolved local bindings. `replace_external` first removes bindings previously written by the same provider and then writes the current login result. It does not remove locally managed bindings that an administrator assigned from the user page.

Role resolution only matches existing local `roles.id` or `roles.name`. Organization resolution only matches existing local organizations by:

- `teams.id`
- `teams.slug`
- `teams.org_path`
- `teams.source` equal to the provider type or provider id plus `teams.external_id` equal to the external organization reference

The login callback does not auto-create local roles or organizations. Administrators must create or import the organization tree first and set provider source and external ID values on mapped organizations. This keeps directory synchronization separate from login authorization mapping.

Feishu, DingTalk, and WeCom application credentials such as App Key, App Secret, CorpID, and CorpSecret belong to the Login Settings provider configuration, not to organization nodes. An organization node only stores the mapping source and the external department or organization ID. When multiple applications exist for the same provider family, prefer mapping the organization to the concrete provider id instead of the broad `feishu`, `dingtalk`, or `wecom` type.

This is not full Feishu, DingTalk, or WeCom directory sync. Full directory connectors, organization-change webhooks, and departure/disable synchronization should be designed as later connector capabilities. The current stage only enriches the local authorization context when a user successfully logs in.

SAML providers currently store configuration-only fields such as:

- `metadataUrl`
- `entityId`
- `certificate`

## Backward Compatibility

Legacy single-provider OIDC settings still exist under `identity.oidc`.

Current compatibility rules:

- the multi-provider document is the new source of truth
- runtime OIDC resolution prefers the default or first available `oidc` provider from that document
- writing the multi-provider document also backfills the legacy `identity.oidc` shape
- if only the legacy OIDC config exists, the settings service projects it into one synthetic `oidc` provider at read time

This keeps old configuration and database contents working while the new model takes over.

## Runtime Flow

## Browser Session And Token Storage

The browser console now moves toward a BFF-style boundary:

- `accessToken` stays in frontend memory and is used for ordinary API `Authorization: Bearer ...` requests
- `refreshToken` is written by the backend as an `HttpOnly` cookie; the frontend no longer persists or reads it
- `/api/v1/auth/refresh` accepts an empty body and reads the refresh session from the cookie before refreshing the cookie
- `/api/v1/auth/logout` clears the refresh cookie, so frontend sign-out must call that endpoint before clearing local state
- in development, the console defaults to the Vite `/api` proxy and same-origin `/api/v1` to keep refresh cookies usable

After a page reload, protected routes first try to restore the session through the refresh endpoint when the in-memory `accessToken` is missing. Only a failed restore redirects to the login page.

Native browser `WebSocket`, `EventSource`, and noVNC connections cannot reliably attach an `Authorization` header. When the Console opens Pod log streams, Pod terminals, virtualization operation streams, or VNC/noVNC connections, it first calls `POST /api/v1/auth/stream-ticket` with the current session access token and then appends the returned one-time `stream_ticket` to the same-origin stream URL. The backend accepts only whitelisted stream paths. Tickets expire after 60 seconds, are exactly path-bound, and are consumed from PostgreSQL on first validation. Consumption still checks session status and `authz_version`, so role, organization, or account-status changes can invalidate later stream connections.

## Machine Calls And Runner Tokens

Browser login and machine calls intentionally use different boundaries:

- the browser console uses Soha session access/refresh tokens, and server-side sessions plus `authz_version` let role, organization, and account status changes force a refresh
- API clients, CI runners, Docker runners, AI agent runners, and AI Gateway clients use Bearer tokens
- long-term machine tokens should converge on the existing AI Gateway PAT/SAT shape: persist only token hashes, bind the token to a user or service account, and constrain access with `permissionKeys` plus scopes

The current transition keeps `runtime.execution_runner_token` compatible while also allowing external runner endpoints to accept `service_account_token`:

- Delivery execution runners require `delivery.execution-tasks.manage`
- Docker runners require `docker.operations.manage`
- AI agent runners require `ai.gateway.invoke` or `observe.ai.chat`

Task-level callback tokens are still validated by their owning execution task or agent run. SAT replaces only the global shared token used to enter the runner claim/status/callback channel; it does not replace per-task replay protection or callback ownership checks.

### 1. Login page rendering

The login page reads `/api/v1/auth/providers`.

The response now includes:

- `id`
- `type`
- `name`
- `enabled`
- `loginUrl`

The frontend renders every enabled third-party provider instead of filtering down to one OIDC button.

The login page also reads `/api/v1/auth/login-options`. When
`auth.login_verification.slider_enabled` is `true` in the config file, the
frontend renders a slider interaction before password login.

### 2. Browser entry

Each provider login entry is:

- `GET /api/v1/auth/login/:providerID/start`

The legacy OIDC browser entry still exists:

- `GET /api/v1/auth/oidc/login`

### 3. Callback processing

The unified provider callback entry is:

- `GET /api/v1/auth/login/:providerID/callback`

The legacy OIDC callback still exists:

- `GET /api/v1/auth/oidc/callback`

During callback handling the backend:

- validates state
- resolves the provider type
- exchanges the authorization code
- fetches or constructs a user profile
- binds or creates the local user
- writes `user_identities`
- creates a session
- creates a one-time exchange code
- redirects to `/login/callback?code=...`

### 4. Frontend session exchange

The frontend callback page still calls:

- `POST /api/v1/auth/oidc/exchange`

Despite the legacy name, this endpoint now carries the final session exchange for OAuth2-style providers as well.

## Current Provider Status

### OIDC

Runtime-complete.

- issuer discovery
- code exchange
- ID token verification
- claims and userinfo reconciliation

### Generic OAuth2

Runtime-complete as long as the operator provides working:

- `authorizeUrl`
- `tokenUrl`
- `userInfoUrl`
- field mappings

### Feishu

The current implementation uses a dedicated Feishu code exchange path and then fetches user info.

It still depends on operator validation of:

- app credentials
- callback URLs
- profile-field mappings

The preset endpoints are practical defaults, not a guarantee that every Feishu app mode is covered.

### DingTalk

The current implementation uses OAuth2 code exchange and then fetches user information through configured open-platform endpoints.

DingTalk app modes and user-info surfaces vary enough that soha treats it as a configurable provider instead of hard-coding one universal contract.

### WeCom

WeCom does not behave like a standard generic OAuth2 token exchange.

The current flow is:

- webpage authorization returns `code`
- backend exchanges `corpid + corpsecret` for a corporate access token
- backend uses `code + access_token` to fetch `UserId`

So in practice:

- `tokenUrl` points to the corporate access token API
- `userInfoUrl` points to the `getuserinfo` API
- `clientId` is effectively `corpid`
- `clientSecret` is effectively `corpsecret`

The current implementation reliably gets enterprise `UserId`. Email or display-name enrichment may still require additional operator-provided profile APIs and mappings.

### SAML

Currently configuration-visible, not runtime-complete.

Supported now:

- settings UI
- persistence
- login-page visibility
- explicit user-facing warning that runtime is not enabled

Not supported yet:

- SP metadata generation
- ACS endpoint
- assertion verification
- nameID and attribute parsing
- formal SAML-to-local-user mapping

The server must therefore treat SAML as a configuration-stage capability and must not imply that the SAML runtime is production-ready.

## Audit And Local Identity Binding

Regardless of provider type, every successful external login resolves into the same local identity model:

- local `users`
- local `roles`
- local `sessions`
- local `user_identities`

On first successful external login the backend:

- first looks up `provider_type + provider_id + provider_user_id`
- otherwise tries to merge by email
- otherwise creates a new local user
- assigns provider `defaultRoles` when the local user has no roles yet
- when `syncRolesOnLogin` is enabled, writes provider-managed external role bindings from `defaultRoles` and `roleField`
- when `syncOrgsOnLogin` is enabled, writes provider-managed external organization bindings from `organizationField`

Authorization still belongs to soha. The external IdP identifies the user source, but it does not bypass the local permission model.

## Frontend And Permission Boundary

`Login Settings` is only the configuration surface.

It still uses:

- `settings.identity.view`
- `settings.identity.manage`

After login, menu visibility, route access, and API authorization continue to come from the local permission snapshot and backend authorization checks, not directly from the external IdP.
