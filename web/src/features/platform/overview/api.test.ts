import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/services/api-client'
import {
  getOverviewEvents,
  getOverviewMonitoringSummary,
  getOverviewWorkload,
  searchOverviewResources,
} from './api'
import { platformOverviewKeys } from './keys'

vi.mock('@/services/api-client', () => ({
  api: {
    get: vi.fn(),
  },
}))

describe('platform overview data boundary', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.get).mockResolvedValue({ data: {} })
  })

  it('preserves overview query tuples', () => {
    expect(platformOverviewKeys.clusters()).toEqual(['platform', 'clusters', 'list'])
    expect(platformOverviewKeys.monitoringSummary()).toEqual(['monitoring-summary'])
    expect(platformOverviewKeys.events('cluster-1', 12)).toEqual([
      'overview-events',
      'cluster-1',
      '__all__',
      { limit: 12 },
    ])
    expect(platformOverviewKeys.workload('cluster-1')).toEqual([
      'overview-workload',
      'cluster-1',
      '__all__',
    ])
  })

  it('preserves overview wire paths', async () => {
    await getOverviewMonitoringSummary()
    await getOverviewWorkload('cluster-1')
    await getOverviewEvents('cluster-1', 12)

    expect(api.get).toHaveBeenNthCalledWith(1, '/monitoring/summary')
    expect(api.get).toHaveBeenNthCalledWith(2, '/clusters/cluster-1/workloads/overview')
    expect(api.get).toHaveBeenNthCalledWith(3, '/clusters/cluster-1/events?limit=12')
  })

  it('searches one authorized resource kind through the aggregated endpoint', async () => {
    vi.mocked(api.get).mockResolvedValueOnce({
      data: {
        items: [
          {
            resource: {
              apiVersion: 'v1',
              clusterId: 'cluster-1',
              kind: 'Pod',
              name: 'api-0',
              namespace: 'prod',
              scopeMode: 'namespace',
            },
            status: 'Running',
          },
        ],
        truncated: false,
      },
    })

    await expect(searchOverviewResources('cluster-1', 'pods', 'api')).resolves.toEqual({
      items: [
        {
          key: 'pods:prod:api-0',
          kind: 'pods',
          name: 'api-0',
          namespace: 'prod',
          status: 'Running',
          path: '/workloads/pods/api-0?clusterId=cluster-1&namespace=prod',
        },
      ],
      truncated: false,
    })
    expect(api.get).toHaveBeenCalledWith(
      '/clusters/cluster-1/resources/search?q=api&kinds=Pod&limit=12',
    )
  })
})
