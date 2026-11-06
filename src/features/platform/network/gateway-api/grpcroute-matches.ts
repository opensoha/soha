const object = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
const text = (value: unknown) => (typeof value === 'string' ? value : '')

/** gRPC matches use service/method and headers; they do not have an HTTP path predicate. */
export function grpcRouteMatches(value: unknown): string[][] {
  const values: unknown[] = Array.isArray(value) && value.length ? value : [{}]
  return values.map((raw) => {
    const match = object(raw),
      method = object(match.method),
      type = text(method.type) || 'Exact'
    const parts =
      match.method === undefined
        ? ['Any gRPC service / method']
        : [
            text(method.service) ? `Service ${type} · ${text(method.service)}` : 'Service Any',
            text(method.method) ? `Method ${type} · ${text(method.method)}` : 'Method Any',
          ]
    if (match.method !== undefined && !text(method.service) && !text(method.method)) {
      parts.splice(0, parts.length, `Method ${type} · service/method not specified`)
    }
    for (const rawHeader of Array.isArray(match.headers) ? match.headers : []) {
      const header = object(rawHeader)
      parts.push(
        `Header ${text(header.type) || 'Exact'} · ${text(header.name)} = ${text(header.value)}`,
      )
    }
    for (const [key, value] of Object.entries(match))
      if (!['method', 'headers'].includes(key)) parts.push(`${key}: ${JSON.stringify(value)}`)
    return parts
  })
}
