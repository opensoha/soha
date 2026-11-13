import path from "node:path";

import { BaseConnector } from "../../sdk/runtime.js";
import {
  actionResultFromError,
  executeWithRetry,
  FileDeadLetterQueue,
  FileIdempotencyStore,
  FileRetryQueue,
  FixedWindowRateLimiter,
  MemoryDeadLetterQueue,
  MemoryIdempotencyStore,
  RetryableConnectorError,
  type DeadLetterEntry,
  type DeadLetterQueue,
  type IdempotencyStore,
  type RetryQueue,
  type RetryQueueEntry
} from "../../sdk/reliability.js";
import type {
  ConnectorActionRequest,
  ConnectorActionResult,
  ConnectorHealth,
  ConnectorWebhookRequest,
  ConnectorWebhookResponse
} from "../../sdk/types.js";
import { FeishuAuthClient } from "./auth.js";
import type { FeishuConnectorConfig, NormalizedFeishuConfig } from "./config.js";
import { normalizeFeishuConfig } from "./config.js";
import { feishuManifest } from "./manifest.js";
import {
  isFeishuUrlVerification,
  normalizeFeishuEvent,
  parseFeishuWebhook
} from "./webhook.js";

interface SendTextInput {
  receiveIdType: "chat_id" | "open_id" | "user_id" | "union_id" | "email";
  receiveId: string;
  text: string;
}

interface FeishuApiResponse {
  code: number;
  msg?: string;
  data?: Record<string, unknown>;
}

export interface FeishuConnectorMetrics {
  webhooksReceived: number;
  webhookDuplicates: number;
  actionsSucceeded: number;
  actionsFailed: number;
  actionRetries: number;
  deadLetters: number;
  retryQueueDepth: number;
}

export class FeishuConnector extends BaseConnector<NormalizedFeishuConfig> {
  readonly id = "feishu";
  readonly manifest = feishuManifest;
  private auth?: FeishuAuthClient;
  private idempotencyStore: IdempotencyStore = new MemoryIdempotencyStore();
  private rateLimiter?: FixedWindowRateLimiter;
  private deadLetters: DeadLetterQueue = new MemoryDeadLetterQueue();
  private retryQueue: RetryQueue | undefined;
  private metrics: FeishuConnectorMetrics = emptyMetrics();

  override configure(config: FeishuConnectorConfig): void {
    const normalized = normalizeFeishuConfig(config);
    this.setConfiguredConfig(normalized);
    this.auth = new FeishuAuthClient(normalized, () => this.now());
    this.idempotencyStore = normalized.reliability.persistenceDir
      ? new FileIdempotencyStore(path.join(normalized.reliability.persistenceDir, "idempotency.json"))
      : new MemoryIdempotencyStore();
    this.rateLimiter = new FixedWindowRateLimiter(normalized.reliability.actionRateLimitPerMinute, 60_000);
    this.deadLetters = normalized.reliability.persistenceDir
      ? new FileDeadLetterQueue(
          path.join(normalized.reliability.persistenceDir, "dead-letter.json"),
          normalized.reliability.maxDeadLetters
        )
      : new MemoryDeadLetterQueue(normalized.reliability.maxDeadLetters);
    this.retryQueue = normalized.reliability.persistenceDir
      ? new FileRetryQueue(
          path.join(normalized.reliability.persistenceDir, "retry-queue.json"),
          normalized.reliability.maxRetryQueueEntries
        )
      : undefined;
    this.metrics = emptyMetrics();
  }

  override async health(): Promise<ConnectorHealth> {
    const base = await super.health();
    if (base.status !== "ok") {
      return base;
    }
    return {
      ...base,
      details: {
        baseUrl: this.config?.baseUrl,
        autoReplyEnabled: this.config?.autoReply.enabled ?? false,
        deadLetterCount: this.deadLetters.list().length,
        metrics: this.getMetrics(),
        persistenceDir: this.config?.reliability.persistenceDir,
        idempotencyTtlMs: this.config?.reliability.idempotencyTtlMs,
        maxRetries: this.config?.reliability.maxRetries,
        actionRateLimitPerMinute: this.config?.reliability.actionRateLimitPerMinute,
        maxRetryQueueEntries: this.config?.reliability.maxRetryQueueEntries
      }
    };
  }

  getDeadLetters(): DeadLetterEntry[] {
    return this.deadLetters.list();
  }

