export interface ScopeKey {
  readonly clusterId: string | null
  readonly namespace: string | null
}

export function toScopeKey(clusterId?: string | null, namespace?: string | null): ScopeKey {
  const normalizedClusterId = clusterId?.trim() || null
  return {
    clusterId: normalizedClusterId,
    namespace: normalizedClusterId ? namespace?.trim() || null : null,
  }
}
