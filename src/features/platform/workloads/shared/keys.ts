import type { ScopeKey } from '@/types'
import { normalizeWorkloadScope } from './scope'
import type { WorkloadKind } from './types'

function normalizeName(name: string) {
  return name.trim()
}

function normalizeSelector(selector?: Record<string, string>) {
  return Object.fromEntries(
    Object.entries(selector ?? {}).sort(([left], [right]) => left.localeCompare(right)),
  )
}

export const workloadKeys = {
  all: ['platform', 'workloads'] as const,
  resource: (kind: WorkloadKind) => [...workloadKeys.all, kind] as const,
  lists: (kind: WorkloadKind) => [...workloadKeys.resource(kind), 'list'] as const,
  list: (kind: WorkloadKind, scope: ScopeKey) =>
    [...workloadKeys.lists(kind), normalizeWorkloadScope(scope)] as const,
  details: (kind: WorkloadKind) => [...workloadKeys.resource(kind), 'detail'] as const,
  detail: (kind: WorkloadKind, scope: ScopeKey, name: string) =>
    [...workloadKeys.details(kind), normalizeWorkloadScope(scope), normalizeName(name)] as const,
  yaml: (kind: WorkloadKind, scope: ScopeKey, name: string) =>
    [...workloadKeys.detail(kind, scope, name), 'yaml'] as const,
  events: (kind: WorkloadKind, scope: ScopeKey, name: string, limit = 100) =>
    [...workloadKeys.detail(kind, scope, name), 'events', { limit }] as const,
  relatedPods: (
    kind: WorkloadKind,
    scope: ScopeKey,
    name: string,
    selector?: Record<string, string>,
  ) => [...workloadKeys.detail(kind, scope, name), 'pods', normalizeSelector(selector)] as const,
  metrics: (
    kind: WorkloadKind,
    scope: ScopeKey,
    name: string,
    rangeMinutes?: number,
    resourceUid?: string,
  ) =>
    [
      ...workloadKeys.detail(kind, scope, name),
      'metrics',
      ...(rangeMinutes == null ? [] : [{ rangeMinutes }]),
      ...(resourceUid == null ? [] : [{ resourceUid }]),
    ] as const,
  rolloutStatus: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('deployments', scope, name), 'rollout-status'] as const,
  rollouts: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('deployments', scope, name), 'rollouts'] as const,
  overviewEvents: (scope: ScopeKey, limit = 200) =>
    [...workloadKeys.all, 'overview', normalizeWorkloadScope(scope), 'events', { limit }] as const,
  childJobs: (scope: ScopeKey, name: string) =>
    [...workloadKeys.detail('cronjobs', scope, name), 'jobs'] as const,
}
