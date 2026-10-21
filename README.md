# Soha Agent

Standalone agent runtime for OpenSoha.

This repository builds the `soha-agent` binary from `./cmd/agent`. It is split from the open-source Soha core and must not import core repository internal packages.

## Development

```sh
go mod tidy
go test ./...
go build ./cmd/agent
```

Build metadata is injected with Go ldflags and is available from the CLI and HTTP API:

```sh
go build -trimpath \
  -ldflags "-X github.com/opensoha/soha-agent/internal/agent/buildinfo.Version=v0.1.7 -X github.com/opensoha/soha-agent/internal/agent/buildinfo.Commit=$(git rev-parse --short HEAD) -X github.com/opensoha/soha-agent/internal/agent/buildinfo.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o bin/soha-agent ./cmd/agent
bin/soha-agent --version
curl -s http://127.0.0.1:18080/api/v1/build-info
```

The default config is `configs/agent.config.yaml`. Override it with `SOHA_AGENT_CONFIG_FILE` when running the binary.

```sh
SOHA_AGENT_CONFIG_FILE=configs/agent.config.yaml go run ./cmd/agent
```

## Production hardening

The Kubernetes starter and development example use the explicit **workbench** action set: resource CRUD, node drain, workload actions, Pod exec, port forwarding and Helm mutations. Existing installations must review the action/RBAC diff before applying the updated starter. The development example additionally opts into Docker terminal access; this is not part of the Kubernetes preset. CRD definition deletion remains a separate opt-in.

Soha-generated installations provide **legacy**, **workbench** and **read-only** presets. Existing cluster registrations retain legacy permissions until a preset is explicitly selected and the installation manifest is regenerated and applied. Read-only excludes writes and execution, while retaining authorization review creation for diagnostics. These are installation presets; the running Agent continues to enforce its explicit `security.allowed_actions` and Kubernetes permissions.

The authenticated `GET /api/v1/diagnostics/actions` separates implemented actions, the running Agent's configured allowlist, and cluster-wide SelfSubjectAccessReview evidence (cached for 30 seconds). Generic resources, custom resources, Helm and target-dependent forwarding remain `unknown` until a concrete target can be checked. Diagnostic reads never authorize a mutation. A failed drain retains the observed cordon state; inspect remaining Pods and schedulability before deciding to uncordon.

Set `app.env: production` only with non-demo bearer tokens. Production config validation rejects wildcard mutation allowlists, unknown mutation actions, wildcard Docker operation kinds, and Docker terminal access without explicit `http.allowed_origins`.

Mutation actions are denied by default. Add only the operations this agent is expected to perform:

```yaml
security:
  allowed_actions:
    - platform.pods.exec
    - platform.deployments.restart
    - platform.deployments.scale
    - platform.deployments.image
    - platform.deployments.rollback
    - platform.statefulsets.restart
    - platform.statefulsets.scale
    - platform.daemonsets.restart
    - runtime.execution_tasks.cancel
    - docker.runtime.terminal
```

Docker runner operation kinds are a separate allowlist under `control_plane.docker.operation_kinds`; keep it to the exact kinds the control plane should claim, for example `host_provision`, `project_deploy`, or `service_action`.

Structured action audit is always written to the configured logger. To persist high-risk action decisions as JSON Lines, set:

```yaml
audit:
  file_path: /var/log/soha-agent/actions.jsonl
```

Runner execution controls live under `control_plane`:

```yaml
control_plane:
  max_concurrency: 1
  default_timeout: 30m
  callback_retry:
    max_attempts: 3
    backoff: 500ms
```

Kubernetes cluster Agents can initiate a reverse session to a publicly reachable Soha access URL. This mode does not require a Service, NodePort, or inbound route to the Agent:

```yaml
control_plane:
  enabled: false
  base_url: https://soha.example.com
  bearer_token: REPLACE_WITH_CLUSTER_AGENT_TOKEN
  agent_id: cluster-id
  runtime_endpoint: http://127.0.0.1:18080
  session:
    enabled: true
    reconnect_min: 1s
    reconnect_max: 30s
    handshake_timeout: 15s
    max_streams: 64
```

The Soha-generated `kubectl apply -f <access-url>/.../manifest.yaml` manifest provisions these values and the per-cluster token. Do not reuse that token across clusters.

Runtime metrics are available from `GET /api/v1/runtime/metrics` when the runner is enabled.
Local diagnostics are available from `GET /api/v1/diagnostics`; the response is a safe summary of build info, enabled runtimes, worker counts, metrics availability, and managed-agent capability readiness. It does not return bearer tokens or kubeconfig contents.

