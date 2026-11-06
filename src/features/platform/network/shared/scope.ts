import { toScopeKey, type ScopeKey } from '@/types'
import type { NetworkResourceRecord, NetworkTarget } from './types'

export function normalizeNetworkScope(scope: ScopeKey): ScopeKey {
  return toScopeKey(scope.clusterId, scope.namespace)
}

export function hasNetworkCluster(scope: ScopeKey) {
  return Boolean(normalizeNetworkScope(scope).clusterId)
}

export function hasNamespacedNetworkScope(scope: ScopeKey) {
  const normalized = normalizeNetworkScope(scope)
  return Boolean(normalized.clusterId && normalized.namespace)
}

export function requireNetworkClusterId(scope: ScopeKey) {
  const clusterId = normalizeNetworkScope(scope).clusterId
  if (!clusterId) throw new Error('A cluster is required for network requests')
  return clusterId
}

export function requireNetworkNamespace(scope: ScopeKey) {
  const namespace = normalizeNetworkScope(scope).namespace
  if (!namespace) throw new Error('A namespace is required for this network request')
  return namespace
}

export function resolveNetworkDetailScope(selected: ScopeKey, params: URLSearchParams): ScopeKey {
  const requestedCluster = params.get('clusterId')?.trim()
  const namespace = params.has('namespace')
    ? params.get('namespace')
    : requestedCluster && requestedCluster !== selected.clusterId
      ? null
      : selected.namespace
  return toScopeKey(requestedCluster || selected.clusterId, namespace)
}

export function networkTargetFromRecord(
  clusterId: string | null | undefined,
  record: Pick<NetworkResourceRecord, 'name' | 'namespace'>,
): NetworkTarget {
  return { scope: toScopeKey(clusterId, record.namespace), name: record.name }
}
