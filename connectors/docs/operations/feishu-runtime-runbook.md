# Feishu Runtime Runbook

This runbook covers the local Feishu connector runtime packaged by
`@opensoha/connectors`. The package is private for npm; release artifacts are
published as GitHub release tarballs with checksums and a release manifest.

## Configuration

- Build the package with `npm run build`.
- Start the runtime with `npm run start:feishu`.
- Set `SOHA_CONNECTOR_HTTP_TOKEN` before exposing the HTTP runtime.
- Configure Feishu app credentials through the environment variables listed in
  `.env.example`.
- Keep upstream App Secret, Verification Token, Encrypt Key, tenant token, and
  runtime bearer token out of logs and issue trackers.

## Health

- `GET /healthz` returns runtime liveness.
- `GET /manifest` returns the connector manifest used by Soha core or a plugin
  installer.
- Propagate a request id when calling runtime actions so Soha audit records can
  correlate action attempts with connector logs.

## Action Dispatch

- `POST /actions/feishu.message.send_text` sends a text message through the
  Feishu message API.
- The runtime requires bearer authentication for action endpoints.
- Treat every action request as externally triggered. Validate the action name,
  target receive id, message body, and request id before dispatch.
- Text is limited to 4096 Unicode code points, including combining characters
  individually; astral characters count once. Invalid text never reaches Feishu.
- A send action has one 15-second overall deadline covering authentication,
  response bodies, retries, and retry delays. Caller cancellation can end it
  earlier. Direct `FeishuAuthClient.getTenantAccessToken(signal?)` calls also
  have a 15-second default bound, including cached-token access.
- Disconnecting an ordinary HTTP action caller aborts its Feishu work. The
  credential-delivery path retains its existing credential lifetime and
  cancellation boundary.
- Cancellation and timeout return nonretryable `feishu_operation_canceled` or
  `feishu_operation_timeout`, without exposing the caller's abort reason. A send
  may already have reached Feishu; these results are not automatically retried
  or stored for replay. Check the recipient/provider state before a manual retry.

## Retry And Dead Letter

- Action retry is still process-local and runs during the active request.
- Retry queue, idempotency, and dead-letter implementations are in-memory by
  default. Set `SOHA_FEISHU_RELIABILITY_DIR` to a writable directory to use
  file-backed retry queue, idempotency, and dead-letter persistence across
  runtime restarts.
- `GET /retry-queue` exposes retryable final action failures that a managed
  runtime can inspect or replay.
- `GET /dead-letter` exposes failed action records for local diagnosis.
- `GET /metrics` exposes webhook, duplicate, action, retry, and dead-letter
  counters for local scraping or Core-side diagnostics.
- Restarting the process clears process-local rate-limit counters and active
  request retry state. Use file-backed reliability storage before relying on
  cross-process retry inspection, webhook duplicate detection, or dead-letter
  audit.

## Upgrade

- Verify the GitHub release tarball checksum and release manifest before
  replacing a running connector package.
- Compare `connectors/feishu/connector.manifest.json` and
  `connectors/capability-matrix.json` with the previous release before rollout.
- Roll one runtime instance first and confirm `/healthz`, `/manifest`, and a
  synthetic authenticated action request before widening rollout.

## Rollback

- Restore the previous verified release tarball and checksum pair.
- Reuse the previous connector manifest and capability matrix.
- Preserve dead-letter output from the failed version for audit and incident
  follow-up.
