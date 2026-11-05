import { daemonSetKeys } from './keys'
import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import {
  hasNamespacedWorkloadScope,
  hasWorkloadCluster,
} from '@/features/platform/workloads/shared/scope'
import {
  getDaemonSetDetail,
  getDaemonSetMetrics,
  listDaemonSetEvents,
  listDaemonSets,
  listDaemonSetRollouts,
  getDaemonSetRevision,
  getDaemonSetConfiguration,
} from './api'
import type { ResourceMetrics, DaemonSet, DaemonSetDetail } from './types'

function target(scope: ScopeKey, name: string) {
  return { scope, name }
}

function hasDaemonSetReference(scope: ScopeKey, name: string) {
  return hasNamespacedWorkloadScope(scope) && Boolean(name.trim())
}

export const daemonSetQueries = {
  configuration: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: daemonSetKeys.configuration(scope, name),
      queryFn: () => getDaemonSetConfiguration(target(scope, name)),
      enabled: hasDaemonSetReference(scope, name),
    }),
  rollouts: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: daemonSetKeys.rollouts(scope, name),
      queryFn: () => listDaemonSetRollouts(target(scope, name)),
      enabled: hasDaemonSetReference(scope, name),
    }),
  revision: (scope: ScopeKey, name: string, record: import('@/types').RolloutHistory) =>
    queryOptions({
      queryKey: daemonSetKeys.revision(scope, name, record.name, record.revision),
      queryFn: () => getDaemonSetRevision(target(scope, name), record),
      enabled: hasDaemonSetReference(scope, name) && Boolean(record.name),
    }),

  list: (scope: ScopeKey) =>
    queryOptions<DaemonSet[]>({
      queryKey: workloadKeys.list('daemonsets', scope),
      queryFn: () => listDaemonSets(scope),
      enabled: hasWorkloadCluster(scope),
    }),
  detail: (scope: ScopeKey, name: string) =>
    queryOptions<DaemonSetDetail>({
      queryKey: workloadKeys.detail('daemonsets', scope, name),
      queryFn: () => getDaemonSetDetail(target(scope, name)),
      enabled: hasDaemonSetReference(scope, name),
    }),
  metrics: (scope: ScopeKey, name: string, rangeMinutes?: number) =>
    queryOptions<ResourceMetrics>({
      queryKey: workloadKeys.metrics('daemonsets', scope, name, rangeMinutes),
      queryFn: () => getDaemonSetMetrics(target(scope, name), rangeMinutes),
      enabled: hasDaemonSetReference(scope, name),
    }),
  events: (scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: workloadKeys.events('daemonsets', scope, name),
      queryFn: () => listDaemonSetEvents(target(scope, name)),
      enabled: hasDaemonSetReference(scope, name),
    }),
}
