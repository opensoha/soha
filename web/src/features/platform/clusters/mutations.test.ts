import { MutationObserver, QueryClient, type MutationOptions } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { clusterKeys } from './keys'
import { clusterMutations } from './mutations'

const apiMocks = vi.hoisted(() => ({
  createAgentInstallation: vi.fn(),
  createCluster: vi.fn(),
  deleteCluster: vi.fn(),
  updateCluster: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const target = { scope: { clusterId: 'cluster-a', namespace: null } }

async function expectTargetInvalidation<TData, TVariables>(
  createOptions: (queryClient: QueryClient) => MutationOptions<TData, Error, TVariables>,
  variables: TVariables,
) {
  const queryClient = new QueryClient()
  const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
  const observer = new MutationObserver(queryClient, createOptions(queryClient))

  await observer.mutate(variables)
  expect(invalidate).toHaveBeenCalledWith({ queryKey: clusterKeys.list() })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: clusterKeys.detail(target.scope) })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: clusterKeys.nodes(target.scope) })
}

describe('cluster mutation options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('centralizes update and delete target invalidation', async () => {
    apiMocks.updateCluster.mockResolvedValueOnce({ id: 'cluster-a' })
    apiMocks.deleteCluster.mockResolvedValueOnce(undefined)

    await expectTargetInvalidation(clusterMutations.update, { ...target, values: { name: 'A' } })
    await expectTargetInvalidation(clusterMutations.remove, target)
  })

  it('invalidates every deleted target after batch deletion', async () => {
    const secondScope = { clusterId: 'cluster-b', namespace: null }
    apiMocks.deleteCluster.mockResolvedValue(undefined)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, clusterMutations.removeMany(queryClient))

    await observer.mutate({ scopes: [target.scope, secondScope] })
    expect(apiMocks.deleteCluster).toHaveBeenCalledTimes(2)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: clusterKeys.list() })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: clusterKeys.detail(secondScope) })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: clusterKeys.nodes(secondScope) })
  })

  it('does not invalidate when delete transport fails', async () => {
    const failure = new Error('delete failed')
    apiMocks.deleteCluster.mockRejectedValueOnce(failure)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, clusterMutations.remove(queryClient))

    await expect(observer.mutate(target)).rejects.toBe(failure)
    expect(invalidate).not.toHaveBeenCalled()
  })

  it('keeps each batch target result and invalidates partial deletion records', async () => {
    const failure = new Error('cluster-b refused deletion')
    apiMocks.deleteCluster.mockResolvedValueOnce(undefined).mockRejectedValueOnce(failure)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, clusterMutations.removeMany(queryClient))
    const secondScope = { clusterId: 'cluster-b', namespace: null }

    const results = await observer.mutate({ scopes: [target.scope, secondScope] })
    expect(results).toEqual([
      { status: 'fulfilled', value: undefined },
      { status: 'rejected', reason: failure },
    ])
    expect(invalidate).toHaveBeenCalledWith({ queryKey: clusterKeys.list() })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: clusterKeys.detail(secondScope) })
  })
})
