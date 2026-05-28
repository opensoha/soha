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

### 1. Login page rendering

The login page reads `/api/v1/auth/providers`.

The response now includes:

- `id`
- `type`
- `name`
- `enabled`
- `loginUrl`

The frontend renders every enabled third-party provider instead of filtering down to one OIDC button.

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

Authorization still belongs to soha. The external IdP identifies the user source, but it does not bypass the local permission model.

## Frontend And Permission Boundary

`Login Settings` is only the configuration surface.

It still uses:

- `settings.identity.view`
- `settings.identity.manage`

After login, menu visibility, route access, and API authorization continue to come from the local permission snapshot and backend authorization checks, not directly from the external IdP.
