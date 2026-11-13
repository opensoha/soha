import { createServer, type IncomingHttpHeaders, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { timingSafeEqual } from "node:crypto";

import { parseCredentialDelivery, redactCredentialResult } from "./credentials.js";

import type {
  Connector,
  ConnectorActionRequest,
  ConnectorActionResult,
  ConnectorContext,
  ConnectorCredentialDelivery,
  ConnectorEvent,
  ConnectorLogger,
  ConnectorWebhookResponse
} from "./types.js";

export interface ConnectorHttpServerOptions<TConfig> {
  connector: Connector<TConfig>;
  config: TConfig;
  host?: string;
  port?: number;
  actionToken?: string;
  eventSink?: ConnectorEventSinkOptions;
  maxBodyBytes?: number;
  context?: Omit<ConnectorContext, "connectorId">;
  logger?: ConnectorLogger;
}

export interface ConnectorEventSinkOptions {
  url: string;
  token?: string;
  timeoutMs?: number;
  fetch?: typeof fetch;
}

export interface ConnectorHttpServerHandle {
  server: Server;
  url: string;
  close(): Promise<void>;
}

type DeadLetterReadable = {
  getDeadLetters(): unknown[];
};

type RetryQueueReadable = {
  getRetryQueue(): unknown[];
};

type MetricsReadable = {
  getMetrics(): Record<string, unknown>;
};

const DEFAULT_MAX_BODY_BYTES = 1024 * 1024;

export async function startConnectorHttpServer<TConfig>(
  options: ConnectorHttpServerOptions<TConfig>
): Promise<ConnectorHttpServerHandle> {
  const host = options.host ?? "127.0.0.1";
  const port = options.port ?? 0;
  const maxBodyBytes = options.maxBodyBytes ?? DEFAULT_MAX_BODY_BYTES;
  const connector = options.connector;
  const logger = options.logger ?? options.context?.logger;

  await connector.configure(options.config);
  await connector.start({
    connectorId: connector.id,
    ...options.context,
    ...(logger ? { logger } : {})
  });

  const server = createServer(async (request, response) => {
    try {
      await routeRequest({
        connector,
        actionToken: options.actionToken,
        eventSink: options.eventSink,
        maxBodyBytes,
        logger,
        request,
        response
      });
    } catch (error) {
      logger?.error("connector http request failed", {
        connectorId: connector.id,
        method: request.method,
        url: request.url,
        error: error instanceof Error ? error.message : String(error)
      });
      writeJson(response, 500, {
        ok: false,
        error: {
          code: "connector_http_error",
          message: "connector runtime failed to handle the request"
        }
      });
    }
  });

  await listen(server, port, host);
  const address = server.address();
  const actualPort = typeof address === "object" && address ? address.port : port;
  const url = `http://${host}:${actualPort}`;
  logger?.info("connector http server started", {
    connectorId: connector.id,
    url
  });

  return {
    server,
    url,
    close: async () => {
      await connector.stop();
      await closeServer(server);
      logger?.info("connector http server stopped", {
        connectorId: connector.id
      });
    }
  };
}

export function installGracefulShutdown(handle: ConnectorHttpServerHandle, logger?: ConnectorLogger): void {
  let closing = false;
  const close = async (signal: NodeJS.Signals) => {
    if (closing) {
      return;
    }
    closing = true;
    logger?.info("connector runtime received shutdown signal", { signal });
    try {
      await handle.close();
      process.exitCode = 0;
    } catch (error) {
      logger?.error("connector runtime graceful shutdown failed", {
        signal,
        error: error instanceof Error ? error.message : String(error)
      });
      process.exitCode = 1;
    }
  };

  process.once("SIGINT", close);
  process.once("SIGTERM", close);
}

async function routeRequest<TConfig>(input: {
  connector: Connector<TConfig>;
  actionToken: string | undefined;
  eventSink: ConnectorEventSinkOptions | undefined;
  maxBodyBytes: number;
  logger: ConnectorLogger | undefined;
  request: IncomingMessage;
  response: ServerResponse;
}): Promise<void> {
  const method = input.request.method ?? "GET";
  const url = new URL(input.request.url ?? "/", "http://connector.local");

  if (method === "GET" && url.pathname === "/healthz") {
    const health = await input.connector.health();
    writeJson(input.response, health.status === "ok" ? 200 : 503, health);
    return;
  }

  if (method === "GET" && url.pathname === "/manifest") {
    writeJson(input.response, 200, input.connector.manifest);
    return;
  }

  if (method === "GET" && url.pathname === "/dead-letter") {
    if (!authorizeAction(input.request, input.actionToken)) {
      writeJson(input.response, 401, unauthorizedBody());
      return;
    }
    writeJson(input.response, 200, {
      entries: isDeadLetterReadable(input.connector) ? input.connector.getDeadLetters() : []
    });
    return;
  }

  if (method === "GET" && url.pathname === "/retry-queue") {
    if (!authorizeAction(input.request, input.actionToken)) {
      writeJson(input.response, 401, unauthorizedBody());
      return;
    }
    writeJson(input.response, 200, {
      entries: isRetryQueueReadable(input.connector) ? input.connector.getRetryQueue() : []
    });
    return;
  }

  if (method === "GET" && url.pathname === "/metrics") {
    writeJson(input.response, 200, {
      connectorId: input.connector.id,
      metrics: isMetricsReadable(input.connector) ? input.connector.getMetrics() : {}
    });
    return;
  }

  if (method === "POST" && url.pathname === `/webhooks/${input.connector.id}`) {
    await handleWebhookRequest(input);
    return;
  }

  if (method === "POST" && (url.pathname === "/actions" || url.pathname.startsWith("/actions/"))) {
    await handleActionRequest(input, url);
    return;
  }

  writeJson(input.response, 404, {
    ok: false,
    error: {
      code: "not_found",
      message: "connector runtime endpoint not found"
    }
  });
}

async function handleWebhookRequest<TConfig>(input: {
  connector: Connector<TConfig>;
  eventSink: ConnectorEventSinkOptions | undefined;
  maxBodyBytes: number;
  logger: ConnectorLogger | undefined;
  request: IncomingMessage;
  response: ServerResponse;
}): Promise<void> {
  const rawBody = await readBody(input.request, input.maxBodyBytes);
  try {
    const result = await input.connector.handleWebhook({
      headers: input.request.headers,
      rawBody
    });
    input.logger?.info("connector webhook handled", {
      connectorId: input.connector.id,
      status: result.status,
      eventCount: result.events?.length ?? 0,
      actionCount: result.actions?.length ?? 0
    });
    if (result.events?.length && input.eventSink) {
      try {
        await deliverConnectorEvents({
          connectorId: input.connector.id,
          eventSink: input.eventSink,
          events: result.events,
          logger: input.logger
        });
      } catch (error) {
        input.logger?.error("connector event sink delivery failed", {
          connectorId: input.connector.id,
          eventCount: result.events.length,
          error: error instanceof Error ? error.message : String(error)
        });
        writeJson(input.response, 503, {
          ok: false,
          error: {
            code: "connector_event_sink_failed",
            message: "connector runtime could not deliver normalized events to the configured sink",
            retryable: true
          }
        });
        return;
      }
    }
    writeConnectorResponse(input.response, result);
  } catch (error) {
    input.logger?.warn("connector webhook rejected", {
      connectorId: input.connector.id,
      error: error instanceof Error ? error.message : String(error)
    });
    writeJson(input.response, 400, {
      ok: false,
      error: {
        code: "connector_webhook_rejected",
        message: error instanceof Error ? error.message : "webhook rejected"
      }
    });
  }
}

async function deliverConnectorEvents(input: {
  connectorId: string;
  eventSink: ConnectorEventSinkOptions;
  events: ConnectorEvent[];
  logger: ConnectorLogger | undefined;
}): Promise<void> {
  const fetchImpl = input.eventSink.fetch ?? globalThis.fetch;
  if (typeof fetchImpl !== "function") {
    throw new Error("connector event sink requires a fetch implementation");
  }

  const timeoutMs = input.eventSink.timeoutMs ?? 5000;
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "User-Agent": "opensoha-connector-runtime/0.1"
    };
    if (input.eventSink.token) {
      headers.Authorization = `Bearer ${input.eventSink.token}`;
    }
    const response = await fetchImpl(input.eventSink.url, {
      method: "POST",
      headers,
      body: JSON.stringify({
        connectorId: input.connectorId,
        events: input.events
      }),
      signal: controller.signal
    });
    if (!response.ok) {
      throw new Error(`event sink responded with HTTP ${response.status}`);
    }
    input.logger?.info("connector events delivered to sink", {
      connectorId: input.connectorId,
      eventCount: input.events.length,
      status: response.status
    });
  } finally {
    clearTimeout(timeout);
  }
}

