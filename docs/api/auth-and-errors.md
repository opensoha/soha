# Auth And Errors

## Auth Entry

The current backend uses real identity and session handling.

- Password login: `POST /api/v1/auth/login`
- Token refresh: `POST /api/v1/auth/refresh`
- Current user: `GET /api/v1/auth/me`
- Logout: `POST /api/v1/auth/logout`
- Provider discovery: `GET /api/v1/auth/providers`
- Login options: `GET /api/v1/auth/login-options`
- Provider browser entry: `GET /api/v1/auth/login/:providerID/start`
- Provider callback: `GET /api/v1/auth/login/:providerID/callback`
- OIDC browser entry: `GET /api/v1/auth/oidc/login`
- OIDC callback: `GET /api/v1/auth/oidc/callback`
- OIDC frontend exchange: `POST /api/v1/auth/oidc/exchange`

Production-style requests use `Authorization: Bearer <access-token>`.

When `auth.enable_dev_auth` is enabled in `config.yaml`, the backend can still attach the configured bootstrap principal to requests without a bearer token. That fallback is for local development only and does not replace the real password or third-party login paths.

`auth.login_verification.slider_enabled` controls whether the login page renders the frontend slider interaction before password submission.

## Provider Notes

- `POST /api/v1/auth/oidc/exchange` is still the final frontend session exchange endpoint for OIDC and OAuth2-style providers.
- `GET /api/v1/auth/providers` returns every enabled third-party provider instead of assuming a single OIDC entry.
- OIDC, OAuth2, Feishu, DingTalk, and WeCom providers can supplement local role and organization bindings at login time when `syncRolesOnLogin` or `syncOrgsOnLogin` is enabled. The callback only matches existing local roles and organizations; it does not create directory objects.
- SAML may appear in configuration and provider discovery, but the runtime ACS/assertion flow is not enabled yet. A provider without a `loginUrl` should be treated by the frontend as non-runnable.

## Error Envelope

```json
{
  "error": {
    "code": "access_denied",
    "message": "principal is not allowed to list deployments in namespace payments",
    "request_id": "req_123"
  }
}
```

## Error Codes

- `invalid_argument`
- `unauthorized`
- `access_denied`
- `not_found`
- `cluster_unavailable`
- `internal_error`
