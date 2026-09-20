<h1 align="center">soha</h1>

<p align="center">
  <strong>A unified Kubernetes platform console for modern platform teams.</strong>
</p>

<p align="center">
  Operate clusters, ship applications, investigate incidents, and manage runtime work from one permission-aware control plane.
</p>

<p align="center">
  <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/Go-1.23-00ADD8?logo=go&logoColor=white"></a>
  <a href="https://react.dev/"><img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=111111"></a>
  <a href="https://ant.design/"><img alt="Ant Design" src="https://img.shields.io/badge/Ant%20Design-6-1677FF?logo=antdesign&logoColor=white"></a>
  <a href="https://kubernetes.io/"><img alt="Kubernetes" src="https://img.shields.io/badge/Kubernetes-client--go-326CE5?logo=kubernetes&logoColor=white"></a>
  <a href="https://www.postgresql.org/"><img alt="PostgreSQL" src="https://img.shields.io/badge/PostgreSQL-18.4-4169E1?logo=postgresql&logoColor=white"></a>
  <a href="https://docs.opensoha.dev/"><img alt="Docs" src="https://img.shields.io/badge/Docs-Nextra-111111?logo=nextdotjs&logoColor=white"></a>
</p>

<p align="center">
  <a href="#overview">Overview</a>
  · <a href="#why-soha">Why soha</a>
  · <a href="#features">Features</a>
  · <a href="#quick-start">Quick Start</a>
  · <a href="#deployment">Deployment</a>
  · <a href="#contributing">Contributing</a>
</p>

<p align="center">
  <a href="./README.md">English</a> | <a href="./README-cn.md">简体中文</a>
</p>

## Overview

Soha is a control plane for platform teams operating Kubernetes and adjacent runtime infrastructure. This repository owns the open-source Go core/server and consumes the web console as a versioned build artifact.

The project is intentionally broader than a resource viewer. Soha connects cluster operations, application delivery, observability, runtime evidence, access control, AI investigation, virtualization, and Docker operations into one cohesive console.

## Why soha

- **One runtime**: ship the API and embedded console as a single application container when you want a compact deployment.
- **Operator-first workflows**: list-first resource pages, scoped actions, YAML, events, metrics, logs, and long-running operation records are treated as first-class surfaces.
- **Permission-aware by design**: menus, routes, buttons, API authorization, audit logs, and scope grants are modeled as separate but aligned control points.
- **Agent-ready architecture**: remote clusters, AI providers, Docker operations, and durable execution tasks can run through token-protected runner claim/callback paths.
- **Built to evolve**: platform, delivery, observability, AI, virtualization, and Docker workbench capabilities share one modular-monolith backend and one route-driven frontend shell.

## Features

| Area | What Soha Provides |
| --- | --- |
| Platform operations | Multi-cluster inventory, nodes, namespaces, workloads, network, storage, CRDs, Helm, YAML, logs, events, metrics, and action surfaces. |
| Application delivery | Applications, services, containers, build templates, workflow templates, release bundles, execution tasks, approvals, releases, registries, and delivery records. |
| Observability | Monitoring overview, alert inventory, alert events, notification policy, healing policy, on-call routing, schedules, escalations, and event streams. |
| AI workbench | Session-first chat, root-cause analysis, performance analysis, inspection review, MCP-backed evidence collection, toolsets, skills, and provider execution. |
| Agent runtime | Remote cluster mode, runner claim/callback APIs, execution heartbeats, task cancellation, Docker host runtime proxy endpoints, Docker operation callbacks, and provider-agnostic AI execution. |
| Virtualization | KubeVirt and Proxmox VE connections, VM lifecycle, image and flavor catalogs, console access, metrics, operations, and sync tasks. |
| Docker workbench | Docker host inventory, Compose projects, container management, services, port mappings, templates, single-container startup, agent-backed runtime logs, Shell access, volume browsing, and token-protected runner operations. |
| Access and system | Users, roles, organizations, policies, scope grants, menus, settings, announcements, audit logs, and operation logs. |

## Architecture

Soha follows a modular-monolith backend and a route-driven frontend shell.

```text
Browser Console
      |
      v
React 18 + Vite + Ant Design
      |
      v
Gin API Server
      |
      +--> Application services
      +--> Policy engine
      +--> Repositories
      +--> Kubernetes / Agent / Docker / Virtualization / MCP integrations
      |
      v
PostgreSQL + Kubernetes clusters
```

### Backend

