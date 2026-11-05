import { api } from '@/services/api-client'
import {
  getWorkloadDetail,
  getWorkloadMetrics,
  getWorkloadYAML,
  listWorkloadEvents,
  listWorkloads,
} from '@/features/platform/workloads/shared/api'
import {
  buildWorkloadActionPath,
  buildWorkloadItemPath,
} from '@/features/platform/workloads/shared/paths'
import { requireWorkloadNamespace } from '@/features/platform/workloads/shared/scope'
import type { WorkloadEvent } from '@/features/platform/workloads/shared/types'
import type {
  ResourceMetrics,
  ScaleStatefulSetVariables,
  StatefulSet,
  StatefulSetDetail,
  StatefulSetTarget,
} from './types'

const statefulSetKind = 'statefulsets' as const

function normalizeName(name: string) {
  const normalized = name.trim()
  if (!normalized) throw new Error('A StatefulSet name is required')
  return normalized
}

export function listStatefulSets(scope: StatefulSetTarget['scope']): Promise<StatefulSet[]> {
  return listWorkloads<StatefulSet>(statefulSetKind, scope)
}

export function getStatefulSetDetail(target: StatefulSetTarget): Promise<StatefulSetDetail> {
  return getWorkloadDetail<StatefulSetDetail>(statefulSetKind, target.scope, target.name)
}

export function getStatefulSetMetrics(
  target: StatefulSetTarget,
  rangeMinutes?: number,
): Promise<ResourceMetrics> {
  return getWorkloadMetrics(statefulSetKind, target.scope, target.name, rangeMinutes)
}

export async function listStatefulSetEvents(target: StatefulSetTarget): Promise<WorkloadEvent[]> {
  const events = await listWorkloadEvents(target.scope, 100)
  const name = normalizeName(target.name)
  return events.filter(
    (event) =>
      event.involvedName === name &&
      (!event.involvedKind || event.involvedKind.toLowerCase() === 'statefulset'),
  )
}

export async function restartStatefulSet(target: StatefulSetTarget): Promise<void> {
  await api.post<unknown>(buildWorkloadActionPath(statefulSetKind, target.scope, 'restart'), {
    namespace: requireWorkloadNamespace(target.scope),
    name: normalizeName(target.name),
  })
}

export async function scaleStatefulSet({
  scope,
  name,
  replicas,
}: ScaleStatefulSetVariables): Promise<void> {
  await api.post<unknown>(buildWorkloadActionPath(statefulSetKind, scope, 'scale'), {
    namespace: requireWorkloadNamespace(scope),
    name: normalizeName(name),
    replicas,
  })
}

export async function deleteStatefulSet(target: StatefulSetTarget): Promise<void> {
  await api.delete<unknown>(buildWorkloadItemPath(statefulSetKind, target.scope, target.name))
}

export async function listStatefulSetRollouts(target: StatefulSetTarget) {
  const result = await api.get<{ data: import('@/types').RolloutHistory[] }>(
    statefulSetSubresourcePath(target, 'rollouts'),
  )
  return result.data
}
export async function getStatefulSetRevision(
  target: StatefulSetTarget,
  record: import('@/types').RolloutHistory,
) {
  const result = await api.get<{ data: import('@/types').ResourceYAMLView }>(
    statefulSetSubresourcePath(target, `rollouts/${encodeURIComponent(record.name)}/yaml`),
  )
  const { readStatefulSetRevision } = await import('./statefulset-configuration')
  return readStatefulSetRevision(result.data.content, record)
}
export async function getStatefulSetConfiguration(target: StatefulSetTarget) {
  const result = await getWorkloadYAML(statefulSetKind, target.scope, target.name)
  const { readStatefulSetConfiguration } = await import('./statefulset-configuration')
  return readStatefulSetConfiguration(
    result.content,
    target.name,
    requireWorkloadNamespace(target.scope),
  )
}

function statefulSetSubresourcePath(target: StatefulSetTarget, suffix: string) {
  const [path, query] = buildWorkloadItemPath(statefulSetKind, target.scope, target.name).split('?')
  return `${path}/${suffix}${query ? `?${query}` : ''}`
}
