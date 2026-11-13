import assert from "node:assert/strict";
import { mkdtemp } from "node:fs/promises";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import { FeishuConnector } from "../src/connectors/feishu/connector.js";
import {
  loadFeishuHttpRuntimeConfigFromEnv,
  startFeishuConnectorServer
} from "../src/connectors/feishu/server.js";
import type { ConnectorLogger } from "../src/sdk/types.js";

test("Feishu HTTP runtime exposes health, manifest, webhook, action, and dead-letter endpoints", async (t) => {
  const feishuApi = await startMockFeishuApi({ failSendCount: 1 });
  t.after(async () => {
    await feishuApi.close();
  });

  const handle = await startFeishuConnectorServer(
    {
      connector: {
        appId: "cli_a",
        appSecret: "app-secret",
        verificationToken: "verification-token",
        baseUrl: feishuApi.url,
        autoReply: {
          enabled: true,
          text: "received {{eventType}}"
        },
        reliability: {
          maxRetries: 1,
          retryBackoffMs: 1,
          actionRateLimitPerMinute: 10
        }
      },
      http: {
        host: "127.0.0.1",
        port: 0,
        actionToken: "runtime-token",
        maxBodyBytes: 1024 * 1024
      },
      logLevel: "error"
    },
    silentLogger
  );
  t.after(async () => {
    await handle.close();
  });

  const health = await fetchJson(`${handle.url}/healthz`);
  assert.equal(health.status, 200);
  assert.equal(health.body.status, "ok");

  const manifest = await fetchJson(`${handle.url}/manifest`);
  assert.equal(manifest.status, 200);
  assert.equal(manifest.body.runtime.entrypoint, "src/connectors/feishu/server.ts");

  const webhookBody = JSON.stringify(messagePayload());
  const webhook = await fetchJson(`${handle.url}/webhooks/feishu`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json"
    },
    body: webhookBody
  });
  assert.equal(webhook.status, 200);
  assert.equal(webhook.body.eventCount, 1);
  assert.equal(webhook.body.actionCount, 1);
  assert.equal(feishuApi.sendBodies.length, 2);
  assert.equal(JSON.parse(feishuApi.sendBodies[1]?.content ?? "{}").text, "received im.message.receive_v1");

  const duplicateWebhook = await fetchJson(`${handle.url}/webhooks/feishu`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json"
    },
    body: webhookBody
  });
  assert.equal(duplicateWebhook.status, 200);
  assert.equal(duplicateWebhook.body.duplicate, true);
  assert.equal(feishuApi.sendBodies.length, 2);

  const unauthorized = await fetchJson(`${handle.url}/actions/feishu.message.send_text`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json"
    },
    body: JSON.stringify({
      input: {
        receiveIdType: "chat_id",
        receiveId: "oc_chat",
        text: "manual"
      }
    })
  });
  assert.equal(unauthorized.status, 401);

  const action = await fetchJson(`${handle.url}/actions/feishu.message.send_text`, {
    method: "POST",
    headers: {
      Authorization: "Bearer runtime-token",
      "Content-Type": "application/json",
      "X-Request-ID": "request-1"
    },
    body: JSON.stringify({
      input: {
        receiveIdType: "chat_id",
        receiveId: "oc_chat",
        text: "manual"
      }
    })
  });
  assert.equal(action.status, 200);
  assert.equal(action.body.ok, true);

  const deadLetter = await fetchJson(`${handle.url}/dead-letter`, {
    headers: {
      Authorization: "Bearer runtime-token"
    }
  });
  assert.equal(deadLetter.status, 200);
  assert.deepEqual(deadLetter.body.entries, []);

  const retryQueue = await fetchJson(`${handle.url}/retry-queue`, {
    headers: {
      Authorization: "Bearer runtime-token"
    }
  });
  assert.equal(retryQueue.status, 200);
  assert.deepEqual(retryQueue.body.entries, []);

  const metrics = await fetchJson(`${handle.url}/metrics`);
  assert.equal(metrics.status, 200);
  assert.equal(metrics.body.connectorId, "feishu");
  assert.equal(metrics.body.metrics.webhooksReceived, 2);
  assert.equal(metrics.body.metrics.webhookDuplicates, 1);
  assert.equal(metrics.body.metrics.actionsSucceeded, 2);
  assert.equal(metrics.body.metrics.actionRetries, 1);
  assert.equal(metrics.body.metrics.deadLetters, 0);
});

