import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  getWorkloadDetail,
  getWorkloadMetrics,
  getWorkloadYAML,
  listWorkloadEvents,
  listWorkloads,
  updateWorkloadYAML,
} from './api'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const scope = { clusterId: 'cluster-a', namespace: 'team/a' }

describe('workload shared api', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('unwraps list data and tolerates an empty envelope', async () => {
    apiMocks.get.mockResolvedValueOnce({})

    await expect(listWorkloads('pods', scope)).resolves.toEqual([])
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/clusters/cluster-a/workloads/pods?namespace=team%2Fa',
    )
  })

  it('unwraps detail, yaml, metrics, and event domain values', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: { name: 'api/server' } })
      .mockResolvedValueOnce({ data: { kind: 'Deployment', content: 'yaml' } })
      .mockResolvedValueOnce({ data: { resourceKind: 'Deployment' } })
      .mockResolvedValueOnce({ data: [{ name: 'event-a' }] })

    await expect(getWorkloadDetail('deployments', scope, 'api/server')).resolves.toEqual({
      name: 'api/server',
    })
    await expect(getWorkloadYAML('deployments', scope, 'api/server')).resolves.toMatchObject({
      content: 'yaml',
    })
    await expect(getWorkloadMetrics('deployments', scope, 'api/server')).resolves.toMatchObject({
      resourceKind: 'Deployment',
    })
    await expect(listWorkloadEvents(scope, 100)).resolves.toEqual([{ name: 'event-a' }])

    expect(apiMocks.get.mock.calls[0][0]).toContain('/api%2Fserver/detail?')
    expect(apiMocks.get.mock.calls[1][0]).toContain('/api%2Fserver/yaml?')
    expect(apiMocks.get.mock.calls[2][0]).toContain('/api%2Fserver/metrics?')
    expect(apiMocks.get.mock.calls[3][0]).toContain('/events?namespace=team%2Fa&limit=100')
  })

  it('passes YAML input through and returns the updated domain value', async () => {
    apiMocks.put.mockResolvedValueOnce({ data: { kind: 'Deployment', content: 'updated' } })

    await expect(
      updateWorkloadYAML('deployments', scope, 'api/server', { content: 'updated' }),
    ).resolves.toMatchObject({ content: 'updated' })
    expect(apiMocks.put).toHaveBeenCalledWith(expect.stringContaining('/api%2Fserver/yaml?'), {
      content: 'updated',
    })
  })
})
