# Deployment Modes

## Why Single-Project Works Here

- `internal/api/routes/router.go` serves the embedded SPA and docs when those assets are present.
- `web/embed.go` embeds `web/dist`.
- `docs/embed.go` embeds `docs/build`.

That means a production-style image can build the frontend and docs first, then compile one Go binary that serves:

- `/api/v1/*`
- `/`
- `/docs/`

## Choose the Right Asset

### Docker Compose

Use for:

- local demo
- one-host testing
- quick stakeholder preview

Start from `docker-compose.yaml`.

### Raw Kubernetes YAML

Use for:

- a one-off cluster deployment
- environments where Helm is not wanted
- fast manifest review in GitOps bootstrap work

Start from `deployment.yaml`.

### Helm

Use for:

- repeated installs
- multiple clusters or environments
- overrides through values files

Start from `chart/`.

## Scope of the Starter Assets

- one kubecrux application container
- one PostgreSQL dependency
- same-origin console and API
- docs served from the same container

These assets are intentionally conservative. If the user later wants external PostgreSQL, separate web hosting, Redis, or ingress-controller-specific annotations, extend the templates instead of rebuilding deployment logic from scratch.
