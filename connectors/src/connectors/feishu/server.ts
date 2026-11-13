import { pathToFileURL } from "node:url";

import {
  startConnectorHttpServer,
  installGracefulShutdown,
  type ConnectorEventSinkOptions,
  type ConnectorHttpServerHandle
} from "../../sdk/http.js";
import { createJsonLogger, type ConnectorLogLevel } from "../../sdk/logger.js";
import type { ConnectorLogger } from "../../sdk/types.js";
import { loadFeishuConfigFromEnv, type FeishuConnectorConfig } from "./config.js";
import { FeishuConnector } from "./connector.js";

export interface FeishuHttpRuntimeConfig {
  connector: FeishuConnectorConfig;
  http: {
    host: string;
    port: number;
    actionToken?: string;
    eventSink?: ConnectorEventSinkOptions;
    maxBodyBytes: number;
  };
  logLevel: ConnectorLogLevel;
}

export async function startFeishuConnectorServer(
  runtimeConfig: FeishuHttpRuntimeConfig,
  logger?: ConnectorLogger
): Promise<ConnectorHttpServerHandle> {
  const runtimeLogger = logger ?? createJsonLogger({ name: "feishu-connector", level: runtimeConfig.logLevel });
  return startConnectorHttpServer({
    connector: new FeishuConnector(),
    config: runtimeConfig.connector,
    host: runtimeConfig.http.host,
    port: runtimeConfig.http.port,
    maxBodyBytes: runtimeConfig.http.maxBodyBytes,
    ...(runtimeConfig.http.actionToken ? { actionToken: runtimeConfig.http.actionToken } : {}),
    ...(runtimeConfig.http.eventSink ? { eventSink: runtimeConfig.http.eventSink } : {}),
    logger: runtimeLogger,
    context: {
      logger: runtimeLogger
    }
  });
}

export function loadFeishuHttpRuntimeConfigFromEnv(env: NodeJS.ProcessEnv = process.env): FeishuHttpRuntimeConfig {
  const actionToken = optionalEnv(env, "SOHA_CONNECTOR_HTTP_TOKEN");
  const http: FeishuHttpRuntimeConfig["http"] = {
    host: optionalEnv(env, "SOHA_CONNECTOR_HTTP_HOST") ?? "0.0.0.0",
    port: positiveIntEnv(env, "SOHA_CONNECTOR_HTTP_PORT", 3000),
    maxBodyBytes: positiveIntEnv(env, "SOHA_CONNECTOR_HTTP_MAX_BODY_BYTES", 1024 * 1024)
  };
  if (actionToken) {
    http.actionToken = actionToken;
  }
  const eventSinkUrl = optionalEnv(env, "SOHA_CONNECTOR_EVENT_SINK_URL");
  if (eventSinkUrl) {
    const eventSink: ConnectorEventSinkOptions = {
      url: eventSinkUrl,
      timeoutMs: positiveIntEnv(env, "SOHA_CONNECTOR_EVENT_SINK_TIMEOUT_MS", 5000)
    };
    const eventSinkToken = optionalEnv(env, "SOHA_CONNECTOR_EVENT_SINK_TOKEN");
    if (eventSinkToken) {
      eventSink.token = eventSinkToken;
    }
    http.eventSink = eventSink;
  }

  return {
    connector: loadFeishuConfigFromEnv(env),
    http,
    logLevel: logLevelEnv(env, "SOHA_CONNECTOR_LOG_LEVEL", "info")
  };
}

export async function runFeishuConnectorServerFromEnv(env: NodeJS.ProcessEnv = process.env): Promise<ConnectorHttpServerHandle> {
  const runtimeConfig = loadFeishuHttpRuntimeConfigFromEnv(env);
  const logger = createJsonLogger({ name: "feishu-connector", level: runtimeConfig.logLevel });
  const handle = await startFeishuConnectorServer(runtimeConfig, logger);
  installGracefulShutdown(handle, logger);
  return handle;
}

if (isDirectRun(import.meta.url)) {
  runFeishuConnectorServerFromEnv().catch((error) => {
    const logger = createJsonLogger({ name: "feishu-connector", level: "error" });
    logger.error("failed to start Feishu connector runtime", {
      error: error instanceof Error ? error.message : String(error)
    });
    process.exitCode = 1;
  });
}

function optionalEnv(env: NodeJS.ProcessEnv, name: string): string | undefined {
  const value = env[name];
  return typeof value === "string" && value.trim() !== "" ? value.trim() : undefined;
}

function positiveIntEnv(env: NodeJS.ProcessEnv, name: string, fallback: number): number {
  const value = optionalEnv(env, name);
  if (!value) {
    return fallback;
  }
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 1) {
    throw new Error(`${name} must be a positive integer`);
  }
  return parsed;
}

function logLevelEnv(env: NodeJS.ProcessEnv, name: string, fallback: ConnectorLogLevel): ConnectorLogLevel {
  const value = optionalEnv(env, name);
  if (!value) {
    return fallback;
  }
  if (value === "debug" || value === "info" || value === "warn" || value === "error") {
    return value;
  }
  throw new Error(`${name} must be one of debug, info, warn, error`);
}

function isDirectRun(importMetaUrl: string): boolean {
  return Boolean(process.argv[1] && importMetaUrl === pathToFileURL(process.argv[1]).href);
}
