import { toScopeKey } from '@/types'
import type { ClusterScope } from './types'

export function toClusterScope(clusterId?: string | null): ClusterScope {
  const normalized = toScopeKey(clusterId, null)
  return { clusterId: normalized.clusterId, namespace: null }
}

export function normalizeClusterScope(scope: ClusterScope): ClusterScope {
  return toClusterScope(scope.clusterId)
}

export function hasClusterScope(scope: ClusterScope) {
  return Boolean(normalizeClusterScope(scope).clusterId)
}

export function requireClusterId(scope: ClusterScope) {
  const clusterId = normalizeClusterScope(scope).clusterId
  if (!clusterId) throw new Error('A cluster is required for cluster resource requests')
  return clusterId
}

export function requireResourceName(name: string, resourceLabel: string) {
  const normalized = name.trim()
  if (!normalized) throw new Error(`A ${resourceLabel} name is required`)
  return normalized
}
