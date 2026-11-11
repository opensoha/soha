import { MutationObserver, QueryClient, type MutationOptions } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import { daemonSetMutations } from './mutations'

const apiMocks = vi.hoisted(() => ({
  deleteDaemonSet: vi.fn(),
  restartDaemonSet: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const target = {
  scope: { clusterId: 'cluster-a', namespace: 'team-a' },
  name: 'agent',
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
  expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('daemonsets') })
  expect(invalidate).toHaveBeenCalledWith({
    queryKey: workloadKeys.detail('daemonsets', target.scope, target.name),
  })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('pods') })
}

describe('daemonset mutation options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('invalidates list, detail, and pod dependencies after every action', async () => {
    apiMocks.restartDaemonSet.mockResolvedValueOnce(undefined)
    apiMocks.deleteDaemonSet.mockResolvedValueOnce(undefined)

    await expectInvalidation(daemonSetMutations.restart, target)
    await expectInvalidation(daemonSetMutations.remove, target)
  })

  it('does not invalidate caches after a failed action', async () => {
    const failure = new Error('restart failed')
    apiMocks.restartDaemonSet.mockRejectedValueOnce(failure)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const observer = new MutationObserver(queryClient, daemonSetMutations.restart(queryClient))

    await expect(observer.mutate(target)).rejects.toBe(failure)
    expect(invalidate).not.toHaveBeenCalled()
  })
})
