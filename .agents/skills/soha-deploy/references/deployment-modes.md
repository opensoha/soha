# Deployment Modes

## Why Single-Project Works Here

- `internal/api/routes/router.go` serves the embedded SPA when the artifact is present.
- `internal/staticassets` embeds `internal/staticassets/web/dist`.

That means a production-style image can build the frontend and docs first, then compile one Go binary that serves:

- `/api/v1/*`
- `/`

## Choose the Right Asset

### Docker Compose

Use for:

- local demo
- one-host testing
- quick stakeholder preview

Start from `deploy/docker-compose.yaml`.

### Raw Kubernetes YAML

Use for:

- a one-off cluster deployment
- environments where Helm is not wanted
- fast manifest review in GitOps bootstrap work

Start from `deploy/deployment.yaml`.

## Scope of the Starter Assets

- one soha application container
- one PostgreSQL dependency
- same-origin console and API

These assets are intentionally conservative. If the user later wants external PostgreSQL, separate web hosting, Helm packaging, additional runtime dependencies, or ingress-controller-specific annotations, extend the templates or use the dedicated `soha-helm` repo instead of rebuilding deployment logic from scratch.
