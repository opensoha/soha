import { statefulSetKeys } from './keys'
import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import {
  hasNamespacedWorkloadScope,
  hasWorkloadCluster,
} from '@/features/platform/workloads/shared/scope'
import {
  getStatefulSetDetail,
  getStatefulSetMetrics,
  listStatefulSetEvents,
  listStatefulSets,
  listStatefulSetRollouts,
  getStatefulSetRevision,
  getStatefulSetConfiguration,
} from './api'
import type { ResourceMetrics, StatefulSet, StatefulSetDetail } from './types'

function target(scope: ScopeKey, name: string) {
  return { scope, name }
}

function hasStatefulSetReference(scope: ScopeKey, name: string) {
  return hasNamespacedWorkloadScope(scope) && Boolean(name.trim())
}

export const statefulSetQueries = {
  configuration: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: statefulSetKeys.configuration(scope, name),
      queryFn: () => getStatefulSetConfiguration(target(scope, name)),
      enabled: hasStatefulSetReference(scope, name),
    }),
  rollouts: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: statefulSetKeys.rollouts(scope, name),
      queryFn: () => listStatefulSetRollouts(target(scope, name)),
      enabled: hasStatefulSetReference(scope, name),
    }),
  revision: (scope: ScopeKey, name: string, record: import('@/types').RolloutHistory) =>
    queryOptions({
      queryKey: statefulSetKeys.revision(scope, name, record.name, record.revision),
      queryFn: () => getStatefulSetRevision(target(scope, name), record),
      enabled: hasStatefulSetReference(scope, name) && Boolean(record.name),
    }),

  list: (scope: ScopeKey) =>
    queryOptions<StatefulSet[]>({
      queryKey: workloadKeys.list('statefulsets', scope),
      queryFn: () => listStatefulSets(scope),
      enabled: hasWorkloadCluster(scope),
    }),
  detail: (scope: ScopeKey, name: string) =>
    queryOptions<StatefulSetDetail>({
      queryKey: workloadKeys.detail('statefulsets', scope, name),
      queryFn: () => getStatefulSetDetail(target(scope, name)),
      enabled: hasStatefulSetReference(scope, name),
    }),
  metrics: (scope: ScopeKey, name: string, rangeMinutes?: number) =>
    queryOptions<ResourceMetrics>({
      queryKey: workloadKeys.metrics('statefulsets', scope, name, rangeMinutes),
      queryFn: () => getStatefulSetMetrics(target(scope, name), rangeMinutes),
      enabled: hasStatefulSetReference(scope, name),
    }),
  events: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: workloadKeys.events('statefulsets', scope, name),
      queryFn: () => listStatefulSetEvents(target(scope, name)),
      enabled: hasStatefulSetReference(scope, name),
    }),
}
