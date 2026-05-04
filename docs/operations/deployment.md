# Deployment

## Runtime Shape

- kubecrux can ship as a single-project application container
- `web` builds the Vite SPA console and is embedded into the server binary at build time
- `docs` builds the Docusaurus site and is embedded into the server binary at build time
- `cmd/server` serves the HTTP API, the SPA, and `/docs/`
- PostgreSQL is the durable system of record
- Redis backs cache, session state, locks, and transient coordination
- cluster credentials are provided by environment configuration or future secret providers

## Repo Deployment Assets

The canonical deployment assets live under the repo-root `deploy/` directory.

- `deploy/docker/Dockerfile.single-project`
- `deploy/config/config.api.single-project.yaml`
- `deploy/compose/docker-compose.single-project.yml`
- `deploy/k8s/kubecrux-single-project.yaml`
- `deploy/helm/kubecrux/`

Use these paths as the default baseline for image build, local stack startup, raw Kubernetes rollout, and Helm packaging.

## Quick Commands

Build the application image:

```bash
docker build -f deploy/docker/Dockerfile.single-project -t kubecrux:single-project .
```

Start the local single-project stack:

```bash
docker compose -f deploy/compose/docker-compose.single-project.yml up -d --build
```

Lint the Helm chart:

```bash
helm lint deploy/helm/kubecrux
```

## Local Run Assumptions

- PostgreSQL at `localhost:5432`, database `kubecrux`, user `pgsql`, password `pgsql`
- Redis at `localhost:6379`
- kubeconfig available at `$HOME/.kube/config` unless overridden
- frontend dev server at `http://localhost:5173`
- docs dev server at `http://localhost:3000/docs/`
