import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { workloadQueries } from './queries'

const apiMocks = vi.hoisted(() => ({
  getWorkloadDetail: vi.fn(),
  getWorkloadMetrics: vi.fn(),
  getWorkloadYAML: vi.fn(),
  listWorkloadEvents: vi.fn(),
  listWorkloads: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

describe('workload shared query options', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('enables cluster-wide lists but requires namespaces for detail queries', () => {
    const clusterScope = { clusterId: 'cluster-a', namespace: null }

    expect(workloadQueries.list('deployments', clusterScope).enabled).toBe(true)
    expect(workloadQueries.detail('deployments', clusterScope, 'api').enabled).toBe(false)
    expect(
      workloadQueries.detail('deployments', { clusterId: 'cluster-a', namespace: 'team-a' }, 'api')
        .enabled,
    ).toBe(true)
    expect(workloadQueries.list('deployments', { clusterId: null, namespace: null }).enabled).toBe(
      false,
    )
  })

  it('keeps the list key and API function together', async () => {
    apiMocks.listWorkloads.mockResolvedValueOnce([{ name: 'api' }])
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const scope = { clusterId: 'cluster-a', namespace: 'team-a' }

    await expect(
      queryClient.fetchQuery(workloadQueries.list('deployments', scope)),
    ).resolves.toEqual([{ name: 'api' }])
    expect(apiMocks.listWorkloads).toHaveBeenCalledWith('deployments', scope)
  })

  it('filters the shared event endpoint by resource name and kind', async () => {
    apiMocks.listWorkloadEvents.mockResolvedValueOnce([
      { involvedKind: 'Deployment', involvedName: 'api' },
      { involvedKind: 'Pod', involvedName: 'api' },
      { involvedKind: 'Deployment', involvedName: 'other' },
    ])
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const scope = { clusterId: 'cluster-a', namespace: 'team-a' }

    await expect(
      queryClient.fetchQuery(workloadQueries.events('deployments', scope, 'api')),
    ).resolves.toEqual([{ involvedKind: 'Deployment', involvedName: 'api' }])
  })
})
