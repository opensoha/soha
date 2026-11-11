import { MutationObserver, QueryClient, type MutationOptions } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { workloadKeys } from '@/features/platform/workloads/shared/keys'
import { deploymentMutations } from './mutations'

const apiMocks = vi.hoisted(() => ({
  deleteDeployment: vi.fn(),
  restartDeployment: vi.fn(),
  rollbackDeployment: vi.fn(),
  scaleDeployment: vi.fn(),
}))

vi.mock('./api', () => apiMocks)

const target = {
  scope: { clusterId: 'cluster-a', namespace: 'team-a' },
  name: 'api',
}

function queryClientWithInvalidationSpy() {
  const queryClient = new QueryClient()
  const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
  return { invalidate, queryClient }
}

async function expectDeploymentInvalidation<TVariables>(
  createOptions: (queryClient: QueryClient) => MutationOptions<void, Error, TVariables>,
  variables: TVariables,
) {
  const { invalidate, queryClient } = queryClientWithInvalidationSpy()
  const observer = new MutationObserver(queryClient, createOptions(queryClient))

  await expect(observer.mutate(variables)).resolves.toBeUndefined()
  expect(invalidate).toHaveBeenCalledTimes(3)
  expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('deployments') })
  expect(invalidate).toHaveBeenCalledWith({
    queryKey: workloadKeys.detail('deployments', target.scope, target.name),
  })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: workloadKeys.lists('pods') })
}

describe('deployment mutation options', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('invalidates deployment and pod dependencies after every action', async () => {
    apiMocks.restartDeployment.mockResolvedValueOnce(undefined)
    apiMocks.scaleDeployment.mockResolvedValueOnce(undefined)
    apiMocks.rollbackDeployment.mockResolvedValueOnce(undefined)
    apiMocks.deleteDeployment.mockResolvedValueOnce(undefined)

    await expectDeploymentInvalidation(deploymentMutations.restart, target)
    await expectDeploymentInvalidation(deploymentMutations.scale, { ...target, replicas: 3 })
    await expectDeploymentInvalidation(deploymentMutations.rollback, { ...target, revision: '7' })
    await expectDeploymentInvalidation(deploymentMutations.remove, target)
  })

  it('does not invalidate caches when the transport fails', async () => {
    const failure = new Error('restart failed')
    apiMocks.restartDeployment.mockRejectedValueOnce(failure)
    const { invalidate, queryClient } = queryClientWithInvalidationSpy()
    const observer = new MutationObserver(queryClient, deploymentMutations.restart(queryClient))

    await expect(observer.mutate(target)).rejects.toBe(failure)
    expect(invalidate).not.toHaveBeenCalled()
  })
})
