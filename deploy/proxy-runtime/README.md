# Independent Proxy runtime installation / 独立代理运行时安装

The Core server does not spawn a proxy engine. Both chart features default to disabled;
install the runtime and executor as separate workloads. This is an operator procedure,
not automatic migration or adoption. No live cluster operation has been performed here.

Core Server 不启动代理内核；两个 chart 开关默认关闭。以下为安装者步骤，不自动迁移或接管。

## Images and identities / 镜像与身份

Build `deploy/Dockerfile` targets `proxy-runtime-mihomo` (Mihomo v1.19.30),
`proxy-runtime-sing-box` (v1.14.2), or `proxy-runtime-v2ray` (v5.53.0, amd64).
The final `soha-runtime` target includes `/app/proxy-runtime-executor`.
Use an immutable tested image digest. The chart does not build sibling sources.
For sing-box set `engine=sing-box`, `engineBinary=/usr/local/bin/sing-box`;
for V2Ray set `engine=v2ray`, `engineBinary=/usr/local/bin/v2ray`.
Runtime images need `/bin/sh` for the ordinal identity entrypoint.

For every ordinal independently enroll a real runtime through the existing enrollment
and CA workflow. Never copy one runtime certificate across members. Precreate one
Secret per member containing `control.crt`, `control.key`, `control-ca.crt`, `ingest.crt`,
`ingest.key`, `ingest-ca.crt`, `ingress.crt`, `ingress.key`, `controller-secret`.
Control/ingest identities must authenticate that member's real runtime ID. Ingress
certificates must chain to the employees' trusted CA and contain the public endpoint
hostname SAN. Use distinct private keys even when ingress SANs match.

每 ordinal 独立登记 runtime，生成自己的控制面/ingest mTLS 身份及私钥，不复制身份横向扩容。
Secret 仅引用，不在 values、命令日志或 ConfigMap 写私钥。非 root init container 仅只读挂载
对应 POD_NAME 的 Secret 子目录，将明确列出的九个字段复制到内存 emptyDir 的 0700 私有
子目录，文件权限 0600；主容器只读挂载该私有子目录到原 `/run/identity`，不挂载源 Secret。
每成员独立 RWO PVC，缩容和卸载均保留 state。副本仅在 Pod 启动时生成，没有自动重载；
轮换须停止/排空并重启对应成员，核对新身份与最低 revision，不删除 PVC。
本次 PodTemplate 挂载变更仍须按下文重新审查 live template pin 并显式绑定。

## Install then approve / 先安装再批准

1. Adapt `values.example.json` to your chart values (or use the equivalent `proxyRuntime`
   section in soha-helm). Set actual immutable runtime image, HTTPS network-control and
   ingest origins, endpointId, ordered pod-name/runtimeId/Secret mapping. Leave
   `replicas: 0`, `proxyExecutor.enabled: false`. Install the runtime StatefulSet first.
   The HTTPS LoadBalancer is TCP/TLS passthrough, port 443 to runtime 8443; an ordinary
   HTTP Ingress that cannot support CONNECT is unsuitable. ClusterIP requires an
   explicitly provisioned reachable TCP/TLS ingress. Do not expose the local controller.
2. Read the *live* StatefulSet UID and typed PodTemplateSpec digest using the same
   canonical JSON sha256 algorithm as `internal/infrastructure/proxydeployment`.
   Use the Core read-only helper from its repository: save `kubectl get statefulset <name> -n <namespace> -o json` to a private file, then run `go run ./deploy/proxy-runtime/pin < live.json`. The helper never connects to the cluster. Do not hash Helm YAML text, invent a UID, or use a render test fixture UID.
   Review namespace/name/kind, every member/runtime mapping and replica ceiling.
3. Create a dedicated random executor credential (at least 32 non-whitespace bytes),
   separate from execution-runner and Outpost tokens. Mount it as a private file
   (0400 or dedicated fsGroup 0440) in Core and executor. Configure top-level
   `runtime.proxy_runtime_executors` and `runtime.proxy_slots` with these reviewed pins.
   `network-control` consumes the same top-level slot catalog, including fixed HTTPS
   employee endpoints; it does not maintain a second policy copy.