test("Feishu HTTP runtime rejects action endpoints when the runtime token is unset", async (t) => {
  const handle = await startFeishuConnectorServer(
    {
      connector: {
        appId: "cli_a",
        appSecret: "app-secret",
        verificationToken: "verification-token",
        baseUrl: "http://127.0.0.1:1/open-apis/"
      },
      http: {
        host: "127.0.0.1",
        port: 0,
        maxBodyBytes: 1024 * 1024
      },
      logLevel: "error"
    },
    silentLogger
  );
  t.after(async () => {
    await handle.close();
  });

  const action = await fetchJson(`${handle.url}/actions/feishu.message.send_text`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json"
    },
    body: JSON.stringify({
      input: {
        receiveIdType: "chat_id",
        receiveId: "oc_chat",
        text: "manual"
      }
    })
  });
  assert.equal(action.status, 401);
  assert.equal(action.body.error?.code, "unauthorized");

  const deadLetter = await fetchJson(`${handle.url}/dead-letter`);
  assert.equal(deadLetter.status, 401);
  assert.equal(deadLetter.body.error?.code, "unauthorized");
});

test("Feishu HTTP runtime delivers normalized webhook events to the configured event sink", async (t) => {
  const eventSink = await startMockEventSink({ status: 202 });
  t.after(async () => {
    await eventSink.close();
  });

  const handle = await startFeishuConnectorServer(
    {
      connector: {
        appId: "cli_a",
        appSecret: "app-secret",
        verificationToken: "verification-token",
        baseUrl: "http://127.0.0.1:1/open-apis/"
      },
      http: {
        host: "127.0.0.1",
        port: 0,
        actionToken: "runtime-token",
        maxBodyBytes: 1024 * 1024,
        eventSink: {
          url: eventSink.url,
          token: "sink-token",
          timeoutMs: 1000
        }
      },
      logLevel: "error"
    },
    silentLogger
  );
  t.after(async () => {
    await handle.close();
  });

  const webhook = await fetchJson(`${handle.url}/webhooks/feishu`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json"
    },
    body: JSON.stringify(messagePayload())
  });

  assert.equal(webhook.status, 200);
  assert.equal(eventSink.requests.length, 1);
  assert.equal(eventSink.requests[0]?.authorization, "Bearer sink-token");
  assert.equal(eventSink.requests[0]?.contentType, "application/json");
  assert.equal(eventSink.requests[0]?.userAgent, "opensoha-connector-runtime/0.1");
  const sinkBody = eventSink.requests[0]?.body;
  assert.deepEqual(Object.keys(sinkBody ?? {}).sort(), ["connectorId", "events"]);
  assert.equal(sinkBody?.connectorId, "feishu");
  assert.equal(sinkBody?.events?.length, 1);
  const event = sinkBody?.events?.[0] as Record<string, any> | undefined;
  assert.deepEqual(Object.keys(event ?? {}).sort(), ["id", "occurredAt", "payload", "source", "subject", "type"]);
  assert.equal(event?.id, "event-1");
  assert.equal(event?.source, "feishu");
  assert.equal(event?.type, "im.message.receive_v1");
  assert.equal(event?.occurredAt, "2024-03-09T16:00:00.000Z");
  assert.equal(event?.subject, "oc_chat");
  assert.equal(typeof event?.payload, "object");
});

test("Feishu HTTP runtime returns retryable 503 when the configured event sink rejects events", async (t) => {
  const eventSink = await startMockEventSink({ status: 500 });
  t.after(async () => {
    await eventSink.close();
  });

  const handle = await startFeishuConnectorServer(
    {
      connector: {
        appId: "cli_a",
        appSecret: "app-secret",
        verificationToken: "verification-token",
        baseUrl: "http://127.0.0.1:1/open-apis/"
      },
      http: {
        host: "127.0.0.1",
        port: 0,
        actionToken: "runtime-token",
        maxBodyBytes: 1024 * 1024,
        eventSink: {
          url: eventSink.url,
          token: "sink-token",
          timeoutMs: 1000
        }
      },
      logLevel: "error"
    },
    silentLogger
  );
  t.after(async () => {
    await handle.close();
  });

  const webhook = await fetchJson(`${handle.url}/webhooks/feishu`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json"
    },
    body: JSON.stringify(messagePayload())
  });

  assert.equal(webhook.status, 503);
  assert.equal(webhook.body.error?.code, "connector_event_sink_failed");
  assert.equal(webhook.body.error?.retryable, true);
  assert.equal(eventSink.requests.length, 1);
});

