# Deployment

## Runtime Shape

- soha can ship as a single-project application container
- `web` builds the Vite SPA console and is embedded into the server binary at build time
- `docs` builds the Docusaurus site and is embedded into the server binary at build time
- `cmd/server` serves the HTTP API, the SPA, and `/docs/`
- PostgreSQL is the durable system of record
- deployment assets pin PostgreSQL 18.4 for fresh local, manifest, and Helm installs
- cluster credentials are provided by environment configuration or future secret providers

## Repo Deployment Assets

Deployment assets now live under `deploy/`.

- `deploy/Dockerfile`
- `deploy/Dockerfile.hermes-agent-runner`
- `deploy/docker-compose.yaml`
- `configs/config.yaml`
- `configs/config.compose.yaml`
- `deploy/deployment.yaml`
- `deploy/chart/`

Use these paths as the default baseline for image build, local stack startup, raw Kubernetes rollout, Helm packaging, and the optional Hermes provider runner. `configs/config.compose.yaml` is the app-container config for compose; it points the database host at the `postgres` service and does not seed host-local kubeconfig paths.

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
make deploy-hermes-setup
make deploy-hermes-runner-up
```

## Local Run Assumptions

- PostgreSQL at `localhost:5432`, database `soha`, user `pgsql`, password `pgsql`
- kubeconfig available at `$HOME/.kube/config` unless overridden
- frontend dev server at `http://localhost:5173`
- docs dev server at `http://localhost:3000/docs/`

## Hermes Agent Runner with Docker

Hermes is deployed as a provider runner, not as a browser-facing dependency of the console. The runner image in [../../deploy/Dockerfile.hermes-agent-runner](../../deploy/Dockerfile.hermes-agent-runner) inherits from the official `nousresearch/hermes-agent` image and adds the soha `cmd/agent` binary. The unified compose file in [../../deploy/docker-compose.yaml](../../deploy/docker-compose.yaml) defines both the local soha stack and the optional Hermes runner services:

- mounts persistent Hermes state at the `soha-hermes-data` volume (`/opt/data`)
- mounts provider workspaces at `soha-hermes-runtime` (`/var/lib/soha-agent-runtime`)
- claims only `hermes` Agent Runtime runs
- executes Hermes through `hermes chat -Q -q`
- callbacks to the soha control plane with status, tool calls, and `AnalysisArtifact` results

Initialize Hermes once before starting the runner:

```bash
make deploy-hermes-setup
```

Start the runner against the local compose stack:

```bash
docker compose -f deploy/docker-compose.yaml up -d postgres soha
make deploy-hermes-runner-up
```

The default endpoint is `http://soha:8080` on the `soha_default` Docker network and the default runner token matches `configs/config.yaml` for local development. For a host-run or remote control plane, override both values:

```bash
SOHA_CONTROL_PLANE_URL=http://host.docker.internal:8080 \
SOHA_EXECUTION_RUNNER_TOKEN=replace-with-runtime-token \
make deploy-hermes-runner-up
```

Operational checks:

```bash
docker compose -f deploy/docker-compose.yaml logs -f hermes-agent-runner
docker compose -f deploy/docker-compose.yaml exec hermes-agent-runner hermes --version
curl -s http://localhost:8080/api/v1/copilot/agent-runs
```

Do not commit real provider keys or runner tokens. Store Hermes model credentials in the mounted Hermes data volume through `make deploy-hermes-setup` or inject them through your runtime secret manager.

## PostgreSQL 18.4 Upgrade Note

New local volumes and fresh cluster installs use PostgreSQL 18.4. PostgreSQL 18 stores its default `PGDATA` below `/var/lib/postgresql/18/docker`, so compose, raw Kubernetes, and Helm mounts keep the persistent volume at `/var/lib/postgresql`. If an existing environment already has a PostgreSQL 16 data directory, do not point the 18.4 image at the same volume directly. Use `pg_dump`/`pg_restore`, logical backup restore, or a controlled `pg_upgrade` path. For disposable local development data, remove the old PostgreSQL volume and recreate the stack. Current compose pins `soha-postgres-data`.

## Virtualization Lab Notes

- The local k3s cluster started by `make init-cluster` can be used for KubeVirt API-path validation when the underlying Linux node exposes `/dev/kvm` and supports privileged workloads.
- On Docker Desktop for macOS or other environments without Linux KVM passthrough, use local k3s only for control-plane or software-emulation tests.
- Proxmox VE can run inside this lab only as a full KubeVirt VM; otherwise it must run as a standalone external host or lab VM. soha connects to it through the PVE API instead of deploying PVE as a Kubernetes Pod.
- Use `make init-pve-vm` to start a KVM-enabled local k3s profile, install KubeVirt/CDI, and create the `virt-lab/pve-lab` VM. After the ISO installer finishes, run `make pve-vm-boot-root`.
- See [KubeVirt / PVE 虚拟化实验环境 Runbook](./virtualization-lab-runbook.md) for the full topology and checklist.