async function handleActionRequest<TConfig>(
  input: {
    connector: Connector<TConfig>;
    actionToken: string | undefined;
    maxBodyBytes: number;
    logger: ConnectorLogger | undefined;
    request: IncomingMessage;
    response: ServerResponse;
  },
  url: URL
): Promise<void> {
  if (!authorizeAction(input.request, input.actionToken)) {
    writeJson(input.response, 401, unauthorizedBody());
    return;
  }

  const body = await readJsonBody(input.request, input.maxBodyBytes);
  const actionName = actionNameFromPath(url.pathname);
  const requestId = headerValue(input.request.headers["x-request-id"]) ?? stringField(body, "requestId");
  const actionRequest: ConnectorActionRequest = {
    action: actionName ?? requiredStringField(body, "action"),
    input: recordField(body, "input")
  };
  if (body.event && typeof body.event === "object") {
    actionRequest.event = body.event as ConnectorEvent;
  }
  if (requestId) {
    actionRequest.requestId = requestId;
  }

  let credentials: ConnectorCredentialDelivery | undefined;
  try {
    credentials = parseCredentialDelivery(body, actionRequest.action, input.connector.manifest.actions.find((action) => action.name === actionRequest.action));
  } catch {
    writeJson(input.response, 400, { ok: false, error: { code: "invalid_credential_delivery", message: "credential protocol, declaration or lifetime is invalid", retryable: false } });
    return;
  }
  const result = credentials
    ? await dispatchCredentialAction(input.connector, actionRequest, credentials, input.response)
    : await input.connector.dispatchAction(actionRequest);
  if (input.response.destroyed) return;
  const fields = {
    connectorId: input.connector.id,
    action: actionRequest.action,
    ...(credentials ? {} : { requestId }),
    ok: result.ok,
    errorCode: result.error?.code
  };
  input.logger?.info("connector action handled", credentials ? redactCredentialResult(fields, credentials) as Record<string, unknown> : fields);
  writeJson(input.response, statusForActionResult(result), result);
}

