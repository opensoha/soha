# Deployment Modes

## Why Single-Project Works Here

- `internal/api/routes/router.go` serves the embedded SPA when the artifact is present.
- `internal/staticassets` embeds `internal/staticassets/web/dist`.

That means a production-style image can build the frontend and docs first, then compile one Go binary that serves:

- `/api/v1/*`
- `/`

## Choose the Right Asset

### Raw Docker

Use for:

- a single Soha container with an existing reachable PostgreSQL service
- hosts where Compose is intentionally unavailable
- validating image startup and environment-variable overrides

Follow the `docker run` commands in `README.md` or `README-cn.md`. Provide a database host reachable from the container and override the four public system-key defaults before public exposure. No SecretStore volume or initialization command is required.

### Docker Compose

Use for:

- local demo
- one-host testing
- quick stakeholder preview

Start from `deploy/docker-compose.yaml`.

Compose uses the same standard defaults as local and raw Docker startup. Override
the four system-key environment variables together when the stack is public, and
give the Hermes runner the same execution-runner token as the control plane.

### Raw Kubernetes YAML

Use for:

- a one-off cluster deployment
- environments where Helm is not wanted
- fast manifest review in GitOps bootstrap work

Start from `deploy/deployment.yaml`.

Use normal rolling updates and horizontal scaling when needed. Keep all Soha pods
on the same system-key Secret values; no SecretStore PVC or single-writer strategy
is part of this topology.

## Scope of the Starter Assets

- one soha application container
- one PostgreSQL dependency
- same-origin console and API

These assets are intentionally conservative. If the user later wants external PostgreSQL, separate web hosting, Helm packaging, additional runtime dependencies, or ingress-controller-specific annotations, extend the templates or use the dedicated `soha-helm` repo instead of rebuilding deployment logic from scratch.
