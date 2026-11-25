import { actionName, identity, isJSON, isNonce, matches, maxPending, randomNonce, timeout, validContext } from "./protocol.js";

export function connectPlugin({ pluginId, version, parentOrigin, onContext, timeoutMs = 15000, window: page = window }) {
  const id = identity(pluginId, version);
  if (!page.parent || page.parent === page) throw new Error("Plugin must run in a frame");
  if (parentOrigin === "*") throw new TypeError("Expected a specific parent origin");
  const documentNonce = new URLSearchParams(page.location.hash.slice(1)).get("sohaDocument");
  if (!isNonce(documentNonce)) throw new Error("Missing plugin activation. Open this page from its Soha host.");
  timeout(timeoutMs);
  const pending = new Map(), boot = randomNonce();
  let port, nonce, next = 0, closed = false;
  function postParent(message, origin, transfer = []) {
    page.parent.postMessage({ ...id, ...message }, origin && origin !== "null" ? origin : "*", transfer);
  }
  function send(message) { port.postMessage({ ...id, nonce, ...message }); }
  function finish(request, ok, value) {
    const call = pending.get(request);
    if (!call) return;
    clearTimeout(call.timer);
    pending.delete(request);
    if (ok) call.resolve(value);
    else call.reject(new Error(value));
  }
  function receive({ data: m }) {
    if (!matches(m, id) || closed) return;
    if (m.type === "init" && !nonce && m.boot === boot && isNonce(m.nonce) && validContext(m.context)) {
      nonce = m.nonce;
      onContext?.(m.context);
      return;
    }
    if (!nonce || m.nonce !== nonce) return;
    if (m.type === "context" && validContext(m.context)) onContext?.(m.context);
    if (m.type === "reply" && Number.isSafeInteger(m.request)) {
      if (m.ok === true && isJSON(m.result)) finish(m.request, true, m.result);
      if (m.ok === false && ["action_denied", "action_failed", "stale_version", "busy", "invalid_payload"].includes(m.code)) finish(m.request, false, m.code);
    }
    if (m.type === "closed") dispose();
  }
  function discover(event) {
    const m = event.data;
    if (port || closed || event.source !== page.parent || (parentOrigin !== undefined && event.origin !== parentOrigin) ||
        !matches(m, id) || m.type !== "discover" || !isNonce(m.challenge)) return;
    const channel = new MessageChannel();
    port = channel.port1;
    port.onmessage = receive;
    // The port stays with this document when its WindowProxy navigates.
    postParent({ type: "hello", challenge: m.challenge, documentNonce, boot }, event.origin, [channel.port2]);
  }
  function dispose() {
    if (closed) return;
    closed = true;
    page.removeEventListener("message", discover);
    page.removeEventListener("pagehide", dispose);
    if (nonce) send({ type: "closed" });
    port?.close();
    for (const request of pending.keys()) finish(request, false, "channel_closed");
  }
  page.addEventListener("message", discover);
  page.addEventListener("pagehide", dispose);
  postParent({ type: "ready" }, parentOrigin);
  return {
    async invokePluginAction(actionId, payload = null) {
      if (!nonce || closed) throw new Error("channel_not_ready");
      if (typeof actionId !== "string" || !actionName.test(actionId) || !isJSON(payload)) throw new Error("invalid_payload");
      if (pending.size >= maxPending) throw new Error("busy");
      const request = ++next;
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          send({ type: "cancel", request });
          finish(request, false, "request_timeout");
        }, timeoutMs);
        pending.set(request, { resolve, reject, timer });
        try { send({ type: "invoke", request, actionId, payload }); }
        catch { finish(request, false, "invalid_payload"); }
      });
    },
    dispose,
  };
}
