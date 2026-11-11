import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { getNetworkYAML, listNetworkResources } from './api'
import { networkKeys } from './keys'
import { hasNamespacedNetworkScope, hasNetworkCluster } from './scope'
import type { NetworkKind } from './types'

function hasReference(scope: ScopeKey, name: string, clusterScoped = false) {
  return (
    (clusterScoped ? hasNetworkCluster(scope) : hasNamespacedNetworkScope(scope)) &&
    Boolean(name.trim())
  )
}

export const networkQueries = {
  list: <T>(kind: NetworkKind, scope: ScopeKey) =>
    queryOptions<T[]>({
      queryKey: networkKeys.list(kind, scope),
      queryFn: () => listNetworkResources<T>(kind, scope),
      enabled: hasNetworkCluster(scope),
    }),
  detail: <T extends { name: string }>(
    kind: NetworkKind,
    scope: ScopeKey,
    name: string,
    clusterScoped = false,
  ) =>
    queryOptions<T | undefined>({
      queryKey: networkKeys.detail(kind, scope, name),
      queryFn: async () => {
        const items = await listNetworkResources<T>(kind, scope)
        return items.find((item) => item.name === name.trim())
      },
      enabled: hasReference(scope, name, clusterScoped),
    }),
  yaml: (kind: NetworkKind, scope: ScopeKey, name: string, clusterScoped = false) =>
    queryOptions({
      queryKey: networkKeys.yaml(kind, scope, name),
      queryFn: () => getNetworkYAML(kind, { scope, name }),
      enabled: hasReference(scope, name, clusterScoped),
    }),
}
