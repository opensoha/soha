# KubeVirt / PVE 虚拟化实验环境 Runbook

## 目标

本 runbook 用于在真实实验环境中验证 soha 的虚拟化闭环能力。

覆盖范围：

- KubeVirt 集群连接、DataSource / PVC 准备、VM 同步、创建和电源操作
- Proxmox VE 连接、template / ISO 准备、VM 同步、创建和电源操作
- 取消、重试、指标检查、常见故障排查
- 真实环境闭环验证清单

优先级说明：

- P1：上线或演示前必须完成
- P2：建议完成，用于稳定性、排障和回归验证

## 实验部署拓扑

soha 的虚拟化验证环境应拆成两个不同运行面：

- KubeVirt 运行在 Kubernetes 集群内，可以使用 k3s 作为轻量 Kubernetes 发行版。
- Proxmox VE 运行在 KubeVirt VM、独立裸金属宿主机、独立 Debian 宿主机，或仅用于实验的其他嵌套虚拟机中。
- soha server 不托管 PVE 本身，只通过 PVE API endpoint、token、节点名和存储池配置接入 PVE。

推荐的实验拓扑：

```text
soha server + PostgreSQL
        |
        +-- external Kubernetes cluster with KubeVirt/CDI
        |
        +-- KubeVirt-backed pve-lab VM or external Proxmox VE node API
```

### 本地开发边界

本地 Docker Desktop for macOS，尤其是 Apple Silicon，不再作为 KubeVirt 或嵌套 PVE 的推荐验证路径。根目录 `make init-cluster` 启动的 compose k3s 只用于 soha 平台连接、资源列表、作用域和 Kubernetes API 路径验证。

KubeVirt 与 PVE 功能验证应连接真实外部服务器或专用实验主机：

| 路径 | 适用场景 |
| --- | --- |
| 外部 KubeVirt 集群 | 验证 VM 同步、创建、电源操作、DataVolume/DataSource、指标和事件。 |
| 外部 Proxmox VE 节点或集群 | 验证 PVE 连接、模板/ISO、VM clone/create、电源操作、任务状态和指标。 |
| 支持 nested virtualization 的外部实验 VM | 仅用于隔离演示或适配测试，不作为生产能力依据。 |

### KubeVirt on k3s

k3s 可以作为 KubeVirt 的实验 Kubernetes 集群，但节点必须满足 KubeVirt 的虚拟化前提：

- k3s 节点运行在 Linux 上，并使用支持 KVM 的 x86_64 CPU。
- 宿主机已开启 VT-x 或 AMD-V，嵌套虚拟化环境还需要上层虚拟化平台透传 nested virtualization。
- 节点上存在并可访问 `/dev/kvm`，`kvm` 和 `vhost_net` 等内核模块可用。
- Kubernetes 允许 privileged workload；KubeVirt 需要运行 privileged DaemonSet。
- 容器运行时使用 KubeVirt 支持的 containerd 或 CRI-O。
- 至少有一个可用 StorageClass；如需 DataVolume、DataSource、上传或克隆镜像，需安装 CDI。

根目录 `make init-cluster` 启动的 compose k3s 适合做 soha 平台连接和 Kubernetes API 路径演示。若运行在 Docker Desktop for macOS 等没有向容器暴露 Linux KVM 的环境中，不应作为 KubeVirt 功能、性能或稳定性验收环境。

### PVE 是否能跑在 k3s 里

结论：可以，但只能作为 KubeVirt VM 跑；不能作为普通 Pod 或 Deployment 跑。

Proxmox VE 是完整虚拟化平台和宿主机操作系统栈，官方安装形态是 Proxmox ISO 裸金属安装，或在已有 Debian 系统上安装 PVE 软件包。它依赖 Proxmox VE Linux kernel、KVM、LXC、系统网络、存储管理、APT 仓库和长期运行的宿主机服务。Kubernetes Pod 共享节点内核，生命周期、网络、存储和 systemd 管理模型都不等价于 PVE 宿主机。

即使通过 privileged Pod、hostPath、systemd-in-container 等方式强行拼出 PVE 组件，本质上也是把 Kubernetes 节点当宿主机改造，风险包括：

- 覆盖或污染 k3s 节点的内核模块、网络桥、iptables/nftables 和存储配置。
- PVE 的 LXC/QEMU 生命周期与 Kubernetes Pod 生命周期冲突。
- VM、磁盘和网络状态无法被 Kubernetes 正确调度、迁移和恢复。
- 升级、故障恢复和权限边界不可控。