4. Enable `proxyExecutor.enabled` with image, HTTPS Core URL, executorId, token Secret
   and approved slots, keeping `start: false`. Each slot has `id`, `proxyId`, `executorId`,
   `workloadKind`, `namespace`, `name`, actual `uid`, `templateDigest`, `maxReplicas`,
   ordered `members` (`instanceId`, `runtimeId`, `ordinal`) and `endpoints`
   (`endpointId`, HTTPS origin `url`). Keep `allowAdoption` and `previousOwnerStopped`
   false until an operator has actually checked any previous owner has stopped.
5. Cluster-admin installs and verifies both v1 ValidatingAdmissionPolicy and Binding;
   unsupported clusters fail closed. RBAC grants named workload GET and named scale
   GET/UPDATE only. Admission restricts the executor service account to the same
   namespace/name/kind and 0..maxReplicas, including omitted scale replicas=0.
   Verify denied wrong target/kind/over-limit requests with a dedicated test account.
   Only then set `proxyExecutor.start: true`.
6. Explicitly bind/adopt the slot via the authenticated deployment API and use control
   start/scale/stop. No install step issues these actions automatically. For HA, publish
   a shared HTTPS endpoint whose certificate SAN is valid on *every* member and bind
   its credential pool to the exact approved member set. Confirm server-ready/readback
   before releasing employee projections. Plain CONNECT remains TCP-only. Guarded UDP
   uses the explicit `Soha-Transport: udp/1` TLS extension with datagram framing,
   current authorization and the owned backend; no extra public UDP Service port is
   required. Only verified Mihomo gates advertise `guarded_udp_v1`. A TCP-only
   upstream returns explicit unsupported, never an unguarded fallback.

先核验真实 UID/typed template pin，再启用 admission 与 executor，最后显式绑定/接管和控制。
未知外部写结果先只读对账，不重放。已有 owner 未停止不得接管。

## Upgrade, rollback, uninstall / 升级回退与卸载

A template or runtime-policy change requires a new accepted pin. Stop through the
normal durable control API first and wait for the successful current-generation
stop task and observed zero replicas. An unknown result or read-only reconcile is
not stop evidence. When installed and accepted policy differ, lifecycle operations
and runtime grants fail with `rebind_required`; explicitly bind the reviewed new
policy only after the successful stop. Reverting to an old artifact requires the
same proof and bind, rather than trusting a previously accepted digest.

Before replacing the artifact, retain its immutable source/digest for rollback and
run the candidate kernel against a private copy of the current native rendered
configuration. The runtime image exposes:

```text
/usr/local/bin/soha-proxy-runtime --kernel-preflight-help
/usr/local/bin/soha-proxy-runtime --kernel-preflight /absolute/private.json
```

The strict `kernel-preflight/v1` input contains the approved absolute binary path,
expected binary SHA256 and version, redacted sourceRef, expected GOOS/GOARCH,
absolute native configFile, configurationRevision, native-file configurationHash,
and protectionVersion. The input and native configuration must be private files.
`configurationHash` is the native file byte hash, not `Configuration.Digest`.
Preflight checks hashes before executing copied candidate files in a private stage;
it downloads nothing and does not rewrite the live configuration. A compatible
report proves only `native-parser-compatibility`, not network or protection
acceptance. Run fixed approved candidates in an isolated no-network environment.

Install the approved immutable artifact, manually inspect the live workload UID
and typed PodTemplateSpec digest, update the approved installer policy, then
explicitly bind and start. Check actual kernel version and artifact diagnostics,
configuration/protection readback, and allowed/denied traffic. A failed candidate
remains stopped; restore the approved rollback artifact, repeat preflight/live-pin
review/bind/start, and preserve PVCs, identities and monotonic revision floors. Helm upgrades must preserve the intended current replica value (the example
is deliberately zero); do not use a stale zero-valued file to stop a running workload.
Never let Helm, HPA, GitOps and executor simultaneously own replicas. Maintain explicit
operator coordination for install/upgrade before handing scale ownership to executor.
Reverting a template also requires a reviewed pin; never lower runtime revision floors.
Stop, await drain and scale-zero receipts before uninstalling executor/runtime assets.
Remove only their exact RBAC/VAP/Binding identities after executor termination. Retain
PVCs and CA/private identity material until the operator verifies backup and ownership.
No namespace-wide cleanup or unrelated workload takeover is authorized.

## Verification / 验证边界

Render/lint checks do not prove admission enforcement, image startup, enrollment, HA
routing, TLS or drain SLO. These require the root batch's isolated live environment.
Local assets and fixtures contain no live UID or credentials. Fixture UIDs in Helm tests
are explicitly test-only. No real image build or cluster installation is claimed here.

