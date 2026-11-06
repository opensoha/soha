import type { ClusterNode, NodeCondition } from './types'
import type { MonitoringView } from '../resource-monitoring/types'
import { alignedMetricDenominator, monitoringRatio } from '../resource-monitoring/model'

export function nodeReady(status: string) {
  return ['ready', 'true'].includes(status.toLowerCase())
}
export function conditionHealthy(condition: NodeCondition): boolean | undefined {
  if (!['True', 'False'].includes(condition.status)) return undefined
  if (condition.type === 'Ready') return condition.status === 'True'
  if (
    ['MemoryPressure', 'DiskPressure', 'PIDPressure', 'NetworkUnavailable'].includes(condition.type)
  )
    return condition.status === 'False'
  return undefined
}
export function filterNodes(
  nodes: ClusterNode[],
  keyword: string,
  ready: string,
  scheduling: string,
  role: string,
) {
  const terms = keyword.trim().toLowerCase().split(/\s+/).filter(Boolean)
  return nodes.filter((node) => {
    const text = [
      node.name,
      node.internalIp,
      ...(node.roles ?? []),
      ...Object.entries(node.labels ?? {}).map(([k, v]) => `${k}=${v}`),
      ...(node.taints ?? []).map((t) => `${t.key}=${t.value ?? ''}:${t.effect}`),
    ]
      .join(' ')
      .toLowerCase()
    return (
      terms.every((term) => text.includes(term)) &&
      (!ready || (ready === 'ready' ? nodeReady(node.status) : !nodeReady(node.status))) &&
      (!scheduling || node.unschedulable === (scheduling === 'cordoned')) &&
      (!role || (node.roles ?? []).includes(role))
    )
  })
}
export function nodeMetric(
  data: MonitoringView | undefined,
  clusterId: string | null,
  name: string,
  key: string,
) {
  const panel = data?.clusterId === clusterId ? data.panels.find((p) => p.key === key) : undefined
  const series = panel?.series.find(
    (s) => s.nodeRef.name === name && s.nodeRef.clusterId === clusterId,
  )
  const quality = series?.quality ?? panel?.quality ?? 'empty'
  const value = series?.latest
  return {
    value:
      quality === 'valid' && value != null && Number.isFinite(value) && value >= 0 ? value : null,
    quality,
    reason: series?.reasonCode ?? panel?.reasonCode,
    sampledAt: series?.sampledAt,
  }
}
export function nodeUsage(
  data: MonitoringView | undefined,
  clusterId: string | null,
  name: string,
  kind: 'cpu' | 'memory',
) {
  const metric = nodeMetric(data, clusterId, name, `host.${kind}.used`)
  const snapshot = data?.nodes.find(
    (n) => n.nodeRef.name === name && n.nodeRef.clusterId === clusterId,
  )
  const total =
    kind === 'cpu'
      ? snapshot?.quality === 'valid'
        ? snapshot.capacity.cpuCores
        : null
      : nodeMetric(data, clusterId, name, 'host.memory.total').value === null
        ? null
        : alignedMetricDenominator(data, 'host.memory.used', 'host.memory.total', name)
  return { ...metric, total, percent: monitoringRatio(metric.value, total) }
}
