import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { workloadKeys } from '../shared/keys'
import { workloadOverviewQueries } from './queries'

const apiMocks = vi.hoisted(() => ({ listWorkloadEvents: vi.fn() }))

vi.mock('../shared/api', () => apiMocks)

describe('workload overview query options', () => {
  it('uses a scoped key and passes the overview event limit to transport', async () => {
    const scope = { clusterId: 'cluster-a', namespace: 'team-a' }
    apiMocks.listWorkloadEvents.mockResolvedValueOnce([{ name: 'scheduled' }])
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    const options = workloadOverviewQueries.events(scope)
    expect(options.queryKey).toEqual(workloadKeys.overviewEvents(scope, 200))
    await expect(client.fetchQuery(options)).resolves.toEqual([{ name: 'scheduled' }])
    expect(apiMocks.listWorkloadEvents).toHaveBeenCalledWith(scope, 200)
  })

  it('disables events without a cluster', () => {
    expect(workloadOverviewQueries.events({ clusterId: null, namespace: null }).enabled).toBe(false)
  })
})