可接受的 PVE 实验形态包括：

- 在 KubeVirt 中启动完整 PVE VM，并通过 Service 暴露 8006 API。
- 独立 PVE 裸金属或独立 Debian 宿主机，soha 通过 API 接入。
- 在支持 nested virtualization 的外部虚拟化平台中启动一台完整 PVE VM，仅用于功能演示和适配测试。

### 关于 MCP skills 和虚拟化控制

KubeVirt 与 PVE 的常规控制能力仍应走 soha 后端的标准 API、任务和回调路径，不依赖大模型或 MCP。创建、同步、电源操作、取消、重试、审计和权限判断都必须在无 AI provider 的情况下可用。

MCP skills 更适合作为 AI 接入后的排障增强：例如把 PVE 事件、KubeVirt VMI 状态、Pod 日志、Prometheus 指标和操作日志组合成一次调查上下文。未接入大模型时，不应让 MCP 成为虚拟化基础功能的前置条件。

### 外部服务器接入路径

本仓库不再提供 Mac 本地 KubeVirt/PVE lab make 目标。虚拟化验证按外部环境接入：

1. 准备外部 KubeVirt 集群或 Proxmox VE 节点。
2. 确认 soha server 能访问目标 Kubernetes API Server 或 PVE API。
3. 在 soha 集群管理或虚拟化连接配置中登记 endpoint、token、节点名和存储池。
4. 使用本 runbook 后续检查项完成同步、创建、电源操作、指标和任务状态验证。

## P1 前置条件

### 通用条件

- soha server 已启动，数据库迁移已完成。
- 操作账号具备虚拟化资源查看、创建、更新、删除或电源操作所需权限。
- 实验网络允许 soha server 访问目标 Kubernetes API Server 或 PVE API。
- 已确认实验环境不会影响生产 VM、生产模板或生产存储池。
- 所有实验 VM 使用明确前缀，例如 `soha-lab-*`。
- 为每次演练准备记录表，至少包含操作人、环境、集群或 PVE endpoint、VM 名称、开始时间、结束时间和结果。

### KubeVirt 条件

- Kubernetes 集群可访问，并已安装 KubeVirt。
- 如需使用 DataVolume、DataSource 或克隆能力，已安装 CDI。
- 至少存在一个可用 StorageClass。
- 实验 namespace 已创建，例如 `virt-lab`。
- soha 使用的 kubeconfig 或 agent 账号具备以下资源权限：
  - `kubevirt.io` 下的 `virtualmachines`、`virtualmachineinstances`
  - `cdi.kubevirt.io` 下的 `datavolumes`、`datasources`
  - 核心资源 `persistentvolumeclaims`、`pods`、`events`
  - 指标读取所需的 `pods/metrics` 或 Prometheus 查询权限

检查命令：

```bash
kubectl get kubevirt -A
kubectl get storageclass
kubectl get ns virt-lab
kubectl auth can-i list virtualmachines.kubevirt.io -n virt-lab
kubectl auth can-i create datavolumes.cdi.kubevirt.io -n virt-lab
```

### PVE 条件

- Proxmox VE 节点或集群 API 可从 soha server 访问。
- 已准备 API Token 或受控账号，建议只授予实验资源池、实验存储池和实验节点权限。
- PVE 节点已配置可用存储：
  - VM 磁盘存储，例如 `local-lvm`
  - ISO 存储，例如 `local`
  - 模板或 cloud-init 镜像存储
- PVE 防火墙、反向代理或证书策略已确认。
- 实验 VMID 范围已预留，例如 `9000-9099`。

检查命令示例：

```bash
pvesh get /version
pvesh get /nodes
pvesh get /storage
```

## P1 连接配置

### KubeVirt 直连 Kubernetes

1. 在 soha 集群管理中新增集群。
2. 选择直连 kubeconfig 模式。
3. 填写集群名称、环境、标签和 kubeconfig。
4. 保存后执行连接测试。
5. 进入平台资源页确认 namespace、pod、event 可正常列出。

验收点：

- 集群状态为健康或可用。
- `virt-lab` namespace 可见。
- soha 能读取 KubeVirt 和 CDI 资源；如果页面尚未显示专用虚拟化对象，至少后端连接和基础资源读取必须成功。

### KubeVirt Agent 模式

1. 在目标集群网络内启动 soha agent。
2. agent 使用可访问 KubeVirt 资源的 kubeconfig。
3. 在 soha 中注册 agent 模式集群。
4. 确认 agent endpoint 和 token 匹配。
5. 执行连接测试和资源同步。