test("Feishu runtime config loads from environment with secret conventions", () => {
  const config = loadFeishuHttpRuntimeConfigFromEnv({
    SOHA_CONNECTOR_HTTP_HOST: "127.0.0.1",
    SOHA_CONNECTOR_HTTP_PORT: "4321",
    SOHA_CONNECTOR_HTTP_TOKEN: "runtime-token",
    SOHA_CONNECTOR_EVENT_SINK_URL: "https://soha.example.test/api/v1/connectors/events",
    SOHA_CONNECTOR_EVENT_SINK_TOKEN: "sink-token",
    SOHA_CONNECTOR_EVENT_SINK_TIMEOUT_MS: "1234",
    SOHA_CONNECTOR_LOG_LEVEL: "debug",
    SOHA_FEISHU_APP_ID: "cli_a",
    SOHA_FEISHU_APP_SECRET: "app-secret",
    SOHA_FEISHU_VERIFICATION_TOKEN: "verification-token",
    SOHA_FEISHU_ENCRYPT_KEY: "encrypt-key",
    SOHA_FEISHU_BASE_URL: "https://feishu.test/open-apis/",
    SOHA_FEISHU_AUTO_REPLY_ENABLED: "true",
    SOHA_FEISHU_AUTO_REPLY_TEXT: "ack",
    SOHA_FEISHU_RETRY_MAX: "3",
    SOHA_FEISHU_RETRY_BACKOFF_MS: "5",
    SOHA_FEISHU_ACTION_RATE_LIMIT_PER_MINUTE: "9",
    SOHA_FEISHU_RELIABILITY_DIR: "/var/lib/soha-connectors/feishu"
  });

  assert.equal(config.http.host, "127.0.0.1");
  assert.equal(config.http.port, 4321);
  assert.equal(config.http.actionToken, "runtime-token");
  assert.equal(config.http.eventSink?.url, "https://soha.example.test/api/v1/connectors/events");
  assert.equal(config.http.eventSink?.token, "sink-token");
  assert.equal(config.http.eventSink?.timeoutMs, 1234);
  assert.equal(config.logLevel, "debug");
  assert.equal(config.connector.encryptKey, "encrypt-key");
  assert.equal(config.connector.autoReply?.enabled, true);
  assert.equal(config.connector.autoReply?.text, "ack");
  assert.equal(config.connector.reliability?.maxRetries, 3);
  assert.equal(config.connector.reliability?.retryBackoffMs, 5);
  assert.equal(config.connector.reliability?.actionRateLimitPerMinute, 9);
  assert.equal(config.connector.reliability?.persistenceDir, "/var/lib/soha-connectors/feishu");
});

test("Feishu connector records retry-exhausted action failures in dead letter queue", async () => {
  const calls: string[] = [];
  const persistenceDir = await mkdtemp(path.join(os.tmpdir(), "soha-feishu-retry-"));
  const connector = new FeishuConnector();
  connector.configure({
    appId: "cli_a",
    appSecret: "app-secret",
    verificationToken: "verification-token",
    reliability: {
      maxRetries: 1,
      retryBackoffMs: 1,
      persistenceDir
    },
    fetch: async (input) => {
      const url = input instanceof Request ? input.url : input.toString();
      calls.push(url);
      if (url.endsWith("/auth/v3/tenant_access_token/internal/")) {
        return jsonResponse({
          code: 0,
          tenant_access_token: "token-1",
          expire: 7200
        });
      }
      return jsonResponse({ code: 500, msg: "temporary" }, 500);
    }
  });
  connector.start({ connectorId: "feishu", logger: silentLogger });

  const result = await connector.dispatchAction({
    action: "feishu.message.send_text",
    requestId: "request-1",
    input: {
      receiveIdType: "chat_id",
      receiveId: "oc_chat",
      text: "manual"
    }
  });

  assert.equal(result.ok, false);
  assert.equal(result.error?.retryable, true);
  assert.equal(calls.filter((url) => url.includes("/im/v1/messages")).length, 2);
  const deadLetters = connector.getDeadLetters();
  assert.equal(deadLetters.length, 1);
  assert.equal(deadLetters[0]?.requestId, "request-1");
  assert.equal(deadLetters[0]?.error.code, "feishu_api_error");
  const retryQueue = connector.getRetryQueue();
  assert.equal(retryQueue.length, 1);
  assert.equal(retryQueue[0]?.requestId, "request-1");
  assert.equal(retryQueue[0]?.attempts, 2);
  assert.equal(connector.getMetrics().retryQueueDepth, 1);
});

