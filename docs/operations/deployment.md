# Deployment

## Runtime Shape

- `web` builds the Vite SPA console
- `cmd/server` serves the HTTP API
- `docs` builds the VitePress site
- PostgreSQL is the durable system of record
- Redis backs cache, session state, locks, and transient coordination
- cluster credentials are provided by environment configuration or future secret providers

## Local Run Assumptions

- PostgreSQL at `localhost:5432`, database `kubecrux`, user `pgsql`, password `pgsql`
- Redis at `localhost:6379`
- kubeconfig available at `$HOME/.kube/config` unless overridden
- frontend dev server at `http://localhost:5173`
- docs dev server at `http://localhost:5174`
