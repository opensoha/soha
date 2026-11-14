import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { getAccessControlDetail, getAccessControlYAML, listAccessControlResources } from './api'
import { accessControlKeys } from './keys'
import { hasAccessControlCluster, hasAccessControlTargetScope } from './scope'
import type { AccessControlKind, AccessControlListFilter } from './types'

function target(scope: ScopeKey, name: string) {
  return { scope, name }
}

export const accessControlQueries = {
  list: <T>(kind: AccessControlKind, scope: ScopeKey, filter?: AccessControlListFilter) =>
    queryOptions<T[]>({
      queryKey: accessControlKeys.list(kind, scope, filter),
      queryFn: () => listAccessControlResources<T>(kind, scope, filter),
      enabled: hasAccessControlCluster(scope),
    }),
  detail: <T>(kind: AccessControlKind, scope: ScopeKey, name: string) =>
    queryOptions<T>({
      queryKey: accessControlKeys.detail(kind, scope, name),
      queryFn: () => getAccessControlDetail<T>(kind, target(scope, name)),
      enabled: hasAccessControlTargetScope(kind, scope) && Boolean(name.trim()),
    }),
  yaml: (kind: AccessControlKind, scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: accessControlKeys.yaml(kind, scope, name),
      queryFn: () => getAccessControlYAML(kind, target(scope, name)),
      enabled: hasAccessControlTargetScope(kind, scope) && Boolean(name.trim()),
    }),
}
