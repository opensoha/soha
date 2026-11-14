import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { listWorkloadEvents } from '../shared/api'
import { workloadKeys } from '../shared/keys'
import { hasWorkloadCluster } from '../shared/scope'

export const workloadOverviewQueries = {
  events: (scope: ScopeKey, limit = 200) =>
    queryOptions({
      queryKey: workloadKeys.overviewEvents(scope, limit),
      queryFn: () => listWorkloadEvents(scope, limit),
      enabled: hasWorkloadCluster(scope),
    }),
}
