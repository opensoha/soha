import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { clusterKeys } from './keys'
import { clusterQueries } from './queries'

const apiMocks = vi.hoisted(() => ({
  getClusterDetail: vi.fn(),
  listClusterNodes: vi.fn(),
  listClusters: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const scope = { clusterId: 'cluster-a', namespace: 'ignored' }

describe('cluster query options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses canonical scope keys and guards empty detail scopes', () => {
    expect(clusterQueries.list().queryKey).toEqual(clusterKeys.list())
    expect(clusterQueries.detail(scope).queryKey).toEqual(
      clusterKeys.detail({ clusterId: 'cluster-a', namespace: null }),
    )
    expect(clusterQueries.detail(scope).enabled).toBe(true)
    expect(clusterQueries.detail({ clusterId: null, namespace: null }).enabled).toBe(false)
  })

  it('binds node query keys and functions to the same scope', async () => {
    apiMocks.listClusterNodes.mockResolvedValueOnce([{ name: 'node-a' }])
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    await expect(queryClient.fetchQuery(clusterQueries.nodes(scope))).resolves.toEqual([
      { name: 'node-a' },
    ])
    expect(apiMocks.listClusterNodes).toHaveBeenCalledWith({ scope })
    expect(clusterQueries.nodes(scope).queryKey).toEqual(clusterKeys.nodes(scope))
  })
})
