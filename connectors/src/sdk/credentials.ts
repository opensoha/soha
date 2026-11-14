import { redactValue } from "./redaction.js";
import { CONNECTOR_ACTION_PROTOCOL, type ConnectorActionDefinition, type ConnectorCredentialDelivery } from "./types.js";

const ALIAS = /^[A-Z][A-Z0-9_]{0,63}$/;
const PINNED_REFERENCE = /^soha:\/\/secrets\/[A-Za-z0-9._-]+\/versions\/[1-9][0-9]*$/;

export function parseCredentialDelivery(
  body: Record<string, unknown>, action: string, declaration: ConnectorActionDefinition | undefined
): ConnectorCredentialDelivery | undefined {
  if (!("credentials" in body) && !("schemaVersion" in body)) {
    return undefined;
  }
  if (body.schemaVersion !== CONNECTOR_ACTION_PROTOCOL || declaration?.credentialProtocol !== CONNECTOR_ACTION_PROTOCOL || body.action !== action || !action.length || [...action].length > 128) {
    throw new Error("unsupported credential delivery");
  }
  if (!exactKeys(body, ["schemaVersion", "action", "input", "credentials", "requestId"]) || !isRecord(body.input)) {
    throw new Error("invalid credential envelope");
  }
  if (Buffer.byteLength(JSON.stringify(body), "utf8") > 1_048_576) throw new Error("credential envelope exceeds 1 MiB");
  if (body.requestId !== undefined && (typeof body.requestId !== "string" || !body.requestId.length || [...body.requestId].length > 128)) {
    throw new Error("invalid request identifier");
  }
  const delivery = body.credentials;
  if (!isRecord(delivery) || !exactKeys(delivery, ["expiresAt", "entries"]) || typeof delivery.expiresAt !== "string" || !isRecord(delivery.entries)) {
    throw new Error("invalid credential delivery");
  }
  const expires = Date.parse(delivery.expiresAt);
  if (!/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/.test(delivery.expiresAt) || !Number.isFinite(expires) || expires <= Date.now() || expires > Date.now() + 120_000) {
    throw new Error("credential delivery expired or unbounded");
  }
  const entries = Object.entries(delivery.entries);
  const allowed = declaration.credentialAliases;
  if (!allowed?.length || allowed.length > 16 || new Set(allowed).size !== allowed.length || allowed.some((alias) => !ALIAS.test(alias)) || !entries.length || entries.length > 16) {
    throw new Error("invalid credential aliases");
  }
  for (const [alias, item] of entries) {
    if (!ALIAS.test(alias) || !allowed.includes(alias) || !isRecord(item) || !exactKeys(item, ["reference", "value"]) || typeof item.reference !== "string" || item.reference.length > 512 || !PINNED_REFERENCE.test(item.reference) || typeof item.value !== "string" || !item.value.length || Buffer.byteLength(item.value, "utf8") > 65_536) {
      throw new Error("invalid pinned credential");
    }
  }
  const validated = delivery as unknown as ConnectorCredentialDelivery;
  credentialStrings(validated);
  return validated;
}

export function redactCredentialResult(value: unknown, delivery: ConnectorCredentialDelivery): unknown {
  const values = credentialStrings(delivery);
  const replace = (text: string): string => values.reduce((result, secret) => result.split(secret).join("[REDACTED]"), text);
  const visit = (item: unknown): unknown => {
    if (typeof item === "string") return replace(item);
    if (Array.isArray(item)) return item.map(visit);
    if (isRecord(item)) return Object.fromEntries(Object.entries(item).map(([key, child]) => [replace(key), visit(child)]));
    return item;
  };
  return visit(redactValue(value));
}

function credentialStrings(delivery: ConnectorCredentialDelivery): string[] {
  const values = new Set<string>();
  const collect = (value: unknown, depth: number): void => {
    if (depth > 16 || values.size > 64) throw new Error("credential material exceeds bounded structure");
    if (typeof value === "string" && value.length) {
      if (!values.has(value) && values.size >= 64) throw new Error("credential material exceeds bounded structure");
      values.add(value);
    }
    else if (Array.isArray(value)) value.forEach((item) => collect(item, depth + 1));
    else if (isRecord(value)) Object.entries(value).forEach(([key, item]) => { collect(key, depth + 1); collect(item, depth + 1); });
  };
  for (const entry of Object.values(delivery.entries)) {
    collect(entry.value, 0);
    let parsed: unknown;
    try { parsed = JSON.parse(entry.value); } catch { continue; }
    collect(parsed, 0);
  }
  return [...values].sort((a, b) => b.length - a.length);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

function exactKeys(value: Record<string, unknown>, allowed: string[]): boolean {
  return Object.keys(value).every((key) => allowed.includes(key));
}
