import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { hasNetworkCluster } from '../shared/scope'
import { getNetworkTopology } from './api'
import { topologyKeys } from './keys'

export const topologyQueries = {
  detail: (scope: ScopeKey, principal?: string, permissions?: string) =>
    queryOptions({
      queryKey: topologyKeys.snapshot(scope, principal, permissions),
      queryFn: ({ signal }) => getNetworkTopology(scope, signal),
      enabled: hasNetworkCluster(scope),
      staleTime: 0,
      gcTime: 0,
    }),
}
