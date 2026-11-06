import { describe, it, expect } from 'vitest'
import { prometheusCoverage } from './prometheus-check'
import type { MonitoringView } from '../resource-monitoring/types'
const fixture = () =>
  ({
    clusterId: 'real-id',
    snapshotQuality: 'valid',
    truncated: false,
    nodes: [
      {
        nodeRef: { clusterId: 'real-id', kind: 'Node', uid: 'uid-a', name: 'node-a' },
        quality: 'valid',
      },
    ],
    panels: ['host.cpu.used', 'host.memory.used'].map((key) => ({
      key,
      quality: 'valid',
      series: [
        {
          nodeRef: { clusterId: 'real-id', kind: 'Node', uid: 'uid-a', name: 'node-a' },
          quality: 'valid',
          latest: 0,
        },
      ],
    })),
  }) as MonitoringView
describe('Prometheus connection coverage', () => {
  it('accepts genuine zero host samples only with matching node identity', () => {
    const v = fixture()
    expect(prometheusCoverage(v, 'real-id')).toMatchObject({
      total: 1,
      cpu: 1,
      memory: 1,
      complete: true,
    })
    v.panels[0].series[0].nodeRef.uid = 'different'
    expect(prometheusCoverage(v, 'real-id').complete).toBe(false)
  })
  it('never calls stale or partial host samples a completed connection', () => {
    for (const quality of ['partial', 'stale'] as const) {
      const v = fixture()
      v.panels[0].series[0].quality = quality
      expect(prometheusCoverage(v, 'real-id')).toMatchObject({ cpu: 0, memory: 1, complete: false })
    }
  })
  it('retains the actual data-quality reason and rejects another cluster', () => {
    const v = fixture()
    v.panels[0].reasonCode = 'cluster_label_required'
    v.panels[0].series = []
    expect(prometheusCoverage(v, 'real-id')).toMatchObject({
      reason: 'cluster_label_required',
      complete: false,
    })
    expect(prometheusCoverage(v, 'another-id')).toMatchObject({
      total: 0,
      cpu: 0,
      memory: 0,
      reason: 'cluster_identity_mismatch',
      complete: false,
    })
  })
  it('does not claim full coverage after truncated or failed inventory', () => {
    const v = fixture()
    v.truncated = true
    expect(prometheusCoverage(v, 'real-id').complete).toBe(false)
    v.truncated = false
    v.snapshotQuality = 'error'
    expect(prometheusCoverage(v, 'real-id')).toMatchObject({
      reason: 'node_snapshot_unavailable',
      complete: false,
    })
  })
})
