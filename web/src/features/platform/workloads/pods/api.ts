import { api } from '@/services/api-client'
import { buildWorkloadItemPath } from '../shared/paths'
import { requireWorkloadNamespace } from '../shared/scope'
import {
  getWorkloadDetail,
  getWorkloadMetrics,
  getWorkloadYAML,
  listWorkloadEvents,
  listWorkloads,
} from '../shared/api'
import type { WorkloadEvent } from '../shared/types'
import type { Pod, PodDetail, PodMetrics, PodTarget } from './types'

const podKind = 'pods' as const

function normalizePodName(name: string) {
  const normalized = name.trim()
  if (!normalized) {
    throw new Error('A pod name is required')
  }
  return normalized
}

export function listPods(scope: PodTarget['scope']): Promise<Pod[]> {
  return listWorkloads<Pod>(podKind, scope)
}

export function getPodDetail(target: PodTarget): Promise<PodDetail> {
  return getWorkloadDetail<PodDetail>(podKind, target.scope, target.name)
}

export function getPodMetrics(target: PodTarget, rangeMinutes: number): Promise<PodMetrics> {
  return getWorkloadMetrics(podKind, target.scope, target.name, rangeMinutes)
}

export async function getPodContainerConfiguration(target: PodTarget) {
  const yaml = await getWorkloadYAML(podKind, target.scope, target.name)
  const { readPodContainerConfiguration } = await import('./pod-container-configuration')
  return readPodContainerConfiguration(
    yaml.content,
    target.name.trim(),
    target.scope.namespace?.trim() || '',
  )
}

export async function listPodEvents(target: PodTarget): Promise<WorkloadEvent[]> {
  const events = await listWorkloadEvents(target.scope, 100)
  const name = normalizePodName(target.name)
  return events.filter(
    (event) =>
      event.involvedName === name &&
      (!event.involvedKind || event.involvedKind.toLowerCase() === 'pod'),
  )
}

export async function deletePod(target: PodTarget): Promise<void> {
  requireWorkloadNamespace(target.scope)
  await api.delete<unknown>(
    buildWorkloadItemPath(podKind, target.scope, normalizePodName(target.name), {
      expectedUid: target.uid,
    }),
  )
}
