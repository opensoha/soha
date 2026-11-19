import assert from "node:assert/strict";
import { request as httpRequest } from "node:http";
import { test } from "node:test";
import timers from "node:timers/promises";

import { FeishuAuthClient } from "../src/connectors/feishu/auth.js";
import { FeishuConnector } from "../src/connectors/feishu/connector.js";
import { startConnectorHttpServer } from "../src/sdk/http.js";
import { RetryableConnectorError } from "../src/sdk/reliability.js";
import type { ConnectorActionResult } from "../src/sdk/types.js";

const config = { appId: "cli_a", appSecret: "app-secret", verificationToken: "verification-token", baseUrl: "https://feishu.test/open-apis" };
const action = { action: "feishu.message.send_text", input: { receiveIdType: "chat_id", receiveId: "oc_chat", text: "hello" } };
const tokenBody = { code: 0, tenant_access_token: "test-token", expire: 7200 };

for (const [name, text, valid] of [
  ["ASCII limit", "a".repeat(4096), true],
  ["ASCII overflow", "a".repeat(4097), false],
  ["astral limit", "😀".repeat(4096), true],
  ["astral overflow", "😀".repeat(4097), false],
  ["combining limit", "e\u0301".repeat(2048), true],
  ["combining overflow", "e\u0301".repeat(2048) + "e", false]
] as const) {
  test(`send_text validates Unicode code points before outbound calls: ${name}`, async () => {
    let calls = 0;
    const connector = createConnector(async (input, init) => {
      calls += 1;
      if (String(input).includes("/auth/")) return jsonResponse(tokenBody);
      assert.equal(JSON.parse(JSON.parse(String(init?.body)).content).text, text);
      return jsonResponse({ code: 0, data: {} });
    });
    const result = await connector.dispatchAction({ ...action, input: { ...action.input, text } });
    assert.equal(result.ok, valid);
    assert.equal(calls, valid ? 2 : 0);
    if (!valid) assert.equal(result.error?.code, "invalid_action_input");
  });
}

test("preaborted action does not consume rate capacity or expose the caller reason", async () => {
  let calls = 0;
  const connector = createConnector(async (input) => {
    calls += 1;
    return jsonResponse(String(input).includes("/auth/") ? tokenBody : { code: 0, data: {} });
  }, { actionRateLimitPerMinute: 1 });
  const controller = new AbortController();
  controller.abort(new Error("private caller abort reason"));
  const result = await connector.dispatchAction({ ...action, signal: controller.signal });
  assertInterrupted(result, "feishu_operation_canceled");
  assert.equal(JSON.stringify(result).includes("private caller abort reason"), false);
  assert.equal(calls, 0);
  assert.deepEqual(connector.getDeadLetters(), []);
  assert.deepEqual(connector.getRetryQueue(), []);
  assert.equal((await connector.dispatchAction(action)).ok, true);
});

test("canceled noncooperative auth fetch cannot later send or report success", async () => {
  const authResponse = deferred<Response>();
  const authStarted = deferred<void>();
  let authCalls = 0;
  let sends = 0;
  const connector = createConnector(async (input) => {
    if (String(input).includes("/auth/")) {
      if (++authCalls === 1) { authStarted.resolve(); return authResponse.promise; }
      return jsonResponse(tokenBody);
    }
    sends += 1;
    return jsonResponse({ code: 0, data: {} });
  });
  const controller = new AbortController();
  const pending = connector.dispatchAction({ ...action, signal: controller.signal });
  await authStarted.promise;
  controller.abort(new Error("private caller reason"));
  assertInterrupted(await pending, "feishu_operation_canceled");
  authResponse.resolve(jsonResponse(tokenBody));
  await nextTurn();
  assert.equal(sends, 0);
  assert.equal(connector.getMetrics().actionsSucceeded, 0);
  assert.equal((await connector.dispatchAction(action)).ok, true);
  assert.equal(authCalls, 2, "late canceled auth must not populate the token cache");
  assert.equal(sends, 1);
});

test("public auth token requests have a bounded default even when fetch ignores abort", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  let signal: AbortSignal | undefined;
  const auth = new FeishuAuthClient({ ...config, fetch: async (_input, init) => {
    signal = init?.signal ?? undefined;
    return new Promise<Response>(() => {});
  } });
  const pending = auth.getTenantAccessToken();
  let settled = false;
  const checked = assert.rejects(pending, (error: unknown) => {
    settled = true;
    return error instanceof RetryableConnectorError && error.code === "feishu_operation_timeout" && !error.retryable;
  });
  await nextTurn();
  t.mock.timers.tick(14_999);
  await nextTurn();
  assert.equal(settled, false);
  t.mock.timers.tick(1);
  await checked;
  assert.equal(signal?.aborted, true);
});

