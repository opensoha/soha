export interface FeishuConnectorConfig {
  appId: string;
  appSecret: string;
  verificationToken: string;
  encryptKey?: string;
  baseUrl?: string;
  autoReply?: {
    enabled: boolean;
    text: string;
  };
  reliability?: Partial<FeishuReliabilityConfig>;
  fetch?: typeof fetch;
}

export interface FeishuReliabilityConfig {
  maxRetries: number;
  retryBackoffMs: number;
  idempotencyTtlMs: number;
  actionRateLimitPerMinute: number;
  maxDeadLetters: number;
  maxRetryQueueEntries: number;
  persistenceDir?: string;
}

export interface NormalizedFeishuConfig {
  appId: string;
  appSecret: string;
  verificationToken: string;
  encryptKey?: string;
  baseUrl: string;
  autoReply: {
    enabled: boolean;
    text: string;
  };
  reliability: FeishuReliabilityConfig;
  fetch: typeof fetch;
}

export function normalizeFeishuConfig(config: FeishuConnectorConfig): NormalizedFeishuConfig {
  const missing = [
    ["appId", config.appId],
    ["appSecret", config.appSecret],
    ["verificationToken", config.verificationToken]
  ].filter(([, value]) => typeof value !== "string" || value.trim() === "");

  if (missing.length > 0) {
    throw new Error(`missing Feishu connector config: ${missing.map(([name]) => name).join(", ")}`);
  }

  const normalized: NormalizedFeishuConfig = {
    appId: config.appId.trim(),
    appSecret: config.appSecret,
    verificationToken: config.verificationToken,
    baseUrl: trimTrailingSlash(config.baseUrl ?? "https://open.feishu.cn/open-apis"),
    autoReply: config.autoReply ?? {
      enabled: false,
      text: "OpenSoha received your Feishu message."
    },
    reliability: normalizeReliabilityConfig(config.reliability),
    fetch: config.fetch ?? globalThis.fetch
  };

  if (typeof normalized.fetch !== "function") {
    throw new Error("Feishu connector requires a fetch implementation");
  }

  if (typeof config.encryptKey === "string" && config.encryptKey.trim() !== "") {
    normalized.encryptKey = config.encryptKey;
  }

  return normalized;
}

export function loadFeishuConfigFromEnv(env: NodeJS.ProcessEnv = process.env): FeishuConnectorConfig {
  const config: FeishuConnectorConfig = {
    appId: requiredEnv(env, "SOHA_FEISHU_APP_ID"),
    appSecret: requiredEnv(env, "SOHA_FEISHU_APP_SECRET"),
    verificationToken: requiredEnv(env, "SOHA_FEISHU_VERIFICATION_TOKEN")
  };

  const encryptKey = optionalEnv(env, "SOHA_FEISHU_ENCRYPT_KEY");
  if (encryptKey) {
    config.encryptKey = encryptKey;
  }

  const baseUrl = optionalEnv(env, "SOHA_FEISHU_BASE_URL");
  if (baseUrl) {
    config.baseUrl = baseUrl;
  }

  const autoReplyEnabled = booleanEnv(env, "SOHA_FEISHU_AUTO_REPLY_ENABLED");
  const autoReplyText = optionalEnv(env, "SOHA_FEISHU_AUTO_REPLY_TEXT");
  if (autoReplyEnabled !== undefined || autoReplyText) {
    config.autoReply = {
      enabled: autoReplyEnabled ?? false,
      text: autoReplyText ?? "OpenSoha received your Feishu message."
    };
  }

  const reliability: Partial<FeishuReliabilityConfig> = {};
  assignPositiveInt(reliability, "maxRetries", env, "SOHA_FEISHU_RETRY_MAX");
  assignPositiveInt(reliability, "retryBackoffMs", env, "SOHA_FEISHU_RETRY_BACKOFF_MS");
  assignPositiveInt(reliability, "idempotencyTtlMs", env, "SOHA_FEISHU_IDEMPOTENCY_TTL_MS");
  assignPositiveInt(reliability, "actionRateLimitPerMinute", env, "SOHA_FEISHU_ACTION_RATE_LIMIT_PER_MINUTE");
  assignPositiveInt(reliability, "maxDeadLetters", env, "SOHA_FEISHU_DEAD_LETTER_MAX");
  assignPositiveInt(reliability, "maxRetryQueueEntries", env, "SOHA_FEISHU_RETRY_QUEUE_MAX");
  const persistenceDir = optionalEnv(env, "SOHA_FEISHU_RELIABILITY_DIR");
  if (persistenceDir) {
    reliability.persistenceDir = persistenceDir;
  }
  if (Object.keys(reliability).length > 0) {
    config.reliability = reliability;
  }

  return config;
}

function normalizeReliabilityConfig(config: Partial<FeishuReliabilityConfig> | undefined): FeishuReliabilityConfig {
  return {
    maxRetries: config?.maxRetries ?? 2,
    retryBackoffMs: config?.retryBackoffMs ?? 250,
    idempotencyTtlMs: config?.idempotencyTtlMs ?? 24 * 60 * 60 * 1000,
    actionRateLimitPerMinute: config?.actionRateLimitPerMinute ?? 120,
    maxDeadLetters: config?.maxDeadLetters ?? 100,
    maxRetryQueueEntries: config?.maxRetryQueueEntries ?? 1000,
    ...(config?.persistenceDir ? { persistenceDir: config.persistenceDir } : {})
  };
}

function trimTrailingSlash(value: string): string {
  return value.replace(/\/+$/, "");
}

function requiredEnv(env: NodeJS.ProcessEnv, name: string): string {
  const value = optionalEnv(env, name);
  if (!value) {
    throw new Error(`${name} is required`);
  }
  return value;
}

function optionalEnv(env: NodeJS.ProcessEnv, name: string): string | undefined {
  const value = env[name];
  return typeof value === "string" && value.trim() !== "" ? value.trim() : undefined;
}

function booleanEnv(env: NodeJS.ProcessEnv, name: string): boolean | undefined {
  const value = optionalEnv(env, name);
  if (!value) {
    return undefined;
  }
  if (["1", "true", "yes", "on"].includes(value.toLowerCase())) {
    return true;
  }
  if (["0", "false", "no", "off"].includes(value.toLowerCase())) {
    return false;
  }
  throw new Error(`${name} must be a boolean`);
}

function assignPositiveInt(
  target: Partial<FeishuReliabilityConfig>,
  key: Exclude<keyof FeishuReliabilityConfig, "persistenceDir">,
  env: NodeJS.ProcessEnv,
  name: string
): void {
  const value = optionalEnv(env, name);
  if (!value) {
    return;
  }
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 1) {
    throw new Error(`${name} must be a positive integer`);
  }
  target[key] = parsed;
}
