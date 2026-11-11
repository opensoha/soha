import type { ScopeKey } from '@/types'
import type { StorageKind } from './types'

function normalizeScope(scope: ScopeKey) {
  return {
    clusterId: scope.clusterId?.trim() || null,
    namespace: scope.namespace?.trim() || null,
  }
}

function normalizeName(name: string) {
  return name.trim()
}

export const storageKeys = {
  all: ['platform', 'storage'] as const,
  resource: (kind: StorageKind) => [...storageKeys.all, kind] as const,
  lists: (kind: StorageKind) => [...storageKeys.resource(kind), 'list'] as const,
  list: (kind: StorageKind, scope: ScopeKey) =>
    [...storageKeys.lists(kind), normalizeScope(scope)] as const,
  details: (kind: StorageKind) => [...storageKeys.resource(kind), 'detail'] as const,
  detail: (kind: StorageKind, scope: ScopeKey, name: string) =>
    [...storageKeys.details(kind), normalizeScope(scope), normalizeName(name)] as const,
  yaml: (kind: StorageKind, scope: ScopeKey, name: string) =>
    [...storageKeys.detail(kind, scope, name), 'yaml'] as const,
}
