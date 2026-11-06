import { beforeEach, describe, expect, it, vi } from 'vitest'
import { toScopeKey } from '@/types'
import {
  deleteNetworkResource,
  getNetworkYAML,
  listNetworkResources,
  updateNetworkYAML,
} from './api'
import { networkKeys } from './keys'
import { buildNetworkListPath, buildNetworkRoutePath, buildNetworkYAMLPath } from './paths'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('network shared contracts', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('normalizes canonical scope in paths and keys', () => {
    const scope = toScopeKey(' cluster-a ', ' team/a ')

    expect(buildNetworkListPath('services', scope)).toBe(
      '/clusters/cluster-a/network/services?namespace=team%2Fa',
    )
    expect(buildNetworkYAMLPath('ingresses', scope, ' app/api ')).toBe(
      '/clusters/cluster-a/network/ingresses/app%2Fapi/yaml?namespace=team%2Fa',
    )
    expect(buildNetworkRoutePath('services', ' app/api ', ' team/a ')).toBe(
      '/network/services/app%2Fapi?namespace=team%2Fa',
    )
    expect(buildNetworkRoutePath('services', ' app/api ', ' team/a ', { tab: 'yaml' })).toBe(
      '/network/services/app%2Fapi?namespace=team%2Fa&tab=yaml',
    )
    expect(
      buildNetworkRoutePath('services', 'api', 'team', {
        tab: 'backends',
        clusterId: ' cluster-a ',
      }),
    ).toBe('/network/services/api?namespace=team&clusterId=cluster-a&tab=backends')
    expect(networkKeys.list('services', scope)).toEqual([
      'platform',
      'network',
      'services',
      'list',
      { clusterId: 'cluster-a', namespace: 'team/a' },
    ])
  })

  it('unwraps list and YAML domain data and preserves mutation wire shape', async () => {
    const scope = toScopeKey('cluster-a', 'team-a')
    const yaml = { kind: 'Service', name: 'api', namespace: 'team-a', content: 'kind: Service' }
    apiMocks.get
      .mockResolvedValueOnce({ data: [{ name: 'api' }] })
      .mockResolvedValueOnce({ data: yaml })
    apiMocks.put.mockResolvedValueOnce({ data: yaml })
    apiMocks.delete.mockResolvedValueOnce(undefined)

    await expect(listNetworkResources<{ name: string }>('services', scope)).resolves.toEqual([
      { name: 'api' },
    ])
    await expect(getNetworkYAML('services', { scope, name: 'api' })).resolves.toEqual(yaml)
    await expect(
      updateNetworkYAML('services', { scope, name: 'api', content: 'kind: Service' }),
    ).resolves.toEqual(yaml)
    await deleteNetworkResource('services', { scope, name: 'api' })

    const yamlPath = '/clusters/cluster-a/network/services/api/yaml?namespace=team-a'
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      1,
      '/clusters/cluster-a/network/services?namespace=team-a',
    )
    expect(apiMocks.get).toHaveBeenNthCalledWith(2, yamlPath)
    expect(apiMocks.put).toHaveBeenCalledWith(yamlPath, { content: 'kind: Service' })
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster-a/network/services/api?namespace=team-a',
    )
  })
})
