import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { manifestKeys } from './keys'
import { manifestMutations } from './mutations'

const apiMocks = vi.hoisted(() => ({
  create: vi.fn(),
  publish: vi.fn(),
  remove: vi.fn(),
  update: vi.fn(),
  updateBinding: vi.fn(),
}))
vi.mock('./api', () => ({ manifestApi: apiMocks }))

describe('manifest mutations', () => {
  it('invalidates delivery and platform manifest views after publishing', async () => {
    apiMocks.publish.mockResolvedValue({ id: 'manifest-1' })
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, manifestMutations.publish(queryClient))

    await observer.mutate({ id: 'manifest-1', note: 'release' })

    expect(invalidate).toHaveBeenCalledWith({ queryKey: manifestKeys.all })
  })

  it('refreshes only the changed package environment views after updating a binding', async () => {
    apiMocks.updateBinding.mockResolvedValue({ id: 'binding-1' })
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, manifestMutations.updateBinding(queryClient))
    const input = {
      applicationEnvironmentId: 'environment-1',
      clusterId: 'cluster-1',
      namespace: 'demo',
      overlay: {},
      rolloutStrategyId: 'strategy-1',
      verificationPolicyId: 'policy-1',
      driftPolicy: 'report' as const,
      deletionPolicy: 'orphan' as const,
      enabled: true,
      expectedVersion: 2,
    }

    await observer.mutate({ id: 'binding-1', packageId: 'package-1', input })

    expect(apiMocks.updateBinding).toHaveBeenCalledWith('binding-1', input)
    expect(invalidate).toHaveBeenNthCalledWith(1, {
      queryKey: manifestKeys.bindings('package-1'),
    })
    expect(invalidate).toHaveBeenNthCalledWith(2, {
      queryKey: manifestKeys.deployments('package-1'),
    })
  })
})
