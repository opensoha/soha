import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { hasNamespacedNetworkScope, hasNetworkCluster } from '../shared/scope'
import { getNetworkCoreResource, listNetworkCoreResources } from './api'
import { networkCoreKeys } from './keys'
import type { NetworkCoreKind } from './types'

export const networkCoreQueries = {
  list: <T>(kind: NetworkCoreKind, scope: ScopeKey) =>
    queryOptions({
      queryKey: networkCoreKeys.list(kind, scope),
      queryFn: () => listNetworkCoreResources<T>(kind, scope),
      enabled: hasNetworkCluster(scope),
    }),
  detail: <T extends { name: string }>(
    kind: NetworkCoreKind,
    scope: ScopeKey,
    name: string,
    clusterScoped = false,
  ) =>
    queryOptions({
      queryKey: networkCoreKeys.detail(kind, scope, name),
      queryFn: () => getNetworkCoreResource<T>(kind, scope, name),
      enabled:
        (clusterScoped ? hasNetworkCluster(scope) : hasNamespacedNetworkScope(scope)) &&
        Boolean(name.trim()),
    }),
}
