import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { workloadKeys } from '../shared/keys'
import { POD_BATCH_DELETE_CONCURRENCY, deletePodsWithConcurrency, podMutations } from './mutations'

const apiMocks = vi.hoisted(() => ({
  deletePod: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const targets = Array.from({ length: 20 }, (_, index) => ({
  scope: { clusterId: 'cluster-a', namespace: `team-${index}` },
  name: `pod-${index}`,
}))

describe('pod mutation options', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('caps batch deletion at eight and preserves every record scope', async () => {
    let active = 0
    let maxActive = 0
    apiMocks.deletePod.mockImplementation(async () => {
      active += 1
      maxActive = Math.max(maxActive, active)
      await new Promise((resolve) => setTimeout(resolve, 1))
      active -= 1
    })

    const results = await deletePodsWithConcurrency({ targets })

    expect(POD_BATCH_DELETE_CONCURRENCY).toBe(8)
    expect(maxActive).toBe(8)
    expect(results).toHaveLength(targets.length)
    expect(results.every((item) => item.status === 'fulfilled')).toBe(true)
    expect(apiMocks.deletePod.mock.calls.map(([target]) => target.scope.namespace)).toEqual(
      targets.map((target) => target.scope.namespace),
    )
  })

  it('settles failures without cancelling remaining pod deletes', async () => {
    apiMocks.deletePod.mockImplementation(async (target) => {
      if (target.name === 'pod-3') throw new Error('delete failed')
    })

    const results = await deletePodsWithConcurrency({ targets: targets.slice(0, 5) })

    expect(results.map((item) => item.status)).toEqual([
      'fulfilled',
      'fulfilled',
      'fulfilled',
      'rejected',
      'fulfilled',
    ])
  })

  it('invalidates list and record detail after a single delete', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    apiMocks.deletePod.mockResolvedValueOnce(undefined)
    const observer = new MutationObserver(queryClient, podMutations.remove(queryClient))

    await expect(observer.mutate(targets[0])).resolves.toBeUndefined()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('pods') })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: workloadKeys.detail('pods', targets[0].scope, targets[0].name),
    })
  })
})
