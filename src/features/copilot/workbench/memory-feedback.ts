function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function memoryEventMetadata(value: unknown) {
  return isRecord(value) && isRecord(value.metadata) ? value.metadata : undefined
}

export const MEMORY_UNAVAILABLE_WARNING =
  '长期记忆暂不可用，本次对话将继续；不会使用不可用的记忆，自动记忆可能尚未保存。'

export function hasMemoryDegradation(value: unknown): boolean {
  if (!isRecord(value)) return false
  if (value.memoryStatus === 'degraded') return true
  if (Array.isArray(value.truncations) && value.truncations.includes('memory_unavailable'))
    return true
  return ['contextSnapshot', 'contextEnvelope', 'sessionPatch', 'metadata', 'agentStatus'].some(
    (key) => isRecord(value[key]) && hasMemoryDegradation(value[key]),
  )
}
