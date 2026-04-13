# Environment Overrides

## Backend

kubecrux no longer uses `.env.example` as its primary configuration source.

Backend configuration lives in `configs/config.yaml`. Environment variables are only used for overrides when deployment environments need to change file defaults.

Typical overrides:

- `KC_CONFIG_FILE`
- `KC_HTTP_ADDR`
- `KC_LOGGER_LEVEL`
- `KC_DATABASE_HOST`
- `KC_DATABASE_PORT`
- `KC_DATABASE_NAME`
- `KC_DATABASE_USER`
- `KC_DATABASE_PASSWORD`
- `KC_REDIS_ADDR`
- `KC_AUTH_ENABLE_DEV_AUTH`

## Frontend

The current SPA does not expose a documented public env contract.

- API requests always target same-origin `/api/v1`
- local development relies on the Vite proxy in `web/vite.config.ts`
- the in-app docs page targets same-origin `/docs/`

If you deploy the frontend and API on separate origins, use a reverse proxy or update `web/src/services/api-client.ts` accordingly.
