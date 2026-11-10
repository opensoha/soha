# Optional dsh / Hindsight deployment assets (B3)

Both services and Core enablement default to off. Core can run without these
services. New Chat and Heya require dsh on this Core build, and Hindsight is
optional. Before upgrading an installation with Chat or Heya, drain existing Native
work using the prior runtime, back up PostgreSQL and complete the offline cutover
below. Apply Core migrations through **0145** before Hindsight cutover, and through
**0147** before using Task124 runtime profiles and managed assets.

## Inputs and rendering

Use a validated Core and dsh image pair. Build the dsh image using Agent
`deploy/Dockerfile.dsh-runtime` with the Contracts schema from the same reviewed
version used to build Core. Supply the validated dsh image as `DSH_IMAGE` and set
`DSH_CORE_BASE_URL` to a private Core origin reachable from the Node container.
Compose derives `/api/v1/copilot/dsh/model/v1` and `/api/v1/copilot/dsh/tools`.
Node uses `SOHA_DSH_MODEL_AUTH_MODE=core` and the virtual model `soha-workbench`;
Core owns actual route/provider/model choice. Node does not read a Gateway PAT in
this mode. The Core and Node service-token files must contain the same secret.
Do not use an Agent cluster-proxy route for either Core callback. Explicit Node
`gateway` auth mode/PAT is diagnostic-only and absent from these production assets.

Hindsight still needs `HINDSIGHT_GATEWAY_URL`, the explicit Soha Gateway
OpenAI-compatible provider URL, with a scoped Gateway PAT,
`HINDSIGHT_LLM_MODEL`, `HINDSIGHT_EMBEDDING_MODEL`, and its exact vector dimension
`HINDSIGHT_EMBEDDING_DIMENSIONS`. No provider/model fallback is supplied.
Set optional `HINDSIGHT_EMBEDDING_GATEWAY_URL` when embedding uses another
provider route; empty reuses `HINDSIGHT_GATEWAY_URL`. The qualified Qwen pair uses
`/api/v1/ai-gateway/llm/qwen/v1` for both: extraction model
`Qwen/Qwen3-30B-A3B-Instruct-2507`, embedding model
`Qwen/Qwen3-Embedding-0.6B`, dimensions `1024`. Hindsight's protocol provider
remains `openai`; `qwen` is the Gateway route kind. An independent OpenAI route
may still be selected explicitly. The startup script requests
float vectors using Hindsight's official `EMBEDDINGS_OPENAI_EXTRA_BODY` setting;
it does not tokenize Qwen inputs with an OpenAI tokenizer. Scope the PAT to the
selected provider/model pairs.

Provide readable secret files through `DSH_SERVICE_TOKEN_FILE`, `GATEWAY_PAT_FILE`,
`HINDSIGHT_SERVICE_TOKEN_FILE`, and `HINDSIGHT_DATABASE_URL_FILE`.
Service tokens must contain at least 32 ASCII non-whitespace characters.
The database URL file must point at a separately provisioned Hindsight database
and role with pgvector/pg_trgm installed and the required migrations privileges.
Never use Core's database, schema or role. Hindsight initializes its own schema;
these assets do not provision a DB or migrate existing Soha data.
Keep secret files outside Git and readable by container UID 1000; Compose local
file-secret permissions are the source file's permissions, not a portable chown.
Core mode uses the service-authenticated per-run `Start.tools` frozen by Core;
Node validates its schema and binds the opaque revision. No startup catalog file
or mount is needed. `catalog.example.json` is diagnostic-only, for explicit
`gateway` mode with a startup whitelist; it is not mounted by production Compose.

```sh
# From the Core repository; render is offline and does not start services.
opensoha-verify run -- docker compose -f deploy/dsh-hindsight/compose.yaml config --quiet
opensoha-verify run -- docker compose -f deploy/dsh-hindsight/compose.yaml --profile dsh --profile hindsight config --quiet
# After configuring the dsh inputs and verifying the image:
docker compose -p my-soha-runtimes -f deploy/dsh-hindsight/compose.yaml --profile dsh up --wait --wait-timeout 180
```

Use a distinct Compose project name. This standalone file never joins or replaces
the existing Soha project. No profile enabled means no services start. The only
host ports bind loopback (8097 and 8889). Use authenticated private transport/TLS
for remote Core connections; do not change bindings to public wildcard addresses.