验收点：

- agent health endpoint 正常。
- soha 集群状态不应显示为离线。
- 如果当前 agent 还未支持某些 KubeVirt CRUD 能力，页面或操作结果必须明确提示不支持，而不是伪装成功。

### PVE 连接

1. 在 PVE 中创建实验用 API Token。
2. 记录 endpoint、realm、token id、token secret、节点名、默认存储池。
3. 在 soha 虚拟化连接配置中新增 PVE 连接。
4. 保存后执行连接测试。
5. 同步节点、存储、template、ISO 和 VM 列表。

验收点：

- PVE 版本和节点列表可读取。
- 实验存储池可读取。
- token 权限不足时应返回明确错误，不应退化为空列表。

## P1 KubeVirt DataSource / PVC 准备

### 使用 DataSource

适用于 CDI 已启用 DataSource 的环境。

示例：

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

检查：

```bash
kubectl get datasource -n virt-lab
kubectl describe datasource ubuntu-2204 -n virt-lab
kubectl get pvc ubuntu-2204-golden -n virt-lab
```

验收点：

- DataSource 指向的 PVC 存在并处于 `Bound`。
- clone 或 import 所需的 StorageClass 支持对应访问模式。

### 使用 PVC

适用于直接从 golden PVC 克隆的环境。

检查：

```bash
kubectl get pvc -n virt-lab
kubectl describe pvc ubuntu-2204-golden -n virt-lab
```

验收点：

- golden PVC 不直接作为实验 VM 的运行盘反复复用。
- 每个实验 VM 创建独立 PVC 或 DataVolume。
- PVC 容量、访问模式、volumeMode 与 VM 磁盘配置一致。

## P1 PVE Template / ISO 准备

### Template

1. 准备基础 VM。
2. 安装 cloud-init 或实验所需 agent。
3. 清理机器唯一标识、临时文件和 shell history。
4. 转换为 template。
5. 标记模板名称，例如 `soha-lab-ubuntu-2204-template`。

检查：

```bash
qm list
qm config <template-vmid>
```

验收点：

- template 处于 stopped 状态。
- template 使用实验存储池。
- cloud-init 配置可被 clone 后覆盖。

### ISO

1. 上传 ISO 到实验 ISO 存储。
2. 确认 ISO 可在目标节点上选择。
3. 记录 ISO 路径，例如 `local:iso/ubuntu-22.04-live-server-amd64.iso`。

检查：

```bash
pvesh get /nodes/<node>/storage/local/content --content iso
```

验收点：

- ISO 列表可被 soha 同步。
- ISO 名称和路径在页面显示一致。

## P1 同步流程

### KubeVirt 同步

1. 选择目标集群和 namespace。
2. 触发虚拟化资源同步。
3. 确认 VM、VMI、DataSource、PVC 资源数量。
4. 对比 kubectl 输出。

检查：

```bash
kubectl get vm,vmi,datasource,pvc -n virt-lab
```

验收点：

- soha 列表数量与 kubectl 输出一致。
- namespace 过滤生效。
- 空 namespace 表示全 namespace 聚合时，结果必须带 namespace 字段。

### PVE 同步

1. 选择 PVE 连接。
2. 触发节点、存储、template、ISO、VM 同步。
3. 对比 PVE 控制台或 `pvesh` 输出。

检查：

```bash
pvesh get /nodes/<node>/qemu
pvesh get /nodes/<node>/storage
```

验收点：

- VMID、名称、节点、状态、CPU、内存、磁盘字段一致。
- template 与普通 VM 可区分。
- 连接失败、节点离线、存储不可用必须有明确状态。

## P1 创建 VM

### KubeVirt 从 DataSource 创建

1. 选择 KubeVirt 集群和 `virt-lab` namespace。
2. 选择 DataSource，例如 `ubuntu-2204`。
3. 输入 VM 名称，例如 `soha-lab-kv-001`。
4. 配置 CPU、内存、磁盘容量、网络和 cloud-init。
5. 提交创建。
6. 等待 DataVolume / PVC 准备完成。
7. 启动 VM。

检查：

```bash
kubectl get vm soha-lab-kv-001 -n virt-lab
kubectl get vmi soha-lab-kv-001 -n virt-lab
kubectl get dv,pvc -n virt-lab | grep soha-lab-kv-001
kubectl describe vm soha-lab-kv-001 -n virt-lab
```

验收点：

- VM 对象创建成功。
- 独立 PVC 或 DataVolume 创建成功。
- VMI 进入 Running。
- 事件中没有镜像拉取、PVC 绑定或调度失败。

