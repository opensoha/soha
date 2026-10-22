import { describe, expect, it } from 'vitest'
import { workloadKeys } from './keys'

describe('workload query keys', () => {
  it('uses a hierarchical domain root', () => {
    expect(workloadKeys.lists('deployments')).toEqual([
      'platform',
      'workloads',
      'deployments',
      'list',
    ])
    expect(
      workloadKeys.detail('deployments', { clusterId: 'cluster-a', namespace: 'team-a' }, 'api'),
    ).toEqual([
      'platform',
      'workloads',
      'deployments',
      'detail',
      { clusterId: 'cluster-a', namespace: 'team-a' },
      'api',
    ])
  })

  it('normalizes scope and names exactly once inside the factory', () => {
    expect(
      workloadKeys.detail(
        'deployments',
        { clusterId: ' cluster-a ', namespace: ' team-a ' },
        ' api ',
      ),
    ).toEqual(
      workloadKeys.detail('deployments', { clusterId: 'cluster-a', namespace: 'team-a' }, 'api'),
    )
    expect(workloadKeys.list('pods', { clusterId: null, namespace: 'ignored' })).toEqual(
      workloadKeys.list('pods', { clusterId: null, namespace: null }),
    )
  })

  it('nests deployment-specific data under the detail prefix', () => {
    const scope = { clusterId: 'cluster-a', namespace: 'team-a' }
    const detailKey = workloadKeys.detail('deployments', scope, 'api')

    expect(workloadKeys.rolloutStatus(scope, 'api').slice(0, detailKey.length)).toEqual(detailKey)
    expect(workloadKeys.rollouts(scope, 'api').slice(0, detailKey.length)).toEqual(detailKey)
    expect(
      workloadKeys.relatedPods('deployments', scope, 'api').slice(0, detailKey.length),
    ).toEqual(detailKey)
  })

  it('normalizes related pod selectors independently of object insertion order', () => {
    const scope = { clusterId: 'cluster-a', namespace: 'team-a' }

    expect(
      workloadKeys.relatedPods('deployments', scope, 'api', { tier: 'web', app: 'api' }),
    ).toEqual(workloadKeys.relatedPods('deployments', scope, 'api', { app: 'api', tier: 'web' }))
  })
  it('isolates explicit resource UIDs while preserving legacy name-based metric keys', () => {
    const scope = { clusterId: 'cluster-a', namespace: 'team' }
    const legacy = workloadKeys.metrics('pods', scope, 'pod', 60)
    const first = workloadKeys.metrics('pods', scope, 'pod', 60, 'uid-1')
    const second = workloadKeys.metrics('pods', scope, 'pod', 60, 'uid-2')
    expect(first).not.toEqual(second)
    expect(first.slice(0, legacy.length)).toEqual(legacy)
    expect(workloadKeys.metrics('pods', scope, 'pod', 60, undefined)).toEqual(legacy)
  })
})
