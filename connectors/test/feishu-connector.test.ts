import assert from "node:assert/strict";
import { test } from "node:test";

import { FeishuConnector } from "../src/connectors/feishu/connector.js";
import { calculateFeishuSignature } from "../src/connectors/feishu/webhook.js";

test("Feishu connector lifecycle, webhook normalization, and auto-reply action loop", async () => {
  const calls: Array<{ url: string; init?: RequestInit }> = [];
  const connector = new FeishuConnector();

  connector.configure({
    appId: "cli_a",
    appSecret: "app-secret",
    verificationToken: "verification-token",
    encryptKey: "encrypt-key",
    baseUrl: "https://feishu.test/open-apis",
    autoReply: {
      enabled: true,
      text: "received {{eventType}}"
    },
    fetch: mockFetch(calls)
  });
  assert.equal(connector.state, "configured");

  connector.start({
    connectorId: "feishu",
    now: () => new Date("2024-03-09T16:00:00.000Z")
  });
  assert.equal(connector.state, "started");

  const rawBody = JSON.stringify(messagePayload());
  const response = await connector.handleWebhook({
    headers: {
      "X-Lark-Request-Timestamp": "1710000000",
      "X-Lark-Request-Nonce": "nonce",
      "X-Lark-Signature": calculateFeishuSignature({
        timestamp: "1710000000",
        nonce: "nonce",
        encryptKey: "encrypt-key",
        rawBody
      })
    },
    rawBody
  });

  assert.equal(response.status, 200);
  assert.equal(response.events?.[0]?.id, "event-1");
  assert.equal(response.events?.[0]?.type, "im.message.receive_v1");
  assert.equal(response.events?.[0]?.subject, "oc_chat");
  assert.equal(response.actions?.[0]?.ok, true);
  assert.equal(calls.length, 2);

  const tokenRequest = JSON.parse(String(calls[0]?.init?.body));
  assert.deepEqual(tokenRequest, {
    app_id: "cli_a",
    app_secret: "app-secret"
  });

  const sendUrl = new URL(calls[1]?.url ?? "");
  assert.equal(sendUrl.pathname, "/open-apis/im/v1/messages");
  assert.equal(sendUrl.searchParams.get("receive_id_type"), "chat_id");
  assert.equal(calls[1]?.init?.headers && (calls[1].init.headers as Record<string, string>).Authorization, "Bearer token-1");

  const sendBody = JSON.parse(String(calls[1]?.init?.body));
  assert.equal(sendBody.receive_id, "oc_chat");
  assert.equal(JSON.parse(sendBody.content).text, "received im.message.receive_v1");
});

test("Feishu connector handles URL verification challenge", async () => {
  const connector = new FeishuConnector();
  connector.configure({
    appId: "cli_a",
    appSecret: "app-secret",
    verificationToken: "verification-token",
    fetch: mockFetch([])
  });
  connector.start({
    connectorId: "feishu",
    now: () => new Date("2024-03-09T16:00:00.000Z")
  });

  const response = await connector.handleWebhook({
    headers: {},
    rawBody: JSON.stringify({
      type: "url_verification",
      token: "verification-token",
      challenge: "challenge-value"
    })
  });

  assert.equal(response.status, 200);
  assert.deepEqual(response.body, {
    challenge: "challenge-value"
  });
});

test("Feishu connector rejects unsupported actions", async () => {
  const connector = new FeishuConnector();
  connector.configure({
    appId: "cli_a",
    appSecret: "app-secret",
    verificationToken: "verification-token",
    fetch: mockFetch([])
  });
  connector.start({
    connectorId: "feishu"
  });

  const result = await connector.dispatchAction({
    action: "feishu.unknown",
    input: {}
  });

  assert.equal(result.ok, false);
  assert.equal(result.error?.code, "unsupported_action");
});

function mockFetch(calls: Array<{ url: string; init?: RequestInit }>): typeof fetch {
  return async (input: string | URL | Request, init?: RequestInit) => {
    const url = input instanceof Request ? input.url : input.toString();
    calls.push(init ? { url, init } : { url });

    if (url.endsWith("/auth/v3/tenant_access_token/internal/")) {
      return new Response(
        JSON.stringify({
          code: 0,
          tenant_access_token: "token-1",
          expire: 7200
        }),
        {
          status: 200,
          headers: {
            "Content-Type": "application/json"
          }
        }
      );
    }

    if (url.includes("/im/v1/messages")) {
      return new Response(
        JSON.stringify({
          code: 0,
          data: {
            message_id: "om_reply"
          }
        }),
        {
          status: 200,
          headers: {
            "Content-Type": "application/json"
          }
        }
      );
    }

    return new Response(JSON.stringify({ code: 404, msg: "not found" }), {
      status: 404,
      headers: {
        "Content-Type": "application/json"
      }
    });
  };
}

function messagePayload() {
  return {
    schema: "2.0",
    header: {
      event_id: "event-1",
      event_type: "im.message.receive_v1",
      create_time: "1710000000000",
      token: "verification-token",
      tenant_key: "tenant-1"
    },
    event: {
      sender: {
        sender_id: {
          open_id: "ou_sender"
        }
      },
      message: {
        message_id: "om_message",
        chat_id: "oc_chat",
        message_type: "text",
        content: "{\"text\":\"hello\"}"
      }
    }
  };
}
