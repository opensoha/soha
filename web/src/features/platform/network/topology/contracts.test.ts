import { beforeEach, describe, expect, it, vi } from 'vitest'
import { toScopeKey } from '@/types'
import { getNetworkTopology } from './api'
import { topologyKeys } from './keys'
import apiSource from './api.ts?raw'
import pageSource from './page.tsx?raw'
import queriesSource from './queries.ts?raw'
import runtimeSource from './runtime-page.tsx?raw'

const apiMocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('Network Topology contracts', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps the compact topology response with canonical scope', async () => {
    const scope = toScopeKey('cluster-a', 'team-a')
    const topology = {
      clusterId: 'cluster-a',
      source: 'kubernetes',
      generatedAt: 'now',
      summary: {},
      traces: [],
      warnings: [],
    }
    apiMocks.get.mockResolvedValue({ data: topology })

    const controller = new AbortController()
    await expect(getNetworkTopology(scope, controller.signal)).resolves.toBe(topology)
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/clusters/cluster-a/network/topology?namespace=team-a',
      { signal: controller.signal },
    )
    expect(topologyKeys.detail(scope)).toEqual([
      'platform',
      'network',
      'topology',
      { clusterId: 'cluster-a', namespace: 'team-a' },
    ])
    expect(topology).not.toHaveProperty('services')
    expect(topology).not.toHaveProperty('ingresses')
    expect(topology).not.toHaveProperty('httpRoutes')
    expect(topology).not.toHaveProperty('gateways')
    expect(topology).not.toHaveProperty('pods')
  })

  it('keeps Flow and dagre exclusively in the dynamically imported runtime', () => {
    expect(pageSource).toContain("import('./runtime-page')")
    expect(pageSource).not.toContain('@xyflow/react')
    expect(pageSource).not.toContain("from 'dagre'")
    expect(apiSource).not.toContain('@xyflow/react')
    expect(queriesSource).not.toContain('@xyflow/react')
    expect(runtimeSource).toContain("from '@xyflow/react'")
    expect(runtimeSource).toContain("from 'dagre'")
  })
})
