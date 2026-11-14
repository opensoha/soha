import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { request as httpRequest } from "node:http";
import { test } from "node:test";

import { feishuManifest } from "../src/connectors/feishu/manifest.js";
import { parseCredentialDelivery } from "../src/sdk/credentials.js";
import { startConnectorHttpServer } from "../src/sdk/http.js";
import { BaseConnector } from "../src/sdk/runtime.js";
import { CONNECTOR_ACTION_PROTOCOL, type ConnectorActionDefinition, type ConnectorActionRequest, type ConnectorActionResult, type ConnectorManifest, type ConnectorWebhookResponse } from "../src/sdk/types.js";

class CredentialFixture extends BaseConnector<Record<string, never>> {
  readonly id = "fixture";
  readonly manifest: ConnectorManifest;
  calls: ConnectorActionRequest[] = [];
  handler: (request: ConnectorActionRequest) => Promise<ConnectorActionResult> = async (request) => ({ ok: true, output: { received: request.credentials?.entries.SOURCE?.value, signal: !!request.signal } });

  constructor(protocol = true) {
    super();
    this.manifest = { ...feishuManifest, id: this.id, actions: [{ name: "fixture.read", description: "Fixture read", inputSchema: {}, ...(protocol ? { credentialProtocol: CONNECTOR_ACTION_PROTOCOL, credentialAliases: ["SOURCE"] } : {}) }] };
  }
  async handleWebhook(): Promise<ConnectorWebhookResponse> { return { status: 404, body: {} }; }
  async dispatchAction(request: ConnectorActionRequest): Promise<ConnectorActionResult> {
    this.calls.push(request);
    return this.handler(request);
  }
}

function frame(value: string = randomUUID()): Record<string, unknown> {
  return { schemaVersion: CONNECTOR_ACTION_PROTOCOL, action: "fixture.read", input: {}, credentials: { expiresAt: new Date(Date.now() + 10_000).toISOString(), entries: { SOURCE: { reference: "soha://secrets/fixture/versions/2", value } } } };
}

test("credential action and request identifiers use the schema character bound", () => {
  const action = "😀".repeat(128);
  const declaration: ConnectorActionDefinition = { name: action, description: "fixture", inputSchema: {}, credentialProtocol: CONNECTOR_ACTION_PROTOCOL, credentialAliases: ["SOURCE"] };
  const body = { ...frame(), action, requestId: action };
  assert.ok(parseCredentialDelivery(body, action, declaration));
  assert.throws(() => parseCredentialDelivery({ ...body, action: action + "😀" }, action + "😀", { ...declaration, name: action + "😀" }));
  assert.throws(() => parseCredentialDelivery({ ...body, requestId: action + "😀" }, action, declaration));
});

test("credential HTTP delivery is per invocation and masks result material", async (t) => {
  const connector = new CredentialFixture();
  const logs: unknown[] = [];
  const runtimeToken = randomUUID();
  const handle = await startConnectorHttpServer({ connector, config: {}, actionToken: runtimeToken, logger: { debug: (_m, fields) => logs.push(fields), info: (_m, fields) => logs.push(fields), warn: (_m, fields) => logs.push(fields), error: (_m, fields) => logs.push(fields) } });
  t.after(() => handle.close());
  const firstSecret = randomUUID();
  const response = await fetch(handle.url + "/actions/fixture.read", { method: "POST", headers: { Authorization: `Bearer ${runtimeToken}`, "Content-Type": "application/json", "X-Request-ID": firstSecret }, body: JSON.stringify(frame(firstSecret)) });
  assert.equal(response.status, 200);
  const body = await response.text();
  assert.equal(body.includes(firstSecret), false);
  assert.equal(JSON.parse(body).output.signal, true);
  assert.equal(JSON.parse(body).output.received, "[REDACTED]");
  assert.equal(JSON.stringify(logs).includes(firstSecret), false);
  const secondSecret = randomUUID();
  await fetch(handle.url + "/actions/fixture.read", { method: "POST", headers: { Authorization: `Bearer ${runtimeToken}`, "Content-Type": "application/json" }, body: JSON.stringify(frame(secondSecret)) });
  assert.deepEqual(connector.calls.map((request) => request.credentials?.entries.SOURCE?.value), [firstSecret, secondSecret]);
});