- `cmd/server`: API server entrypoint
- `internal/api`: domain route registration files, handlers, middleware, request parsing, response shaping
- `internal/application`: use-case orchestration, authorization, scope handling, audit, and view models
- `internal/policy`: RBAC, ABAC, and scope evaluation
- `internal/infrastructure`: config, database, Kubernetes, informer, agent, logger, Swagger, MCP
- `internal/repository`: durable persistence
- `internal/bootstrap`: dependency graph, migration, seed, and startup lifecycle wiring

See the published docs for current public architecture and API behavior.

### Frontend

- Source repository: `github.com/opensoha/soha-web`
- Build artifact: `dist`
- `soha` release staging path: `internal/staticassets/web/dist`
- Runtime modes: `embed`, `dir`, and `proxy`

### Documentation

- Source repository: `github.com/opensoha/soha-docs`
- Published docs URL: `https://docs.opensoha.dev/`
- `soha` redirects `/docs/` to the configured external docs URL by default

### Agent And CLI

- Agent repository: `github.com/opensoha/soha-agent`
- CLI repository: `github.com/opensoha/soha-cli`
- `soha` core exposes the control-plane APIs; agent and CLI clients consume those APIs through contracts and HTTP boundaries.

## Tech Stack

| Layer | Stack |
| --- | --- |
| Backend | Go 1.26.6, Gin, PostgreSQL, Kubernetes `client-go` |
| Frontend | React 18, TypeScript 5, Vite 6, React Router 6, TanStack Query 5, Zustand 5, Ant Design 6, Tailwind CSS 4 |
| Docs | Next.js 16 and Nextra 4 in the sibling `soha-docs` repository |
| Packaging | Docker, Docker Compose, raw Kubernetes YAML; Helm charts live in `soha-helm` |

## Project Layout

```text
.
├── cmd/                 # server entrypoint
├── configs/             # backend configuration
├── internal/            # backend layers and domain modules
├── internal/staticassets # staged web artifacts for embedded release builds
├── migrations/          # PostgreSQL bootstrap and schema migrations
├── deploy/              # Docker, Compose, and raw Kubernetes assets
├── Makefile             # minimal local dev/build commands
└── .agents/skills/      # repository engineering and deployment rules
```

## Quick Start

### Requirements

- Go 1.26.6+
- Node.js 20+
- Docker and Docker Compose
- PostgreSQL 18.4 with pgvector 0.8.5 when using an external database

### Install dependencies and start local services

The standard stack starts with `pgsql` as the PostgreSQL password and
`opensoha` as the initial `opensoha` administrator password:

```bash
make init
```

These are Soha's standard initial credentials across local, Docker, Compose,
Kubernetes, and Helm deployments. Override them only when an installation needs
different database or administrator credentials.

This installs Go dependencies, then starts the local PostgreSQL service from `deploy/docker-compose.yaml`. Frontend dependencies are managed in the sibling `../soha-web` repository.

MinIO exposes its S3 API at `http://127.0.0.1:9000` and console at `http://127.0.0.1:9001`, with persistent data in `opensoha-minio-data`. Its local defaults are `minioadmin` / `minioadmin`; override `SOHA_MINIO_ROOT_USER` and `SOHA_MINIO_ROOT_PASSWORD` in the environment or an untracked `deploy/.env` to preserve existing credentials. Change these defaults before exposing MinIO beyond loopback. Start it separately with `docker compose -f deploy/docker-compose.yaml up -d minio`; `make init` and `make dev` do not start MinIO.

The compose stack uses `pgvector/pgvector:0.8.5-pg18-trixie`, currently based on PostgreSQL 18.4 and the same Debian generation as the standard PostgreSQL 18.4 image, enables `vector` and `pg_trgm`, and preloads `pg_stat_statements`. It mounts the named volume at `/var/lib/postgresql`, which is required for PostgreSQL 18's default data directory layout. Override the image with `SOHA_POSTGRES_IMAGE` only with a PostgreSQL 18 image that provides these extensions and a compatible libc collation version. Existing local volumes created by PostgreSQL 16 cannot be reused by changing only the image tag; recreate disposable volumes or migrate data with `pg_dump`/`pg_restore` or `pg_upgrade`.

### Start the API and console

```bash
make
```

The default target starts the Go API and the Vite frontend together.

- Console: `http://localhost:5173`
- API: `http://localhost:8080`
- Config override: `SOHA_CONFIG_FILE=/abs/path/to/config.yaml`
- Local defaults live in `configs/config.yaml`; override them with environment variables or `SOHA_CONFIG_FILE`.

