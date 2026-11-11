import { beforeEach, describe, expect, it, vi } from 'vitest'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import { statefulSetQueries } from './queries'

const apiMocks = vi.hoisted(() => ({
  getStatefulSetDetail: vi.fn(),
  getStatefulSetMetrics: vi.fn(),
  listStatefulSetEvents: vi.fn(),
  listStatefulSets: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const scope = { clusterId: 'cluster-a', namespace: 'team-a' }

describe('statefulset query options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses canonical keys and scope guards', () => {
    expect(statefulSetQueries.list(scope).queryKey).toEqual(
      workloadKeys.list('statefulsets', scope),
    )
    expect(statefulSetQueries.list({ clusterId: null, namespace: null }).enabled).toBe(false)
    expect(statefulSetQueries.detail(scope, 'db').enabled).toBe(true)
    expect(
      statefulSetQueries.detail({ clusterId: 'cluster-a', namespace: null }, 'db').enabled,
    ).toBe(false)
  })
})