## Docker

The generic cluster agent image can be built with:

```sh
make deploy-agent-image IMAGE_TAG=v0.1.11
```

The Hermes runner image can be built separately with:

```sh
make deploy-hermes-image IMAGE_TAG=v0.1.11
```

The release workflow publishes multi-arch Linux images (`linux/amd64`, `linux/arm64`) to GHCR as `ghcr.io/opensoha/soha-agent` and `ghcr.io/opensoha/soha-hermes-agent`, plus binary archives for Linux, macOS, and Windows. It also publishes the dedicated Buildpacks targets as `ghcr.io/opensoha/soha-buildpacks-agent:v0.1.11` and `ghcr.io/opensoha/soha-buildpacks-agent:daemonless-v0.1.11`. Each archive has a `.sha256` sidecar plus a release-level `SHA256SUMS` manifest.

The packaged Outpost uses the same generic `soha-agent` image. Its protocol-matched Kubernetes install path and NGINX/Traefik ForwardAuth examples are documented in [`deploy/kubernetes/outpost`](./deploy/kubernetes/outpost/README.md). Helm packaging remains in `opensoha/soha-helm`; use the `soha-agent` chart with `mode=outpost`.

## Buildpacks runner

Buildpacks uses a dedicated Agent and Docker-compatible daemon. Build the separate runtime target; the generic cluster and Hermes images do not include pack:

```sh
docker build --build-context contracts=../soha-contracts --target buildpacks-runtime \
  -f deploy/Dockerfile -t soha-buildpacks-agent:local .
```

Set `control_plane.enabled=true`, a distinct `agent_id`, `max_concurrency=1`, `provider_kinds=[]`, and disable Kubernetes, Docker operations, Agent Runtime, and Outpost on this process. Mount only its dedicated daemon socket and private Agent workspace. Do not share the daemon with business workloads or another runner. Preload the approved builder, run image and lifecycle by digest, then configure their exact architecture and the daemon ID returned by `docker info`:

```yaml
control_plane:
  default_timeout: 15m
  buildpacks:
    enabled: true
    builder_image: paketobuildpacks/ubuntu-noble-builder@sha256:79890d38a8230728794f185ad0e34f964e4bed876a4a616e9f93840023959fe3
    run_image: paketobuildpacks/ubuntu-noble-run@sha256:6f53e8386cec5be2b0f9ebebeeddad0e0a7652f8f5f73ef67e090d0a6f928c24
    lifecycle_image: buildpacksio/lifecycle@sha256:874022fe5ece6bda8f04045e69c769d915130aab675b499b319e3eff85e73a8f
    platform: linux/arm64
    docker_host: unix:///run/buildpacks/docker.sock
    cache_max_bytes: 4294967296
    daemon_id: REPLACE_WITH_DEDICATED_DAEMON_ID
    allowed_application_ids: [my-application]
```

The image pins pack 0.40.9. This `linux/arm64` toolchain was verified with Go, Node.js, Java and Python projects, private HTTPS Git, an authenticated registry, and Kubernetes startup. Java's thread and memory budget must fit the deployment limits; the small HTTP sample uses `BPL_JVM_THREAD_COUNT=20` with 512 MiB. A separately pinned `linux/amd64` toolchain also passed Go, Node.js, Java and Python builds from private SSH Git with explicitly bound submodules, authenticated registry publication, SBOM checks and startup by digest. Those amd64 workloads ran with explicit emulation on Apple Silicon; the results do not establish native amd64 performance. Other builders and architectures require their own runtime acceptance.

By default the daemon and image architecture must match. Set `allow_platform_emulation: true` only when the dedicated native daemon has a working emulator for the configured image platform. This opt-in retains daemon ID and image digest/platform checks; emulated acceptance is distinct from native platform performance.

The application Buildpacks capability endpoint checks the toolchain before a build is planned. Configure the Server to reach this Agent through `runtime.buildpacks_runner_endpoint`. The Server freezes its reported timeout (1–3600 seconds); older Agents without a timeout field retain a 300-second budget.

A Buildpacks source selects explicitly bound HTTPS or SSH repositories, fixed commits and a project directory. Use versioned Soha secret references for `GIT_USERNAME` and `GIT_PASSWORD` (HTTPS), or `GIT_SSH_KEY` and `GIT_KNOWN_HOSTS` (SSH with strict host verification). These source-level credentials apply to the selected repositories; use separate build sources when repositories need different credentials. `REGISTRY_AUTH` contains Docker JSON with inline `auths`, without credential helpers. Source credentials are excluded from the build environment, redeemed through the task's secret lease and removed from the workspace after execution.