async function dispatchCredentialAction<TConfig>(
  connector: Connector<TConfig>, request: ConnectorActionRequest,
  credentials: ConnectorCredentialDelivery, response: ServerResponse
): Promise<ConnectorActionResult> {
  const controller = new AbortController();
  const abort = (): void => controller.abort();
  const lifetime = Date.parse(credentials.expiresAt) - Date.now();
  if (lifetime <= 0 || response.destroyed) {
    return { ok: false, error: { code: "credential_action_canceled", message: "credential action is no longer active", retryable: false } };
  }
  const timeout = setTimeout(abort, lifetime);
  response.once("close", abort);
  request.credentials = credentials;
  request.signal = controller.signal;
  try {
    const aborted = new Promise<never>((_resolve, reject) => controller.signal.addEventListener("abort", () => reject(new Error("credential action canceled")), { once: true }));
    const result = await Promise.race([connector.dispatchAction(request), aborted]);
    if (!result || typeof result.ok !== "boolean") throw new Error("invalid action result");
    return redactCredentialResult(result, credentials) as ConnectorActionResult;
  } catch {
    return { ok: false, error: { code: controller.signal.aborted ? "credential_action_canceled" : "credential_action_failed", message: "credential action did not complete", retryable: false } };
  } finally {
    clearTimeout(timeout);
    response.removeListener("close", abort);
    controller.abort();
  }
}

