import { randomUUID } from "node:crypto";
import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import path from "node:path";

import { redactValue } from "./redaction.js";
import type { ConnectorActionRequest, ConnectorActionResult } from "./types.js";

export interface RetryOptions {
  maxRetries: number;
  baseDelayMs: number;
  now?: () => Date;
  sleep?: (delayMs: number) => Promise<void>;
  onRetry?: (attempt: number, delayMs: number, error: unknown) => void;
}

export interface RetryableErrorOptions {
  code: string;
  message: string;
  retryable: boolean;
  retryAfterMs?: number;
}

export class RetryableConnectorError extends Error {
  readonly code: string;
  readonly retryable: boolean;
  readonly retryAfterMs?: number;

  constructor(options: RetryableErrorOptions) {
    super(options.message);
    this.name = "RetryableConnectorError";
    this.code = options.code;
    this.retryable = options.retryable;
    if (options.retryAfterMs !== undefined) {
      this.retryAfterMs = options.retryAfterMs;
    }
  }
}

export async function executeWithRetry<T>(operation: () => Promise<T>, options: RetryOptions): Promise<T> {
  let attempt = 0;
  let lastError: unknown;

  while (attempt <= options.maxRetries) {
    try {
      return await operation();
    } catch (error) {
      lastError = error;
      if (!isRetryableError(error) || attempt >= options.maxRetries) {
        throw error;
      }
      attempt += 1;
      const delayMs = retryDelayMs(error, attempt, options.baseDelayMs);
      options.onRetry?.(attempt, delayMs, error);
      await (options.sleep ?? sleep)(delayMs);
    }
  }

  throw lastError instanceof Error ? lastError : new Error("retry operation failed");
}

export interface IdempotencyStore {
  checkAndRecord(key: string, ttlMs: number, now?: Date): boolean;
}

export class MemoryIdempotencyStore implements IdempotencyStore {
  private readonly entries = new Map<string, number>();

  constructor(private readonly maxEntries = 10_000) {}

  has(key: string, now: Date = new Date()): boolean {
    const expiresAt = this.entries.get(key);
    return expiresAt !== undefined && expiresAt > now.getTime();
  }

  checkAndRecord(key: string, ttlMs: number, now: Date = new Date()): boolean {
    const nowMs = now.getTime();
    this.prune(nowMs);
    const existing = this.entries.get(key);
    if (existing && existing > nowMs) {
      return false;
    }
    while (this.entries.size >= this.maxEntries) {
      const oldest = this.entries.keys().next().value;
      if (oldest === undefined) {
        break;
      }
      this.entries.delete(oldest);
    }
    this.entries.set(key, nowMs + ttlMs);
    return true;
  }

  private prune(nowMs: number): void {
    for (const [key, expiresAt] of this.entries) {
      if (expiresAt <= nowMs) {
        this.entries.delete(key);
      }
    }
  }
}

export class FileIdempotencyStore implements IdempotencyStore {
  private entries = new Map<string, number>();

  constructor(
    private readonly filePath: string,
    private readonly maxEntries = 10_000
  ) {
    this.load();
  }

  has(key: string, now: Date = new Date()): boolean {
    const expiresAt = this.entries.get(key);
    return expiresAt !== undefined && expiresAt > now.getTime();
  }

  checkAndRecord(key: string, ttlMs: number, now: Date = new Date()): boolean {
    if (this.has(key, now)) {
      return false;
    }
    const nowMs = now.getTime();
    const entries = new Map<string, number>();
    for (const [recordedKey, expiresAt] of this.entries) {
      if (expiresAt <= nowMs) {
        continue;
      }
      entries.set(recordedKey, expiresAt);
    }
    while (entries.size >= this.maxEntries) {
      const oldest = entries.keys().next().value;
      if (oldest === undefined) {
        break;
      }
      entries.delete(oldest);
    }
    entries.set(key, nowMs + ttlMs);
    writeJsonFile(this.filePath, { entries: Object.fromEntries(entries) });
    this.entries = entries;
    return true;
  }

  private load(): void {
    let raw = "";
    try {
      raw = readFileSync(this.filePath, "utf8");
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") {
        return;
      }
      throw error;
    }
    const parsed = JSON.parse(raw) as { entries?: Record<string, number> };
    for (const [key, expiresAt] of Object.entries(parsed.entries ?? {})) {
      if (Number.isFinite(expiresAt)) {
        this.entries.set(key, expiresAt);
      }
    }
  }
}

export interface RateLimitDecision {
  allowed: boolean;
  retryAfterMs?: number;
}

export interface RateLimiter {
  consume(key: string, now?: Date): RateLimitDecision;
}

export class FixedWindowRateLimiter implements RateLimiter {
  private readonly counters = new Map<string, { windowStartMs: number; count: number }>();

