import { beforeEach, describe, expect, it, vi } from 'vitest'
import { toScopeKey } from '@/types'
import { getService, getServiceMetrics, listServiceEvents } from './api'

const apiMocks = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('service API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('loads the Service detail without listing all Services or Pods', async () => {
    const scope = toScopeKey('cluster-a', 'team-a')
    const detail = { name: 'api', labels: { app: 'api' }, endpoints: [], backendPods: [] }
    apiMocks.get.mockResolvedValue({ data: detail })

    await expect(getService(scope, 'api')).resolves.toBe(detail)
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/clusters/cluster-a/network/services/api/detail?namespace=team-a',
    )
  })

  it('uses the existing metrics and events endpoints and returns domain values', async () => {
    const scope = toScopeKey('cluster-a', 'team-a')
    const metrics = { resourceKind: 'Service', resourceName: 'api', rangeMinutes: 60 }
    apiMocks.get.mockResolvedValueOnce({ data: metrics }).mockResolvedValueOnce({
      data: [
        { name: 'a', namespace: 'team-a', involvedKind: 'Service', involvedName: 'api' },
        { name: 'b', namespace: 'team-a', involvedKind: 'Ingress', involvedName: 'api' },
        {
          name: 'wrong-namespace',
          namespace: 'team-b',
          involvedKind: 'Service',
          involvedName: 'api',
        },
        { name: 'missing-namespace', involvedKind: 'Service', involvedName: 'api' },
        { name: 'missing-kind', namespace: 'team-a', involvedName: 'api' },
      ],
    })

    await expect(getServiceMetrics(scope, 'api')).resolves.toBe(metrics)
    await expect(listServiceEvents(scope, 'api')).resolves.toEqual([
      { name: 'a', namespace: 'team-a', involvedKind: 'Service', involvedName: 'api' },
    ])
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      1,
      '/clusters/cluster-a/network/services/api/metrics?namespace=team-a',
    )
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      2,
      '/clusters/cluster-a/events?namespace=team-a&limit=100',
    )
  })
})
