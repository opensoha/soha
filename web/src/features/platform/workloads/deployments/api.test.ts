import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  deleteDeployment,
  getDeploymentDetail,
  getDeploymentMetrics,
  getDeploymentRolloutStatus,
  listDeploymentEvents,
  listDeploymentRollouts,
  listDeployments,
  restartDeployment,
  rollbackDeployment,
  scaleDeployment,
} from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const scope = { clusterId: 'cluster-a', namespace: 'team/a' }
const target = { scope, name: 'api/server' }

describe('deployment api', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('unwraps list and detail endpoints with encoded resource names', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: [{ name: 'api/server' }] })
      .mockResolvedValueOnce({
        data: { name: 'api/server', namespace: 'team/a', pods: [{ name: 'api-1' }] },
      })
      .mockResolvedValueOnce({ data: { resourceKind: 'Deployment' } })

    await expect(listDeployments(scope)).resolves.toEqual([{ name: 'api/server' }])
    await expect(getDeploymentDetail(target)).resolves.toMatchObject({
      name: 'api/server',
      pods: [{ name: 'api-1' }],
    })
    await expect(getDeploymentMetrics(target)).resolves.toMatchObject({
      resourceKind: 'Deployment',
    })

    expect(apiMocks.get.mock.calls[0][0]).toBe(
      '/clusters/cluster-a/workloads/deployments?namespace=team%2Fa',
    )
    expect(apiMocks.get.mock.calls[1][0]).toContain('/deployments/api%2Fserver/detail?')
    expect(apiMocks.get.mock.calls[2][0]).toContain('/deployments/api%2Fserver/metrics?')
  })

  it('unwraps rollout endpoints and tolerates an empty rollout envelope', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: { status: 'progressing' } })
      .mockResolvedValueOnce({})

    await expect(getDeploymentRolloutStatus(target)).resolves.toMatchObject({
      status: 'progressing',
    })
    await expect(listDeploymentRollouts(target)).resolves.toEqual([])
    expect(apiMocks.get.mock.calls[0][0]).toContain('/api%2Fserver/rollout-status?')
    expect(apiMocks.get.mock.calls[1][0]).toContain('/api%2Fserver/rollouts?')
  })

  it('filters shared events without loading a Pod list', async () => {
    apiMocks.get.mockResolvedValueOnce({
      data: [
        { involvedKind: 'Deployment', involvedName: 'api/server' },
        { involvedKind: 'Pod', involvedName: 'api/server' },
      ],
    })

    await expect(listDeploymentEvents(target)).resolves.toEqual([
      { involvedKind: 'Deployment', involvedName: 'api/server' },
    ])
    expect(apiMocks.get.mock.calls[0][0]).toContain('/events?namespace=team%2Fa&limit=100')
    expect(apiMocks.get).toHaveBeenCalledTimes(1)
  })

  it('keeps mutation transport responses out of the domain return type', async () => {
    apiMocks.post.mockResolvedValue({ data: { message: 'accepted' } })
    apiMocks.delete.mockResolvedValue({ data: { status: 'deleted' } })

    await expect(restartDeployment(target)).resolves.toBeUndefined()
    await expect(scaleDeployment({ ...target, replicas: 3 })).resolves.toBeUndefined()
    await expect(rollbackDeployment({ ...target, revision: ' 7 ' })).resolves.toBeUndefined()
    await expect(deleteDeployment(target)).resolves.toBeUndefined()

    expect(apiMocks.post).toHaveBeenNthCalledWith(
      1,
      '/clusters/cluster-a/workloads/deployments/restart',
      { namespace: 'team/a', name: 'api/server' },
    )
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      2,
      '/clusters/cluster-a/workloads/deployments/scale',
      { namespace: 'team/a', name: 'api/server', replicas: 3 },
    )
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      3,
      '/clusters/cluster-a/workloads/deployments/rollback',
      { namespace: 'team/a', name: 'api/server', revision: '7' },
    )
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster-a/workloads/deployments/api%2Fserver?namespace=team%2Fa',
    )
  })

  it('requires a namespace for deployment actions', async () => {
    await expect(
      restartDeployment({ scope: { clusterId: 'cluster-a', namespace: null }, name: 'api' }),
    ).rejects.toThrow('A namespace is required')
    expect(apiMocks.post).not.toHaveBeenCalled()
  })

  it('includes the expected UID in a guarded delete', async () => {
    apiMocks.delete.mockResolvedValueOnce({ data: null })
    await deleteDeployment({ ...target, uid: 'deployment/uid' })
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster-a/workloads/deployments/api%2Fserver?namespace=team%2Fa&expectedUid=deployment%2Fuid',
    )
  })
})
