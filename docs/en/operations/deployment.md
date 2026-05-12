# Deployment

## Runtime Shape

- `web` builds the Vite SPA console
- `cmd/server` serves the HTTP API
- `docs` builds the Docusaurus site
- PostgreSQL is the durable system of record
- cluster credentials are provided by environment configuration or future secret providers

## Repo Deployment Assets

- `Dockerfile`
- `docker-compose.yaml`
- `configs/config.yaml`
- `deployment.yaml`
- `chart/`

## Quick Commands

Build the application image:

```bash
docker build -t kubecrux:single-project .
```

Start the local single-project stack:

```bash
docker compose -f docker-compose.yaml up -d --build
```

Lint the Helm chart:

```bash
helm lint chart
```

## Local Run Assumptions

- PostgreSQL at `localhost:5432`, database `kubecrux`, user `pgsql`, password `pgsql`
- kubeconfig available at `$HOME/.kube/config` unless overridden
- frontend dev server at `http://localhost:5173`
- docs dev server at `http://localhost:3000/docs/`
