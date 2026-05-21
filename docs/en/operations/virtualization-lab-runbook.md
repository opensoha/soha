# KubeVirt / PVE Virtualization Lab Runbook

## Goal

This runbook verifies the kubecrux virtualization workflow in a real lab environment.

Scope:

- KubeVirt cluster connection, DataSource / PVC preparation, VM sync, creation, and power operations
- Proxmox VE connection, template / ISO preparation, VM sync, creation, and power operations
- Cancel, retry, metrics checks, and common troubleshooting
- End-to-end validation checklist for a real environment

Priority labels:

- P1: required before demo or production-like validation
- P2: recommended for stability, troubleshooting, and regression coverage

## P1 Prerequisites

### Common Prerequisites

- kubecrux server is running and database migrations are complete.
- The operator account has the required permissions for virtualization read, create, update, delete, and power operations.
- The lab network allows kubecrux server to reach the Kubernetes API Server or PVE API.
- The lab environment is isolated from production VMs, production templates, and production storage pools.
- All lab VMs use a clear prefix, for example `kc-lab-*`.
- Prepare an execution record for each run, including operator, environment, cluster or PVE endpoint, VM name, start time, end time, and result.

### KubeVirt Prerequisites

- The Kubernetes cluster is reachable and KubeVirt is installed.
- CDI is installed when DataVolume, DataSource, or clone flows are required.
- At least one StorageClass is available.
- The lab namespace exists, for example `virt-lab`.
- The kubeconfig or agent account used by kubecrux can access:
  - `virtualmachines` and `virtualmachineinstances` under `kubevirt.io`
  - `datavolumes` and `datasources` under `cdi.kubevirt.io`
  - core `persistentvolumeclaims`, `pods`, and `events`
  - `pods/metrics` or Prometheus query permissions for metrics

Check commands:

```bash
kubectl get kubevirt -A
kubectl get storageclass
kubectl get ns virt-lab
kubectl auth can-i list virtualmachines.kubevirt.io -n virt-lab
kubectl auth can-i create datavolumes.cdi.kubevirt.io -n virt-lab
```

### PVE Prerequisites

- The Proxmox VE node or cluster API is reachable from kubecrux server.
- An API Token or controlled account is available. Prefer granting only lab resource pool, lab storage pool, and lab node permissions.
- PVE has usable storage:
  - VM disk storage, for example `local-lvm`
  - ISO storage, for example `local`
  - template or cloud-init image storage
- PVE firewall, reverse proxy, and certificate rules are confirmed.
- A lab VMID range is reserved, for example `9000-9099`.

Example check commands:

```bash
pvesh get /version
pvesh get /nodes
pvesh get /storage
```

## P1 Connection Configuration

### KubeVirt Direct Kubernetes Mode

1. Add a cluster in kubecrux cluster management.
2. Select direct kubeconfig mode.
3. Fill in cluster name, environment, labels, and kubeconfig.
4. Save and run the connection test.
5. Open platform resource pages and confirm namespaces, pods, and events can be listed.

Acceptance:

- The cluster status is healthy or available.
- The `virt-lab` namespace is visible.
- kubecrux can read KubeVirt and CDI resources. If the dedicated virtualization page is not available yet, backend connection and base resource reads must still pass.

### KubeVirt Agent Mode

1. Start kubecrux agent inside the target cluster network.
2. Configure the agent with a kubeconfig that can access KubeVirt resources.
3. Register the cluster in kubecrux with agent mode.
4. Confirm the agent endpoint and token match.
5. Run connection test and resource sync.

Acceptance:

- The agent health endpoint is healthy.
- kubecrux does not mark the cluster as offline.
- If the current agent does not support specific KubeVirt CRUD operations, the UI or operation result must state unsupported instead of pretending success.

### PVE Connection

1. Create a lab API Token in PVE.
2. Record endpoint, realm, token id, token secret, node name, and default storage pool.
3. Add a PVE connection in kubecrux virtualization connection settings.
4. Save and run the connection test.
5. Sync nodes, storage, templates, ISOs, and VM list.

Acceptance:

- PVE version and nodes can be read.
- Lab storage pools can be read.
- Permission errors return clear errors instead of degrading into empty lists.