### KubeVirt 从 PVC 创建

1. 选择 golden PVC。
2. 选择 clone 或 new PVC 策略。
3. 输入 VM 名称、CPU、内存和网络配置。
4. 提交创建并启动。

验收点：

- 不直接修改 golden PVC。
- 新 PVC 名称可追踪到 VM。
- VM 删除策略清楚说明是否保留 PVC。

### PVE 从 Template 创建

1. 选择 PVE 连接、节点和 template。
2. 输入 VM 名称，例如 `soha-lab-pve-001`。
3. 选择 VMID 自动分配或实验 VMID 范围。
4. 配置 CPU、内存、磁盘、网络和 cloud-init。
5. 提交 clone。
6. 启动 VM。

检查：

```bash
qm list
qm config <vmid>
qm status <vmid>
```

验收点：

- clone task 成功。
- VMID 未冲突。
- VM 配置与提交表单一致。
- 启动后状态为 running。

### PVE 从 ISO 创建

1. 选择 PVE 连接、节点和 ISO。
2. 输入 VM 名称和 VMID。
3. 配置 CPU、内存、磁盘和网络。
4. 挂载 ISO 并设置启动顺序。
5. 提交创建。
6. 启动 VM 并通过控制台完成安装。

验收点：

- ISO 挂载路径正确。
- 磁盘落在实验存储池。
- VM 可进入安装界面。

## P1 电源操作

### 支持操作

- start
- stop
- shutdown
- reboot
- reset
- suspend，如果后端和底层平台支持

### KubeVirt 检查

```bash
kubectl get vm,vmi -n virt-lab
kubectl describe vm <name> -n virt-lab
```

验收点：

- start 后 VMI 创建并进入 Running。
- stop 后 VMI 被删除或 VM 状态转为 stopped。
- shutdown 超时后是否允许强制 stop 必须有明确提示。

### PVE 检查

```bash
qm status <vmid>
```

验收点：

- power task 成功返回。
- soha 状态同步能反映最新电源状态。
- 重复点击同一电源操作不会造成不可解释的状态翻转。

## P1 取消 / 重试

### 取消

适用场景：

- VM 创建任务长时间等待 PVC 绑定。
- PVE clone 或 ISO 创建任务仍处于排队或运行中。
- 电源操作卡住。

验收点：

- 取消成功后任务进入 canceled 或等价终态。
- 已创建的中间资源有明确处理策略：保留、自动清理或提示人工清理。
- 已到达底层平台且不可取消的操作，必须显示真实状态和后续人工处理步骤。

### 重试

适用场景：

- 临时网络失败。
- token 或证书修复后重试。
- StorageClass、DataSource、template 或 ISO 修复后重试。

验收点：

- 重试会创建新的任务尝试，不覆盖旧失败记录。
- 重试前重新读取连接和底层资源状态。
- 失败原因已修复时，重试能够推进到成功终态。

## P1 指标检查

### KubeVirt 指标

检查来源：

- Kubernetes Metrics API
- Prometheus
- KubeVirt exporter 或 virt-handler 指标

建议检查项：

- VM CPU 使用率
- VM 内存使用量
- VMI phase
- pod 调度状态
- PVC 容量和绑定状态
- 网络收发速率，如果监控链路已配置

命令示例：

```bash
kubectl top pod -n virt-lab
kubectl get vmi -n virt-lab
```

验收点：

- soha 页面指标与 Prometheus 或 kubectl 抽样结果趋势一致。
- 指标缺失时显示未配置或暂无数据，不显示为 0。

### PVE 指标

检查来源：

- PVE API current status
- PVE RRD 数据
- 外部 Prometheus exporter，如果已配置

建议检查项：

- VM CPU
- VM memory
- VM disk read/write
- VM network in/out
- node online 状态
- storage usage

验收点：

- running VM 有 CPU、内存状态。
- stopped VM 不应伪造运行时指标。
- PVE API 或 RRD 不可用时给出明确状态。

## P2 常见故障排查

### KubeVirt 连接失败

检查：

```bash
kubectl cluster-info
kubectl get apiservices | grep kubevirt
kubectl get kubevirt -A
```

处理：

- 确认 kubeconfig endpoint、证书和 context。
- 确认 soha server 或 agent 到 API Server 的网络。
- 确认 RBAC 覆盖 `kubevirt.io` 和 `cdi.kubevirt.io` API group。

### DataSource 或 PVC 不可用

检查：

