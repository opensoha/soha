import { actionName, identity, isJSON, isNonce, matches, maxPending, randomNonce, timeout, publicContext } from "./protocol.js";

// Create a different entry URL for every activation of verified, non-redirecting assets.
export function preparePluginFrame(entryURL) {
  const entry = new URL(entryURL);
  if (entry.hash) throw new TypeError("The plugin entry URL must have no fragment");
  const documentNonce = randomNonce();
  entry.hash = new URLSearchParams({ sohaDocument: documentNonce }).toString();
  return { url: entry.href, documentNonce };
}

export function attachPluginFrame({ frame, documentNonce, pluginId, version, context, actions, getActiveVersion, invoke, timeoutMs = 15000 }) {
  const id = identity(pluginId, version);
  if (frame.sandbox?.length !== 1 || !frame.sandbox.contains("allow-scripts")) throw new Error("Expected sandbox=allow-scripts");
  const page = frame.contentWindow, host = frame.ownerDocument.defaultView;
  if (!isNonce(documentNonce) || !page || !host || typeof getActiveVersion !== "function" || typeof invoke !== "function") throw new TypeError("Invalid host configuration");
  timeout(timeoutMs);
  context = publicContext(context);
  const allowed = new Set(actions), active = new Map();
  const challenge = randomNonce(), nonce = randomNonce();
  let port, last = 0, closed = false;
  const discover = () => page.postMessage({ ...id, type: "discover", challenge }, "*");
  const send = message => port.postMessage({ ...id, nonce, ...message });
  function reply(request, value) { if (!closed) send({ type: "reply", request, ...value }); }
  async function dispatch(m) {
    if (!Number.isSafeInteger(m.request) || m.request <= last) return;
    last = m.request;
    const fail = code => reply(m.request, { ok: false, code });
    if (typeof m.actionId !== "string" || !actionName.test(m.actionId) || !allowed.has(m.actionId)) return fail("action_denied");
    if (!isJSON(m.payload)) return fail("invalid_payload");
    if (active.size >= maxPending) return fail("busy");
    const controller = new AbortController();
    active.set(m.request, controller);
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    let onAbort;
    try {
      if (getActiveVersion() !== version) return fail("stale_version");
      const stopped = new Promise((_, reject) => {
        onAbort = () => reject(new Error("aborted"));
        controller.signal.addEventListener("abort", onAbort, { once: true });
      });
      const result = await Promise.race([stopped, invoke(m.actionId, m.payload, { pluginId, version, signal: controller.signal })]);
      if (getActiveVersion() !== version) return fail("stale_version");
      if (!isJSON(result)) return fail("action_failed");
      reply(m.request, { ok: true, result });
    } catch { fail("action_failed"); }
    finally {
      clearTimeout(timer);
      if (onAbort) controller.signal.removeEventListener("abort", onAbort);
      active.delete(m.request);
    }
  }
  function receive({ data: m }) {
    if (closed || !matches(m, id) || m.nonce !== nonce) return;
    if (m.type === "invoke") void dispatch(m);
    if (m.type === "cancel") active.get(m.request)?.abort();
    if (m.type === "closed") dispose();
  }
  function handshake(event) {
    const m = event.data;
    if (closed || port || event.source !== page || event.origin !== "null" || !matches(m, id)) return;
    if (m.type === "ready") { discover(); return; }
    if (m.type !== "hello" || m.challenge !== challenge || m.documentNonce !== documentNonce || !isNonce(m.boot) || event.ports.length !== 1) return;
    port = event.ports[0];
    port.onmessage = receive;
    send({ type: "init", boot: m.boot, context });
  }
  function dispose() {
    if (closed) return;
    closed = true;
    if (port) { send({ type: "closed" }); port.close(); }
    host.removeEventListener("message", handshake);
    frame.removeEventListener("load", dispose);
    for (const controller of active.values()) controller.abort();
  }
  host.addEventListener("message", handshake);
  frame.addEventListener("load", dispose);
  discover();
  return {
    updateContext(next) {
      if (closed) throw new Error("Channel closed");
      context = publicContext(next);
      if (port) send({ type: "context", context });
    },
    dispose,
  };
}
