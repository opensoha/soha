import { alignedMetricDenominator, monitoringRatio } from './model'
import type { MonitoringView } from './types'

export const dashboardMetrics = [
  { view: 'cpu', key: 'host.cpu.used', unit: 'cores', tone: 'metric' },
  { view: 'memory', key: 'host.memory.used', unit: 'bytes', tone: 'service' },
  { view: 'conntrack', key: 'conntrack.entries', unit: 'count', tone: 'span' },
  { view: 'pods', key: 'pods.bound', unit: 'count', tone: 'scope' },
] as const
export type DashboardMetric = (typeof dashboardMetrics)[number]
export type SummaryPoint = { time: string; value: number | null }

const sumComplete = (values: (number | null | undefined)[]) =>
  values.length && values.every((value) => value != null && Number.isFinite(value))
    ? values.reduce<number>((sum, value) => sum + value!, 0)
    : null

export function monitoringPanelSummary(data: MonitoringView, key: string, names: string[]) {
  names = [...new Set(names)]
  const nodes = names.map((name) =>
    data.nodes.find(
      (node) => node.nodeRef.name === name && node.nodeRef.clusterId === data.clusterId,
    ),
  )
  const panel = data.panels.find((panel) => panel.key === key)
  const series = nodes.map((node) =>
    panel?.series.find(
      (item) =>
        node?.nodeRef.uid &&
        item.nodeRef.uid === node.nodeRef.uid &&
        item.nodeRef.name === node.nodeRef.name &&
        item.nodeRef.clusterId === data.clusterId,
    ),
  )
  const instants = series.map((item) => Date.parse(item?.sampledAt || ''))
  const aligned =
    instants.length > 0 && instants.every((time) => Number.isFinite(time) && time === instants[0])
  const latest = aligned ? sumComplete(series.map((item) => item?.latest)) : null
  const byNode = series.map(
    (item) => new Map(item?.points.map((point) => [Date.parse(point.timestamp), point.value])),
  )
  const timestamps = [...new Set(byNode.flatMap((points) => [...points.keys()]))]
    .filter(Number.isFinite)
    .sort((a, b) => a - b)
  const points = timestamps.map((time) => ({
    time: new Date(time).toISOString(),
    value: sumComplete(byNode.map((points) => points.get(time))),
  }))
  return {
    value: latest,
    points,
    aligned,
    observedAt: aligned ? series[0]?.sampledAt : undefined,
    quality: !panel
      ? 'not_collected'
      : latest == null && panel.quality === 'valid'
        ? 'partial'
        : panel.quality,
  }
}

export function monitoringSummary(
  data: MonitoringView,
  metric: DashboardMetric,
  names: string[],
  historical: boolean,
) {
  names = [...new Set(names)]
  const nodes = names.map((name) =>
    data.nodes.find(
      (node) => node.nodeRef.name === name && node.nodeRef.clusterId === data.clusterId,
    ),
  )
  const panel = data.panels.find((panel) => panel.key === metric.key)
  const {
    value: latest,
    points,
    aligned,
    observedAt,
  } = monitoringPanelSummary(data, metric.key, names)
  let denominator: number | null = null
  if (metric.view === 'memory' || metric.view === 'conntrack') {
    denominator = aligned
      ? sumComplete(
          names.map((name) =>
            alignedMetricDenominator(
              data,
              metric.key,
              metric.view === 'memory' ? 'host.memory.total' : 'conntrack.limit',
              name,
            ),
          ),
        )
      : null
  } else if (!historical) {
    denominator = sumComplete(
      nodes.map((node) =>
        metric.view === 'cpu' ? node?.capacity.cpuCores : node?.allocatable.podSlots,
      ),
    )
  }
  const snapshotPods = !historical && metric.view === 'pods'
  const value = snapshotPods ? sumComplete(nodes.map((node) => node?.boundPods)) : latest
  const quality = !panel
    ? 'not_collected'
    : value == null && panel.quality === 'valid'
      ? 'partial'
      : panel.quality
  return {
    value,
    denominator,
    ratio: monitoringRatio(value, denominator),
    points,
    quality,
    observedAt: snapshotPods ? data.snapshotObservedAt : observedAt,
    snapshotPods,
  }
}

export function monitoringAllocations(
  data: MonitoringView,
  names: string[],
  quantity: 'cpuCores' | 'memoryBytes',
) {
  const nodes = [...new Set(names)].map((name) =>
    data.nodes.find(
      (node) => node.nodeRef.name === name && node.nodeRef.clusterId === data.clusterId,
    ),
  )
  return {
    requests: sumComplete(nodes.map((node) => node?.requests[quantity])),
    allocatable: sumComplete(nodes.map((node) => node?.allocatable[quantity])),
    limits: nodes.every((node) =>
      quantity === 'cpuCores' ? node?.cpuLimitsComplete : node?.memoryLimitsComplete,
    )
      ? sumComplete(nodes.map((node) => node?.limits[quantity]))
      : null,
  }
}

export function monitoringPodCapacity(data: MonitoringView, names: string[]) {
  const nodes = [...new Set(names)].map((name) =>
    data.nodes.find(
      (node) => node.nodeRef.name === name && node.nodeRef.clusterId === data.clusterId,
    ),
  )
  return {
    pending: sumComplete(nodes.map((node) => node?.pendingPods)),
    remaining: sumComplete(nodes.map((node) => node?.remainingPodSlots)),
    overflow: sumComplete(nodes.map((node) => node?.overflowPodSlots)),
  }
}