function writeConnectorResponse(response: ServerResponse, result: ConnectorWebhookResponse): void {
  if (typeof result.body === "string") {
    response.writeHead(result.status, {
      "Content-Type": result.headers?.["Content-Type"] ?? "text/plain; charset=utf-8",
      ...result.headers
    });
    response.end(result.body);
    return;
  }
  writeJson(response, result.status, result.body, result.headers);
}

function statusForActionResult(result: ConnectorActionResult): number {
  if (result.ok) {
    return 200;
  }
  if (result.error?.code === "rate_limited" || result.error?.code === "feishu_rate_limited") {
    return 429;
  }
  return result.error?.retryable ? 503 : 400;
}

async function readJsonBody(request: IncomingMessage, maxBodyBytes: number): Promise<Record<string, unknown>> {
  const body = await readBody(request, maxBodyBytes);
  if (body.byteLength === 0) {
    return {};
  }
  try {
    const parsed = JSON.parse(body.toString("utf8")) as unknown;
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : {};
  } catch {
    throw new Error("invalid JSON request body");
  }
}

function readBody(request: IncomingMessage, maxBodyBytes: number): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    let size = 0;
    request.on("data", (chunk: Buffer) => {
      size += chunk.byteLength;
      if (size > maxBodyBytes) {
        reject(new Error(`request body exceeds ${maxBodyBytes} bytes`));
        request.destroy();
        return;
      }
      chunks.push(chunk);
    });
    request.on("end", () => resolve(Buffer.concat(chunks)));
    request.on("error", reject);
  });
}

function writeJson(response: ServerResponse, status: number, body: unknown, headers?: Record<string, string>): void {
  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    ...headers
  });
  response.end(JSON.stringify(body));
}

function authorizeAction(request: IncomingMessage, actionToken: string | undefined): boolean {
  if (!actionToken) {
    return false;
  }
  const count = request.rawHeaders.filter((value, index) => index % 2 === 0 && value.toLowerCase() === "authorization").length;
  const authorization = headerValue(request.headers.authorization);
  if (count !== 1 || !authorization) return false;
  const actual = Buffer.from(authorization);
  const expected = Buffer.from(`Bearer ${actionToken}`);
  return actual.length === expected.length && timingSafeEqual(actual, expected);
}

function unauthorizedBody(): Record<string, unknown> {
  return {
    ok: false,
    error: {
      code: "unauthorized",
      message: "connector action token is required"
    }
  };
}

function actionNameFromPath(pathname: string): string | undefined {
  if (!pathname.startsWith("/actions/")) {
    return undefined;
  }
  const encoded = pathname.slice("/actions/".length);
  return encoded ? decodeURIComponent(encoded) : undefined;
}

function requiredStringField(body: Record<string, unknown>, name: string): string {
  const value = body[name];
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`${name} is required`);
  }
  return value;
}

function stringField(body: Record<string, unknown>, name: string): string | undefined {
  const value = body[name];
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}

function recordField(body: Record<string, unknown>, name: string): Record<string, unknown> {
  const value = body[name];
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function headerValue(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

function isDeadLetterReadable(value: unknown): value is DeadLetterReadable {
  return Boolean(value && typeof value === "object" && typeof (value as DeadLetterReadable).getDeadLetters === "function");
}

function isRetryQueueReadable(value: unknown): value is RetryQueueReadable {
  return Boolean(value && typeof value === "object" && typeof (value as RetryQueueReadable).getRetryQueue === "function");
}

function isMetricsReadable(value: unknown): value is MetricsReadable {
  return Boolean(value && typeof value === "object" && typeof (value as MetricsReadable).getMetrics === "function");
}

function listen(server: Server, port: number, host: string): Promise<void> {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, host, () => {
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
