# Optional dsh runtime image

This image runs the Node service directly, without a Go Agent forwarding hop.
Node 24.14.0 and dsh SDK/runtime 0.2.2-rc.1 are fixed inputs; npm consumes the committed lock.
Resolve the Contracts version pinned in the Agent `go.mod` and build from the
Agent repository with its checksum-verified module directory as the schema context.
The resolver supports both release tags and immutable Go pseudo-versions.

```sh
contracts_dir="$(opensoha-verify run -- python3 -c 'from deploy.resolve_contracts import resolve_contracts; print(resolve_contracts()["dir"])')"
opensoha-verify image -- --build-context "contracts=$contracts_dir" -f deploy/Dockerfile.dsh-runtime .
```

For a retained image use the workspace `build-image` entry with your own tag.
The image packages the Agent runtime and the pinned Contracts schema. CI builds it
and checks authenticated startup with the built-in health probe. The Agent tag
workflow builds both `linux/amd64` and `linux/arm64` and publishes
`ghcr.io/opensoha/soha-dsh:<agent-tag>` on tag refs. It checks that the published
manifest includes both platforms before creating the GitHub release.

The image gate requires `agent/dsh-runtime.schema.json` in that verified module.
A missing schema or a requested version that differs from the reviewed `go.mod`
dependency fails before the build; an unversioned sibling checkout is not used.

Run as UID/GID 1000 with a read-only root filesystem, writable named volume at
`/var/lib/soha-dsh`, and ephemeral `/tmp`. Mount the same service token file used by
Core at `SOHA_DSH_SERVICE_TOKEN_FILE` (at least 32 characters). Production defaults
to `SOHA_DSH_MODEL_AUTH_MODE=core` and does not read or require a Gateway PAT.
Never mount a Docker socket or host workspace. Health uses `/v1/health` with the
service bearer read from its file, without printing it. The image binds
`0.0.0.0:8097` for container networking (standalone service defaults to loopback).

Set `SOHA_DSH_GATEWAY_URL=<Core>/api/v1/copilot/dsh/model/v1` (the service appends
`/chat/completions`) and `SOHA_DSH_CORE_CALLBACK_URL=<Core>/api/v1/copilot/dsh/tools`.
`SOHA_DSH_MODELS=soha-workbench` is a virtual model; Core owns the actual user-authorized
route, provider and model selection. Do not replace it with a provider route or use
an Agent cluster-proxy path for this relay. Core freezes `Start.tools` per run under service authentication; Node validates
the independent schema and binds the opaque revision. Production core mode needs
no `SOHA_DSH_TOOL_CATALOG_FILE` or global catalog mount. Both Core routes must be
reachable from Node.
Explicit `SOHA_DSH_MODEL_AUTH_MODE=gateway` plus `SOHA_DSH_GATEWAY_PAT_FILE` is for
isolated diagnostic qualifications only, and requires a startup catalog whitelist
file; production Compose/Helm do not enable it.
Limits default to `SOHA_DSH_MAX_CONCURRENCY=4`, `SOHA_DSH_MAX_RUN_MS=120000`, and
`SOHA_DSH_MAX_OUTPUT_TOKENS=4096`. The schema path is built into the image.

One service instance owns one journal volume. Do not share it among replicas.
After an unclean stop, prove the previous writer is dead before removing its
`writer.lock`; retain journal receipts for reconciliation. Back up the journal with the service stopped.

Core startup configuration is `SOHA_DSH_RUNTIME_{ENABLED,BASE_URL,SERVICE_TOKEN_FILE,TIMEOUT,ALLOW_INSECURE_HTTP}`.
The Core service-token file and the Node file must contain the same value. Apply
Core migration 0143 before enabling the B3 backend integration. Disabled dsh makes
new Chat/Heya requests explicitly unavailable; it never selects Native as fallback.
Existing native in-flight work may drain. Legacy migration/deletion remains B4 work.
Standalone Compose assets belong to Core `deploy/dsh-hindsight`; Helm assets belong
to soha-helm. Assets are not proof of bootstrap, image or end-to-end acceptance.
