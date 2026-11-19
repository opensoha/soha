import { RetryableConnectorError } from "../../sdk/reliability.js";
import type { NormalizedFeishuConfig } from "./config.js";

interface FeishuTenantTokenResponse {
  code: number;
  msg?: string;
  tenant_access_token?: string;
  expire?: number;
}

export class FeishuAuthClient {
  private token?: {
    value: string;
    expiresAtMs: number;
  };

  constructor(
    private readonly config: Pick<NormalizedFeishuConfig, "appId" | "appSecret" | "baseUrl" | "fetch">,
    private readonly now: () => Date = () => new Date()
  ) {}

  async getTenantAccessToken(signal?: AbortSignal): Promise<string> {
    return runFeishuOperation((signal) => this.loadTenantAccessToken(signal), signal);
  }

  private async loadTenantAccessToken(signal: AbortSignal): Promise<string> {
    const nowMs = this.now().getTime();
    if (this.token && this.token.expiresAtMs > nowMs) {
      return this.token.value;
    }

    const response = await this.config.fetch(`${this.config.baseUrl}/auth/v3/tenant_access_token/internal/`, {
      method: "POST",
      signal,
      headers: {
        "Content-Type": "application/json; charset=utf-8"
      },
      body: JSON.stringify({
        app_id: this.config.appId,
        app_secret: this.config.appSecret
      })
    });
    signal.throwIfAborted();

    if (!response.ok) {
      throw new Error(`Feishu tenant token request failed with HTTP ${response.status}`);
    }

    const body = (await response.json()) as FeishuTenantTokenResponse;
    signal.throwIfAborted();
    if (body.code !== 0 || !body.tenant_access_token || !body.expire) {
      throw new Error(`Feishu tenant token request failed: ${body.msg ?? `code ${body.code}`}`);
    }

    const refreshSkewMs = 300_000;
    this.token = {
      value: body.tenant_access_token,
      expiresAtMs: nowMs + body.expire * 1000 - refreshSkewMs
    };

    return this.token.value;
  }
}

export async function runFeishuOperation<T>(operation: (signal: AbortSignal) => Promise<T>, caller?: AbortSignal): Promise<T> {
  if (caller?.aborted) throw operationAborted(false);
  const controller = new AbortController();
  let timedOut = false;
  const abort = (): void => controller.abort();
  caller?.addEventListener("abort", abort, { once: true });
  const timeout = setTimeout(() => { timedOut = true; abort(); }, 15_000);
  let rejectAborted!: () => void;
  const interrupted = new Promise<never>((_resolve, reject) => {
    rejectAborted = () => reject(operationAborted(timedOut));
    controller.signal.addEventListener("abort", rejectAborted, { once: true });
  });
  try {
    const result = await Promise.race([
      Promise.resolve().then(() => { controller.signal.throwIfAborted(); return operation(controller.signal); }),
      interrupted
    ]);
    controller.signal.throwIfAborted();
    return result;
  } catch (error) {
    if (controller.signal.aborted) throw operationAborted(timedOut);
    throw error;
  } finally {
    clearTimeout(timeout);
    caller?.removeEventListener("abort", abort);
    controller.signal.removeEventListener("abort", rejectAborted);
    controller.abort();
  }
}

function operationAborted(timedOut: boolean): RetryableConnectorError {
  return new RetryableConnectorError({
    code: timedOut ? "feishu_operation_timeout" : "feishu_operation_canceled",
    message: timedOut ? "Feishu operation exceeded its time limit" : "Feishu operation canceled",
    retryable: false
  });
}
