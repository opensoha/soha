import { MutationObserver, QueryClient } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { deliveryApi } from './api'
import { deliveryKeys, deliveryMutationKeys } from './keys'
import {
  deliveryMutations,
  invalidateApplicationQueries,
  invalidateRuntimeQueries,
} from './mutations'

function queryClientWithInvalidationSpy() {
  const queryClient = new QueryClient()
  const invalidateQueries = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
  return { invalidateQueries, queryClient }
}

describe('deliveryMutations', () => {
  afterEach(() => vi.restoreAllMocks())

  it('invalidates application, environment, and release-board capabilities together', async () => {
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()

    await invalidateApplicationQueries(queryClient)

    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.applications.all })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.environments.all })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.releaseBoard.all })
  })

  it('maps typed application updates and invalidates cross-capability state', async () => {
    vi.spyOn(deliveryApi.applications, 'update').mockResolvedValue(undefined)
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(
      queryClient,
      deliveryMutations.applications.update(queryClient),
    )
    const payload = { name: 'Console' }

    await observer.mutate({ id: 'app-1', payload })

    expect(observer.options.mutationKey).toEqual(deliveryMutationKeys.applications('update'))
    expect(deliveryApi.applications.update).toHaveBeenCalledWith('app-1', payload)
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.applications.all })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.releaseBoard.all })
  })

  it('saves application workflows atomically and refreshes application state', async () => {
    vi.spyOn(deliveryApi.environments, 'saveWorkflow').mockResolvedValue({ id: 'binding-1' } as never)
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(
      queryClient,
      deliveryMutations.environments.saveWorkflow(queryClient),
    )
    const payload = { name: 'Release workflow', definition: { nodes: [] }, enabled: true }

    await observer.mutate({ applicationId: 'app-1', id: 'binding-1', payload })

    expect(observer.options.mutationKey).toEqual(
      deliveryMutationKeys.environments('save-workflow'),
    )
    expect(deliveryApi.environments.saveWorkflow).toHaveBeenCalledWith(
      'app-1',
      'binding-1',
      payload,
    )
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.applications.all })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.environments.all })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.releaseBoard.all })
  })

  it('invalidates all runtime consumers after workflow actions', async () => {
    vi.spyOn(deliveryApi.workflows, 'approve').mockResolvedValue(undefined)
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(
      queryClient,
      deliveryMutations.workflows.approve(queryClient),
    )

    await observer.mutate({ id: 'workflow-1', comment: 'Approved from console' })

    expect(deliveryApi.workflows.approve).toHaveBeenCalledWith({
      id: 'workflow-1',
      comment: 'Approved from console',
    })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.workflows.all })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.executionTasks.all })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.runtime.all })
  })

  it('does not invalidate failed mutations', async () => {
    const failure = new Error('retry failed')
    vi.spyOn(deliveryApi.executionTasks, 'retry').mockRejectedValue(failure)
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(
      queryClient,
      deliveryMutations.executionTasks.retry(queryClient),
    )

    await expect(observer.mutate({ id: 'task-1', reason: 'manual retry' })).rejects.toBe(failure)
    expect(invalidateQueries).not.toHaveBeenCalled()
  })

  it('exposes the complete runtime invalidation policy', async () => {
    const { invalidateQueries, queryClient } = queryClientWithInvalidationSpy()

    await invalidateRuntimeQueries(queryClient)

    expect(invalidateQueries).toHaveBeenCalledTimes(10)
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.batches.all })
    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: deliveryKeys.plans.all })
  })
})