test("late auth JSON after cancellation cannot enter the public token cache", async () => {
  const body = deferred<typeof tokenBody>();
  const bodyStarted = deferred<void>();
  let calls = 0;
  const auth = new FeishuAuthClient({ ...config, fetch: async () => {
    if (++calls === 1) return { ok: true, json: () => { bodyStarted.resolve(); return body.promise; } } as Response;
    return jsonResponse({ ...tokenBody, tenant_access_token: "fresh-token" });
  } });
  const controller = new AbortController();
  const pending = auth.getTenantAccessToken(controller.signal);
  await bodyStarted.promise;
  controller.abort(new Error("private auth reason"));
  await assert.rejects(pending, (error: unknown) => error instanceof RetryableConnectorError && error.code === "feishu_operation_canceled" && !error.message.includes("private"));
  body.resolve(tokenBody);
  await nextTurn();
  assert.equal(await auth.getTenantAccessToken(), "fresh-token");
  assert.equal(calls, 2);
  const canceled = new AbortController();
  canceled.abort();
  await assert.rejects(auth.getTenantAccessToken(canceled.signal), (error: unknown) => error instanceof RetryableConnectorError && error.code === "feishu_operation_canceled");
  assert.equal(calls, 2);
});

test("one overall deadline covers auth, retry wait and the next response JSON body", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  let sends = 0;
  let bodyStarted = false;
  const connector = createConnector(async (input, init) => {
    if (String(input).includes("/auth/")) {
      await timers.setTimeout(4000, undefined, { signal: init?.signal ?? undefined });
      return jsonResponse(tokenBody);
    }
    if (++sends === 1) return new Response("", { status: 429, headers: { "Retry-After": "3" } });
    return { ok: true, json: async () => {
      bodyStarted = true;
      await timers.setTimeout(9000, undefined, { signal: init?.signal ?? undefined });
      return { code: 0, data: {} };
    } } as Response;
  });
  let settled = false;
  const pending = connector.dispatchAction(action).then((result) => { settled = true; return result; });
  await nextTurn();
  t.mock.timers.tick(4000);
  await nextTurn();
  assert.equal(sends, 1);
  t.mock.timers.tick(3000);
  await nextTurn();
  assert.equal(sends, 2);
  assert.equal(bodyStarted, true);
  t.mock.timers.tick(7999);
  await nextTurn();
  assert.equal(settled, false);
  t.mock.timers.tick(1);
  assertInterrupted(await pending, "feishu_operation_timeout");
  assert.equal(connector.getMetrics().actionsSucceeded, 0);
  assert.deepEqual(connector.getDeadLetters(), []);
});

for (const mode of ["caller", "deadline"] as const) {
  test(`${mode} abort stops retry backoff without another send or a dead letter`, async (t) => {
    t.mock.timers.enable({ apis: ["setTimeout"] });
    let sends = 0;
    const connector = createConnector(async (input) => {
      if (String(input).includes("/auth/")) return jsonResponse(tokenBody);
      sends += 1;
      return new Response("", { status: 429, headers: { "Retry-After": "999999999" } });
    });
    const controller = new AbortController();
    const pending = connector.dispatchAction({ ...action, signal: controller.signal });
    await nextTurn();
    assert.equal(sends, 1);
    if (mode === "caller") controller.abort(new Error("private backoff reason"));
    else t.mock.timers.tick(15_000);
    assertInterrupted(await pending, mode === "caller" ? "feishu_operation_canceled" : "feishu_operation_timeout");
    t.mock.timers.tick(30_000);
    await nextTurn();
    assert.equal(sends, 1);
    assert.deepEqual(connector.getDeadLetters(), []);
    assert.deepEqual(connector.getRetryQueue(), []);
  });
}

test("HTTP caller disconnect aborts an ordinary Feishu action without credential delivery", { timeout: 3000 }, async (t) => {
  const started = deferred<void>();
  const aborted = deferred<void>();
  const connector = new FeishuConnector();
  const handle = await startConnectorHttpServer({
    connector,
    config: { ...config, fetch: async (_input, init) => {
      started.resolve();
      init?.signal?.addEventListener("abort", () => aborted.resolve(), { once: true });
      return new Promise<Response>(() => {});
    } },
    actionToken: "runtime-token"
  });
  const request = httpRequest(handle.url + "/actions/feishu.message.send_text", { method: "POST", headers: { Authorization: "Bearer runtime-token", "Content-Type": "application/json" } });
  request.on("error", () => {});
  t.after(async () => { request.destroy(); await handle.close(); });
  request.end(JSON.stringify({ input: action.input }));
  await started.promise;
  request.destroy();
  await aborted.promise;
  await nextTurn();
  assert.equal(connector.getMetrics().actionsSucceeded, 0);
  assert.deepEqual(connector.getDeadLetters(), []);
});

function createConnector(fetchImpl: typeof fetch, reliability = {}) {
  const connector = new FeishuConnector();
  connector.configure({ ...config, fetch: fetchImpl, reliability });
  connector.start({ connectorId: "feishu" });
  return connector;
}

function assertInterrupted(result: ConnectorActionResult, code: string): void {
  assert.equal(result.ok, false);
  assert.equal(result.error?.code, code);
  assert.equal(result.error?.retryable, false);
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

function nextTurn(): Promise<void> {
  return new Promise((resolve) => setImmediate(resolve));
}
