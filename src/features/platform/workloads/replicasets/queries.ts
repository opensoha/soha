import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { getDeploymentRolloutStatus, listDeploymentRollouts } from '../deployments/api'
import { workloadKeys } from '../shared/keys'
import { workloadQueries } from '../shared/queries'
import { hasNamespacedWorkloadScope } from '../shared/scope'
import { getReplicaSetConfiguration } from './api'
import { replicaSetKeys } from './keys'
import type { ReplicaSetDetail } from './types'

export const replicaSetQueries = {
  detail: (scope: ScopeKey, name: string) =>
    workloadQueries.detail<ReplicaSetDetail>('replicasets', scope, name),
  events: (scope: ScopeKey, name: string) => workloadQueries.events('replicasets', scope, name),
  metrics: (scope: ScopeKey, name: string, range: number) =>
    workloadQueries.metrics('replicasets', scope, name, range),
  configuration: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: replicaSetKeys.configuration(scope, name),
      queryFn: () => getReplicaSetConfiguration(scope, name),
      enabled: hasNamespacedWorkloadScope(scope) && Boolean(name.trim()),
    }),
  ownerStatus: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: workloadKeys.rolloutStatus(scope, name),
      queryFn: () => getDeploymentRolloutStatus({ scope, name }),
      enabled: hasNamespacedWorkloadScope(scope) && Boolean(name.trim()),
    }),
  ownerHistory: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: workloadKeys.rollouts(scope, name),
      queryFn: () => listDeploymentRollouts({ scope, name }),
      enabled: hasNamespacedWorkloadScope(scope) && Boolean(name.trim()),
    }),
}
