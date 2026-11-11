export function parseStreamMessage<T extends object>(data: unknown): T | null {
  try {
    const value: unknown = JSON.parse(String(data))
    return value !== null && typeof value === 'object' && !Array.isArray(value)
      ? (value as T)
      : null
  } catch {
    return null
  }
}
