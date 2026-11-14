import { beforeEach, describe, expect, it, vi } from 'vitest'
import { toScopeKey } from '@/types'
import { getNetworkCoreResource, listNetworkCoreResources } from './api'

const apiMocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('network core resource contracts', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses canonical cluster and namespace scope for detail and list endpoints', async () => {
    const scope = toScopeKey(' cluster-a ', ' team/a ')
    apiMocks.get
      .mockResolvedValueOnce({ data: { name: 'web-policy' } })
      .mockResolvedValueOnce({ data: [{ name: 'web-policy' }] })

    await expect(
      getNetworkCoreResource<{ name: string }>('networkpolicies', scope, 'web-policy'),
    ).resolves.toEqual({ name: 'web-policy' })
    await expect(
      listNetworkCoreResources<{ name: string }>('endpointslices', scope),
    ).resolves.toEqual([{ name: 'web-policy' }])
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      1,
      '/clusters/cluster-a/network/networkpolicies/web-policy/detail?namespace=team%2Fa',
    )
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      2,
      '/clusters/cluster-a/network/endpointslices?namespace=team%2Fa',
    )
  })
})
