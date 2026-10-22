import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { workloadKeys } from '../shared/keys'
import { hasNamespacedWorkloadScope, hasWorkloadCluster } from '../shared/scope'
import { getPodDetail, getPodMetrics, listPodEvents, listPods } from './api'
import type { Pod, PodDetail, PodMetrics } from './types'

function target(scope: ScopeKey, name: string) {
  return { scope, name }
}

function hasPodReference(scope: ScopeKey, name: string) {
  return hasNamespacedWorkloadScope(scope) && Boolean(name.trim())
}

export const podQueries = {
  list: (scope: ScopeKey) =>
    queryOptions<Pod[]>({
      queryKey: workloadKeys.list('pods', scope),
      queryFn: () => listPods(scope),
      enabled: hasWorkloadCluster(scope),
    }),
  detail: (scope: ScopeKey, name: string) =>
    queryOptions<PodDetail>({
      queryKey: workloadKeys.detail('pods', scope, name),
      queryFn: () => getPodDetail(target(scope, name)),
      enabled: hasPodReference(scope, name),
    }),
  metrics: (scope: ScopeKey, name: string, rangeMinutes: number, uid?: string) =>
    queryOptions<PodMetrics>({
      queryKey: workloadKeys.metrics('pods', scope, name, rangeMinutes, uid),
      queryFn: () => getPodMetrics(target(scope, name), rangeMinutes),
      enabled: hasPodReference(scope, name),
    }),
  events: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: workloadKeys.events('pods', scope, name),
      queryFn: () => listPodEvents(target(scope, name)),
      enabled: hasPodReference(scope, name),
    }),
}
