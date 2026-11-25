import assert from "node:assert/strict";
import test from "node:test";
import { attachPluginFrame, preparePluginFrame, connectPlugin } from "./index.js";

const id = { pluginId: "example.panel", version: "1.0" };
const light = { theme: "light", locale: "en-US", tokens: { "--soha-text": "#222" } };
const hostOrigin = "https://soha.example";
const tick = () => new Promise(resolve => setTimeout(resolve, 0));

function fixture({ invoke = (_action, value) => value, timeoutMs = 1000 } = {}) {
  const host = new EventTarget(), page = new EventTarget(), frame = new EventTarget();
  const log = [], contexts = [];
  let active = id.version, transferred;
  function deliver(target, source, origin, message, ports = []) {
    const event = Object.assign(new Event("message"), { source, origin, ports, data: structuredClone(message) });
    target.dispatchEvent(event);
  }
  const activation = preparePluginFrame("https://soha.example/plugins/panel/index.html");
  page.location = new URL(activation.url);
  page.parent = host;
  page.postMessage = (message, target) => {
    assert.equal(target, "*");
    log.push(message);
    queueMicrotask(() => deliver(page, host, hostOrigin, message));
  };
  host.postMessage = (message, target, ports = []) => {
    if (ports.length) transferred = ports[0];
    assert.ok(target === "*" || target === hostOrigin);
    log.push(message);
    queueMicrotask(() => deliver(host, page, "null", message, ports));
  };
  Object.assign(frame, { contentWindow: page, ownerDocument: { defaultView: host }, sandbox: { length: 1, contains: x => x === "allow-scripts" } });
  const opts = { ...id, frame, documentNonce: activation.documentNonce, context: light, actions: ["storage.probe"], getActiveVersion: () => active, invoke, timeoutMs };
  const author = connectPlugin({ ...id, window: page, parentOrigin: hostOrigin, onContext: c => contexts.push(c), timeoutMs });
  const channel = attachPluginFrame(opts);
  async function ready() {
    for (let i = 0; i < 100 && !contexts.length; i++) await tick();
    assert.equal(contexts.length, 1);
  }
  return { host, page, frame, opts, author, channel, deliver, contexts, log, ready, port: () => transferred, upgrade: () => { active = "2.0"; } };
}

test("page and host handshake, propagate context and correlate concurrent JSON replies", async () => {
  const f = fixture();
  await f.ready();
  assert.deepEqual(f.contexts, [light]);
  const dark = { theme: "dark", locale: "zh-CN", tokens: { "--soha-text": "#eee" } };
  f.channel.updateContext(dark);
  const calls = [1, 2, 3].map(i => f.author.invokePluginAction("storage.probe", i));
  assert.deepEqual(await Promise.all(calls), [1, 2, 3]);
  assert.deepEqual(f.contexts, [light, dark]);
  const second = fixture(); await second.ready();
  assert.notEqual(f.log.find(m => m.type === "discover").challenge, second.log.find(m => m.type === "discover").challenge);
  f.channel.dispose(); second.channel.dispose();
});

test("forged frames, origins, identities and channel nonces cannot execute; replay is ignored", async () => {
  let count = 0;
  const f = fixture({ invoke() { count++; return null; } });
  await f.ready();
  const seen = new Promise(resolve => f.port().addEventListener("message", e => resolve(e.data), { once: true }));
  await f.author.invokePluginAction("storage.probe");
  const m = await seen;
  for (const [source, origin, data] of [
    [new EventTarget(), "null", m], [f.page, hostOrigin, m], [f.page, "null", { ...m, nonce: "0".repeat(64) }],
    [f.page, "null", { ...m, pluginId: "other.page" }], [f.page, "null", { ...m, version: "2" }],
  ]) f.deliver(f.host, source, origin, { ...data, request: 2 });
  f.port().onmessage({ data: m }); // Same request number on the authenticated port.
  f.port().onmessage({ data: { ...m, request: 2, nonce: "forged" } });
  f.deliver(f.host, f.page, "null", m);
  await tick(); assert.equal(count, 1);
  await assert.rejects(f.author.invokePluginAction("forbidden"), /action_denied/);
  f.upgrade();
  await assert.rejects(f.author.invokePluginAction("storage.probe"), /stale_version/);
  assert.equal(count, 1);
  f.channel.dispose();
});

test("rogue replies have no effect, expired calls reject, and navigation revokes the channel", async () => {
  let received;
  const f = fixture({ timeoutMs: 40, invoke(_action, _payload, { signal }) { received = signal; return new Promise(resolve => signal.addEventListener("abort", () => resolve(null))); } });
  await f.ready();
  const seen = new Promise(resolve => f.port().addEventListener("message", e => resolve(e.data), { once: true }));
  const result = f.author.invokePluginAction("storage.probe", {});
  const m = await seen;
  const forged = { ...m, type: "reply", ok: true, result: "forged" };
  f.deliver(f.page, new EventTarget(), hostOrigin, forged);
  f.deliver(f.page, f.host, "https://other.example", forged);
  f.deliver(f.page, f.host, hostOrigin, { ...forged, nonce: "invalid" });
  await assert.rejects(result, /request_timeout/);
  f.frame.dispatchEvent(new Event("load"));
  await new Promise(resolve => setTimeout(resolve, 5));
  assert.ok(received.aborted);
  await assert.rejects(f.author.invokePluginAction("storage.probe"), /channel_not_ready/);
});

test("host errors are redacted and invalid inputs never reach the action", async () => {
  let count = 0;
  const f = fixture({ invoke() { count++; throw new Error("private backend details"); } });
  await f.ready();
  const cyclic = {}; cyclic.self = cyclic;
  for (const payload of [cyclic, new Map(), [new Array(4096)], "x".repeat(65537), Infinity]) {
    await assert.rejects(f.author.invokePluginAction("storage.probe", payload), /invalid_payload/);
  }
  await assert.rejects(f.author.invokePluginAction("storage.probe"), /^Error: action_failed$/);
  assert.equal(count, 1);
  f.frame.sandbox = { length: 2, contains: () => true };
  assert.throws(() => attachPluginFrame(f.opts), /sandbox/);
  f.channel.dispose();
});

test("context exports only the documented fields and pages abort in-flight host work on dispose", async () => {
  let started;
  const pending = new Promise(resolve => { started = resolve; });
  const f = fixture({ invoke(_action, _body, { signal }) { started(signal); return new Promise(() => {}); } });
  const wider = { ...light, tokens: { "--soha-main": "#fff" }, privateKey: "never-send" };
  f.channel.updateContext(wider);
  wider.tokens["--soha-main"] = "mutated";
  await f.ready();
  assert.deepEqual(f.contexts[0], { ...light, tokens: { "--soha-main": "#fff" } });
  const result = f.author.invokePluginAction("storage.probe");
  const signal = await pending;
  const aborted = new Promise(resolve => signal.addEventListener("abort", resolve, { once: true }));
  f.author.dispose();
  await assert.rejects(result, /channel_closed/);
  await aborted;
  f.channel.dispose();
});

test("a replacement document sharing the WindowProxy cannot claim an activation", () => {
  const f = fixture();
  const { challenge } = f.log.find(m => m.type === "discover");
  const fake = new MessageChannel();
  f.deliver(f.host, f.page, "null", { ...id, protocol: "soha.plugin-ui/1", type: "hello", boot: "b".repeat(64), documentNonce: "f".repeat(64), challenge }, [fake.port2]);
  assert.equal(fake.port2.onmessage, null);
  fake.port1.close(); fake.port2.close();
  f.author.dispose(); f.channel.dispose();
});
