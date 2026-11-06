import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  deletePod,
  getPodDetail,
  getPodMetrics,
  getPodContainerConfiguration,
  listPodEvents,
  listPods,
} from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const scope = { clusterId: 'cluster-a', namespace: 'team/a' }
const target = { scope, name: 'api/pod-1' }

describe('pod api', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('uses canonical scoped paths for list, detail, and metrics', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: [{ name: 'api/pod-1' }] })
      .mockResolvedValueOnce({ data: { name: 'api/pod-1', namespace: 'team/a' } })
      .mockResolvedValueOnce({ data: { resourceKind: 'Pod', rangeMinutes: 15 } })

    await expect(listPods(scope)).resolves.toEqual([{ name: 'api/pod-1' }])
    await expect(getPodDetail(target)).resolves.toMatchObject({ name: 'api/pod-1' })
    await expect(getPodMetrics(target, 15)).resolves.toMatchObject({ rangeMinutes: 15 })

    expect(apiMocks.get.mock.calls[0][0]).toBe(
      '/clusters/cluster-a/workloads/pods?namespace=team%2Fa',
    )
    expect(apiMocks.get.mock.calls[1][0]).toContain('/pods/api%2Fpod-1/detail?namespace=team%2Fa')
    expect(apiMocks.get.mock.calls[2][0]).toContain(
      '/pods/api%2Fpod-1/metrics?namespace=team%2Fa&rangeMinutes=15',
    )
  })

  it('filters shared events to the requested pod', async () => {
    apiMocks.get.mockResolvedValueOnce({
      data: [
        { involvedKind: 'Pod', involvedName: 'api/pod-1' },
        { involvedKind: 'Deployment', involvedName: 'api/pod-1' },
        { involvedKind: 'Pod', involvedName: 'other' },
      ],
    })

    await expect(listPodEvents(target)).resolves.toEqual([
      { involvedKind: 'Pod', involvedName: 'api/pod-1' },
    ])
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/clusters/cluster-a/events?namespace=team%2Fa&limit=100',
    )
  })

  it('reads configuration only from the scoped Pod YAML without resolving Secret references', async () => {
    apiMocks.get.mockResolvedValueOnce({
      data: {
        content: JSON.stringify({
          kind: 'Pod',
          metadata: { name: target.name, namespace: scope.namespace },
          spec: {
            containers: [
              {
                name: 'app',
                env: [
                  {
                    name: 'PASSWORD',
                    valueFrom: { secretKeyRef: { name: 'app-secret', key: 'password' } },
                  },
                ],
              },
            ],
          },
        }),
      },
    })
    const result = await getPodContainerConfiguration(target)
    expect(result.containers[0].env[0].source).toMatchObject({
      kind: 'Secret',
      name: 'app-secret',
      key: 'password',
    })
    expect(apiMocks.get).toHaveBeenCalledOnce()
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/clusters/cluster-a/workloads/pods/api%2Fpod-1/yaml?namespace=team%2Fa',
    )
  })

  it('deletes the record namespace and encodes the pod name', async () => {
    apiMocks.delete.mockResolvedValueOnce({ data: null })

    await expect(deletePod(target)).resolves.toBeUndefined()
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster-a/workloads/pods/api%2Fpod-1?namespace=team%2Fa',
    )
  })

  it('rejects deletion without a record namespace', async () => {
    await expect(
      deletePod({ scope: { clusterId: 'cluster-a', namespace: null }, name: 'api-pod' }),
    ).rejects.toThrow('A namespace is required')
    expect(apiMocks.delete).not.toHaveBeenCalled()
  })

  it('includes the expected UID in a guarded delete', async () => {
    apiMocks.delete.mockResolvedValueOnce({ data: null })
    await deletePod({ ...target, uid: 'pod/uid' })
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster-a/workloads/pods/api%2Fpod-1?namespace=team%2Fa&expectedUid=pod%2Fuid',
    )
  })
})
