import { beforeEach, describe, expect, it, vi } from 'vitest'
import { toScopeKey } from '@/types'
import { getGatewayAPIResource, listGatewayAPIResources } from './api'
import { gatewayAPIKeys } from './keys'
import { networkRoutes } from '../routes'

const apiMocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('Gateway API contracts', () => {
  beforeEach(() => vi.clearAllMocks())

  it('keeps GatewayClass cluster-scoped and unwraps domain data', async () => {
    const scope = toScopeKey('cluster-a', null)
    apiMocks.get.mockResolvedValue({ data: [{ name: 'traefik' }] })

    await expect(
      listGatewayAPIResources<{ name: string }>('gatewayclasses', scope),
    ).resolves.toEqual([{ name: 'traefik' }])
    expect(apiMocks.get).toHaveBeenCalledWith('/clusters/cluster-a/network/gatewayclasses')
    expect(gatewayAPIKeys.list('gatewayclasses', scope)).toEqual([
      'platform',
      'network',
      'gatewayclasses',
      'list',
      { clusterId: 'cluster-a', namespace: null },
    ])
  })

  it('uses the lightweight namespaced detail endpoint', async () => {
    const scope = toScopeKey('cluster-a', 'team-a')
    apiMocks.get.mockResolvedValue({ data: { name: 'edge', listeners: [] } })

    await expect(
      getGatewayAPIResource<{ name: string }>('gateways', scope, 'edge'),
    ).resolves.toEqual({ name: 'edge', listeners: [] })
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/clusters/cluster-a/network/gateways/edge/detail?namespace=team-a',
    )
  })

  it('registers all Gateway API detail routes', () => {
    const paths = networkRoutes.map((route) => route.meta.path)
    expect(paths).toEqual(
      expect.arrayContaining([
        '/network/gateway-api/gatewayclasses/:name',
        '/network/gateway-api/gateways/:name',
        '/network/gateway-api/httproutes/:name',
        '/network/gateway-api/grpcroutes/:name',
        '/network/gateway-api/backendtlspolicies/:name',
        '/network/gateway-api/referencegrants/:name',
      ]),
    )
  })
})
