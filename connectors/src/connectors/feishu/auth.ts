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

  async getTenantAccessToken(): Promise<string> {
    const nowMs = this.now().getTime();
    if (this.token && this.token.expiresAtMs > nowMs) {
      return this.token.value;
    }

    const response = await this.config.fetch(`${this.config.baseUrl}/auth/v3/tenant_access_token/internal/`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json; charset=utf-8"
      },
      body: JSON.stringify({
        app_id: this.config.appId,
        app_secret: this.config.appSecret
      })
    });

    if (!response.ok) {
      throw new Error(`Feishu tenant token request failed with HTTP ${response.status}`);
    }

    const body = (await response.json()) as FeishuTenantTokenResponse;
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