For submodules, enable `submodules` on the parent binding and explicitly bind every child repository at its `.gitmodules` path and gitlink commit, including any nested children you enable. The runner rejects missing bindings, URL or commit mismatches, duplicate paths, symbolic-link ancestors, and paths that overwrite source files. It never fetches an unbound URL or follows a submodule branch. Older runners do not advertise `supportsSSH` or `supportsSubmodules` and the Server rejects those combinations during preparation. Repository overrides of the approved toolchain remain unavailable.

A completed build requires a structured report, the pushed registry digest and SBOM file digests. Deployment consumes that immutable output through the existing plan and environment policy. Cancellation waits for the process and its build containers to stop. Unconfirmed cleanup disables the runner until an operator restores its dedicated daemon; it does not report a successful cancellation. Caches are isolated by application and toolchain. `cache_max_bytes` (default 4 GiB) bounds retained cache volumes between builds; it does not bound toolchain images or peak scratch space during a build. pack temporary image exports use the task private directory and are removed with its credentials after success, failure or cancellation. After a runner process or host crash, stop its dedicated daemon workloads and recover leftover private task directories before reusing the runner. Reserve separate disk headroom for image exports and runtime layers; the cache limit is not a filesystem quota.

### Daemonless lifecycle runtime

Build `deploy/Dockerfile --target buildpacks-daemonless-runtime` for Podman 5.7.0. Configure `runtime: podman`, an exclusive absolute `podman_root`, and empty `docker_host` / `daemon_id`; retain the pinned toolchain, application allowlist and one-slot dedicated runner requirements above. This backend calls local Podman directly with `--remote=false`. The separately pinned amd64 four-language matrix also passed on this backend, including registry digest startup and private workspace cleanup; it does not reuse pack acceptance as daemonless evidence. It does not start a Podman service or mount a Docker socket. The Docker CLI is used only for registry manifest verification.

Preload images with the same explicit storage flags used by the Agent, for example `podman --remote=false --root /var/lib/soha-cnb --runroot /var/lib/soha-cnb-run pull IMAGE_AT_DIGEST` for `podman_root: /var/lib/soha-cnb`. The builder must declare a numeric non-root UID:GID. This path uses CNB Platform API 0.14 and the frozen lifecycle; builder image extensions are not supported. Registry auth is mounted only in the trusted analyzer/restorer/exporter image, while detector/builder receive only source, layers, platform variables and the pinned lifecycle binaries.

Run this rootful Linux backend inside an isolated VM or dedicated container host with OCI namespace/storage support and an outer CPU/memory budget. Lifecycle containers inherit that budget and the dedicated runner network; no unrelated workloads or credentials may share that host or Podman store. `podman_root` is an administrator ownership declaration, not a multi-tenant filesystem boundary. Build layers and environment files are task-private and removed after confirmed stop; this backend does not retain an application build cache. Host/process-crash recovery and peak scratch-space requirements still apply.

## Helm

The Helm charts are published from `opensoha/soha-helm`:

```sh
helm repo add opensoha https://opensoha.github.io/soha-helm
helm repo update
helm install soha-agent opensoha/soha-agent \
  --namespace soha-agent \
  --create-namespace \
  --set secrets.agentBearerToken=REPLACE_WITH_AGENT_TOKEN \
  --set secrets.controlPlaneBearerToken=REPLACE_WITH_RUNNER_TOKEN

helm install soha-hermes-agent opensoha/soha-hermes-agent \
  --namespace soha-agent \
  --create-namespace \
  --set secrets.controlPlaneBearerToken=REPLACE_WITH_RUNNER_TOKEN
```

## License

This repository is licensed under the Apache License 2.0. See
[LICENSE](./LICENSE) for the full license text.

### Pod session identity

Pod list/detail responses include `uid`. For saved log/terminal sessions, forward the original identity as `expectedUid` on `/api/v1/platform/workloads/pods/:name/logs`, `/logs/stream` and `/terminal`, alongside namespace/container. The Agent performs a live UID read before opening the Kubernetes subresource; a replacement or missing Pod is rejected. Omission remains compatible with legacy clients and must not be used to restore a saved session. Logs/exec still require normal Agent authentication, action policy and Kubernetes RBAC. Kubernetes log/exec APIs offer no atomic UID precondition.

Terminal clients must read frames so ping/pong can run. A peer without pong is disconnected after 45 seconds; writes have finite deadlines. Closing the socket or sending `{ "type": "close" }` cancels the upstream; disconnect never requires an `exit` command. Only resource descriptors belong in persistent browser storage, never tickets, input or output.