  constructor(
    private readonly limit: number,
    private readonly windowMs: number,
    private readonly maxEntries = 10_000
  ) {
    if (limit < 1) {
      throw new Error("rate limit must be at least 1");
    }
    if (windowMs < 1) {
      throw new Error("rate limit window must be at least 1ms");
    }
  }

  consume(key: string, now: Date = new Date()): RateLimitDecision {
    const nowMs = now.getTime();
    const entry = this.counters.get(key);
    if (!entry || nowMs - entry.windowStartMs >= this.windowMs) {
      if (!entry && this.counters.size >= this.maxEntries) {
        for (const [existingKey, existing] of this.counters) {
          if (nowMs - existing.windowStartMs >= this.windowMs) {
            this.counters.delete(existingKey);
          }
        }
        while (this.counters.size >= this.maxEntries) {
          const oldest = this.counters.keys().next().value;
          if (oldest === undefined) {
            break;
          }
          this.counters.delete(oldest);
        }
      }
      this.counters.set(key, { windowStartMs: nowMs, count: 1 });
      return { allowed: true };
    }
    if (entry.count >= this.limit) {
      return {
        allowed: false,
        retryAfterMs: entry.windowStartMs + this.windowMs - nowMs
      };
    }
    entry.count += 1;
    return { allowed: true };
  }
}

export interface DeadLetterEntry {
  id: string;
  connectorId: string;
  action: string;
  requestId?: string;
  recordedAt: string;
  error: {
    code: string;
    message: string;
    retryable: boolean;
  };
  input: unknown;
}

export interface RetryQueueEntry {
  id: string;
  connectorId: string;
  action: string;
  requestId?: string;
  createdAt: string;
  updatedAt: string;
  nextAttemptAt: string;
  attempts: number;
  error: {
    code: string;
    message: string;
    retryable: boolean;
  };
  input: unknown;
}

export type RetryQueueRecordInput = Omit<RetryQueueEntry, "id" | "createdAt" | "updatedAt" | "input"> & {
  input: unknown;
  now?: Date;
};

export interface RetryQueue {
  enqueue(input: RetryQueueRecordInput): RetryQueueEntry;
  claimDue(now?: Date, limit?: number): RetryQueueEntry[];
  complete(id: string): boolean;
  list(): RetryQueueEntry[];
}

export class FileRetryQueue implements RetryQueue {
  private readonly entries: RetryQueueEntry[] = [];

  constructor(
    private readonly filePath: string,
    private readonly maxEntries = 1000
  ) {
    if (!Number.isSafeInteger(maxEntries) || maxEntries < 1) {
      throw new Error("queue capacity must be a positive safe integer");
    }
    this.load();
  }

  enqueue(input: RetryQueueRecordInput): RetryQueueEntry {
    const now = input.now ?? new Date();
    const entry: RetryQueueEntry = {
      id: `${input.connectorId}:${input.action}:retry:${randomUUID()}`,
      connectorId: input.connectorId,
      action: input.action,
      createdAt: now.toISOString(),
      updatedAt: now.toISOString(),
      nextAttemptAt: input.nextAttemptAt,
      attempts: input.attempts,
      error: redactValue(input.error) as RetryQueueEntry["error"],
      input: redactValue(input.input)
    };
    if (input.requestId) {
      entry.requestId = input.requestId;
    }
    this.entries.push(entry);
    while (this.entries.length > this.maxEntries) {
      this.entries.shift();
    }
    this.persist();
    return entry;
  }

  claimDue(now: Date = new Date(), limit = 100): RetryQueueEntry[] {
    const nowMs = now.getTime();
    return this.entries
      .filter((entry) => Date.parse(entry.nextAttemptAt) <= nowMs)
      .slice(0, Math.max(0, limit));
  }

  complete(id: string): boolean {
    const index = this.entries.findIndex((entry) => entry.id === id);
    if (index === -1) {
      return false;
    }
    this.entries.splice(index, 1);
    this.persist();
    return true;
  }

  list(): RetryQueueEntry[] {
    return [...this.entries];
  }

  private load(): void {
    let raw = "";
    try {
      raw = readFileSync(this.filePath, "utf8");
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") {
        return;
      }
      throw error;
    }
    const parsed = JSON.parse(raw) as { entries?: RetryQueueEntry[] };
    let needsPersist = false;
    for (const entry of parsed.entries ?? []) {
      if (entry && typeof entry.id === "string") {
        const safeEntry = redactValue(entry) as RetryQueueEntry;
        needsPersist ||= JSON.stringify(safeEntry) !== JSON.stringify(entry);
        this.entries.push(safeEntry);
      }
    }
    while (this.entries.length > this.maxEntries) {
      this.entries.shift();
      needsPersist = true;
    }
    if (needsPersist) {
      this.persist();
    }
  }

  private persist(): void {
    writeJsonFile(this.filePath, { entries: this.entries });
  }
}

