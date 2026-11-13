# soha-connectors

This repository owns OpenSoha connector source packages, connector runtime
contracts, manifests, and integration scaffolding.

Connectors should integrate with Soha through published contracts, HTTP APIs,
MCP manifests, event schemas, or other explicit extension points. Do not import
`soha/internal/...`, and do not move SaaS-only lifecycle, billing, quota, or
cloud operations logic into this repository.

## Stack

- TypeScript on Node.js 20+.
- Runtime dependencies are currently zero; connectors use built-in `fetch`,
  `crypto`, and `node:test`.
- Build output is generated into `dist/`. `npm test` compiles into its own
  temporary directory and removes it on success or failure by default; set
  `OPENSOHA_KEEP_VERIFY_ARTIFACTS=1` to retain it for inspection. Explicit
  `npm run build:test` keeps `dist-test/` for inspection.

```bash
npm install
npm run check
```

## Layout

```text
soha-connectors/
  .env.example
  CHANGELOG.md
  docs/
    operations/feishu-runtime-runbook.md
    soha-core-integration.md
  connectors/index.json
  connectors/capability-matrix.json
  connectors/
    feishu/              # experimental connector manifest and docs
    wechat/              # planned connector docs
    wecom/               # planned connector docs
  schemas/
    connector-manifest.schema.json
    connector-event-envelope.schema.json
    connector-action.schema.json
  scripts/
    validate.mjs
  src/
    sdk/                 # connector interface, lifecycle, manifest helpers
    connectors/
      feishu/            # config, auth, webhook verification, action loop
  test/
```

The repository intentionally keeps connector manifests under `connectors/` and
source under `src/connectors/`. `connectors/index.json` is the registry used by
validation and future packaging.

## Connector Registry

| Connector | Path | Status |
| --- | --- | --- |
| Feishu | `connectors/feishu/` | Experimental |
| WeChat | `connectors/wechat/` | Planned |
| WeCom | `connectors/wecom/` | Planned |

The per-connector capability matrix lives in
`connectors/capability-matrix.json`. It records implemented, in-memory, and
planned capabilities with evidence files for each connector.

## Connector Contract

Every connector implements the SDK contract in `src/sdk`:

- `configure(config)`: validate config and secret references without contacting
  the upstream provider.
- `start(context)`: attach runtime context such as logger and clock.
- `health()`: return a lightweight status snapshot.
- `handleWebhook(request)`: verify inbound provider webhooks, normalize events,
  and optionally dispatch connector actions.
- `dispatchAction(request)`: execute an outbound provider action such as sending
  a message.
- `stop()`: release runtime resources.

Required lifecycle states are `created -> configured -> started -> stopped`.
Connector manifests must declare config fields, secrets, capabilities,
permissions, events, and actions.

Connector events are posted to Core as a batch wrapper mirrored at
`schemas/connector-event-envelope.schema.json`. The canonical schema lives in
`soha-contracts/connectors/connector-event-envelope.schema.json`; the local
copy is kept for package compatibility. The wrapper is
`{ connectorId: string, events: ConnectorEvent[] }`. Each `ConnectorEvent`
contains `id`, `type`, `source`, `occurredAt`, provider-specific `payload`, and
optional `subject`.

## Per-call credentials

The HTTP SDK supports `opensoha.dev/connector-action/v1` only for actions
explicitly declaring `credentialProtocol` and `credentialAliases`. Core pins and
authorizes references before delivery over authenticated HTTPS or loopback. The
SDK supplies invocation-local `request.credentials` and a cooperative
`request.signal`, rejects invalid/expired frames and late results, and masks known
credential string material in results and errors. Plugins must honor cancellation
and keep values out of logs, events, retry queues and persistent configuration.
The declaration advertises support; calls without credentials retain the old
envelope. Authenticated provider actions must reject missing credentials.

Feishu does not opt into this protocol. HTTP/Git/Object knowledge fetch plugins
are not implemented by this SDK feature. See
[Core integration](docs/soha-core-integration.md#per-call-credential-envelope).

## Feishu Status

`feishu` is the first end-to-end skeleton and is marked `experimental`.

Implemented:

- Node HTTP runtime adapter with `/healthz`, `/manifest`,
  `/webhooks/feishu`, `/actions/{actionName}`, `/retry-queue`,
  `/dead-letter`, and `/metrics`.
- Environment config loading through `.env.example` conventions.
- Graceful shutdown for `SIGINT` and `SIGTERM`.
- Config normalization for App ID, App Secret, Verification Token, optional
  Encrypt Key, base URL, and auto-reply settings.
- Tenant access token exchange through Feishu's internal-app token endpoint.
- Webhook URL verification challenge handling.
- Verification Token checks for all callbacks.
- Encrypt Key based `X-Lark-Signature` verification for non-challenge events.
- Encrypted callback payload decryption.
- Event normalization for `im.message.receive_v1`.
- Optional event sink forwarding to `POST /api/v1/connectors/events` with the
  connector event batch wrapper.
- `feishu.message.send_text` action using `POST /im/v1/messages`.
- Optional message event auto-reply loop for local end-to-end testing.
- In-memory or file-backed webhook idempotency, retry queue, and dead-letter
  tracking, outbound action rate limiting, in-process action retry, and runtime
  metrics.
- Structured JSON logs with secret/token redaction.

Run the Feishu HTTP runtime after building:

```bash
cp .env.example .env
npm run build
npm run start:feishu
```

Soha core integration is documented in
`docs/soha-core-integration.md`. The connector runtime stays outside the core
source tree and integrates over HTTP, connector manifests, plugin marketplace
records, and event/action schemas.

Feishu runtime operations are documented in
`docs/operations/feishu-runtime-runbook.md`, including health checks,
dead-letter handling, upgrade, and rollback. Release-facing changes are tracked
in `CHANGELOG.md`.

Reference docs used for the skeleton:

- [Feishu/Lark encrypted event callback and signature verification](https://open.larksuite.com/document/ukTMukTMukTM/uYDNxYjL2QTM24iN0EjN/event-subscription-configure-/encrypt-key-encryption-configuration-case)
- [Internal app tenant access token](https://open.larksuite.com/document/ukTMukTMukTM/ukDNz4SO0MjL5QzM/auth-v3/auth/tenant_access_token_internal)
- [Send message API](https://open.larksuite.com/document/server-docs/im-v1/message/create?lang=zh-CN)

## Boundary

- Keep connector implementations independent from the open-source core source
  tree.
- Prefer contracts, generated SDKs, HTTP APIs, MCP manifests, event schemas, and
  published artifacts over source-level coupling.
- Hosted or cloud-only connector operations belong in `soha-cloud`.

## Validation

```bash
npm run lint
npm run test
npm run build
npm run validate
```

CI runs `npm ci` followed by `npm run check`.

Generate and verify local GitHub release artifacts with:

```bash
npm run release:artifacts
npm run release:verify -- dist/release/opensoha-connectors-0.1.0.tgz
```

The package remains `private: true`; the generated tarball, checksum, and
release manifest are intended for GitHub release assets, not npm publish. The
release manifest records SHA-256 checksums for packaged connector manifests and
declares GitHub build provenance attestation as a required signature/source
proof. Tagged releases use `.github/workflows/release.yml` to attest the
tarball, checksum, and release manifest before publishing.

## License

This repository is licensed under the Apache License 2.0. See
[LICENSE](./LICENSE) for the full license text.
