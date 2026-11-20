import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import { FeishuConnector } from "../src/connectors/feishu/connector.js";
import { calculateFeishuSignature } from "../src/connectors/feishu/webhook.js";
import { startConnectorHttpServer } from "../src/sdk/http.js";
import type { ConnectorWebhookRequest, ConnectorWebhookResponse } from "../src/sdk/types.js";

const config = {
  appId: "cli_a",
  appSecret: "app-secret",
  verificationToken: "verification-token"
};
const rawBody = JSON.stringify({
  schema: "2.0",
  header: { event_id: "event-1", event_type: "im.message.receive_v1", token: config.verificationToken },
  event: { message: { chat_id: "oc_chat", message_type: "text", content: "hello" } }
});

for (const firstSinkStatus of [202, 500]) {
  test(`concurrent Feishu deliveries share sink ${firstSinkStatus} and failures remain retryable`, { timeout: 10_000 }, async (t) => {
    let releaseSink!: (response: Response) => void;
    const sinkResult = new Promise<Response>((resolve) => { releaseSink = resolve; });
    let enteredSink!: () => void;
    const sinkEntered = new Promise<void>((resolve) => { enteredSink = resolve; });
    let receivedBoth!: () => void;
    const bothReceived = new Promise<void>((resolve) => { receivedBoth = resolve; });
    let received = 0;
    let settled = 0;
    class ObservedConnector extends FeishuConnector {
      override handleWebhook(request: ConnectorWebhookRequest): Promise<ConnectorWebhookResponse> {
        const processing = super.handleWebhook(request);
        if (++received === 2) receivedBoth();
        return processing.finally(() => { settled += 1; });
      }
    }
    let sinkCalls = 0;
    let autoReplies = 0;
    const handle = await startConnectorHttpServer({
      connector: new ObservedConnector(),
      config: {
        ...config,
        autoReply: { enabled: true, text: "received" },
        fetch: async (input: string | URL | Request) => {
          if (String(input).includes("/auth/")) return jsonResponse({ code: 0, tenant_access_token: "test-token", expire: 7200 });
          autoReplies += 1;
          return jsonResponse({ code: 0, data: { message_id: "reply-1" } });
        }
      },
      eventSink: {
        url: "https://sink.test/events",
        timeoutMs: 5000,
        fetch: async () => {
          sinkCalls += 1;
          if (sinkCalls === 1) {
            enteredSink();
            return sinkResult;
          }
          return jsonResponse({}, 202);
        }
      }
    });
    t.after(async () => {
      releaseSink(jsonResponse({}, 500));
      await handle.close();
    });
    const first = postWebhook(handle.url);
    await sinkEntered;
    const second = postWebhook(handle.url);
    await bothReceived;
    await new Promise<void>((resolve) => setImmediate(resolve));
    assert.equal(settled, 0, "neither handler may acknowledge an event while sink delivery is pending");
    assert.equal(sinkCalls, 1);
    assert.equal(autoReplies, 0);

    releaseSink(jsonResponse({}, firstSinkStatus));
    const results = await Promise.all([first, second]);
    if (firstSinkStatus === 500) {
      for (const result of results) {
        assert.equal(result.status, 503);
        assert.equal(result.body.error.code, "connector_event_sink_failed");
        assert.equal(result.body.error.retryable, true);
      }
      assert.equal(autoReplies, 0);
      const retry = await postWebhook(handle.url);
      assert.equal(retry.status, 200);
      assert.equal(retry.body.eventCount, 1);
      assert.equal(sinkCalls, 2);
    } else {
      assert.deepEqual(results.map((result) => result.status), [200, 200]);
      assert.equal(results.filter((result) => result.body.duplicate).length, 1);
      assert.equal(sinkCalls, 1, "the SDK must not deliver hook-managed events again");
    }
    assert.equal(autoReplies, 1);
    const duplicate = await postWebhook(handle.url);
    assert.equal(duplicate.status, 200);
    assert.equal(duplicate.body.duplicate, true);
    assert.equal(sinkCalls, firstSinkStatus === 500 ? 2 : 1);
    assert.equal(autoReplies, 1);
  });
}