### Run services separately

```bash
make dev-api
make dev-web
```

For a direct server run without Make:

```bash
go run ./cmd/server
```

### Start the agent runtime

```bash
cd ../soha-agent
go run ./cmd/agent
```

The default agent config is in the sibling `soha-agent` repository at `configs/agent.config.yaml`. Override it with:

```bash
SOHA_AGENT_CONFIG_FILE=/abs/path/to/agent.config.yaml go run ./cmd/agent
```

The same agent binary can also expose Docker host runtime APIs used by Docker
Workbench for project logs, interactive Shell sessions, and volume file browsing.
Configure host records with the agent runtime endpoint and bearer token. Browser
WebSocket streams still go through the control plane and use short-lived stream
tickets instead of query-string access tokens.

## Common Commands

```bash
make
make init
make dev-api
make dev-web
make build
make test
make deploy-image
```

## Deployment

Soha runs one process per container. The default process serves the management API and embedded SPA; independent `network-control`, `ingest`, and privileged `network-gateway` workloads keep realtime authorization, high-frequency telemetry, and the WireGuard data plane outside the management-server process. Documentation is published from `soha-docs` and linked through the configured docs URL.

- [deploy/Dockerfile](./deploy/Dockerfile): multi-stage image build
- [deploy/docker-compose.yaml](./deploy/docker-compose.yaml): local stack with PostgreSQL, MinIO, and optional Hermes runner services
- [configs/config.yaml](./configs/config.yaml): default application config
- [deploy/deployment.yaml](./deploy/deployment.yaml): raw Kubernetes manifest baseline
- [deploy/network-runtime.yaml](./deploy/network-runtime.yaml): independent network-control, ingest, ingest PostgreSQL, and WireGuard gateway workloads
- [deploy/kustomization.yaml](./deploy/kustomization.yaml): Kustomize entrypoint for image tag, namespace, and patch overrides without Helm

### Remote Kubernetes development

`ops.popicorns.com` is the shared development endpoint; no separate
`dev.ops.popicorns.com` environment is used. The remote development workload
reuses the existing `opensoha` PostgreSQL database, `soha-config` Secret, and
`soha-data` PVC.

To develop from pushed branches or refs:

```bash
make remote-dev-up \
  REMOTE_DEV_SOHA_REF=my-branch \
  REMOTE_DEV_WEB_REF=my-branch \
  REMOTE_DEV_CONTRACTS_REF=my-branch
```

The workload polls those refs inside the cluster. Backend source changes trigger
an automatic rebuild; Vite handles frontend HMR. This workflow changes the Helm
Deployment replica count and the existing Service selector, so restore the release
before running a Helm upgrade:

```bash
make remote-dev-status
make remote-dev-logs
make remote-dev-down
```

This is a shared-data development environment: migrations and application writes
affect the existing `soha` database.

```bash
make deploy-image
docker compose -f deploy/docker-compose.yaml up -d --build
```

The network runtimes are opt-in in Compose because their mTLS private keys must
not be committed. Place `tls.crt`, `tls.key`, and `ca.crt` in separate server
and client-identity directories, keep private files at mode `0440` or `0600`,
then start the profile:

```bash
SOHA_NETWORK_CONTROL_TLS_DIR=/absolute/path/network-control-tls \
SOHA_INGEST_TLS_DIR=/absolute/path/ingest-tls \
SOHA_NETWORK_INGEST_QUERY_TLS_DIR=/absolute/path/core-ingest-query-tls \
SOHA_NETWORK_INGEST_QUERY_URL=https://ingest:8083 \
SOHA_NETWORK_INGEST_QUERY_CA_FILE=/run/soha-network-ingest-query/ca.crt \
SOHA_NETWORK_INGEST_QUERY_CERT_FILE=/run/soha-network-ingest-query/tls.crt \
SOHA_NETWORK_INGEST_QUERY_KEY_FILE=/run/soha-network-ingest-query/tls.key \
SOHA_NETWORK_INGEST_QUERY_SERVER_NAME=ingest \
SOHA_NETWORK_GATEWAY_CONTROL_CLIENT_TLS_DIR=/absolute/path/gateway-control-tls \
SOHA_NETWORK_GATEWAY_INGEST_CLIENT_TLS_DIR=/absolute/path/gateway-ingest-tls \
docker compose -f deploy/docker-compose.yaml --profile network-access up -d --build
```

