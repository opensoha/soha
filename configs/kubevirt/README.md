# KubeVirt Lab Manifests

This directory contains lab-only manifests for running virtualization targets inside a KubeVirt-enabled Kubernetes cluster.

## Recommended Local Flows

For local virtualization development there are three useful paths:

| Flow | Command | Purpose |
| --- | --- | --- |
| KubeVirt on k3s | `make init-kubevirt-lab` | Starts the local k3s compose service with KubeVirt-friendly mounts/devices, then installs KubeVirt and CDI. |
| PVE as a KubeVirt VM | `make init-pve-vm` | Boots the real Proxmox VE installer ISO inside KubeVirt for full nested-virtualization experiments. |
| PVE as a Docker container | `make pve-docker-up` | Starts a lab-only containerized PVE endpoint based on `ghcr.io/longqt-sea/proxmox-ve` for adapter and API-flow development. |

`make init-virtualization-lab` starts the KubeVirt-on-k3s lab and the Docker-based PVE lab together.

## Proxmox VE VM

`pve-vm.yaml` creates:

- namespace `virt-lab`
- CDI DataVolume `pve-installer-iso` imported from the Proxmox VE ISO URL
- CDI DataVolume `pve-rootdisk` as the VM installation disk
- KubeVirt VirtualMachine `pve-lab`
- NodePort Service `pve-lab` exposing:
  - PVE API and UI: `https://127.0.0.1:8006` when used with the local k3s compose override
  - SSH: `127.0.0.1:2222`

The VM boots from the installer ISO first. After the Proxmox installer completes, run:

```bash
make pve-vm-boot-root
```

Then connect soha to PVE with endpoint `https://127.0.0.1:8006` when soha runs on the host. If soha runs in the compose app container, use `https://k3s:30006`.

This lab path requires a Linux k3s node with `/dev/kvm` exposed. It is not expected to work on Docker Desktop for macOS unless the environment provides Linux KVM passthrough.

On lab hosts without `/dev/kvm`, enable KubeVirt software emulation before starting the VM:

```bash
make enable-kubevirt-emulation
```

The manifest sets the VM guest architecture to `amd64` because Proxmox VE ISO images are amd64. Running an amd64 PVE installer on an arm64 host through software emulation is only a fallback for connection-flow experiments and can be very slow.

If `virt-handler` fails with `path "/var/run/kubevirt" is mounted on "/" but it is not a shared mount`, run:

```bash
make fix-kubevirt-mounts
```

## PVE API Mock

On arm64 lab hosts, the amd64 Proxmox VE ISO cannot be scheduled as a real KubeVirt VM. Use the mock API for soha connection-flow validation:

```bash
make deploy-pve-mock
```

Use `http://127.0.0.1:8006` from a host-run soha server, or `http://k3s:30006` from the compose app container.

## Docker-based Proxmox VE Lab

`configs/proxmox/docker-compose.pve.yaml` starts a containerized Proxmox VE lab inspired by [LongQT-sea/containerized-proxmox](https://github.com/LongQT-sea/containerized-proxmox). It is useful when you need a reachable PVE API endpoint for soha adapter development but do not need to validate a full KubeVirt-backed PVE installation.

```bash
make pve-docker-up
make pve-docker-status
```

Default endpoints and credentials:

- host-run soha: `https://127.0.0.1:8006`
- compose-run soha: `https://host.docker.internal:8006`
- PVE SSH: `127.0.0.1:2222`
- root password: `soha`

Override defaults from make:

```bash
make pve-docker-up PVE_DOCKER_PASSWORD=change-me PVE_DOCKER_UI_PORT=18006
```

This lab uses a privileged container with broad device access. Keep it on isolated development machines only, do not treat it as production PVE, and do not use real credentials or production VM storage.