## P1 KubeVirt DataSource / PVC Preparation

### DataSource Flow

Use this when CDI DataSource is enabled.

Example:

```yaml
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataSource
metadata:
  name: ubuntu-2204
  namespace: virt-lab
spec:
  source:
    pvc:
      name: ubuntu-2204-golden
      namespace: virt-lab
```

Checks:

```bash
kubectl get datasource -n virt-lab
kubectl describe datasource ubuntu-2204 -n virt-lab
kubectl get pvc ubuntu-2204-golden -n virt-lab
```

Acceptance:

- The PVC referenced by the DataSource exists and is `Bound`.
- The StorageClass supports the access mode required by clone or import.

### PVC Flow

Use this when cloning directly from a golden PVC.

Checks:

```bash
kubectl get pvc -n virt-lab
kubectl describe pvc ubuntu-2204-golden -n virt-lab
```

Acceptance:

- The golden PVC is not reused as a mutable runtime disk for lab VMs.
- Each lab VM gets an independent PVC or DataVolume.
- PVC size, access mode, and volumeMode match the VM disk configuration.

## P1 PVE Template / ISO Preparation

### Template

1. Prepare the base VM.
2. Install cloud-init or the required lab agent.
3. Clean machine identity, temporary files, and shell history.
4. Convert the VM to a template.
5. Mark the template clearly, for example `kc-lab-ubuntu-2204-template`.

Checks:

```bash
qm list
qm config <template-vmid>
```

Acceptance:

- The template is stopped.
- The template uses lab storage.
- cloud-init settings can be overridden after clone.

### ISO

1. Upload the ISO to lab ISO storage.
2. Confirm the ISO can be selected on the target node.
3. Record the ISO path, for example `local:iso/ubuntu-22.04-live-server-amd64.iso`.

Check:

```bash
pvesh get /nodes/<node>/storage/local/content --content iso
```

Acceptance:

- The ISO list can be synced into kubecrux.
- ISO name and path are displayed consistently.

## P1 Sync Flow

### KubeVirt Sync

1. Select the target cluster and namespace.
2. Trigger virtualization resource sync.
3. Confirm VM, VMI, DataSource, and PVC counts.
4. Compare with kubectl output.

Check:

```bash
kubectl get vm,vmi,datasource,pvc -n virt-lab
```

Acceptance:

- kubecrux list counts match kubectl output.
- Namespace filtering works.
- Empty namespace means all-namespace aggregation, and returned rows must include namespace.

### PVE Sync

1. Select the PVE connection.
2. Trigger sync for nodes, storage, templates, ISOs, and VMs.
3. Compare with PVE console or `pvesh` output.

Checks:

```bash
pvesh get /nodes/<node>/qemu
pvesh get /nodes/<node>/storage
```

Acceptance:

- VMID, name, node, status, CPU, memory, and disk fields are consistent.
- Templates and normal VMs can be distinguished.
- Connection failure, offline nodes, and unavailable storage show explicit status.

## P1 Create VM

### KubeVirt From DataSource

1. Select the KubeVirt cluster and `virt-lab` namespace.
2. Select a DataSource, for example `ubuntu-2204`.
3. Enter VM name, for example `kc-lab-kv-001`.
4. Configure CPU, memory, disk size, network, and cloud-init.
5. Submit create.
6. Wait for DataVolume / PVC preparation.
7. Start the VM.

Checks:

```bash
kubectl get vm kc-lab-kv-001 -n virt-lab
kubectl get vmi kc-lab-kv-001 -n virt-lab
kubectl get dv,pvc -n virt-lab | grep kc-lab-kv-001
kubectl describe vm kc-lab-kv-001 -n virt-lab
```

Acceptance:

- VM object is created.
- Independent PVC or DataVolume is created.
- VMI enters Running.
- Events do not show image pull, PVC binding, or scheduling failures.

### KubeVirt From PVC

1. Select the golden PVC.
2. Select clone or new PVC strategy.
3. Enter VM name, CPU, memory, and network configuration.
4. Submit create and start the VM.

Acceptance:

- The golden PVC is not modified.
- The new PVC name can be traced back to the VM.
- VM deletion policy clearly states whether PVCs are retained.