This starts `network-control` on 8082, `ingest` on 8083, and a dedicated ingest
PostgreSQL instance, plus the separately privileged WireGuard gateway on UDP
51820. `/healthz` is process health; network-control and gateway `/readyz`
become ready only after their required policy/configuration has been published.
The core ingest-query client certificate must have the exact URI SAN
`spiffe://opensoha.local/network-ingest/core/soha-server`; do not reuse the
gateway ingest identity. Only bounded aggregate summaries return to core.

Component smoke containers live in the same Compose file and remain disabled by
default. `network-test` provides mihomo, `radclient`, a one-shot EAP-TLS
`eapol_test` client, and a NET_ADMIN-scoped WireGuard/HTTP client:

```bash
docker compose -f deploy/docker-compose.yaml --profile network-test up -d --build \
  mihomo-test network-test-client radius-test-client
docker compose -f deploy/docker-compose.yaml --profile network-test exec -T network-test-client \
  sh -c 'curl -fsS --proxy http://mihomo-test:7890 -H "Authorization: Bearer soha-network-test" http://mihomo-test:9090/version >/dev/null && ip link add wg-smoke type wireguard && wg show interfaces && ip link del wg-smoke'
docker compose -f deploy/docker-compose.yaml --profile network-test exec -T radius-test-client radclient -v
```

For a real EAP-TLS exchange, generate a seven-day disposable lab CA and matching
network-control, NAS, FreeRADIUS, and endpoint identities under the ignored
runtime directory. The endpoint certificate is deliberately shared with the
EAP client so its serial and authority key ID match the credential created by
endpoint enrollment:

```bash
sh deploy/network-test/generate-eap-test-pki.sh \
  deploy/network-runtime-tls/eap-lab <soha-subject-id>
```

Start `network-control` and FreeRADIUS with the generated directories, create
an enrollment for runtime `endpoint-eap-test`, and consume it with
`eap-test-client/tls.crt` and `tls.key`. Do not insert a certificate binding
directly. Then run the client in the FreeRADIUS network namespace so the
loopback-only lab NAS remains closed to other containers:

```bash
SOHA_NETWORK_CONTROL_TLS_DIR=./network-runtime-tls/eap-lab/network-control \
SOHA_RADIUS_CONTROL_CLIENT_TLS_DIR=./network-runtime-tls/eap-lab/freeradius-control \
SOHA_RADIUS_EAP_TLS_DIR=./network-runtime-tls/eap-lab/freeradius-eap \
SOHA_RADIUS_EAP_TEST_TLS_DIR=./network-runtime-tls/eap-lab/eap-test-client \
docker compose -f deploy/docker-compose.yaml --profile network-access \
  --profile network-test run --rm --build radius-eap-test-client
```

Success requires `CTRL-EVENT-EAP-SUCCESS` and an `Access-Accept`; a TLS failure,
unknown or revoked certificate binding, or denied policy must exit non-zero.
The client command exposes the disposable lab shared secret in its own container
process arguments because that is the only interface provided by `eapol_test`;
never use a production NAS secret for this test. Keep `issuer/ca.key` offline
from every container and delete the whole lab directory after validation.

For an A/B/C lab, run `network-access` together with
`network-multisite-test`. The existing `network-gateway` is A; the profile adds
B on UDP 51821 and C on UDP 51822 with separate keys, enrollment identities,
and data volumes. In the workbench, leave A's `hubGatewayId` empty, set B and C
to A's gateway ID, and advertise only non-overlapping CIDRs owned by each site.
This is a static routed hub-and-spoke topology: B-to-C traffic transits A.

```bash
docker compose -f deploy/docker-compose.yaml --profile network-access \
  --profile network-multisite-test up -d --build \
  network-gateway network-gateway-b network-gateway-c
```

Raw Kubernetes uses the same process split. The raw manifests use `:local`
images until a release containing the network runtimes is available; v0.1.7
and v0.1.8 do not contain these binaries. Build both images from this checkout:

```bash
make deploy-image
docker build --build-context contracts=../soha-contracts \
  --target network-gateway-runtime -f deploy/Dockerfile \
  -t ghcr.io/opensoha/soha-network-gateway:local .
```

Load these images into every target cluster node, or push them to your own
registry and replace both image references in `deploy/kustomization.yaml`.
Use the same application image for server, network-control, and ingest.
Before `kubectl apply -k deploy`,
create `soha-network-runtime-tls` in namespace `soha` with the keys named
at the top of `deploy/network-runtime.yaml`. Certificates must cover the actual
Service/load-balancer names and be issued by the mounted client CA; never store
their private keys in this repository. The core query identity uses the same
URI SAN stated above and its own `network-core-ingest-query.*` Secret keys.

