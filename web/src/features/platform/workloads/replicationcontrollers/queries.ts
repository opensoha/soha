import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { workloadQueries } from '../shared/queries'
import { replicationControllerKeys } from './keys'
import { hasNamespacedWorkloadScope } from '../shared/scope'
import { getReplicationControllerConfiguration } from './api'
import type { ReplicationControllerDetail } from './types'
export const replicationControllerQueries = {
  detail: (scope: ScopeKey, name: string) =>
    workloadQueries.detail<ReplicationControllerDetail>('replicationcontrollers', scope, name),
  events: (scope: ScopeKey, name: string) =>
    workloadQueries.events('replicationcontrollers', scope, name),
  metrics: (scope: ScopeKey, name: string, range: number) =>
    workloadQueries.metrics('replicationcontrollers', scope, name, range),
  configuration: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: replicationControllerKeys.configuration(scope, name),
      queryFn: () => getReplicationControllerConfiguration(scope, name),
      enabled: hasNamespacedWorkloadScope(scope) && Boolean(name.trim()),
    }),
}
