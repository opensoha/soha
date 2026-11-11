import { toScopeKey, type ScopeKey } from '@/types'

export function normalizeWorkloadScope(scope: ScopeKey): ScopeKey {
  return toScopeKey(scope.clusterId, scope.namespace)
}

export function hasWorkloadCluster(scope: ScopeKey) {
  return Boolean(normalizeWorkloadScope(scope).clusterId)
}

export function hasNamespacedWorkloadScope(scope: ScopeKey) {
  const normalized = normalizeWorkloadScope(scope)
  return Boolean(normalized.clusterId && normalized.namespace)
}

export function requireWorkloadClusterId(scope: ScopeKey) {
  const clusterId = normalizeWorkloadScope(scope).clusterId
  if (!clusterId) {
    throw new Error('A cluster is required for workload requests')
  }
  return clusterId
}

export function requireWorkloadNamespace(scope: ScopeKey) {
  const namespace = normalizeWorkloadScope(scope).namespace
  if (!namespace) {
    throw new Error('A namespace is required for this workload request')
  }
  return namespace
}
