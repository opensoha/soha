import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { namespaceQueries, nodeQueries } from './queries'

const apiMocks = vi.hoisted(() => ({
  getNodeDetail: vi.fn(),
  getNodeYAML: vi.fn(),
  listNamespaces: vi.fn(),
  listNodes: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

describe('cluster resource query options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('requires a cluster and keeps YAML explicitly lazy', () => {
    const emptyScope = { clusterId: null, namespace: null } as const
    const scope = { clusterId: 'cluster-a', namespace: null } as const

    expect(nodeQueries.list(emptyScope).enabled).toBe(false)
    expect(namespaceQueries.list(emptyScope).enabled).toBe(false)
    expect(nodeQueries.detail(scope, '').enabled).toBe(false)
    expect(nodeQueries.detail(scope, 'node-a').enabled).toBe(true)
    expect(nodeQueries.yaml(scope, 'node-a', false).enabled).toBe(false)
  })

  it('binds node list keys to the unwrapped API function', async () => {
    const scope = { clusterId: 'cluster-a', namespace: null } as const
    apiMocks.listNodes.mockResolvedValueOnce([{ name: 'node-a' }])
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    await expect(queryClient.fetchQuery(nodeQueries.list(scope))).resolves.toEqual([
      { name: 'node-a' },
    ])
    expect(apiMocks.listNodes).toHaveBeenCalledWith(scope)
  })
})
