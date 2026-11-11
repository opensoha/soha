import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { hasNetworkCluster } from '../shared/scope'
import { listPortForwards } from './api'
import { portForwardKeys } from './keys'

export const portForwardQueries = {
  list: (scope: ScopeKey) =>
    queryOptions({
      queryKey: portForwardKeys.list(scope),
      queryFn: () => listPortForwards(scope),
      enabled: hasNetworkCluster(scope),
    }),
}