## Core startup wiring

`core.env.example` lists the startup settings with both backends disabled. Set
`SOHA_DSH_RUNTIME_ENABLED=true` and `SOHA_SECURITY_PROTECTED_DURABLE_STORAGE=true`
after all Core workers can read encrypted checkpoints. Use the installation's
active credential encryption key from its existing secret configuration. Preserve
that key and protected storage while encrypted checkpoints still need recovery,
including when dsh is disabled. If Core runs on the host, use the Compose loopback
ports. For containerized Core, use an explicitly allocated private shared network
or TLS endpoint reachable from its container. Configure Hindsight separately.

Mount the dsh service-token file read-only at `/run/soha-dsh/service-token` and the
Hindsight service-token file at `/run/soha-hindsight/service-token` in the existing
Core container. For Docker, the corresponding arguments are:

```sh
--env-file /YOUR_CONFIG/core.env \
--mount type=bind,src=/YOUR_SECRETS/dsh-service-token,dst=/run/soha-dsh/service-token,readonly \
--mount type=bind,src=/YOUR_SECRETS/hindsight-service-token,dst=/run/soha-hindsight/service-token,readonly
```

Keep the existing DB configuration and other arguments. UID 10001 must be able to read
these files. Core receives only service tokens; the Hindsight database credential
and model PAT stay with the Hindsight API. `ALLOW_INSECURE_HTTP=true` is an explicit
opt-in for the isolated private Core→dsh link; use HTTPS and false otherwise.
The Hindsight `INSTANCE_NAMESPACE` is stable, unique and server-owned; changing it
changes bank mapping. Timeouts default to 15s (Hindsight maximum 30s).

The dsh image health check verifies authenticated runtime readiness. Core
`/readyz` checks PostgreSQL. Before directing user traffic to the candidate,
verify that a new authenticated Chat and an eligible Heya request each complete
and that the corresponding Run is recorded. For Task124 verify
`runtime-assembly-v1` and one approved test profile before publishing a default.

## Hindsight protection

Fixed upstream: 1.2.0, source `34a138684a899b6ec34175f7a2b633860466d289`, image index
`sha256:19d0c08f3df79acac29384826419a1ca158129e81ec527cdcbd8c97ff4ef76da`.
The published API has no default bearer protection. `gateway.py` supplies a separate
service-token boundary and exposes only bank creation with `{name: bank}`, retain,
operation lookup, recall, document deletion, and authenticated health. It rejects
reflect, configuration, management, URL-encoded paths, redirects and oversized bodies;
upstream errors are redacted. This token authorizes Core as a service, not individual
users; Core still owns bank mapping, source revision, ACL checks and instance namespace.

The API binds loopback in the gateway network namespace; only the gateway has a
published port. `start.py` sanitizes inherited Hindsight/provider overrides, forces
Gateway OpenAI LLM+embedding settings, disables automatic consolidation, model/LLM
health configuration writes and traces, selects `rrf`, and disables Hugging Face
network downloads. No external worker, UI/control-plane or model downloader starts.
LLM and reflect requests use 95-second timeouts and one official model retry.
The pinned provider also retries HTTP 400/429 and JSON parsing once; this does not
relax response validation or Soha source checks. This is a model request retry,
not permission to resubmit a retain operation with an uncertain outcome. Core
continues reconciling the original operation and retaining its deletion barrier.
The API must retain outbound access to the configured private Gateway and independent
DB. Enforce that destination allowlist in the deployment network/firewall; environment
variables alone are not an egress firewall. Do not reuse a database containing bank
provider overrides or mental-model schedules; inspect/clean those under a separate
approved migration before adoption. Reflect and derived-memory behavior remain gated.

Document deletion also purges **all mental models in that same bank**, including
manual models and knowledge-page backing models, through private loopback API calls.
Deleting each model cascades its refresh history in the pinned PostgreSQL schema;
other raw documents are retained. This is solely for Soha-owned banks in a new,
independent PostgreSQL deployment: Soha does not expose manual mental-model or
Hindsight knowledge-page editing. It is not a safe generic cleanup proxy for a
shared Hindsight database, external index store, bank templates or concurrent
administrative imports/creates. Those deployments require separate reconciliation.
Core owns the opaque instance/owner/scope bank mapping; the service token and path
syntax alone do not establish ownership of an arbitrary existing bank.

