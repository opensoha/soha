import { createCipheriv, createDecipheriv, createHash, timingSafeEqual } from "node:crypto";

import type { ConnectorEvent, ConnectorWebhookRequest } from "../../sdk/types.js";

export interface FeishuWebhookPayload {
  type?: string;
  token?: string;
  challenge?: string;
  uuid?: string;
  schema?: string;
  header?: {
    event_id?: string;
    event_type?: string;
    create_time?: string;
    token?: string;
    tenant_key?: string;
    app_id?: string;
  };
  event?: Record<string, unknown>;
  encrypt?: string;
  [key: string]: unknown;
}

export interface FeishuWebhookVerificationOptions {
  verificationToken: string;
  encryptKey?: string;
}

export function parseFeishuWebhook(
  request: ConnectorWebhookRequest,
  options: FeishuWebhookVerificationOptions
): FeishuWebhookPayload {
  const rawBody = rawBodyToBuffer(request.rawBody);
  const headers = normalizeHeaders(request.headers);
  const parsed = parseJson(rawBody.toString("utf8"));
  const payload = decryptIfNeeded(parsed, options.encryptKey);

  if (options.encryptKey && !isFeishuUrlVerification(payload)) {
    assertFeishuSignature({
      rawBody,
      signature: headers["x-lark-signature"],
      timestamp: headers["x-lark-request-timestamp"],
      nonce: headers["x-lark-request-nonce"],
      encryptKey: options.encryptKey
    });
  }

  verifyToken(payload, options.verificationToken);

  return payload;
}

export function isFeishuUrlVerification(payload: FeishuWebhookPayload): boolean {
  return payload.type === "url_verification" && typeof payload.challenge === "string";
}

export function normalizeFeishuEvent(payload: FeishuWebhookPayload, occurredAtFallback: Date): ConnectorEvent {
  const event = (payload.event ?? {}) as Record<string, unknown>;
  const message = asRecord(event.message);
  const sender = asRecord(event.sender);
  const senderId = asRecord(sender.sender_id);
  const eventType = payload.header?.event_type ?? payload.type ?? "feishu.event";
  const eventId = payload.header?.event_id ?? payload.uuid ?? stableEventId(eventType, payload);
  const occurredAt = parseFeishuTimestamp(payload.header?.create_time, occurredAtFallback);
  const subject =
    stringValue(message.chat_id) ??
    stringValue(message.message_id) ??
    stringValue(senderId.open_id) ??
    stringValue(senderId.user_id);

  const normalized: ConnectorEvent = {
    id: eventId,
    type: eventType,
    source: "feishu",
    occurredAt,
    payload: {
      schema: payload.schema,
      tenantKey: payload.header?.tenant_key,
      event
    }
  };

  if (subject) {
    normalized.subject = subject;
  }

  return normalized;
}

export function calculateFeishuSignature(input: {
  timestamp: string;
  nonce: string;
  encryptKey: string;
  rawBody: string | Uint8Array;
}): string {
  return createHash("sha256")
    .update(input.timestamp)
    .update(input.nonce)
    .update(input.encryptKey)
    .update(rawBodyToBuffer(input.rawBody))
    .digest("hex");
}

export function encryptFeishuWebhookPayload(payload: FeishuWebhookPayload, encryptKey: string, iv: Uint8Array): string {
  if (iv.byteLength !== 16) {
    throw new Error("Feishu encrypted payload IV must be 16 bytes");
  }
  const plaintext = Buffer.from(JSON.stringify(payload), "utf8");
  const padded = addPkcs7Padding(plaintext, 16);
  const cipher = createCipheriv("aes-256-cbc", feishuAesKey(encryptKey), Buffer.from(iv));
  cipher.setAutoPadding(false);
  return Buffer.concat([Buffer.from(iv), cipher.update(padded), cipher.final()]).toString("base64");
}

function assertFeishuSignature(input: {
  rawBody: Buffer;
  signature: string | undefined;
  timestamp: string | undefined;
  nonce: string | undefined;
  encryptKey: string;
}): void {
  if (!input.signature || !input.timestamp || !input.nonce) {
    throw new Error("Feishu webhook signature headers are incomplete");
  }

  const expected = calculateFeishuSignature({
    timestamp: input.timestamp,
    nonce: input.nonce,
    encryptKey: input.encryptKey,
    rawBody: input.rawBody
  });

  if (!safeEqualHex(input.signature, expected)) {
    throw new Error("Feishu webhook signature mismatch");
  }
}

