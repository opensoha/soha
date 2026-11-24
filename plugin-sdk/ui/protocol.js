export const protocol = "soha.plugin-ui/1";
export const maxPending = 64;
export const actionName = /^[a-zA-Z][a-zA-Z0-9._-]{0,127}$/;
export const isNonce = value => typeof value === "string" && /^[0-9a-f]{64}$/.test(value);
export function randomNonce() {
  return Array.from(crypto.getRandomValues(new Uint8Array(32)), b => b.toString(16).padStart(2, "0")).join("");
}
export function identity(pluginId, version) {
  if (typeof pluginId !== "string" || pluginId.length > 255 || !/^[a-z0-9][a-z0-9.-]*\.[a-z0-9][a-z0-9._-]*$/.test(pluginId) ||
      typeof version !== "string" || !/^[A-Za-z0-9][A-Za-z0-9.+_-]{0,127}$/.test(version)) throw new TypeError("Invalid plugin identity");
  return { protocol, pluginId, version };
}
export function matches(message, id) {
  return message?.protocol === protocol && message.pluginId === id.pluginId && message.version === id.version;
}
export function isJSON(value) {
  let nodes = 0, chars = 0;
  function visit(item, depth) {
    if (++nodes > 4096 || depth > 16 || chars > 65536) return false;
    if (item === null || typeof item === "boolean") return true;
    if (typeof item === "number") return Number.isFinite(item);
    if (typeof item === "string") { chars += item.length; return chars <= 65536; }
    if (typeof item !== "object") return false;
    if (Array.isArray(item)) {
      if (item.length > 4096) return false;
      for (let i = 0; i < item.length; i++) {
        if (!Object.hasOwn(item, i) || !visit(item[i], depth + 1)) return false;
      }
      return true;
    }
    if (![Object.prototype, null].includes(Object.getPrototypeOf(item))) return false;
    for (const key of Object.keys(item)) {
      chars += key.length;
      if (["__proto__", "constructor", "prototype"].includes(key) || !visit(item[key], depth + 1)) return false;
    }
    return true;
  }
  return visit(value, 0) && new TextEncoder().encode(JSON.stringify(value)).length <= 65536;
}
export function validContext(context) {
  return !!context && ["light", "dark"].includes(context.theme) &&
    ["en-US", "zh-CN"].includes(context.locale) && isJSON(context.tokens) &&
    context.tokens !== null && typeof context.tokens === "object" && !Array.isArray(context.tokens) &&
    Object.entries(context.tokens).every(([key, value]) => /^--soha-[a-z0-9-]+$/.test(key) && typeof value === "string");
}
export function publicContext(context) {
  if (!validContext(context)) throw new TypeError("Invalid host context");
  return { theme: context.theme, locale: context.locale, tokens: { ...context.tokens } };
}
export function timeout(value) {
  if (!Number.isInteger(value) || value < 1 || value > 300000) throw new RangeError("Invalid request timeout");
  return value;
}
