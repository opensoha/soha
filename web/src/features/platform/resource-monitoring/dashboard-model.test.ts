import { describe, expect, it } from 'vitest'
import {
  dashboardMetrics,
  monitoringSummary,
  monitoringAllocations,
  monitoringPodCapacity,
  monitoringPanelSummary,
} from './dashboard-model'
import type { MonitoringNode, MonitoringView } from './types'

const time = '2026-10-19T01:00:00Z'
const earlier = '2026-10-19T00:59:00Z'
function data(): MonitoringView {
  const nodes = ['a', 'b'].map((name) => ({
    nodeRef: {
      name,
      uid: name,
      clusterId: 'cluster',
      apiVersion: 'v1',
      kind: 'Node',
      scopeMode: 'cluster',
    },
    capacity: { cpuCores: 4, memoryBytes: 100, podSlots: 10 },
    allocatable: { cpuCores: 3, memoryBytes: 90, podSlots: 10 },
    boundPods: 0,
  })) as MonitoringNode[]
  return {
    clusterId: 'cluster',
    view: 'cpu',
    from: earlier,
    to: time,
    generatedAt: time,
    snapshotObservedAt: time,
    stepSeconds: 60,
    snapshotQuality: 'valid',
    unscheduledPendingPods: 0,
    nodes,
    panels: [
      {
        key: 'host.cpu.used',
        label: 'CPU',
        unit: 'cores',
        quality: 'valid',
        series: nodes.map((node, i) => ({
          nodeRef: node.nodeRef,
          metricKey: 'host.cpu.used',
          unit: 'cores',
          source: 'prometheus',
          quality: 'valid',
          latest: i,
          sampledAt: time,
          points: [
            { timestamp: earlier, value: i === 0 ? null : 1 },
            { timestamp: time, value: i },
          ],
        })),
      },
    ],
  }
}
describe('monitoring overview aggregation', () => {
  it('keeps genuine zero and requires all selected nodes at every sample', () => {
    const result = monitoringSummary(data(), dashboardMetrics[0], ['a', 'b'], false)
    expect(result.value).toBe(1)
    expect(result.denominator).toBe(8)
    expect(result.points.map((point) => point.value)).toEqual([null, 1])
    expect(monitoringSummary(data(), dashboardMetrics[0], ['a'], false).value).toBe(0)
  })
  it('never combines different latest instants or foreign/changed node identities', () => {
    const d = data()
    d.panels[0].series[1].sampledAt = earlier
    expect(monitoringSummary(d, dashboardMetrics[0], ['a', 'b'], false).value).toBeNull()
    d.panels[0].series[1].sampledAt = time
    d.panels[0].series[1].nodeRef = { ...d.panels[0].series[1].nodeRef, clusterId: 'other' }
    expect(monitoringSummary(d, dashboardMetrics[0], ['a', 'b'], false).quality).toBe('partial')
    d.panels[0].series[0].nodeRef = { ...d.panels[0].series[0].nodeRef, uid: 'replaced' }
    expect(monitoringSummary(d, dashboardMetrics[0], ['a'], false).value).toBeNull()
  })
  it('does not use current CPU capacity as a historical denominator or double count selections', () => {
    const d = data()
    expect(monitoringSummary(d, dashboardMetrics[0], ['a', 'b'], true).denominator).toBeNull()
    expect(monitoringSummary(d, dashboardMetrics[0], ['b', 'b'], false).value).toBe(1)
  })
  it('uses current Pod snapshots only in live mode and never fills historical gaps with them', () => {
    const d = data()
    d.panels[0].key = 'pods.bound'
    d.panels[0].series[0].latest = null
    expect(monitoringSummary(d, dashboardMetrics[3], ['a'], false).value).toBe(0)
    expect(monitoringSummary(d, dashboardMetrics[3], ['a'], true).value).toBeNull()
    expect(monitoringSummary(d, dashboardMetrics[3], ['a'], true).denominator).toBeNull()
  })
})

describe('monitoring diagnostic highlights', () => {
  it('keeps snapshot requests, allocatable and incomplete limits distinct', () => {
    const d = data()
    for (const node of d.nodes) {
      node.requests = { cpuCores: 0, memoryBytes: 20, podSlots: null }
      node.limits = { cpuCores: 2, memoryBytes: 50, podSlots: null }
      node.cpuLimitsComplete = true
      node.memoryLimitsComplete = true
    }
    expect(monitoringAllocations(d, ['a', 'a', 'b'], 'cpuCores')).toEqual({
      requests: 0,
      limits: 4,
      allocatable: 6,
    })
    d.nodes[1].cpuLimitsComplete = false
    expect(monitoringAllocations(d, ['a', 'b'], 'cpuCores').limits).toBeNull()
    expect(monitoringAllocations(d, ['a', 'missing'], 'memoryBytes').requests).toBeNull()
    d.nodes[0].nodeRef = { ...d.nodes[0].nodeRef, clusterId: 'foreign' }
    expect(monitoringAllocations(d, ['a'], 'cpuCores').requests).toBeNull()
  })
  it('does not turn missing Pod slots or pending observations into zero', () => {
    const d = data()
    Object.assign(d.nodes[0], { pendingPods: 0, remainingPodSlots: 0, overflowPodSlots: 2 })
    expect(monitoringPodCapacity(d, ['a'])).toEqual({ pending: 0, remaining: 0, overflow: 2 })
    expect(monitoringPodCapacity(d, ['a', 'b'])).toEqual({
      pending: null,
      remaining: null,
      overflow: null,
    })
  })
  it('uses the same identity and timestamp safeguards for conntrack event rates', () => {
    const d = data()
    d.panels[0].key = 'conntrack.drop.rate'
    expect(monitoringPanelSummary(d, 'conntrack.drop.rate', ['a', 'b']).value).toBe(1)
    d.panels[0].series[1].sampledAt = earlier
    expect(monitoringPanelSummary(d, 'conntrack.drop.rate', ['a', 'b']).value).toBeNull()
    expect(monitoringPanelSummary(d, 'conntrack.insert_failed.rate', ['a']).quality).toBe(
      'not_collected',
    )
  })
})
