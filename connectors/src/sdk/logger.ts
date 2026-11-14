import type { Writable } from "node:stream";

import { redactRecord } from "./redaction.js";
import type { ConnectorLogger } from "./types.js";

export type ConnectorLogLevel = "debug" | "info" | "warn" | "error";

export interface JsonLoggerOptions {
  name: string;
  level?: ConnectorLogLevel;
  stream?: Writable;
  now?: () => Date;
}

const LEVEL_ORDER: Record<ConnectorLogLevel, number> = {
  debug: 10,
  info: 20,
  warn: 30,
  error: 40
};

export function createJsonLogger(options: JsonLoggerOptions): ConnectorLogger {
  const threshold = LEVEL_ORDER[options.level ?? "info"];
  const stream = options.stream ?? process.stdout;
  const now = options.now ?? (() => new Date());

  function write(level: ConnectorLogLevel, message: string, fields?: Record<string, unknown>): void {
    if (LEVEL_ORDER[level] < threshold) {
      return;
    }
    const payload = {
      ts: now().toISOString(),
      level,
      logger: options.name,
      message,
      ...(fields ? { fields: redactRecord(fields) } : {})
    };
    stream.write(`${JSON.stringify(payload)}\n`);
  }

  return {
    debug: (message, fields) => write("debug", message, fields),
    info: (message, fields) => write("info", message, fields),
    warn: (message, fields) => write("warn", message, fields),
    error: (message, fields) => write("error", message, fields)
  };
}
