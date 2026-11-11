import type { ObservabilityJsonValue, ObservabilityPayloadMap } from './types'

export function emptyPayloadMap(): ObservabilityPayloadMap {
  return {}
}

export function isObservabilityPayloadMap(value: unknown): value is ObservabilityPayloadMap {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

export function toText(value: unknown) {
  return String(value || '')
}

export function parseObservabilityJson(
  raw: string,
  fallback: ObservabilityPayloadMap,
): ObservabilityPayloadMap
export function parseObservabilityJson(
  raw: string,
  fallback: ObservabilityJsonValue[],
): ObservabilityJsonValue[]
export function parseObservabilityJson(
  raw: string,
  fallback: ObservabilityPayloadMap | ObservabilityJsonValue[],
) {
  const text = raw.trim()
  if (!text) return fallback
  const parsed: unknown = JSON.parse(text)
  if (Array.isArray(fallback)) {
    if (Array.isArray(parsed)) return parsed as ObservabilityJsonValue[]
    throw new Error('需要合法 JSON 数组')
  }
  if (isObservabilityPayloadMap(parsed)) return parsed
  throw new Error('需要合法 JSON 对象')
}