## Fixed probe profiles / 固定探测目标

Core `runtime.proxy_probe_profiles` (chart `proxyProbeProfiles`) lists only
`instance_id`/`profile_id` (`instanceId`/`profileId` in values). The operator separately
creates a private runtime profile Secret with key `profiles.json`, for example
`{"known-profile":{"url":"https://approved-target.example/health","nodeId":"approved-node"}}`.
Set chart `proxyRuntime.existingProbeProfilesSecret`; raw installs mount the private
file and set `SOHA_PROXY_PROBE_PROFILES_FILE`. The host validates these targets and
still applies its egress protection. Never accept a user-supplied URL. Core ID allowlist
and runtime profile IDs must agree; absent configuration is explicitly unavailable.

The state directory is `/var/lib/soha-proxy-runtime/state`, created privately (0700)
by UID 10001 under its fsGroup-writable PVC root; do not use the PVC root itself
for private revision-floor files. The Pod fixes `fsGroup: 10001` and
`fsGroupChangePolicy: OnRootMismatch`. On a fresh empty volume, kubelet prepares
the volume before the runtime creates its private child. On subsequent Pod
recreation or image upgrade, a matching volume-root GID, owner/group rwx bits
and setgid bit let kubelet skip recursive permission changes, preserving private
state modes, enrollment markers and monotonic revision/policy floors. Keep the
volume-root group and modes stable; do not chmod the PVC root to 0700.

This policy prevents repeated kubelet permission changes; it does not repair an
already affected volume. If the root GID, required permissions or setgid bit
change, kubelet can recurse again and the runtime must reject unsafe private
modes. CSI drivers that manage volume-group ownership themselves may not honor
this policy. Verify fresh installation and retained-volume Pod recreation/image
upgrade on the actual storage driver, including state directory/file modes,
unchanged identity and enrollment state, retained floors and rejected stale
revisions. Render tests alone do not prove these storage behaviors.

For an existing unsafe volume, stop normal workload activity through the managed
lifecycle and obtain exclusive maintenance access to the exact PVC before an
explicit operator repair. Establish trusted ownership/history first; permission
repair cannot establish the integrity of previously writable content. Check the
parent chain, reject symlinks and unexpected owners/types, and require UID 10001
for the private state directory and each selected regular state file. Use
anchored no-follow file descriptors and fchmod on the checked objects to restore
0700 directories and 0600 files; verify identity, complete file inventory and
content hashes before/after without dropping files. Do not recursively chmod
unknown paths, delete/reset floors, replay enrollment challenges, or silently
migrate legacy flat state into a fresh empty child. Legacy layouts require a
separately reviewed operator migration that preserves every state file and its
identity/floors. The default workload stays nonroot with no extra capabilities or
automatic state repair. After a template change, obtain the actual live typed
PodTemplateSpec pin again, review/bind it and use the normal Start flow before
checking retained-volume restart behavior.

## Pod readiness and traffic / Pod 就绪与流量准入

The chart fixes `SOHA_PROXY_HEALTH_LISTEN=0.0.0.0:9091` and uses HTTP
`GET /readyz` on the Pod-only named `health` port. It does not expose this port in
any Service or employee ingress. The independent listener offers only `/readyz`
and `/healthz` without sensitive metadata. Legacy processes keep the listener disabled
unless explicitly configured. No new configurable probe options are needed.

Readiness is separate from employee traffic readiness: a valid mTLS configuration,
Apply ACK and current control lease can make a legally disabled/non-publication
runtime ready. An enabled engine must also be healthy. This does not grant traffic
admission or imply `TrafficReady=true`. Drain refusal, unknown control state and lease
expiry keep readiness false. Never use business ingress TCP availability as Pod readiness.

NoNew stops accepting new sessions; existing sessions may still carry traffic while
Draining. Wait for the durable drain receipts and the separate confirmed-stop step
before scale-zero; do not interpret NoNew, readiness=false or an unknown connection
count as proof that existing sessions have stopped. Keep the existing worker
ReadyReplicas checks and business gate enforcement.

Pod 就绪使用独立 9091 `/readyz`，不向员工暴露健康端口；合法 disabled 配置可就绪但不放行流量。
NoNew 与已有会话排空、最终确认停止是不同阶段，仍有会话流量时不能提前视为排空完成。
