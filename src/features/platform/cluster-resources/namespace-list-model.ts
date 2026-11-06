import { containerTone } from '../workloads/pods/container-status'
import type { Pod } from '../workloads/pods/types'
import type { ClusterNamespace } from './types'

export interface NamespaceListRow extends ClusterNamespace {
  podCount?: number
  attentionCount?: number
  unknownReadiness?: number
}

export function namespaceLabels(labels: ClusterNamespace['labels']) {
  return Object.entries(labels ?? {}).sort(([a], [b]) => {
    const aDefault = a === 'kubernetes.io/metadata.name'
    const bDefault = b === 'kubernetes.io/metadata.name'
    return aDefault === bDefault ? a.localeCompare(b) : aDefault ? 1 : -1
  })
}

/** Completed jobs are retained Pods, not readiness incidents. */
export function podNeedsAttention(pod: Pod): boolean | undefined {
  const phase = pod.phase.toLowerCase()
  if (phase === 'succeeded' || phase === 'completed') return false
  if (['pending', 'failed', 'unknown', 'terminating'].includes(phase)) return true
  const statuses = pod.containerStatuses ?? []
  if (statuses.some((status) => ['warning', 'danger'].includes(containerTone(status)))) return true
  if (phase !== 'running') return undefined
  const ratio = /^(\d+)\s*\/\s*(\d+)$/.exec(pod.readyContainers?.trim() ?? '')
  if (ratio && Number(ratio[2]) > 0 && Number(ratio[1]) <= Number(ratio[2]))
    return Number(ratio[1]) < Number(ratio[2])
  const containers = statuses.filter((status) => status.kind !== 'init')
  if (containers.length && containers.every((status) => containerTone(status) === 'success'))
    return false
  return undefined
}

export function namespaceRows(
  namespaces: readonly ClusterNamespace[],
  pods: readonly Pod[] | undefined,
): NamespaceListRow[] {
  if (!pods) return [...namespaces]
  const summaries = new Map<string, { total: number; attention: number; unknown: number }>()
  for (const pod of pods) {
    const summary = summaries.get(pod.namespace) ?? { total: 0, attention: 0, unknown: 0 }
    summary.total++
    const attention = podNeedsAttention(pod)
    if (attention === true) summary.attention++
    else if (attention === undefined) summary.unknown++
    summaries.set(pod.namespace, summary)
  }
  return namespaces.map((namespace) => {
    const summary = summaries.get(namespace.name)
    return {
      ...namespace,
      podCount: summary?.total ?? 0,
      attentionCount: summary?.attention ?? 0,
      unknownReadiness: summary?.unknown ?? 0,
    }
  })
}

export function filterNamespaces<T extends ClusterNamespace>(
  rows: readonly T[],
  keyword: string,
  status: string,
) {
  const normalized = keyword.trim().toLowerCase()
  return rows.filter(
    (row) =>
      (!status || row.status === status) &&
      (!normalized ||
        [row.name, ...Object.entries(row.labels ?? {}).map(([key, value]) => `${key}=${value}`)]
          .join(' ')
          .toLowerCase()
          .includes(normalized)),
  )
}
