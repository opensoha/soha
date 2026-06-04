# Deployment

## Runtime Shape

- `web` builds the Vite SPA console
- `cmd/server` serves the HTTP API
- `docs` builds the Docusaurus site
- PostgreSQL is the durable system of record
- deployment assets pin PostgreSQL 18.4 for fresh local, manifest, and Helm installs
- cluster credentials are provided by environment configuration or future secret providers

## Repo Deployment Assets

- `deploy/Dockerfile`
- `deploy/Dockerfile.hermes-agent-runner`
- `deploy/docker-compose.yaml`
- `configs/config.yaml`
- `configs/config.compose.yaml`
- `deploy/deployment.yaml`
- `deploy/chart/`

`configs/config.compose.yaml` is the app-container config for compose; it points the database host at the `postgres` service and does not seed host-local kubeconfig paths.

## Quick Commands

Build the application image:

```bash
docker build -f deploy/Dockerfile -t soha:single-project .
```

Start the local single-project stack:

```bash
docker compose -f deploy/docker-compose.yaml up -d --build
```

Lint the Helm chart:

```bash
helm lint deploy/chart
```

Run Hermes as the first external Agent Runtime provider:

```bash
make init-hermes
```

## Local Run Assumptions

- PostgreSQL at `localhost:5432`, database `soha`, user `pgsql`, password `pgsql`
- kubeconfig available at `$HOME/.kube/config` unless overridden
- frontend dev server at `http://localhost:5173`
- docs dev server at `http://localhost:3000/docs/`

## Hermes Agent Runner with Docker

Hermes is deployed as a provider runner, not as a browser-facing dependency of the console. The runner image in [../../deploy/Dockerfile.hermes-agent-runner](../../deploy/Dockerfile.hermes-agent-runner) inherits from the official `nousresearch/hermes-agent` image and adds the soha `cmd/agent` binary. The unified compose file in [../../deploy/docker-compose.yaml](../../deploy/docker-compose.yaml) defines the local soha stack and optional Hermes runner service:

- mounts persistent Hermes state at the `soha-hermes-data` volume (`/opt/data`)
- mounts provider workspaces at `soha-hermes-runtime` (`/var/lib/soha-agent-runtime`)
- claims only `hermes` Agent Runtime runs
- executes Hermes through `hermes chat -Q -q`
- callbacks to the soha control plane with status, tool calls, and `AnalysisArtifact` results

Initialize Hermes once if provider credentials or local Hermes state need setup:

```bash
docker compose -f deploy/docker-compose.yaml --profile hermes-setup run --rm hermes-agent-setup
```

Start the runner against a host-run local development API:

```bash
make init-hermes
```

The default `init-hermes` endpoint is `http://host.docker.internal:8080`, which matches `make dev` when the Go API runs on the host. For a remote control plane, override the control-plane URL and token:

```bash
HERMES_CONTROL_PLANE_URL=http://host.docker.internal:8080 \
SOHA_EXECUTION_RUNNER_TOKEN=replace-with-runtime-token \
make init-hermes
```

Operational checks:

```bash
docker compose -f deploy/docker-compose.yaml logs -f hermes-agent-runner
docker compose -f deploy/docker-compose.yaml exec hermes-agent-runner hermes --version
curl -s http://localhost:8080/api/v1/copilot/agent-runs
```

Do not commit real provider keys or runner tokens. Store Hermes model credentials in the mounted Hermes data volume through the `hermes-setup` compose profile or inject them through your runtime secret manager.

## PostgreSQL 18.4 Upgrade Note

New local volumes and fresh cluster installs use PostgreSQL 18.4. PostgreSQL 18 stores its default `PGDATA` below `/var/lib/postgresql/18/docker`, so compose, raw Kubernetes, and Helm mounts keep the persistent volume at `/var/lib/postgresql`. If an existing environment already has a PostgreSQL 16 data directory, do not point the 18.4 image at the same volume directly. Use `pg_dump`/`pg_restore`, logical backup restore, or a controlled `pg_upgrade` path. For disposable local development data, remove the old PostgreSQL volume and recreate the stack. Current compose pins `soha-postgres-data`.

## Virtualization Runtime Notes

- Docker Desktop for macOS, especially Apple Silicon, is not a reliable validation path for local KubeVirt or nested Proxmox VE labs.
- Keep local k3s for platform control-plane and Kubernetes API-path testing.
- Validate KubeVirt and PVE features against real external servers or dedicated lab hosts, then register those endpoints in soha.
- See [KubeVirt / PVE Virtualization Lab Runbook](./virtualization-lab-runbook.md) for external environment prerequisites and checklist.
