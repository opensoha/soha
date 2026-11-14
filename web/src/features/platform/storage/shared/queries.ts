import { queryOptions } from '@tanstack/react-query'
import type { ResourceYAMLView, ScopeKey } from '@/types'
import { getStorageDetail, getStorageYAML, listStorageResources } from './api'
import { storageKeys } from './keys'
import { hasNamespacedStorageScope, hasStorageCluster } from './scope'
import type { StorageKind, StorageTarget } from './types'

type StorageListQueryFn<T> = (scope: ScopeKey) => Promise<T[]>
type StorageDetailQueryFn<T> = (target: StorageTarget) => Promise<T>
type StorageYAMLQueryFn = (target: StorageTarget) => Promise<ResourceYAMLView>

export function storageListQuery<T>(
  kind: StorageKind,
  scope: ScopeKey,
  queryFn: StorageListQueryFn<T> = (targetScope) => listStorageResources<T>(kind, targetScope),
) {
  return queryOptions<T[]>({
    queryKey: storageKeys.list(kind, scope),
    queryFn: () => queryFn(scope),
    enabled: hasStorageCluster(scope),
  })
}

export function storageDetailQuery<T>(
  kind: StorageKind,
  scope: ScopeKey,
  name: string,
  namespaced: boolean,
  queryFn: StorageDetailQueryFn<T> = (target) => getStorageDetail<T>(kind, target),
) {
  const enabledScope = namespaced ? hasNamespacedStorageScope(scope) : hasStorageCluster(scope)
  return queryOptions<T>({
    queryKey: storageKeys.detail(kind, scope, name),
    queryFn: () => queryFn({ scope, name }),
    enabled: enabledScope && Boolean(name.trim()),
  })
}

export function storageYAMLQuery(
  kind: StorageKind,
  scope: ScopeKey,
  name: string,
  namespaced: boolean,
  queryFn: StorageYAMLQueryFn = (target) => getStorageYAML(kind, target),
) {
  const enabledScope = namespaced ? hasNamespacedStorageScope(scope) : hasStorageCluster(scope)
  return queryOptions({
    queryKey: storageKeys.yaml(kind, scope, name),
    queryFn: () => queryFn({ scope, name }),
    enabled: enabledScope && Boolean(name.trim()),
  })
}
