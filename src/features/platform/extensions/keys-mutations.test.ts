import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { crdKeys } from './crds/keys'
import { crdMutations } from './crds/mutations'
import { helmKeys } from './helm/keys'
import { helmMutations } from './helm/mutations'

const apiMocks = vi.hoisted(() => ({
  deleteCustomResource: vi.fn(),
  deleteHelmRelease: vi.fn(),
  getHelmReleaseDetail: vi.fn(),
  getHelmReleaseHistory: vi.fn(),
  rollbackHelmRelease: vi.fn(),
}))

vi.mock('./crds/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./crds/api')>()),
  deleteCustomResource: apiMocks.deleteCustomResource,
}))

vi.mock('./helm/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./helm/api')>()),
  deleteHelmRelease: apiMocks.deleteHelmRelease,
  getHelmReleaseDetail: apiMocks.getHelmReleaseDetail,
  getHelmReleaseHistory: apiMocks.getHelmReleaseHistory,
  rollbackHelmRelease: apiMocks.rollbackHelmRelease,
}))

describe('extensions cache invalidation', () => {
  it('invalidates the exact CRD resource scope after row deletion', async () => {
    apiMocks.deleteCustomResource.mockResolvedValueOnce(undefined)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const crd = {
      name: 'widgets.example.io',
      group: 'example.io',
      kind: 'Widget',
      plural: 'widgets',
      version: 'v1',
      scope: 'Namespaced',
    }
    const target = {
      clusterId: 'cluster-a',
      crd,
      namespace: 'row-ns',
      resourceName: 'widget-a',
    }
    await new MutationObserver(queryClient, crdMutations.remove(queryClient)).mutate(target)
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: crdKeys.resources('cluster-a', crd, 'row-ns'),
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: crdKeys.resources('cluster-a', crd, null),
    })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: crdKeys.yaml(target) })
  })

  it('invalidates all cluster release views after Helm deletion', async () => {
    apiMocks.deleteHelmRelease.mockResolvedValueOnce(undefined)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const target = { clusterId: 'cluster-a', name: 'release-a', namespace: 'row-ns' }
    await new MutationObserver(queryClient, helmMutations.removeRelease(queryClient)).mutate(target)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: helmKeys.releases('cluster-a') })
  })

  it('invalidates the observed Helm release values after rollback', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const target = { clusterId: 'cluster-a', name: 'release-a', namespace: 'row-ns' }
    apiMocks.getHelmReleaseDetail.mockResolvedValueOnce({ ...target, revision: '3' })
    apiMocks.getHelmReleaseHistory.mockResolvedValueOnce([
      { revision: '2', allowedActions: ['rollback'] },
    ])
    apiMocks.rollbackHelmRelease.mockResolvedValueOnce({ revision: '4' })
    await new MutationObserver(queryClient, helmMutations.rollbackRelease(queryClient)).mutate({
      ...target,
      revision: 2,
      currentRevision: '3',
      wait: true,
      timeoutSeconds: 300,
    })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: helmKeys.releaseValues(target) })
  })
})
