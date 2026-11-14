import { toScopeKey, type ScopeKey } from '@/types'
import type { ConfigurationResourceRecord, ConfigurationTarget } from './types'

export function normalizeConfigurationScope(scope: ScopeKey): ScopeKey {
  return toScopeKey(scope.clusterId, scope.namespace)
}

export function hasConfigurationCluster(scope: ScopeKey) {
  return Boolean(normalizeConfigurationScope(scope).clusterId)
}

export function hasNamespacedConfigurationScope(scope: ScopeKey) {
  const normalized = normalizeConfigurationScope(scope)
  return Boolean(normalized.clusterId && normalized.namespace)
}

export function requireConfigurationClusterId(scope: ScopeKey) {
  const clusterId = normalizeConfigurationScope(scope).clusterId
  if (!clusterId) throw new Error('A cluster is required for configuration requests')
  return clusterId
}

export function requireConfigurationNamespace(scope: ScopeKey) {
  const namespace = normalizeConfigurationScope(scope).namespace
  if (!namespace) throw new Error('A namespace is required for this configuration request')
  return namespace
}

export function resolveConfigurationNamespace(
  selectedNamespace: string | null | undefined,
  searchNamespace: string | null | undefined,
  recordNamespace?: string | null,
) {
  return selectedNamespace?.trim() || searchNamespace?.trim() || recordNamespace?.trim() || ''
}

export function configurationTargetFromRecord(
  clusterId: string | null | undefined,
  record: Pick<ConfigurationResourceRecord, 'name' | 'namespace'>,
): ConfigurationTarget {
  return {
    scope: toScopeKey(clusterId, record.namespace),
    name: record.name,
  }
}
