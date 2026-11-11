import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import type { ScopeKey } from '@/types'
import { requireStorageCluster } from './scope'
import type { StorageKind } from './types'

function normalizeName(name: string) {
  const normalized = name.trim()
  if (!normalized) throw new Error('A storage resource name is required')
  return normalized
}

function resourcePath(kind: StorageKind) {
  return `storage/${kind}`
}

export function buildStorageListPath(kind: StorageKind, scope: ScopeKey) {
  return buildClusterScopedPath(requireStorageCluster(scope), resourcePath(kind), scope.namespace)
}

export function buildStorageItemPath(kind: StorageKind, scope: ScopeKey, name: string) {
  return buildClusterScopedPath(
    requireStorageCluster(scope),
    `${resourcePath(kind)}/${encodeURIComponent(normalizeName(name))}`,
    scope.namespace,
  )
}

export function buildStorageDetailPath(kind: StorageKind, scope: ScopeKey, name: string) {
  return buildClusterScopedPath(
    requireStorageCluster(scope),
    `${resourcePath(kind)}/${encodeURIComponent(normalizeName(name))}/detail`,
    scope.namespace,
  )
}

export function buildStorageYAMLPath(kind: StorageKind, scope: ScopeKey, name: string) {
  return buildClusterScopedPath(
    requireStorageCluster(scope),
    `${resourcePath(kind)}/${encodeURIComponent(normalizeName(name))}/yaml`,
    scope.namespace,
  )
}
