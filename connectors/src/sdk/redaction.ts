const REDACTED = "[REDACTED]";
const SENSITIVE_KEY_PATTERN =
  /(?:secret|token|authorization|password|passwd|encrypt[-_]?key|private[-_]?key|credential|signature|cookie|api[-_]?key|^pass$)/i;
const TOKEN_VALUE_PATTERN =
  /\b(?:Bearer\s+)?(?:soha_(?:pat|sat)_[A-Za-z0-9._-]+|[A-Za-z0-9_-]{24,}\.[A-Za-z0-9._-]{12,})\b/g;
const AUTHORIZATION_TEXT_PATTERN =
  /(["']?authorization["']?\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|Bearer\s+[^\s,;]+|[^\s,;]+)/gi;
const SENSITIVE_ASSIGNMENT_TEXT_PATTERN =
  /(["']?(?:token|password|passwd|pass|secret|api[_-]?key|encrypt[_-]?key|private[_-]?key|credential|signature|cookie)["']?\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)/gi;
const BEARER_TEXT_PATTERN = /\bBearer\s+[A-Za-z0-9._~+/=-]+/gi;

export function redactText(value: string): string {
  return value
    .replace(AUTHORIZATION_TEXT_PATTERN, `$1${REDACTED}`)
    .replace(SENSITIVE_ASSIGNMENT_TEXT_PATTERN, `$1${REDACTED}`)
    .replace(BEARER_TEXT_PATTERN, REDACTED)
    .replace(TOKEN_VALUE_PATTERN, REDACTED);
}

export function redactValue(value: unknown, seen: WeakSet<object> = new WeakSet()): unknown {
  if (typeof value === "string") {
    return redactText(value);
  }
  if (value instanceof Error) {
    return {
      name: value.name,
      message: redactText(value.message)
    };
  }
  if (Array.isArray(value)) {
    if (seen.has(value)) {
      return "[Circular]";
    }
    seen.add(value);
    return value.map((item) => redactValue(item, seen));
  }
  if (value && typeof value === "object") {
    if (seen.has(value)) {
      return "[Circular]";
    }
    seen.add(value);
    const output: Record<string, unknown> = {};
    for (const [key, item] of Object.entries(value as Record<string, unknown>)) {
      output[key] = isSensitiveKey(key) ? REDACTED : redactValue(item, seen);
    }
    return output;
  }
  return value;
}

export function redactRecord(fields: Record<string, unknown> | undefined): Record<string, unknown> | undefined {
  if (!fields) {
    return undefined;
  }
  return redactValue(fields) as Record<string, unknown>;
}

function isSensitiveKey(key: string): boolean {
  return SENSITIVE_KEY_PATTERN.test(key);
}