### PVE From Template

1. Select PVE connection, node, and template.
2. Enter VM name, for example `kc-lab-pve-001`.
3. Select auto VMID assignment or the reserved lab VMID range.
4. Configure CPU, memory, disk, network, and cloud-init.
5. Submit clone.
6. Start the VM.

Checks:

```bash
qm list
qm config <vmid>
qm status <vmid>
```

Acceptance:

- Clone task succeeds.
- VMID does not conflict.
- VM configuration matches the submitted form.
- Status is running after start.

### PVE From ISO

1. Select PVE connection, node, and ISO.
2. Enter VM name and VMID.
3. Configure CPU, memory, disk, and network.
4. Mount ISO and set boot order.
5. Submit create.
6. Start the VM and finish installation through console.

Acceptance:

- ISO path is mounted correctly.
- Disk is created on lab storage.
- VM reaches the installer screen.

## P1 Power Operations

### Supported Operations

- start
- stop
- shutdown
- reboot
- reset
- suspend, if supported by backend and the underlying platform

### KubeVirt Checks

```bash
kubectl get vm,vmi -n virt-lab
kubectl describe vm <name> -n virt-lab
```

Acceptance:

- start creates a VMI and reaches Running.
- stop deletes the VMI or transitions the VM to stopped.
- shutdown timeout and force stop behavior are clearly communicated.

### PVE Checks

```bash
qm status <vmid>
```

Acceptance:

- Power task succeeds.
- kubecrux sync reflects the latest power state.
- Repeated clicks on the same power action do not cause unexplained state flips.

## P1 Cancel / Retry

### Cancel

Use cases:

- VM creation waits too long for PVC binding.
- PVE clone or ISO create task remains queued or running.
- Power operation is stuck.

Acceptance:

- Successful cancellation moves the task to `canceled` or an equivalent terminal state.
- Intermediate resources have an explicit policy: retain, auto-clean, or require manual cleanup.
- Operations that already reached the underlying platform and cannot be canceled must show the real state and manual follow-up steps.

### Retry

Use cases:

- Temporary network failure.
- Retry after token or certificate fix.
- Retry after StorageClass, DataSource, template, or ISO fix.

Acceptance:

- Retry creates a new attempt and does not overwrite the old failed record.
- Connection and underlying resource state are refreshed before retry.
- After the root cause is fixed, retry can reach a successful terminal state.

## P1 Metrics Checks

### KubeVirt Metrics

Possible sources:

- Kubernetes Metrics API
- Prometheus
- KubeVirt exporter or virt-handler metrics

Recommended checks:

- VM CPU usage
- VM memory usage
- VMI phase
- pod scheduling status
- PVC capacity and binding status
- network receive/transmit rate, if monitoring is configured

Example commands:

```bash
kubectl top pod -n virt-lab
kubectl get vmi -n virt-lab
```

Acceptance:

- kubecrux metric trends match sampled Prometheus or kubectl results.
- Missing metrics are shown as not configured or no data, not as zero.

### PVE Metrics

Possible sources:

- PVE API current status
- PVE RRD data
- External Prometheus exporter, if configured

Recommended checks:

- VM CPU
- VM memory
- VM disk read/write
- VM network in/out
- node online status
- storage usage

Acceptance:

- Running VMs have CPU and memory status.
- Stopped VMs do not show fabricated runtime metrics.
- PVE API or RRD failure is shown explicitly.

## P2 Common Troubleshooting

### KubeVirt Connection Failure

Checks:

```bash
kubectl cluster-info
kubectl get apiservices | grep kubevirt
kubectl get kubevirt -A
```

Actions:

- Confirm kubeconfig endpoint, certificate, and context.
- Confirm kubecrux server or agent network access to the API Server.
- Confirm RBAC covers `kubevirt.io` and `cdi.kubevirt.io` API groups.

### DataSource or PVC Unavailable

Checks:

```bash
kubectl describe datasource <name> -n virt-lab
kubectl describe pvc <name> -n virt-lab
kubectl get events -n virt-lab --sort-by=.lastTimestamp
```

Actions:

- Fix StorageClass, access mode, or capacity.
- Confirm source PVC exists and is `Bound`.
- Check CDI importer / cloner pod logs.