test("credential material matching action metadata is also masked in logs", async (t) => {
  const connector = new CredentialFixture();
  const logs: unknown[] = [];
  const token = randomUUID();
  const handle = await startConnectorHttpServer({ connector, config: {}, actionToken: token, logger: { debug: (_m, fields) => logs.push(fields), info: (_m, fields) => logs.push(fields), warn: (_m, fields) => logs.push(fields), error: (_m, fields) => logs.push(fields) } });
  t.after(() => handle.close());
  const secret = connector.manifest.actions[0]!.name;
  const response = await fetch(handle.url + "/actions/fixture.read", { method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" }, body: JSON.stringify(frame(secret)) });
  assert.equal(response.status, 200);
  assert.equal(JSON.stringify(logs).includes(secret), false);
});

test("invalid, expired, undeclared and old-runtime frames never dispatch", async (t) => {
  for (const scenario of ["version", "alias", "unpinned", "expiry", "unbounded", "unknown", "action", "old-runtime", "auth", "oversized", "bytes", "material", "depth"] as const) {
    await t.test(scenario, async (t) => {
      const connector = new CredentialFixture(scenario !== "old-runtime");
      const token = randomUUID();
      const handle = await startConnectorHttpServer({ connector, config: {}, actionToken: token, maxBodyBytes: 2_097_152 });
      t.after(() => handle.close());
      const body = frame() as { schemaVersion: string; action: string; input: object; credentials: { expiresAt: string; entries: Record<string, { reference: string; value: string }> }; owner?: string };
      if (scenario === "version") body.schemaVersion = "unsupported";
      if (scenario === "alias") body.credentials.entries = { OTHER: body.credentials.entries.SOURCE! };
      if (scenario === "unpinned") body.credentials.entries.SOURCE!.reference = "soha://secrets/fixture";
      if (scenario === "expiry") body.credentials.expiresAt = new Date(Date.now() - 1).toISOString();
      if (scenario === "unbounded") body.credentials.expiresAt = new Date(Date.now() + 130_000).toISOString();
      if (scenario === "unknown") body.owner = "untrusted";
      if (scenario === "action") body.action = "fixture.other";
      if (scenario === "oversized") body.input = { content: " ".repeat(1_048_576) };
      if (scenario === "bytes") body.credentials.entries.SOURCE!.value = "界".repeat(22_000);
      if (scenario === "material") body.credentials.entries.SOURCE!.value = JSON.stringify(Array.from({ length: 64 }, () => randomUUID()));
      if (scenario === "depth") body.credentials.entries.SOURCE!.value = "[".repeat(17) + '"value"' + "]".repeat(17);
      const response = await fetch(handle.url + "/actions/fixture.read", { method: "POST", headers: { Authorization: `Bearer ${scenario === "auth" ? randomUUID() : token}`, "Content-Type": "application/json" }, body: JSON.stringify(body) });
      assert.equal(response.status, scenario === "auth" ? 401 : 400);
      assert.equal(connector.calls.length, 0);
    });
  }
});

test("credential support declaration preserves calls without credentials", async (t) => {
  const connector = new CredentialFixture();
  connector.handler = async (request) => ({ ok: true, output: { input: request.input, credential: !!request.credentials } });
  const token = randomUUID();
  const handle = await startConnectorHttpServer({ connector, config: {}, actionToken: token });
  t.after(() => handle.close());
  const response = await fetch(handle.url + "/actions/fixture.read", { method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" }, body: JSON.stringify({ input: { public: true } }) });
  assert.equal(response.status, 200);
  assert.deepEqual((await response.json() as ConnectorActionResult).output, { input: { public: true }, credential: false });
});

test("structured credential keys and values are protected in results", async (t) => {
  const connector = new CredentialFixture();
  const key = randomUUID();
  const value = randomUUID();
  connector.handler = async () => ({ ok: true, output: { key, value } });
  const token = randomUUID();
  const handle = await startConnectorHttpServer({ connector, config: {}, actionToken: token });
  t.after(() => handle.close());
  const response = await fetch(handle.url + "/actions/fixture.read", { method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" }, body: JSON.stringify(frame(JSON.stringify({ [key]: value }))) });
  assert.equal(response.status, 200);
  const body = await response.text();
  assert.equal(body.includes(key), false);
  assert.equal(body.includes(value), false);
});

test("credential expiry aborts the cooperative action and hides thrown material", async (t) => {
  const connector = new CredentialFixture();
  const token = randomUUID();
  const secret = randomUUID();
  connector.handler = async (request) => {
    assert.equal(request.credentials?.entries.SOURCE?.value, secret);
    await new Promise<void>((resolve) => request.signal?.addEventListener("abort", () => resolve(), { once: true }));
    throw new Error(secret);
  };
  const handle = await startConnectorHttpServer({ connector, config: {}, actionToken: token });
  t.after(() => handle.close());
  const body = frame(secret) as { credentials: { expiresAt: string } };
  body.credentials.expiresAt = new Date(Date.now() + 100).toISOString();
  const response = await fetch(handle.url + "/actions/fixture.read", { method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" }, body: JSON.stringify(body), signal: AbortSignal.timeout(2_000) });
  assert.notEqual(response.status, 200);
  assert.equal((await response.text()).includes(secret), false);
  assert.equal(connector.calls.length, 1);
  assert.equal(connector.calls[0]?.signal?.aborted, true);
});

test("duplicate authorization headers do not select the first credential", async (t) => {
  const connector = new CredentialFixture();
  const token = randomUUID();
  const handle = await startConnectorHttpServer({ connector, config: {}, actionToken: token });
  t.after(() => handle.close());
  const status = await new Promise<number>((resolve, reject) => {
    const request = httpRequest(handle.url + "/actions/fixture.read", { method: "POST", headers: { Authorization: [`Bearer ${token}`, `Bearer ${randomUUID()}`], "Content-Type": "application/json" } }, (response) => { response.resume(); response.on("end", () => resolve(response.statusCode ?? 0)); });
    request.on("error", reject);
    request.end(JSON.stringify(frame()));
  });
  assert.equal(status, 401);
  assert.equal(connector.calls.length, 0);
});

test("caller disconnect aborts the credential action signal", { timeout: 3_000 }, async (t) => {
  const connector = new CredentialFixture();
  let started!: () => void;
  let aborted!: () => void;
  const start = new Promise<void>((resolve) => { started = resolve; });
  const abort = new Promise<void>((resolve) => { aborted = resolve; });
  connector.handler = async (request) => {
    started();
    await new Promise<void>((resolve) => request.signal?.addEventListener("abort", () => { aborted(); resolve(); }, { once: true }));
    return { ok: true, output: { late: true } };
  };
  const token = randomUUID();
  const handle = await startConnectorHttpServer({ connector, config: {}, actionToken: token });
  t.after(() => handle.close());
  const request = httpRequest(handle.url + "/actions/fixture.read", { method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" } });
  request.on("error", () => {});
  request.end(JSON.stringify(frame()));
  await start;
  request.destroy();
  await Promise.race([abort, new Promise<never>((_resolve, reject) => { const timer = setTimeout(() => reject(new Error("disconnect did not abort action")), 2_000); timer.unref(); })]);
  assert.equal(connector.calls[0]?.signal?.aborted, true);
});

test("expiry rejects late success even when the connector ignores cancellation", async (t) => {
  const connector = new CredentialFixture();
  let finish!: (result: ConnectorActionResult) => void;
  connector.handler = () => new Promise<ConnectorActionResult>((resolve) => { finish = resolve; });
  const token = randomUUID();
  const secret = randomUUID();
  const handle = await startConnectorHttpServer({ connector, config: {}, actionToken: token });
  t.after(() => handle.close());
  const body = frame(secret) as { credentials: { expiresAt: string } };
  body.credentials.expiresAt = new Date(Date.now() + 100).toISOString();
  const response = await fetch(handle.url + "/actions/fixture.read", { method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" }, body: JSON.stringify(body), signal: AbortSignal.timeout(2_000) });
  const result = await response.json() as ConnectorActionResult;
  assert.equal(result.ok, false);
  assert.equal(result.error?.code, "credential_action_canceled");
  finish({ ok: true, output: { received: secret } });
  assert.equal(connector.calls[0]?.signal?.aborted, true);
});

test("credential exceptions never enter HTTP results or runtime logs", async (t) => {
  const connector = new CredentialFixture();
  const secret = randomUUID();
  const logs: unknown[] = [];
  connector.handler = async () => { throw new Error(secret); };
  const token = randomUUID();
  const handle = await startConnectorHttpServer({ connector, config: {}, actionToken: token, logger: { debug: (_m, fields) => logs.push(fields), info: (_m, fields) => logs.push(fields), warn: (_m, fields) => logs.push(fields), error: (_m, fields) => logs.push(fields) } });
  t.after(() => handle.close());
  const response = await fetch(handle.url + "/actions/fixture.read", { method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" }, body: JSON.stringify(frame(secret)) });
  assert.equal(response.status, 400);
  assert.equal((await response.text()).includes(secret), false);
  assert.equal(JSON.stringify(logs).includes(secret), false);
});