function decryptIfNeeded(payload: FeishuWebhookPayload, encryptKey: string | undefined): FeishuWebhookPayload {
  if (typeof payload.encrypt !== "string") {
    return payload;
  }
  if (!encryptKey) {
    throw new Error("Feishu webhook payload is encrypted but encryptKey is not configured");
  }

  const encrypted = Buffer.from(payload.encrypt, "base64");
  if (encrypted.byteLength <= 16) {
    throw new Error("Feishu encrypted webhook payload is too short");
  }

  const iv = encrypted.subarray(0, 16);
  const ciphertext = encrypted.subarray(16);
  const decipher = createDecipheriv("aes-256-cbc", feishuAesKey(encryptKey), iv);
  decipher.setAutoPadding(false);
  const padded = Buffer.concat([decipher.update(ciphertext), decipher.final()]);
  return parseJson(stripPkcs7Padding(padded).toString("utf8"));
}

function verifyToken(payload: FeishuWebhookPayload, expectedToken: string): void {
  const actualToken = payload.header?.token ?? payload.token;
  if (typeof actualToken !== "string") {
    throw new Error("Feishu webhook token is missing");
  }
  if (!safeEqualText(actualToken, expectedToken)) {
    throw new Error("Feishu webhook token mismatch");
  }
}

function normalizeHeaders(headers: ConnectorWebhookRequest["headers"]): Record<string, string | undefined> {
  const normalized: Record<string, string | undefined> = {};
  for (const [name, value] of Object.entries(headers)) {
    normalized[name.toLowerCase()] = Array.isArray(value) ? value[0] : value;
  }
  return normalized;
}

function rawBodyToBuffer(rawBody: string | Uint8Array): Buffer {
  return typeof rawBody === "string" ? Buffer.from(rawBody, "utf8") : Buffer.from(rawBody);
}

function parseJson(value: string): FeishuWebhookPayload {
  try {
    return JSON.parse(value) as FeishuWebhookPayload;
  } catch (error) {
    throw new Error(`invalid Feishu webhook JSON: ${(error as Error).message}`);
  }
}

function safeEqualHex(actual: string, expected: string): boolean {
  const normalizedActual = actual.trim().toLowerCase();
  if (!/^[0-9a-f]+$/.test(normalizedActual) || normalizedActual.length !== expected.length) {
    return false;
  }
  return timingSafeEqual(Buffer.from(normalizedActual, "hex"), Buffer.from(expected, "hex"));
}

function safeEqualText(actual: string, expected: string): boolean {
  const actualBuffer = Buffer.from(actual);
  const expectedBuffer = Buffer.from(expected);
  return actualBuffer.byteLength === expectedBuffer.byteLength && timingSafeEqual(actualBuffer, expectedBuffer);
}

function feishuAesKey(encryptKey: string): Buffer {
  return createHash("sha256").update(encryptKey).digest();
}

function addPkcs7Padding(value: Buffer, blockSize: number): Buffer {
  const remainder = value.byteLength % blockSize;
  const paddingLength = remainder === 0 ? blockSize : blockSize - remainder;
  return Buffer.concat([value, Buffer.alloc(paddingLength, paddingLength)]);
}

function stripPkcs7Padding(value: Buffer): Buffer {
  if (value.byteLength === 0) {
    throw new Error("Feishu decrypted payload is empty");
  }
  const paddingLength = value[value.byteLength - 1];
  if (paddingLength < 1 || paddingLength > 16 || paddingLength > value.byteLength) {
    throw new Error("Feishu decrypted payload has invalid padding");
  }
  return value.subarray(0, value.byteLength - paddingLength);
}

function parseFeishuTimestamp(value: string | undefined, fallback: Date): string {
  if (!value) {
    return fallback.toISOString();
  }
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) {
    return fallback.toISOString();
  }
  const milliseconds = numeric > 10_000_000_000 ? numeric : numeric * 1000;
  return new Date(milliseconds).toISOString();
}

function stableEventId(eventType: string, payload: FeishuWebhookPayload): string {
  return createHash("sha256").update(eventType).update(JSON.stringify(payload.event ?? payload)).digest("hex").slice(0, 32);
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}