export type DeadLetterRecordInput = Omit<DeadLetterEntry, "id" | "recordedAt" | "input"> & { input: unknown; now?: Date };

export interface DeadLetterQueue {
  record(input: DeadLetterRecordInput): DeadLetterEntry;
  list(): DeadLetterEntry[];
}

export class MemoryDeadLetterQueue implements DeadLetterQueue {
  private readonly entries: DeadLetterEntry[] = [];

  constructor(private readonly maxEntries = 100) {
    if (!Number.isSafeInteger(maxEntries) || maxEntries < 1) {
      throw new Error("queue capacity must be a positive safe integer");
    }
  }

  record(input: DeadLetterRecordInput): DeadLetterEntry {
    const entry: DeadLetterEntry = {
      id: `${input.connectorId}:${input.action}:${randomUUID()}`,
      connectorId: input.connectorId,
      action: input.action,
      recordedAt: (input.now ?? new Date()).toISOString(),
      error: redactValue(input.error) as DeadLetterEntry["error"],
      input: redactValue(input.input)
    };
    if (input.requestId) {
      entry.requestId = input.requestId;
    }
    this.entries.push(entry);
    if (this.entries.length > this.maxEntries) {
      this.entries.shift();
    }
    return entry;
  }

  list(): DeadLetterEntry[] {
    return [...this.entries];
  }
}

export class FileDeadLetterQueue implements DeadLetterQueue {
  private readonly entries: DeadLetterEntry[] = [];

  constructor(
    private readonly filePath: string,
    private readonly maxEntries = 100
  ) {
    if (!Number.isSafeInteger(maxEntries) || maxEntries < 1) {
      throw new Error("queue capacity must be a positive safe integer");
    }
    this.load();
  }

  record(input: DeadLetterRecordInput): DeadLetterEntry {
    const entry: DeadLetterEntry = {
      id: `${input.connectorId}:${input.action}:${randomUUID()}`,
      connectorId: input.connectorId,
      action: input.action,
      recordedAt: (input.now ?? new Date()).toISOString(),
      error: redactValue(input.error) as DeadLetterEntry["error"],
      input: redactValue(input.input)
    };
    if (input.requestId) {
      entry.requestId = input.requestId;
    }
    this.entries.push(entry);
    while (this.entries.length > this.maxEntries) {
      this.entries.shift();
    }
    this.persist();
    return entry;
  }

  list(): DeadLetterEntry[] {
    return [...this.entries];
  }

  private load(): void {
    let raw = "";
    try {
      raw = readFileSync(this.filePath, "utf8");
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") {
        return;
      }
      throw error;
    }
    const parsed = JSON.parse(raw) as { entries?: DeadLetterEntry[] };
    let needsPersist = false;
    for (const entry of parsed.entries ?? []) {
      if (entry && typeof entry.id === "string") {
        const safeEntry = redactValue(entry) as DeadLetterEntry;
        needsPersist ||= JSON.stringify(safeEntry) !== JSON.stringify(entry);
        this.entries.push(safeEntry);
      }
    }
    while (this.entries.length > this.maxEntries) {
      this.entries.shift();
      needsPersist = true;
    }
    if (needsPersist) {
      this.persist();
    }
  }

  private persist(): void {
    writeJsonFile(this.filePath, { entries: this.entries });
  }
}

export function actionResultFromError(error: unknown): ConnectorActionResult {
  if (error instanceof RetryableConnectorError) {
    return {
      ok: false,
      error: {
        code: error.code,
        message: error.message,
        retryable: error.retryable
      }
    };
  }
  return {
    ok: false,
    error: {
      code: "connector_action_failed",
      message: error instanceof Error ? error.message : "connector action failed",
      retryable: false
    }
  };
}

export function idempotencyKeyForAction(request: ConnectorActionRequest): string | undefined {
  return request.requestId ? `action:${request.action}:${request.requestId}` : undefined;
}

function retryDelayMs(error: unknown, attempt: number, baseDelayMs: number): number {
  if (error instanceof RetryableConnectorError && error.retryAfterMs !== undefined) {
    return error.retryAfterMs;
  }
  return baseDelayMs * 2 ** (attempt - 1);
}

function isRetryableError(error: unknown): boolean {
  return error instanceof RetryableConnectorError && error.retryable;
}

function sleep(delayMs: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, delayMs));
}

function writeJsonFile(filePath: string, value: unknown): void {
  mkdirSync(path.dirname(filePath), { recursive: true, mode: 0o700 });
  const tempPath = `${filePath}.${process.pid}.${Date.now()}.tmp`;
  writeFileSync(tempPath, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600, flush: true });
  renameSync(tempPath, filePath);
}