### VM Scheduling Failure

Checks:

```bash
kubectl describe vmi <name> -n virt-lab
kubectl get pods -n virt-lab -o wide
kubectl describe pod <virt-launcher-pod> -n virt-lab
```

Actions:

- Check node resources, taints, affinity, and KVM support.
- Check network plugin and Multus configuration.
- Check image pull and PVC attach.

### PVE Authentication Failure

Check:

```bash
pvesh get /version
```

Actions:

- Confirm token id format, secret, and realm.
- Check whether PVE permissions cover the target node, storage, and VMID range.
- Check PVE API certificate or reverse proxy configuration.

### PVE Clone Failure

Checks:

```bash
qm config <template-vmid>
pvesh get /nodes/<node>/tasks/<upid>/status
```

Actions:

- Confirm template state and target storage capacity.
- Confirm VMID is not occupied.
- Check linked clone and full clone requirements for the storage type.

### Power Operation Stuck

Actions:

- Refresh underlying state first to avoid resubmitting based on stale state.
- For KubeVirt, check VMI, virt-launcher pod, and events.
- For PVE, check task status, VM lock, and qemu guest agent state.
- When force operation is required, record the reason and underlying command output.

### Empty Metrics

Actions:

- Confirm metrics-server, Prometheus, or PVE RRD is available.
- Confirm kubecrux monitoring settings point to the correct endpoint.
- Confirm the VM was running during the query window.
- The page should distinguish not configured, no data, and query failure.

## P2 Cleanup

### KubeVirt

```bash
kubectl delete vm kc-lab-kv-001 -n virt-lab
kubectl delete dv,pvc -n virt-lab -l app.kubernetes.io/managed-by=kubecrux-lab
kubectl get vm,vmi,dv,pvc -n virt-lab
```

Acceptance:

- VM and VMI are deleted.
- Temporary PVCs / DataVolumes are cleaned according to policy.
- Golden PVC and DataSource are retained.

### PVE

```bash
qm shutdown <vmid>
qm destroy <vmid> --purge
qm list
```

Acceptance:

- Lab VM is deleted.
- Template and ISO are retained.
- The lab VMID range has no unexpected leftover resources.

## Real Environment End-to-End Checklist

### KubeVirt P1

- [ ] kubecrux can connect to the KubeVirt cluster.
- [ ] `virt-lab` namespace can be read.
- [ ] DataSource or golden PVC can be read.
- [ ] VM creation from DataSource succeeds.
- [ ] VM creation from PVC succeeds without modifying the golden PVC.
- [ ] VM start / stop / reboot each succeeds at least once.
- [ ] A creating task can be canceled and the result is traceable.
- [ ] A failed task can be retried successfully after fixing the root cause.
- [ ] VM metrics show real data or an explicit not configured state.
- [ ] kubectl and kubecrux list, status, and key fields are consistent.

### PVE P1

- [ ] kubecrux can connect to PVE API.
- [ ] Nodes, storage, templates, ISOs, and VMs can be synced.
- [ ] VM clone from template succeeds.
- [ ] VM creation from ISO succeeds.
- [ ] VM start / shutdown / stop / reboot each succeeds at least once.
- [ ] Clone or create task can be canceled and the result is traceable.
- [ ] A failed task can be retried successfully after fixing the root cause.
- [ ] VM metrics show real data or an explicit not configured state.
- [ ] PVE console, `qm` / `pvesh`, and kubecrux state are consistent.

### P2 Regression

- [ ] Insufficient permissions return clear errors.
- [ ] Insufficient storage returns clear errors.
- [ ] Missing DataSource / PVC returns clear errors.
- [ ] Missing template / ISO returns clear errors.
- [ ] Sync can recover after connection interruption and restoration.
- [ ] Repeated power operation clicks do not create uncontrolled duplicate tasks.
- [ ] Cleanup does not delete golden PVC, DataSource, template, or ISO.

## Exercise Record Template

```text
Date:
Operator:
Environment:
KubeVirt cluster:
PVE endpoint:
Namespace:
VM prefix:
Validated items:
Failed items:
Underlying command output location:
Screenshots or log location:
Conclusion:
Follow-up actions:
```