test("successful sink delivery retains file-backed TTL dedupe across connector restarts", async (t) => {
  const persistenceDir = await mkdtemp(path.join(os.tmpdir(), "soha-feishu-delivery-"));
  t.after(() => rm(persistenceDir, { recursive: true, force: true }));
  let now = new Date("2026-11-19T00:00:00.000Z");
  const createConnector = () => {
    const connector = new FeishuConnector();
    connector.configure({ ...config, reliability: { persistenceDir, idempotencyTtlMs: 1000 } });
    connector.start({ connectorId: "feishu", now: () => now });
    return connector;
  };
  const first = createConnector();
  await assert.rejects(first.handleWebhook({ headers: {}, rawBody, deliverEvents: async () => { throw new Error("sink unavailable"); } }), /sink unavailable/);
  first.stop();

  let deliveries = 0;
  const request = { headers: {}, rawBody, deliverEvents: async () => { deliveries += 1; } };
  const second = createConnector();
  assert.equal((await second.handleWebhook(request)).status, 200);
  assert.equal(deliveries, 1, "failed delivery must not survive restart as completed dedupe");
  second.stop();

  const third = createConnector();
  t.after(() => third.stop());
  assert.deepEqual((await third.handleWebhook(request)).events, []);
  assert.equal(deliveries, 1);
  now = new Date(now.getTime() + 1001);
  assert.equal((await third.handleWebhook(request)).events?.length, 1);
  assert.equal(deliveries, 2);
});

test("SDK fallback still delivers providers that ignore the request hook", async (t) => {
  class LegacyConnector extends FeishuConnector {
    override async handleWebhook(): Promise<ConnectorWebhookResponse> {
      return { status: 200, body: { ok: true }, events: [{ id: "legacy-1", type: "legacy.event", source: "legacy", occurredAt: new Date().toISOString(), payload: {} }] };
    }
  }
  for (const sinkStatus of [202, 500]) {
    let calls = 0;
    const handle = await startConnectorHttpServer({
      connector: new LegacyConnector(),
      config,
      eventSink: { url: "https://sink.test/events", fetch: async () => { calls += 1; return jsonResponse({}, sinkStatus); } }
    });
    t.after(() => handle.close());
    const response = await postWebhook(handle.url);
    assert.equal(response.status, sinkStatus === 202 ? 200 : 503);
    assert.equal(calls, 1);
  }
});

test("challenge and signature rejection never enter sink delivery or bypass verification after dedupe", async (t) => {
  let deliveries = 0;
  const handle = await startConnectorHttpServer({
    connector: new FeishuConnector(),
    config: { ...config, encryptKey: "encrypt-key" },
    eventSink: { url: "https://sink.test/events", fetch: async () => { deliveries += 1; return jsonResponse({}, 202); } }
  });
  t.after(() => handle.close());
  const challenge = await postWebhook(handle.url, JSON.stringify({ type: "url_verification", token: config.verificationToken, challenge: "challenge-value" }));
  assert.equal(challenge.status, 200);
  assert.deepEqual(challenge.body, { challenge: "challenge-value" });
  assert.equal((await postWebhook(handle.url)).status, 400);
  assert.equal(deliveries, 0);
  const signedHeaders = {
    "X-Lark-Request-Timestamp": "1710000000",
    "X-Lark-Request-Nonce": "nonce",
    "X-Lark-Signature": calculateFeishuSignature({ timestamp: "1710000000", nonce: "nonce", encryptKey: "encrypt-key", rawBody })
  };
  assert.equal((await postWebhook(handle.url, rawBody, signedHeaders)).status, 200);
  assert.equal((await postWebhook(handle.url)).status, 400);
  assert.equal(deliveries, 1);
});

async function postWebhook(url: string, body = rawBody, headers: Record<string, string> = {}): Promise<{ status: number; body: Record<string, any> }> {
  const response = await fetch(`${url}/webhooks/feishu`, { method: "POST", headers: { "Content-Type": "application/json", ...headers }, body });
  return { status: response.status, body: await response.json() as Record<string, any> };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}
