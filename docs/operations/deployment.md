# Deployment

## Runtime Shape

- kubecrux can ship as a single-project application container
- `web` builds the Vite SPA console and is embedded into the server binary at build time
- `docs` builds the Docusaurus site and is embedded into the server binary at build time
- `cmd/server` serves the HTTP API, the SPA, and `/docs/`
- PostgreSQL is the durable system of record
- cluster credentials are provided by environment configuration or future secret providers

## Repo Deployment Assets

The main image and compose assets now live at the repo root.

- `Dockerfile`
- `docker-compose.yaml`
- `configs/config.yaml`
- `deploy/k8s/kubecrux-single-project.yaml`
- `deploy/helm/kubecrux/`

Use these paths as the default baseline for image build, local stack startup, raw Kubernetes rollout, and Helm packaging.

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
helm lint deploy/helm/kubecrux
```

## Local Run Assumptions

- PostgreSQL at `localhost:5432`, database `kubecrux`, user `pgsql`, password `pgsql`
- kubeconfig available at `$HOME/.kube/config` unless overridden
- frontend dev server at `http://localhost:5173`
- docs dev server at `http://localhost:3000/docs/`