Run the application container without Compose when PostgreSQL is already
reachable. This example keeps every standard default explicit so each value can
be replaced independently before deployment:

The external PostgreSQL server must run PostgreSQL 18 and provide pgvector and
`pg_trgm`. The application migration user creates both extensions automatically;
when that user cannot create extensions, a database administrator must run:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

`pg_stat_statements` remains optional for external PostgreSQL. To enable it, an
administrator must add it to `shared_preload_libraries`, enable
`compute_query_id`, restart PostgreSQL, and create the extension in the Soha
database. Its absence never blocks Soha startup on an external database.

```bash
docker run -d \
  --name soha \
  --restart unless-stopped \
  -p 8080:8080 \
  --mount source=soha-data,target=/app/data \
  --add-host host.docker.internal:host-gateway \
  -e SOHA_DATABASE_HOST=host.docker.internal \
  -e SOHA_DATABASE_PASSWORD=pgsql \
  -e SOHA_AUTH_DEV_PRINCIPAL_PASSWORD=opensoha \
  -e SOHA_AUTH_JWT_SECRET=soha-123456789012345678901234567890 \
  -e SOHA_RUNTIME_EXECUTION_RUNNER_TOKEN=soha-123456789012345678901234567890 \
  -e SOHA_MONITORING_WEBHOOK_TOKEN=soha-123456789012345678901234567890 \
  -e SOHA_SECURITY_CREDENTIAL_ENCRYPTION_KEY=soha-123456789012345678901234567890 \
  ghcr.io/opensoha/soha:v0.1.7
```

The `soha-data` volume persists companion data. Software packages require an enabled S3-compatible system integration and are not stored on the local application volume.

Configure software object storage from **Internal Workbench → Software Library**. S3-compatible connections support AWS S3, MinIO, Alibaba Cloud OSS, Tencent Cloud COS, and custom compatible endpoints through `endpoint`, `bucket`, `region`, path-style addressing, and an optional object prefix. Private or cluster-local endpoints require the explicit **Allow private endpoint** switch; plain HTTP additionally requires **Allow HTTP**. Access keys are stored encrypted with `security.credential_encryption_key`, never returned by the API, and can be checked with **Test connection** before uploading packages. Multiple storage integrations may be enabled for different providers or regions; uploads select a target integration and existing packages keep their original integration for download and deletion. Endpoint and bucket configuration are immutable after creation; rotate credentials in place, or create a new integration for a storage migration.

Upgrades do not silently discard the former filesystem catalog. If `data/software/index.json` (or the legacy `SOHA_SOFTWARE_STORAGE_DIR`) still exists, startup stops with a migration error. Before upgrading, stop the old release and move the complete legacy directory to a backup location. Start the new release, configure and test S3-compatible storage, then re-upload each indexed blob with its original metadata and file name. Keep the backup until package count, byte totals, SHA-256 values, and downloads have been verified.

The bootstrap password is inserted only when the `opensoha` user's password
credential does not already exist, so routine restarts never reset a changed
administrator password.

The JWT, runner, webhook, and credential-encryption settings all default to the
public value `soha-123456789012345678901234567890`. This makes local, raw Docker,
Compose, Kubernetes, and Helm startup deterministic, but it is not secure for a
public installation. Soha logs a startup warning with the unchanged configuration
key names, without logging their values. Override all four settings before exposing Soha. Prefer
separate high-entropy values and keep the same configured values on every Soha
replica. Changing the credential-encryption key does not re-encrypt existing
records: migrate every stored ciphertext to the new key before restarting with
the replacement, or those credentials will become unreadable.

Soha does not require a SecretStore volume, secret bundle, writer lease, or
secret lifecycle CLI. Multiple API containers may use the same database when
they receive the same configuration; use different host ports for parallel raw
Docker instances and a shared load balancer for normal multi-replica delivery.

Recommended boundaries:

- Docker image: use `ghcr.io/opensoha/soha`; local builds default to the `local` tag.
- Agent images: use `ghcr.io/opensoha/soha-agent` and `ghcr.io/opensoha/soha-hermes-agent` from the sibling `soha-agent` repository.
- CLI tool image: use `yshanchui/soha-cli` from the sibling `soha-cli` repository for multi-stage builds and operational containers. It is an image artifact, not a Helm workload.
- Docker Compose: use for local development and single-node trials, not as the primary production orchestrator.
- Helm: use as the primary Kubernetes delivery path. `soha-helm` publishes `soha`, `soha-agent`, and `soha-hermes-agent` charts.
- Kustomize: keep as a lightweight raw YAML customization entrypoint, avoiding a second full Kubernetes template set.

