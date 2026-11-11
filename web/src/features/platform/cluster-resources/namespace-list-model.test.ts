import { describe, expect, it } from 'vitest'
import type { Pod } from '../workloads/pods/types'
import {
  filterNamespaces,
  namespaceLabels,
  namespaceRows,
  podNeedsAttention,
} from './namespace-list-model'

const namespace = { name: 'team-a', status: 'Active', labels: { owner: 'Platform' }, ageSeconds: 0 }
const pod = (patch: Partial<Pod> = {}): Pod => ({
  name: 'worker',
  namespace: 'team-a',
  phase: 'Running',
  readyContainers: '1/1',
  restarts: 0,
  nodeName: 'node-a',
  ageSeconds: 0,
  ...patch,
})

describe('namespace list observations', () => {
  it('distinguishes absent observations from a successful empty Pod list', () => {
    expect(namespaceRows([namespace], undefined)[0].podCount).toBeUndefined()
    expect(namespaceRows([namespace], [])[0]).toMatchObject({
      podCount: 0,
      attentionCount: 0,
      ageSeconds: 0,
    })
  })

  it('groups once by namespace and excludes completed jobs and successful init containers from incidents', () => {
    const rows = namespaceRows(
      [namespace],
      [
        pod(),
        pod({ phase: 'Succeeded', readyContainers: '0/1' }),
        pod({
          containerStatuses: [
            { name: 'init', kind: 'init', state: 'terminated', ready: false, exitCode: 0 },
          ],
        }),
        pod({ phase: 'Pending', readyContainers: '0/1' }),
        pod({ readyContainers: '1/2' }),
        pod({ phase: 'Failed' }),
        pod({ readyContainers: '', containerStatuses: [] }),
        pod({ namespace: 'another', phase: 'Failed' }),
      ],
    )
    expect(rows[0]).toMatchObject({ podCount: 7, attentionCount: 3, unknownReadiness: 1 })
  })

  it('handles current failure states, sidecars and missing readiness without relying on restart totals', () => {
    expect(podNeedsAttention(pod({ restarts: 20 }))).toBe(false)
    expect(
      podNeedsAttention(
        pod({
          containerStatuses: [
            {
              name: 'main',
              kind: 'container',
              state: 'waiting',
              ready: false,
              reason: 'CrashLoopBackOff',
            },
          ],
        }),
      ),
    ).toBe(true)
    expect(
      podNeedsAttention(
        pod({
          containerStatuses: [
            { name: 'sidecar', kind: 'sidecar', state: 'terminated', ready: false, exitCode: 0 },
          ],
        }),
      ),
    ).toBe(true)
    expect(podNeedsAttention(pod({ readyContainers: '0/0' }))).toBeUndefined()
    expect(podNeedsAttention(pod({ readyContainers: '2/1' }))).toBeUndefined()
  })

  it('searches all label keys and values case-insensitively while retaining the native phase', () => {
    const rows = [namespace, { ...namespace, name: 'retiring', status: 'Terminating' }]
    expect(filterNamespaces(rows, ' owner=platform ', 'Active')).toEqual([namespace])
    expect(filterNamespaces(rows, 'RETIR', 'Terminating')).toHaveLength(1)
    expect(filterNamespaces(rows, 'missing', '')).toHaveLength(0)
    expect(
      namespaceLabels({ 'kubernetes.io/metadata.name': 'team-a', owner: 'Platform' })[0],
    ).toEqual(['owner', 'Platform'])
  })
})
