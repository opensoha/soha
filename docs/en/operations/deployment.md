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

## Virtualization Lab Notes

- The local k3s cluster started by `make init-cluster` can be used for KubeVirt API-path validation when the underlying Linux node exposes `/dev/kvm` and supports privileged workloads.
- On Docker Desktop for macOS or other environments without Linux KVM passthrough, use local k3s only for control-plane or software-emulation tests.
- Proxmox VE can run inside this lab only as a full KubeVirt VM; otherwise it must run as a standalone external host or lab VM. kubecrux connects to it through the PVE API instead of deploying PVE as a Kubernetes Pod.
- Use `make init-pve-vm` to start a KVM-enabled local k3s profile, install KubeVirt/CDI, and create the `virt-lab/pve-lab` VM. After the ISO installer finishes, run `make pve-vm-boot-root`.
- See [KubeVirt / PVE Virtualization Lab Runbook](./virtualization-lab-runbook.md) for the full topology and checklist.
