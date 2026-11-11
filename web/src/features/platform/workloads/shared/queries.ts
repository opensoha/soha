import { queryOptions } from '@tanstack/react-query'
import type { ScopeKey } from '@/types'
import {
  getWorkloadDetail,
  getWorkloadMetrics,
  getWorkloadYAML,
  listWorkloadEvents,
  listWorkloads,
} from './api'
import { workloadKeys } from './keys'
import { hasNamespacedWorkloadScope, hasWorkloadCluster } from './scope'
import type { WorkloadKind } from './types'

const eventKinds: Record<WorkloadKind, string> = {
  deployments: 'deployment',
  pods: 'pod',
  statefulsets: 'statefulset',
  daemonsets: 'daemonset',
  replicasets: 'replicaset',
  replicationcontrollers: 'replicationcontroller',
  jobs: 'job',
  cronjobs: 'cronjob',
}

function hasWorkloadReference(scope: ScopeKey, name: string) {
  return hasNamespacedWorkloadScope(scope) && Boolean(name.trim())
}

export const workloadQueries = {
  list: <T>(kind: WorkloadKind, scope: ScopeKey) =>
    queryOptions({
      queryKey: workloadKeys.list(kind, scope),
      queryFn: () => listWorkloads<T>(kind, scope),
      enabled: hasWorkloadCluster(scope),
    }),
  detail: <T>(kind: WorkloadKind, scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: workloadKeys.detail(kind, scope, name),
      queryFn: () => getWorkloadDetail<T>(kind, scope, name),
      enabled: hasWorkloadReference(scope, name),
    }),
  yaml: (kind: WorkloadKind, scope: ScopeKey, name: string) =>
    queryOptions({
      queryKey: workloadKeys.yaml(kind, scope, name),
      queryFn: () => getWorkloadYAML(kind, scope, name),
      enabled: hasWorkloadReference(scope, name),
    }),
  metrics: (kind: WorkloadKind, scope: ScopeKey, name: string, rangeMinutes?: number) =>
    queryOptions({
      queryKey: workloadKeys.metrics(kind, scope, name, rangeMinutes),
      queryFn: () => getWorkloadMetrics(kind, scope, name, rangeMinutes),
      enabled: hasWorkloadReference(scope, name),
    }),
  events: (kind: WorkloadKind, scope: ScopeKey, name: string, limit = 100) =>
    queryOptions({
      queryKey: workloadKeys.events(kind, scope, name, limit),
      queryFn: async () => {
        const events = await listWorkloadEvents(scope, limit)
        const eventKind = eventKinds[kind]
        return events.filter(
          (event) =>
            event.involvedName === name.trim() &&
            (!event.involvedKind || event.involvedKind.toLowerCase() === eventKind),
        )
      },
      enabled: hasWorkloadReference(scope, name),
    }),
}
