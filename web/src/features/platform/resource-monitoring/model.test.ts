import { describe, expect, it } from 'vitest'
import { alignedMetricDenominator, nodeMetricScore } from './model'
import type { MonitoringNode, MonitoringSeries, MonitoringView } from './types'

const quantity = { cpuCores: 4, memoryBytes: 100, podSlots: 10 }
const node: MonitoringNode = {
  nodeRef: {
    clusterId: 'c',
    apiVersion: 'v1',
    kind: 'Node',
    name: 'n',
    uid: 'uid',
    scopeMode: 'cluster',
  },
  ready: 'True',
  cordoned: false,
  memoryPressure: 'False',
  diskPressure: 'False',
  pidPressure: 'False',
  capacity: quantity,
  allocatable: quantity,
  requests: quantity,
  limits: quantity,
  cpuLimitsComplete: true,
  memoryLimitsComplete: true,
  boundPods: 0,
  pendingPods: 0,
  remainingPodSlots: 10,
  overflowPodSlots: 0,
  quality: 'valid',
}
function view(values: [string, number | null, string | undefined][]): MonitoringView {
  return {
    clusterId: 'c',
    view: 'overview',
    from: '2026-10-19T00:00:00Z',
    to: '2026-10-19T01:00:00Z',
    stepSeconds: 60,
    generatedAt: '2026-10-19T01:00:00Z',
    snapshotQuality: 'valid',
    unscheduledPendingPods: 0,
    nodes: [node],
    panels: values.map(([key, latest, sampledAt]) => ({
      key,
      label: key,
      unit: 'count',
      quality: 'valid',
      series: [
        {
          nodeRef: node.nodeRef,
          metricKey: key,
          unit: 'count',
          source: 'prometheus',
          quality: 'valid',
          latest,
          sampledAt,
          points: [],
        } satisfies MonitoringSeries,
      ],
    })),
  }
}
const time = '2026-10-19T00:01:00Z'
describe('monitoring ratios respect time and capacity ownership', () => {
  it('never divides historical CPU usage by current capacity', () => {
    const data = view([['host.cpu.used', 2, time]])
    expect(nodeMetricScore(data, node, 'cpu', false)).toBe(50)
    expect(nodeMetricScore(data, node, 'cpu', true)).toBeNull()
    expect(nodeMetricScore(data, node, 'pods', true)).toBe(0)
  })
  it('requires matching sample instants for memory and conntrack denominators', () => {
    const data = view([
      ['host.memory.used', 25, time],
      ['host.memory.total', 100, '2026-10-19T00:02:00Z'],
      ['conntrack.entries', 0, time],
      ['conntrack.limit', 10, time],
    ])
    expect(nodeMetricScore(data, node, 'memory', true)).toBeNull()
    expect(nodeMetricScore(data, node, 'conntrack', true)).toBe(0)
    data.panels[1].series[0].sampledAt = '2026-10-19T00:01:00+00:00'
    expect(nodeMetricScore(data, node, 'memory', true)).toBe(25)
    data.panels[3].series[0].sampledAt = undefined
    expect(alignedMetricDenominator(data, 'conntrack.entries', 'conntrack.limit', 'n')).toBeNull()
  })
})