const silentLogger: ConnectorLogger = {
  debug: () => {},
  info: () => {},
  warn: () => {},
  error: () => {}
};

interface MockFeishuApi {
  url: string;
  sendBodies: Array<Record<string, string>>;
  close(): Promise<void>;
}

interface MockEventSink {
  url: string;
  requests: Array<{
    authorization: string | undefined;
    contentType: string | undefined;
    userAgent: string | undefined;
    body: Record<string, any>;
  }>;
  close(): Promise<void>;
}

async function startMockEventSink(options: { status: number }): Promise<MockEventSink> {
  const requests: MockEventSink["requests"] = [];
  const server = createServer(async (request, response) => {
    if (request.method === "POST" && request.url === "/api/v1/connectors/events") {
      requests.push({
        authorization: typeof request.headers.authorization === "string" ? request.headers.authorization : undefined,
        contentType: typeof request.headers["content-type"] === "string" ? request.headers["content-type"] : undefined,
        userAgent: typeof request.headers["user-agent"] === "string" ? request.headers["user-agent"] : undefined,
        body: JSON.parse((await readBody(request)).toString("utf8")) as Record<string, any>
      });
      writeJson(response, options.status, { ok: options.status >= 200 && options.status < 300 });
      return;
    }
    writeJson(response, 404, { ok: false });
  });
  await listen(server);
  const address = server.address() as AddressInfo;
  return {
    url: `http://127.0.0.1:${address.port}/api/v1/connectors/events`,
    requests,
    close: () => closeServer(server)
  };
}

async function startMockFeishuApi(options: { failSendCount: number }): Promise<MockFeishuApi> {
  const sendBodies: Array<Record<string, string>> = [];
  let remainingFailures = options.failSendCount;
  const server = createServer(async (request, response) => {
    if (request.method === "POST" && request.url === "/auth/v3/tenant_access_token/internal/") {
      writeJson(response, 200, {
        code: 0,
        tenant_access_token: "token-1",
        expire: 7200
      });
      return;
    }

    if (request.method === "POST" && request.url?.startsWith("/im/v1/messages")) {
      const body = JSON.parse((await readBody(request)).toString("utf8")) as Record<string, string>;
      sendBodies.push(body);
      if (remainingFailures > 0) {
        remainingFailures -= 1;
        writeJson(response, 500, {
          code: 500,
          msg: "temporary"
        });
        return;
      }
      writeJson(response, 200, {
        code: 0,
        data: {
          message_id: `om_${sendBodies.length}`
        }
      });
      return;
    }

    writeJson(response, 404, {
      code: 404,
      msg: "not found"
    });
  });
  await listen(server);
  const address = server.address() as AddressInfo;
  return {
    url: `http://127.0.0.1:${address.port}`,
    sendBodies,
    close: () => closeServer(server)
  };
}

async function fetchJson(url: string, init?: RequestInit): Promise<{ status: number; body: Record<string, any> }> {
  const response = await fetch(url, init);
  return {
    status: response.status,
    body: (await response.json()) as Record<string, any>
  };
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type": "application/json"
    }
  });
}

function readBody(request: IncomingMessage): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    request.on("data", (chunk: Buffer) => chunks.push(chunk));
    request.on("end", () => resolve(Buffer.concat(chunks)));
    request.on("error", reject);
  });
}

function writeJson(response: ServerResponse, status: number, body: unknown): void {
  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8"
  });
  response.end(JSON.stringify(body));
}

function listen(server: Server): Promise<void> {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      resolve();
    });
  });
}

function closeServer(server: Server): Promise<void> {
  return new Promise((resolve, reject) => {
    server.close((error) => {
      if (error) {
        reject(error);
        return;
      }
      resolve();
    });
  });
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
