import { toScopeKey, type ScopeKey } from '@/types'

export function toStorageScope(clusterId?: string | null, namespace?: string | null): ScopeKey {
  return toScopeKey(clusterId, namespace)
}

export function toClusterStorageScope(clusterId?: string | null): ScopeKey {
  return toScopeKey(clusterId, null)
}

export function requireStorageCluster(scope: ScopeKey) {
  const clusterId = scope.clusterId?.trim()
  if (!clusterId) throw new Error('A cluster is required for storage requests')
  return clusterId
}

export function requireStorageNamespace(scope: ScopeKey) {
  const namespace = scope.namespace?.trim()
  if (!namespace) throw new Error('A namespace is required for this storage request')
  return namespace
}

export function hasStorageCluster(scope: ScopeKey) {
  return Boolean(scope.clusterId?.trim())
}

export function hasNamespacedStorageScope(scope: ScopeKey) {
  return hasStorageCluster(scope) && Boolean(scope.namespace?.trim())
}
