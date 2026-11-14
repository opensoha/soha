# Feishu Connector

Status: experimental.

This connector is the first OpenSoha connector skeleton. It validates the
connector interface, lifecycle, manifest shape, webhook security model, and one
event-to-action loop. It now also includes a Node HTTP runtime adapter for local
and deployment smoke tests.

## Capabilities

| Capability | Direction | Status |
| --- | --- | --- |
| URL verification challenge | Inbound | Implemented |
| Verification Token check | Inbound | Implemented |
| Encrypt Key signature verification | Inbound | Implemented for non-challenge events |
| Encrypted event payload decryption | Inbound | Implemented |
| `im.message.receive_v1` normalization | Inbound | Implemented |
| `feishu.message.send_text` | Outbound | Implemented |
| HTTP webhook server | Inbound | Implemented |
| HTTP action endpoint | Outbound | Implemented |
| HTTP event sink forwarding | Outbound | Implemented |
| Config loading from env | Both | Implemented |
| Graceful shutdown | Both | Implemented |
| Rate limits, retries, idempotency cache, dead letters | Both | Implemented in memory |
| Structured logs and secret redaction | Both | Minimal implementation |

## Config

| Field | Env | Required | Description |
| --- | --- | --- | --- |
| `appId` | `SOHA_FEISHU_APP_ID` | Yes | Feishu custom app ID. |
| `appSecret` | `SOHA_FEISHU_APP_SECRET` | Yes | Used to fetch `tenant_access_token`. |
| `verificationToken` | `SOHA_FEISHU_VERIFICATION_TOKEN` | Yes | Event subscription Verification Token. |
| `encryptKey` | `SOHA_FEISHU_ENCRYPT_KEY` | No | Enables signature verification and encrypted payload decryption. |
| `baseUrl` | `SOHA_FEISHU_BASE_URL` | No | Defaults to `https://open.feishu.cn/open-apis`. |
| `autoReply.enabled` | `SOHA_FEISHU_AUTO_REPLY_ENABLED` | No | Enables local smoke-test auto replies. |
| `autoReply.text` | `SOHA_FEISHU_AUTO_REPLY_TEXT` | No | Auto-reply text template. |
| `reliability.maxRetries` | `SOHA_FEISHU_RETRY_MAX` | No | Defaults to `2`. |
| `reliability.retryBackoffMs` | `SOHA_FEISHU_RETRY_BACKOFF_MS` | No | Defaults to `250`. |
| `reliability.idempotencyTtlMs` | `SOHA_FEISHU_IDEMPOTENCY_TTL_MS` | No | Defaults to one day. |
| `reliability.actionRateLimitPerMinute` | `SOHA_FEISHU_ACTION_RATE_LIMIT_PER_MINUTE` | No | Defaults to `120`. |
| `reliability.maxDeadLetters` | `SOHA_FEISHU_DEAD_LETTER_MAX` | No | Defaults to `100`. |
| `reliability.maxRetryQueueEntries` | `SOHA_FEISHU_RETRY_QUEUE_MAX` | No | Defaults to `1000`. |
| `reliability.persistenceDir` | `SOHA_FEISHU_RELIABILITY_DIR` | No | Enables file-backed idempotency and dead-letter persistence. |

Shared HTTP runtime fields are defined in `.env.example`:

- `SOHA_CONNECTOR_HTTP_HOST`
- `SOHA_CONNECTOR_HTTP_PORT`
- `SOHA_CONNECTOR_HTTP_TOKEN`
- `SOHA_CONNECTOR_HTTP_MAX_BODY_BYTES`
- `SOHA_CONNECTOR_LOG_LEVEL`
- `SOHA_CONNECTOR_EVENT_SINK_URL`
- `SOHA_CONNECTOR_EVENT_SINK_TOKEN`
- `SOHA_CONNECTOR_EVENT_SINK_TIMEOUT_MS`

## HTTP Runtime

After `npm run build`, run:

```bash
npm run start:feishu
```

Runtime endpoints:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Health and reliability details. |
| `GET` | `/manifest` | Connector manifest snapshot. |
| `POST` | `/webhooks/feishu` | Feishu webhook ingress. |
| `POST` | `/actions/feishu.message.send_text` | Protected outbound action endpoint. |
| `GET` | `/retry-queue` | Protected retry queue inspection. |
| `GET` | `/dead-letter` | Protected dead-letter inspection. |
| `GET` | `/metrics` | Runtime counters for webhooks, duplicates, actions, retries, and dead letters. |

Action endpoints require `Authorization: Bearer $SOHA_CONNECTOR_HTTP_TOKEN`
when the token is configured.

## Permissions

Minimum bot action permission:

- `im:message:send_as_bot`

Optional read/send permission:

- `im:message`

## Event And Action Loop

`handleWebhook` verifies and normalizes Feishu callbacks into an OpenSoha
`ConnectorEvent`. If `autoReply.enabled` is true and the event contains a
`chat_id`, the connector dispatches `feishu.message.send_text`.

When `SOHA_CONNECTOR_EVENT_SINK_URL` is configured, normalized events are sent
to that sink before the webhook response is acknowledged. Sink failures return
retryable `503` from the webhook endpoint so the provider can retry the current
delivery.

Set `SOHA_CONNECTOR_EVENT_SINK_URL` to the Core sink endpoint:

```text
POST /api/v1/connectors/events
Content-Type: application/json
User-Agent: opensoha-connector-runtime/0.1
Authorization: Bearer <SOHA_CONNECTOR_EVENT_SINK_TOKEN>  # optional
```

The body is the connector event batch wrapper:

```json
{
  "connectorId": "feishu",
  "events": [
    {
      "id": "event-1",
      "type": "im.message.receive_v1",
      "source": "feishu",
      "occurredAt": "2024-03-09T16:00:00.000Z",
      "subject": "oc_chat",
      "payload": {}
    }
  ]
}
```

The auto-reply loop is intentionally small. It proves config, auth, webhook
verification, event normalization, tenant token exchange, and outbound action
execution without taking a dependency on the Soha core source tree.

## Reliability Boundary

The current reliability implementation defaults to memory, with file-backed
retry queue, idempotency, and dead-letter persistence when
`SOHA_FEISHU_RELIABILITY_DIR` is set:

- duplicate webhook events are ignored by normalized event ID;
- Feishu HTTP 429 and 5xx action responses retry with exponential backoff;
- retryable final action failures are recorded in the protected
  `/retry-queue` endpoint when file-backed reliability is enabled;
- retry-exhausted action failures are recorded in memory or under the
  configured reliability directory;
- outbound action execution is protected by a fixed-window in-memory rate
  limiter;
- `/metrics` exposes webhook, duplicate, action, retry, and dead-letter
  counters;
- JSON logs redact common secret, token, signature, and authorization fields.

Core-side query and Soha audit persistence remain runtime/core integration
work.

## Limitations

- Retry and rate-limit state is process-local and resets on restart.
- The HTTP event sink is synchronous and does not provide durable buffering by
  itself.
- Feishu webhook idempotency can record an event before event sink delivery
  succeeds. If the sink fails and the provider retries, the retry may be
  treated as a duplicate and not forwarded to the sink again.
- Message content is not deeply parsed or redacted.
- Only one event type and one action are implemented.