  getMetrics(): FeishuConnectorMetrics {
    return {
      ...this.metrics,
      deadLetters: this.deadLetters.list().length,
      retryQueueDepth: this.retryQueue?.list().length ?? 0
    };
  }

  getRetryQueue(): RetryQueueEntry[] {
    return this.retryQueue?.list() ?? [];
  }

  async handleWebhook(request: ConnectorWebhookRequest): Promise<ConnectorWebhookResponse> {
    this.ensureStarted();
    this.ensureConfigured();
    this.metrics.webhooksReceived += 1;

    const verificationOptions = {
      verificationToken: this.config.verificationToken,
      ...(this.config.encryptKey ? { encryptKey: this.config.encryptKey } : {})
    };
    const payload = parseFeishuWebhook(request, verificationOptions);

    if (isFeishuUrlVerification(payload)) {
      return {
        status: 200,
        body: {
          challenge: payload.challenge
        }
      };
    }

    const event = normalizeFeishuEvent(payload, this.now());
    const idempotencyKey = `event:${event.id}`;
    if (!this.idempotencyStore.checkAndRecord(idempotencyKey, this.config.reliability.idempotencyTtlMs, this.now())) {
      this.metrics.webhookDuplicates += 1;
      this.context?.logger?.info("duplicate Feishu webhook event ignored", {
        connectorId: this.id,
        eventId: event.id,
        eventType: event.type
      });
      return {
        status: 200,
        body: {
          ok: true,
          duplicate: true,
          eventCount: 0,
          actionCount: 0
        },
        events: [],
        actions: []
      };
    }

    const actions = [];

    if (this.shouldAutoReply(event.payload)) {
      const chatId = this.extractChatId(event.payload);
      if (chatId) {
        actions.push(
          await this.dispatchAction({
            action: "feishu.message.send_text",
            input: {
              receiveIdType: "chat_id",
              receiveId: chatId,
              text: this.renderAutoReply(event.type)
            },
            event
          })
        );
      }
    }

    return {
      status: 200,
      body: {
        ok: true,
        eventCount: 1,
        actionCount: actions.length
      },
      events: [event],
      actions
    };
  }

  async dispatchAction(request: ConnectorActionRequest): Promise<ConnectorActionResult> {
    this.ensureStarted();
    this.ensureConfigured();

    if (request.action !== "feishu.message.send_text") {
      this.metrics.actionsFailed += 1;
      return {
        ok: false,
        error: {
          code: "unsupported_action",
          message: `unsupported Feishu action: ${request.action}`,
          retryable: false
        }
      };
    }

    let input: SendTextInput;
    try {
      input = this.parseSendTextInput(request.input);
    } catch (error) {
      this.metrics.actionsFailed += 1;
      return {
        ok: false,
        error: {
          code: "invalid_action_input",
          message: (error as Error).message,
          retryable: false
        }
      };
    }

    const rateLimit = this.rateLimiter?.consume(request.action, this.now());
    if (rateLimit && !rateLimit.allowed) {
      this.metrics.actionsFailed += 1;
      return {
        ok: false,
        error: {
          code: "rate_limited",
          message: `Feishu connector action rate limit exceeded; retry after ${Math.ceil((rateLimit.retryAfterMs ?? 0) / 1000)}s`,
          retryable: true
        }
      };
    }

    try {
      const output = await executeWithRetry(() => this.sendText(input), {
        maxRetries: this.config.reliability.maxRetries,
        baseDelayMs: this.config.reliability.retryBackoffMs,
        onRetry: (attempt, delayMs, error) => {
          this.context?.logger?.warn("retrying Feishu action", {
            connectorId: this.id,
            action: request.action,
            requestId: request.requestId,
            attempt,
            delayMs,
            error: error instanceof Error ? error.message : String(error)
          });
          this.metrics.actionRetries += 1;
        }
      });
      this.metrics.actionsSucceeded += 1;
      return {
        ok: true,
        output
      };
    } catch (error) {
      const result = actionResultFromError(error);
      this.metrics.actionsFailed += 1;
      if (result.error?.retryable) {
        this.deadLetters.record({
          connectorId: this.id,
          action: request.action,
          error: result.error,
          input: request.input,
          now: this.now(),
          ...(request.requestId ? { requestId: request.requestId } : {})
        });
        this.retryQueue?.enqueue({
          connectorId: this.id,
          action: request.action,
          error: result.error,
          input: request.input,
          attempts: this.config.reliability.maxRetries + 1,
          nextAttemptAt: new Date(this.now().getTime() + this.config.reliability.retryBackoffMs).toISOString(),
          now: this.now(),
          ...(request.requestId ? { requestId: request.requestId } : {})
        });
      }
      return result;
    }
  }

