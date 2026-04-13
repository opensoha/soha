# Auth And Errors

## Auth Entry

The current backend uses real identity and session handling.

- Password login: `POST /api/v1/auth/login`
- Token refresh: `POST /api/v1/auth/refresh`
- Current user: `GET /api/v1/auth/me`
- Logout: `POST /api/v1/auth/logout`
- OIDC browser entry: `GET /api/v1/auth/oidc/login`
- OIDC callback: `GET /api/v1/auth/oidc/callback`
- OIDC frontend exchange: `POST /api/v1/auth/oidc/exchange`

Production-style requests use `Authorization: Bearer <access-token>`.

When `auth.enable_dev_auth` is enabled in `config.yaml`, the backend can still attach the configured bootstrap principal to requests without a bearer token. That fallback is for local development only and does not replace the real password or OIDC paths.

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
