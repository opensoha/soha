import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/services/api-client'
import { getKubernetesResourceGraph, getKubernetesSecurityPosture } from './api'

vi.mock('@/services/api-client', () => ({ api: { get: vi.fn() } }))

describe('Kubernetes resource insights API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.get).mockResolvedValue({ data: {} })
  })

  it('encodes graph target and security scope', async () => {
    await getKubernetesResourceGraph(
      { clusterId: 'cluster/a', namespace: 'team a' },
      'Deployment',
      'api/web',
    )
    await getKubernetesSecurityPosture({ clusterId: 'cluster/a', namespace: 'team a' }, 50)

    expect(api.get).toHaveBeenNthCalledWith(
      1,
      '/clusters/cluster%2Fa/resources/graph?namespace=team+a&kind=Deployment&name=api%2Fweb',
    )
    expect(api.get).toHaveBeenNthCalledWith(
      2,
      '/clusters/cluster%2Fa/security/posture?namespace=team+a&limit=50',
    )
  })
})