For attempted retains, Core supplies the settled outbox operation ID on document
deletion. The gateway checks the bank, document, and operation state and removes
the settled child operation payload before removing the parent operation. Core
requires the matching purge receipt on both success and raw-document 404. Deploy
the matching gateway before resuming Core memory synchronization. A retry keeps
the parent lookup available until all children are removed.

The gateway acknowledges a raw-document 404 after completing the applicable
derived and operation purges.
It repeatedly lists the first metadata page (including knowledge-page models),
validates bank/model identities, and deletes at most 1000 models per request under
one 20-second wall deadline. Failure returns a redacted 502; Core's durable delete
retries, including when the raw document is already gone, and continues the purge.
A concurrent deletion receives 503 instead of waiting in an unbounded lock queue.
Allow a 30-second Core Hindsight timeout to observe the 20-second cleanup deadline;
shorter caller timeouts still require durable retry, not a successful-delete claim.
External mental-model, history, refresh and management APIs remain forbidden.

Late refreshes update existing model rows rather than upserting them; model deletion
and the history foreign key prevent those rows/history from being recreated. This
does not fence a concurrent administrator creating new models or replaying raw
retains. The production boundary therefore requires no direct API/UI/MCP/import
writers, no pre-existing bank schedules/templates, disabled auto-consolidation, and
Core's retain-receipt/deletion dependency checks. On the default PostgreSQL index
path, a stale search hit cannot hydrate a deleted model; this is not a guarantee
about an independently configured external index or its storage erasure.

## Persistence, operations and proof limits

Root filesystems are read-only, capabilities dropped, service-account tokens absent
in Helm, and services run as UID/GID 1000. dsh has a dedicated journal volume, one
writer, and Recreate rollout in Helm. Never mount Docker sockets or host workspaces.
Hindsight persistence is the external DB; `/tmp` is ephemeral. Back up the journal
and Hindsight DB separately. Never run `down -v` as routine maintenance. Stop only
this allocated project; retain volumes until their owner authorizes removal.
For stale dsh `writer.lock`, first prove the prior writer dead and reconcile receipts.

Helm instructions are in soha-helm `charts/soha/OPTIONAL-RUNTIMES.md`. Kubernetes
ConfigMaps consume these reviewed deployment scripts; no sibling source is required
inside running containers. For shared dsh or Hindsight service-token rotation,
pause new work and drain the affected runs, update both sides, and recreate the
corresponding runtime services and restart all Core replicas using the new token.
Keep the journal volume and database attached. Repeat the authenticated health and
request checks before reopening admission. Changes to runtime-only Secrets or
ConfigMaps require restarting the consumers of those files.

Verify container startup with the target image digest, architecture, UID,
read-only filesystem and mounts. For Hindsight, check the protected API against
its independent PostgreSQL and Gateway. Keep these checks separate from offline
Compose/Helm renders and from business-data migration acceptance.

Upstream [configuration](https://github.com/vectorize-io/hindsight/blob/34a138684a899b6ec34175f7a2b633860466d289/hindsight-api-slim/hindsight_api/config.py)
and [image layout](https://github.com/vectorize-io/hindsight/blob/34a138684a899b6ec34175f7a2b633860466d289/docker/standalone/Dockerfile)
are the fixed input references.


### Offline upgrade helper

The image recipe includes `/app/runtime-upgrade` as an explicit maintenance entry point. It is never invoked by the server entrypoint. Stop and drain execution/sync workers, apply schema migrations through 0144, and review a read-only inventory before any apply. Supply `SOHA_UPGRADE_DATABASE_URL` through the controlled process environment. Apply additionally requires the reviewed digest and a mounted nonempty mode-0600 backup; never put a DSN or credentials in command arguments. Use `-mode rollback -digest CHECKPOINT` only for the documented conditional compensation; it does not revive old engines, approvals or tombstones.

This recipe addition is not evidence that a released image contains the helper. The Task123 candidate still requires a real image build and compatible published contracts before deployment. The helper can be built from matching source with `GOWORK=off go build -o runtime-upgrade ./cmd/runtime-upgrade` once those dependency prerequisites are satisfied.