Build the image:

```bash
make deploy-image IMAGE_TAG=v0.1.7

# When proxy.golang.org is unstable:
make deploy-image IMAGE_TAG=v0.1.7 GOPROXY=https://goproxy.cn,direct
```

Install with Helm:

```bash
helm repo add opensoha https://raw.githubusercontent.com/opensoha/soha-helm/gh-pages
helm repo update
helm install soha opensoha/soha --namespace soha --create-namespace
helm install soha-agent opensoha/soha-agent \
  --namespace soha \
  --set-string config.controlPlane.baseUrl="https://soha.example.com"
helm install soha-hermes-agent opensoha/soha-hermes-agent \
  --namespace soha \
  --set-string controlPlane.baseUrl="https://soha.example.com"
```

The agent charts read only `soha-config/execution-runner-token` by default and
generate their own inbound agent token when needed. Kubernetes Secret
references cannot cross namespaces; for a separate agent namespace, sync only
that runner key with an External Secrets controller and set
`secrets.controlPlaneExistingSecret.name/key` accordingly.

The chart keeps deployment configuration in Kubernetes Secrets so installations
can override the four public defaults without modifying the image. Keep all
replicas on the same values during a rollout.

To copy the CLI into another image, use the tool image directly:

```Dockerfile
COPY --from=yshanchui/soha-cli:v0.1.0 /usr/local/bin/soha /usr/local/bin/soha
```

Helm chart sources and Artifact Hub publishing live in `opensoha/soha-helm`.

Apply the raw Kubernetes baseline:

```bash
cd deploy
kustomize edit set image ghcr.io/opensoha/soha=ghcr.io/opensoha/soha:vX.Y.Z
cd ..
kubectl apply -k deploy
```

The raw manifest includes the standard `pgsql`/`opensoha` initial credentials
and the four public system-key defaults in `soha-app-config`. Replace them with
an overlay or external Secret integration before a public rollout.

Identity Outpost is deployed from the sibling `soha-agent` repository or from
the `soha-agent` Helm chart with `mode=outpost`. Pin a released agent image that
supports Identity Outpost protocol `v1`; server and agent SemVer values may
differ because the protocol version and signed configuration key are the
compatibility boundary. NGINX Ingress and Traefik ForwardAuth examples live in
`soha-agent/deploy/kubernetes/outpost/examples`.

## Documentation

- [Backend Engineering Rules](./.agents/skills/soha-backend/SKILL.md)
- [Deployment Rules](./.agents/skills/soha-deploy/SKILL.md)
- [Published Docs](https://docs.opensoha.dev/)
- [Docs Source](https://github.com/opensoha/soha-docs)

## Development Principles

- Backend handlers stay thin. Application services own orchestration, authorization, scope semantics, audit, operation logs, and frontend-facing view models.
- Keep central startup and route files thin. Add domain route files under `internal/api/routes` and concern-specific bootstrap files under `internal/bootstrap` instead of growing one monolithic file.
- Split oversized Go files by stable behavior domains first. Platform handlers, platform resource services, and AI Gateway are organized into focused same-package files; protect execution-plane state transitions with unit tests.
- Long-running work is task-backed. Build, release, Docker, Compose, VM control, and provider execution run through durable tasks and callback paths.
- Frontend work belongs in `github.com/opensoha/soha-web`. Routes, metadata, permissions, backend menus, and tests should stay aligned across the artifact boundary.
- Platform APIs return Soha DTOs, not raw Kubernetes objects, except YAML or explicit passthrough routes.
- Module visibility, menu visibility, and backend authorization are separate gates.
- Generated artifacts should not be hand-edited. Update source files and rebuild instead.

## Contributing

Issues and pull requests are welcome. For larger changes, read the relevant
[backend](./.agents/skills/soha-backend/SKILL.md) or
[deployment](./.agents/skills/soha-deploy/SKILL.md) rules first.

Useful validation commands:

```bash
make test
cd ../soha-web && npm run typecheck && npm run build
```

## Project Status

Soha is under active development. The platform, delivery, observability, AI, virtualization, and Docker workbench surfaces are evolving together, so some areas are more mature than others.

## License

Soha is licensed under the Apache License 2.0. See
[LICENSE](./LICENSE) for the full license text.
