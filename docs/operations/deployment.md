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

The main image and compose assets now live at the repo root.

- `Dockerfile`
- `docker-compose.yaml`
- `configs/config.yaml`
- `deployment.yaml`
- `chart/`

Use these paths as the default baseline for image build, local stack startup, raw Kubernetes rollout, and Helm packaging.

## Quick Commands

Build the application image:

```bash
docker build -t soha:single-project .
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

- PostgreSQL at `localhost:5432`, database `soha`, user `pgsql`, password `pgsql`
- kubeconfig available at `$HOME/.kube/config` unless overridden
- frontend dev server at `http://localhost:5173`
- docs dev server at `http://localhost:3000/docs/`

## PostgreSQL 18.4 Upgrade Note

New local volumes and fresh cluster installs use PostgreSQL 18.4. If an existing environment already has a PostgreSQL 16 data directory, do not point the 18.4 image at the same volume directly. Use `pg_dump`/`pg_restore`, logical backup restore, or a controlled `pg_upgrade` path. For disposable local development data, remove the old PostgreSQL volume and recreate the stack. Older compose runs may have created `kubecrux_soha-postgres-data`; current compose pins `soha-postgres-data`.

## Virtualization Lab Notes

- The local k3s cluster started by `make init-cluster` can be used for KubeVirt API-path validation when the underlying Linux node exposes `/dev/kvm` and supports privileged workloads.
- On Docker Desktop for macOS or other environments without Linux KVM passthrough, use local k3s only for control-plane or software-emulation tests.
- Proxmox VE can run inside this lab only as a full KubeVirt VM; otherwise it must run as a standalone external host or lab VM. soha connects to it through the PVE API instead of deploying PVE as a Kubernetes Pod.
- Use `make init-pve-vm` to start a KVM-enabled local k3s profile, install KubeVirt/CDI, and create the `virt-lab/pve-lab` VM. After the ISO installer finishes, run `make pve-vm-boot-root`.
- See [KubeVirt / PVE 虚拟化实验环境 Runbook](./virtualization-lab-runbook.md) for the full topology and checklist.
