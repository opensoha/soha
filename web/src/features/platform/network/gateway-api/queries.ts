import type { ScopeKey } from '@/types'
import { queryOptions } from '@tanstack/react-query'
import { hasNamespacedNetworkScope, hasNetworkCluster } from '../shared/scope'
import { getGatewayAPIResource, listGatewayAPIResources } from './api'
import { gatewayAPIKeys } from './keys'
import type { GatewayAPIKind } from './types'

export const gatewayAPIQueries = {
  list: <T>(kind: GatewayAPIKind, scope: ScopeKey) =>
    queryOptions({
      queryKey: gatewayAPIKeys.list(kind, scope),
      queryFn: () => listGatewayAPIResources<T>(kind, scope),
      enabled: hasNetworkCluster(scope),
    }),
  detail: <T>(kind: GatewayAPIKind, scope: ScopeKey, name: string, clusterScoped = false) =>
    queryOptions({
      queryKey: gatewayAPIKeys.detail(kind, scope, name),
      queryFn: () => getGatewayAPIResource<T>(kind, scope, name),
      enabled:
        (clusterScoped ? hasNetworkCluster(scope) : hasNamespacedNetworkScope(scope)) &&
        Boolean(name.trim()),
    }),
}
