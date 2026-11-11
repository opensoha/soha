import { MutationObserver, QueryClient, type MutationOptions } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import { statefulSetMutations } from './mutations'

const apiMocks = vi.hoisted(() => ({
  deleteStatefulSet: vi.fn(),
  restartStatefulSet: vi.fn(),
  scaleStatefulSet: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const target = {
  scope: { clusterId: 'cluster-a', namespace: 'team-a' },
  name: 'db',
}

async function expectInvalidation<TVariables>(
  createOptions: (queryClient: QueryClient) => MutationOptions<void, Error, TVariables>,
  variables: TVariables,
) {
  const queryClient = new QueryClient()
  const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
  const observer = new MutationObserver(queryClient, createOptions(queryClient))

  await expect(observer.mutate(variables)).resolves.toBeUndefined()
  expect(invalidate).toHaveBeenCalledTimes(3)
  expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('statefulsets') })
  expect(invalidate).toHaveBeenCalledWith({
    queryKey: workloadKeys.detail('statefulsets', target.scope, target.name),
  })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('pods') })
}

describe('statefulset mutation options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('invalidates list, detail, and pod dependencies after every action', async () => {
    apiMocks.restartStatefulSet.mockResolvedValueOnce(undefined)
    apiMocks.scaleStatefulSet.mockResolvedValueOnce(undefined)
    apiMocks.deleteStatefulSet.mockResolvedValueOnce(undefined)

    await expectInvalidation(statefulSetMutations.restart, target)
    await expectInvalidation(statefulSetMutations.scale, { ...target, replicas: 3 })
    await expectInvalidation(statefulSetMutations.remove, target)
  })

  it('does not invalidate caches after a failed action', async () => {
    const failure = new Error('restart failed')
    apiMocks.restartStatefulSet.mockRejectedValueOnce(failure)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, statefulSetMutations.restart(queryClient))

    await expect(observer.mutate(target)).rejects.toBe(failure)
    expect(invalidate).not.toHaveBeenCalled()
  })
})
