import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/services/api-client'
import { planResourceUpdate } from './resource-update-plan'

vi.mock('@/services/api-client', () => ({
  api: { post: vi.fn() },
}))

describe('planResourceUpdate', () => {
  beforeEach(() => vi.clearAllMocks())

  it('posts the selected resource and manifest to the cluster plan endpoint', async () => {
    vi.mocked(api.post).mockResolvedValue({
      data: {
        capability: 'k8s.resources.update',
        target: 'cluster-a/team-a/Deployment/api',
        ready: true,
        riskLevel: 'mutate',
        requiresApproval: false,
        changes: [],
        warnings: [],
      },
    })

    await planResourceUpdate({
      scope: { clusterId: 'cluster-a', namespace: 'team-a' },
      kind: 'Deployment',
      name: 'api',
      content: 'kind: Deployment',
    })

    expect(api.post).toHaveBeenCalledWith('/clusters/cluster-a/resources/update-plan', {
      namespace: 'team-a',
      kind: 'Deployment',
      name: 'api',
      content: 'kind: Deployment',
    })
  })

  it('rejects a plan without a selected cluster', async () => {
    await expect(
      planResourceUpdate({
        scope: { clusterId: null, namespace: null },
        kind: 'Service',
        name: 'api',
        content: 'kind: Service',
      }),
    ).rejects.toThrow('A cluster is required')
  })
})
