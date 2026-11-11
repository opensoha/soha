import { QueryClient, type MutationFunctionContext } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { workloadKeys } from './keys'
import { workloadMutations } from './mutations'

const apiMocks = vi.hoisted(() => ({ deleteWorkload: vi.fn(), updateWorkloadYAML: vi.fn() }))

vi.mock('./api', () => apiMocks)

describe('workload mutation options', () => {
  it('updates YAML and invalidates list, detail, YAML, and pod dependencies', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const target = {
      scope: { clusterId: 'cluster-a', namespace: 'team-a' },
      name: 'worker-a',
      content: 'kind: Deployment',
    }
    apiMocks.updateWorkloadYAML.mockResolvedValueOnce({ content: target.content })

    const options = workloadMutations.updateYAML('deployments', queryClient)
    const context = {} as MutationFunctionContext
    await options.mutationFn?.(target, context)
    await options.onSuccess?.({}, target, undefined, context)

    expect(apiMocks.updateWorkloadYAML).toHaveBeenCalledWith(
      'deployments',
      target.scope,
      target.name,
      { content: target.content },
    )
    expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('deployments') })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: workloadKeys.detail('deployments', target.scope, target.name),
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: workloadKeys.yaml('deployments', target.scope, target.name),
    })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('pods') })
  })

  it('deletes with the target scope and invalidates list and detail dependencies', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const target = {
      scope: { clusterId: 'cluster-a', namespace: 'team-a' },
      name: 'worker-a',
    }
    apiMocks.deleteWorkload.mockResolvedValueOnce(undefined)

    const options = workloadMutations.remove('replicasets', queryClient)
    const context = {} as MutationFunctionContext
    await options.mutationFn?.(target, context)
    await options.onSuccess?.(undefined, target, undefined, context)

    expect(apiMocks.deleteWorkload).toHaveBeenCalledWith('replicasets', target.scope, target.name)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('replicasets') })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: workloadKeys.detail('replicasets', target.scope, target.name),
    })
  })
})