```bash
kubectl describe datasource <name> -n virt-lab
kubectl describe pvc <name> -n virt-lab
kubectl get events -n virt-lab --sort-by=.lastTimestamp
```

处理：

- 修复 StorageClass、访问模式或容量。
- 确认 source PVC 存在且 `Bound`。
- 检查 CDI importer / cloner pod 日志。

### VM 调度失败

检查：

```bash
kubectl describe vmi <name> -n virt-lab
kubectl get pods -n virt-lab -o wide
kubectl describe pod <virt-launcher-pod> -n virt-lab
```

处理：

- 检查节点资源、taint、affinity 和 KVM 支持。
- 检查网络插件和 Multus 配置。
- 检查镜像拉取和 PVC attach。

### PVE 认证失败

检查：

```bash
pvesh get /version
```

处理：

- 确认 token id 格式、secret、realm。
- 检查 PVE 权限路径是否覆盖目标节点、存储和 VMID 范围。
- 检查 PVE API 证书或反向代理配置。

### PVE Clone 失败

检查：

```bash
qm config <template-vmid>
pvesh get /nodes/<node>/tasks/<upid>/status
```

处理：

- 确认 template 状态和目标存储容量。
- 确认 VMID 未被占用。
- 检查 linked clone 与 full clone 对存储类型的要求。

### 电源操作卡住

处理：

- 先刷新底层状态，避免基于旧状态重复提交。
- KubeVirt 检查 VMI、virt-launcher pod 和 events。
- PVE 检查 task status、VM lock 和 qemu guest agent 状态。
- 需要强制操作时记录原因和底层命令输出。

### 指标为空

处理：

- 确认 metrics-server、Prometheus 或 PVE RRD 可用。
- 确认 soha 监控配置指向正确 endpoint。
- 确认查询时间窗口内 VM 处于 running。
- 页面应区分未配置、暂无数据和查询失败。

## P2 清理

### KubeVirt

```bash
kubectl delete vm soha-lab-kv-001 -n virt-lab
kubectl delete dv,pvc -n virt-lab -l app.kubernetes.io/managed-by=soha-lab
kubectl get vm,vmi,dv,pvc -n virt-lab
```

验收点：

- VM 和 VMI 删除完成。
- 临时 PVC / DataVolume 按策略清理。
- golden PVC 和 DataSource 保留。

### PVE

```bash
qm shutdown <vmid>
qm destroy <vmid> --purge
qm list
```

验收点：

- 实验 VM 已删除。
- template 和 ISO 保留。
- 实验 VMID 范围无残留非预期资源。

## 真实环境闭环验证清单

### KubeVirt P1

- [ ] soha 能连接 KubeVirt 集群。
- [ ] `virt-lab` namespace 可读取。
- [ ] DataSource 或 golden PVC 可读取。
- [ ] 从 DataSource 创建 VM 成功。
- [ ] 从 PVC 创建 VM 成功，且未修改 golden PVC。
- [ ] VM start / stop / reboot 至少各成功一次。
- [ ] 创建中的任务可取消，取消结果可追踪。
- [ ] 失败任务修复原因后可重试成功。
- [ ] VM 指标显示真实数据或明确未配置状态。
- [ ] kubectl 与 soha 列表、状态和关键字段一致。

### PVE P1

- [ ] soha 能连接 PVE API。
- [ ] 节点、存储、template、ISO、VM 列表可同步。
- [ ] 从 template clone VM 成功。
- [ ] 从 ISO 创建 VM 成功。
- [ ] VM start / shutdown / stop / reboot 至少各成功一次。
- [ ] clone 或创建任务可取消，取消结果可追踪。
- [ ] 失败任务修复原因后可重试成功。
- [ ] VM 指标显示真实数据或明确未配置状态。
- [ ] PVE 控制台、`qm` / `pvesh` 与 soha 状态一致。

### P2 回归

- [ ] 权限不足返回明确错误。
- [ ] 存储不足返回明确错误。
- [ ] DataSource / PVC 缺失返回明确错误。
- [ ] template / ISO 缺失返回明确错误。
- [ ] 连接断开后恢复连接，同步可恢复。
- [ ] 重复点击电源操作不会产生重复不可控任务。
- [ ] 清理流程不删除 golden PVC、DataSource、template 或 ISO。

## 演练记录模板

```text
日期：
操作人：
环境：
KubeVirt 集群：
PVE endpoint：
namespace：
VM 前缀：
验证项：
失败项：
底层命令输出位置：
截图或日志位置：
结论：
后续动作：
```
