# KubeVirt Lab Manifests

This directory contains lab-only manifests for running virtualization targets inside a KubeVirt-enabled Kubernetes cluster.

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

Then connect kubecrux to PVE with endpoint `https://127.0.0.1:8006` when kubecrux runs on the host. If kubecrux runs in the compose app container, use `https://k3s:30006`.

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

On arm64 lab hosts, the amd64 Proxmox VE ISO cannot be scheduled as a real KubeVirt VM. Use the mock API for kubecrux connection-flow validation:

```bash
make deploy-pve-mock
```

Use `http://127.0.0.1:8006` from a host-run kubecrux server, or `http://k3s:30006` from the compose app container.
