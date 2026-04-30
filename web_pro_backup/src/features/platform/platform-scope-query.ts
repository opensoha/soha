export type ScopedQueryValue = string | number | boolean | null | undefined

export function buildClusterScopedPath(
  clusterId: string,
  resourcePath: string,
  namespace?: string | null,
  params?: Record<string, ScopedQueryValue>,
) {
  const search = new URLSearchParams()
  const normalizedNamespace = typeof namespace === 'string' ? namespace.trim() : ''
  if (normalizedNamespace) {
    search.set('namespace', normalizedNamespace)
  }

  Object.entries(params ?? {}).forEach(([key, value]) => {
    if (value == null) return
    const normalizedValue = typeof value === 'string' ? value.trim() : String(value)
    if (!normalizedValue) return
    search.set(key, normalizedValue)
  })

  const query = search.toString()
  return query ? `/clusters/${clusterId}/${resourcePath}?${query}` : `/clusters/${clusterId}/${resourcePath}`
}
