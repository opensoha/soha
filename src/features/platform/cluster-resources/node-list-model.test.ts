import { describe, it, expect } from 'vitest'
import { conditionHealthy, filterNodes, nodeUsage } from './node-list-model'
import type { ClusterNode } from './types'
import type { MonitoringView } from '../resource-monitoring/types'
const nodes: ClusterNode[] = [
  {
    name: 'worker-a',
    status: 'Ready',
    unschedulable: true,
    roles: ['worker'],
    internalIp: '10.0.0.1',
    labels: { pool: 'gpu' },
    taints: [{ key: 'dedicated', value: 'build', effect: 'NoSchedule' }],
    podCount: 0,
    ageSeconds: 0,
  },
  {
    name: 'master',
    status: 'Unknown',
    unschedulable: false,
    roles: ['control-plane'],
    podCount: 0,
    ageSeconds: 1,
  },
]
describe('node list semantics', () => {
  it('searches names, IP, roles, labels and taints with all terms', () => {
    for (const term of ['worker-a', '10.0.0.1', 'pool=gpu', 'dedicated=build', 'WORKER GPU'])
      expect(filterNodes(nodes, term, '', '', '').map((n) => n.name)).toEqual(['worker-a'])
    expect(filterNodes(nodes, 'worker master', '', '', '')).toEqual([])
  })
  it('keeps Ready and scheduling independent', () => {
    expect(filterNodes(nodes, '', 'ready', 'cordoned', 'worker')).toEqual([nodes[0]])
    expect(filterNodes(nodes, '', 'not-ready', 'schedulable', '')).toEqual([nodes[1]])
  })
  it('treats pressure False as healthy and Ready False as unhealthy', () => {
    expect(conditionHealthy({ type: 'DiskPressure', status: 'False' })).toBe(true)
    expect(conditionHealthy({ type: 'DiskPressure', status: 'True' })).toBe(false)
    expect(conditionHealthy({ type: 'Ready', status: 'False' })).toBe(false)
    expect(conditionHealthy({ type: 'Ready', status: 'Unknown' })).toBeUndefined()
  })
  it('never fabricates zero or accepts wrong-cluster, stale, partial metrics', () => {
    const data = {
      clusterId: 'c',
      nodes: [],
      panels: [
        {
          key: 'host.cpu.used',
          quality: 'valid',
          series: [{ nodeRef: { clusterId: 'c', name: 'worker-a' }, quality: 'valid', latest: 0 }],
        },
      ],
    } as unknown as MonitoringView
    expect(nodeUsage(data, 'c', 'worker-a', 'cpu').value).toBe(0)
    expect(nodeUsage(data, 'other', 'worker-a', 'cpu').value).toBeNull()
    for (const quality of ['partial', 'stale']) {
      data.panels[0].series[0].quality = quality as 'partial' | 'stale'
      expect(nodeUsage(data, 'c', 'worker-a', 'cpu').value).toBeNull()
    }
    expect(nodeUsage(undefined, 'c', 'worker-a', 'memory').value).toBeNull()
  })
})