  private async sendText(input: SendTextInput): Promise<Record<string, unknown>> {
    if (!this.auth) {
      throw new Error("Feishu auth client is not configured");
    }

    this.ensureConfigured();
    const config = this.config;
    const token = await this.auth.getTenantAccessToken();
    const url = new URL(`${config.baseUrl}/im/v1/messages`);
    url.searchParams.set("receive_id_type", input.receiveIdType);

    const response = await config.fetch(url, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json; charset=utf-8"
      },
      body: JSON.stringify({
        receive_id: input.receiveId,
        msg_type: "text",
        content: JSON.stringify({
          text: input.text
        })
      })
    });

    if (!response.ok) {
      const retryAfter = retryAfterMs(response.headers.get("Retry-After"));
      throw new RetryableConnectorError({
        code: response.status === 429 ? "feishu_rate_limited" : "feishu_api_error",
        message: `Feishu send message failed with HTTP ${response.status}`,
        retryable: response.status === 429 || response.status >= 500,
        ...(retryAfter !== undefined ? { retryAfterMs: retryAfter } : {})
      });
    }

    const body = (await response.json()) as FeishuApiResponse;
    if (body.code !== 0) {
      throw new RetryableConnectorError({
        code: "feishu_api_error",
        message: `Feishu send message failed: ${body.msg ?? `code ${body.code}`}`,
        retryable: false
      });
    }

    return body.data ?? {};
  }

  private parseSendTextInput(value: unknown): SendTextInput {
    const input = value && typeof value === "object" ? (value as Record<string, unknown>) : {};
    const receiveIdType = input.receiveIdType;
    const receiveId = input.receiveId;
    const text = input.text;
    const allowedTypes = ["chat_id", "open_id", "user_id", "union_id", "email"];

    if (typeof receiveIdType !== "string" || !allowedTypes.includes(receiveIdType)) {
      throw new Error("receiveIdType must be one of chat_id, open_id, user_id, union_id, email");
    }
    if (typeof receiveId !== "string" || receiveId.trim() === "") {
      throw new Error("receiveId is required");
    }
    if (typeof text !== "string" || text.trim() === "") {
      throw new Error("text is required");
    }

    return {
      receiveIdType: receiveIdType as SendTextInput["receiveIdType"],
      receiveId,
      text
    };
  }

  private shouldAutoReply(payload: Record<string, unknown>): boolean {
    return Boolean(this.config?.autoReply.enabled && this.extractMessage(payload));
  }

  private extractChatId(payload: Record<string, unknown>): string | undefined {
    const message = this.extractMessage(payload);
    const chatId = message?.chat_id;
    return typeof chatId === "string" && chatId !== "" ? chatId : undefined;
  }

  private extractMessage(payload: Record<string, unknown>): Record<string, unknown> | undefined {
    const event = payload.event;
    if (!event || typeof event !== "object" || Array.isArray(event)) {
      return undefined;
    }
    const message = (event as Record<string, unknown>).message;
    return message && typeof message === "object" && !Array.isArray(message)
      ? (message as Record<string, unknown>)
      : undefined;
  }

  private renderAutoReply(eventType: string): string {
    return (this.config?.autoReply.text ?? "").replaceAll("{{eventType}}", eventType);
  }
}

function emptyMetrics(): FeishuConnectorMetrics {
  return {
    webhooksReceived: 0,
    webhookDuplicates: 0,
    actionsSucceeded: 0,
    actionsFailed: 0,
    actionRetries: 0,
    deadLetters: 0,
    retryQueueDepth: 0
  };
}

function retryAfterMs(value: string | null): number | undefined {
  if (!value) {
    return undefined;
  }
  const seconds = Number(value);
  if (Number.isFinite(seconds) && seconds >= 0) {
    return seconds * 1000;
  }
  const dateMs = Date.parse(value);
  if (Number.isFinite(dateMs)) {
    return Math.max(0, dateMs - Date.now());
  }
  return undefined;
}
