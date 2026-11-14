import { beforeEach, describe, expect, it, vi } from 'vitest'

const apiMocks = vi.hoisted(() => ({
  getEnvelope: vi.fn(),
  post: vi.fn(),
  postWithHeaders: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

import { computeApi } from './api'

describe('compute api', () => {
  beforeEach(() => {
    apiMocks.getEnvelope.mockReset()
    apiMocks.post.mockReset()
    apiMocks.postWithHeaders.mockReset()
  })

  it('uses canonical contract endpoints and omits empty filters', async () => {
    apiMocks.getEnvelope.mockResolvedValue({ items: [] })

    await computeApi.overview()
    await computeApi.tasks({
      domain: 'container_runtime',
      status: 'failed',
      category: 'operation',
      resourceKind: 'project',
      resourceId: 'project-1',
      sortBy: 'kind',
      sortOrder: 'asc',
    })

    expect(apiMocks.getEnvelope).toHaveBeenNthCalledWith(1, '/compute/overview')
    expect(apiMocks.getEnvelope).toHaveBeenNthCalledWith(
      2,
      '/compute/tasks?domain=container_runtime&status=failed&category=operation&resourceKind=project&resourceId=project-1&sortBy=kind&sortOrder=asc',
    )
  })

  it('uses the unified task detail, log, cancel, and retry endpoints', async () => {
    apiMocks.getEnvelope.mockResolvedValue({ data: {} })
    apiMocks.postWithHeaders.mockResolvedValue({ data: {} })

    await computeApi.task('virtualization', 'task/one')
    await computeApi.taskLogs('virtualization', 'task/one')
    await computeApi.cancelTask('virtualization', 'task/one')
    await computeApi.retryTask('container_runtime', 'task/two')

    expect(apiMocks.getEnvelope).toHaveBeenNthCalledWith(
      1,
      '/compute/tasks/virtualization/task%2Fone',
    )
    expect(apiMocks.getEnvelope).toHaveBeenNthCalledWith(
      2,
      '/compute/tasks/virtualization/task%2Fone/logs',
    )
    expect(apiMocks.postWithHeaders).toHaveBeenNthCalledWith(
      1,
      '/compute/tasks/virtualization/task%2Fone/cancel',
      {},
      expect.objectContaining({ 'Idempotency-Key': expect.stringMatching(/^compute-cancel-/) }),
    )
    expect(apiMocks.postWithHeaders).toHaveBeenNthCalledWith(
      2,
      '/compute/tasks/container_runtime/task%2Ftwo/retry',
      {},
      expect.objectContaining({ 'Idempotency-Key': expect.stringMatching(/^compute-retry-/) }),
    )
  })

  it('forwards opaque relation cursors without changing resource identity', async () => {
    apiMocks.getEnvelope.mockResolvedValue({ data: {} })
    await computeApi.resourceRelations('container_runtime', 'runtime_host', 'host/one', {
      cursor: 'next+/=',
      limit: 30,
    })
    expect(apiMocks.getEnvelope).toHaveBeenCalledWith(
      '/compute/resources/container_runtime/runtime_host/host%2Fone/relations?cursor=next%2B%2F%3D&limit=30',
    )
  })

  it('uses typed provider, relation, and action endpoints', async () => {
    apiMocks.getEnvelope.mockResolvedValue({ items: [] })
    apiMocks.postWithHeaders.mockResolvedValue({ data: {} })
    apiMocks.postWithHeaders.mockResolvedValueOnce({
      data: { healthy: true, status: 'healthy', checkedAt: '2026-09-22T00:00:00Z' },
    })

    await computeApi.providerInstances({ domain: 'virtualization', providerKey: 'pve', limit: 15 })
    await computeApi.resourceRelations('container_runtime', 'runtime_host', 'host/one')
    await computeApi.checkProviderHealth('virtualization', 'pve', 'connection/one', {
      expectedGeneration: 1,
    })
    await computeApi.executeResourceAction('virtualization', 'vm', 'vm/one', 'restart', {
      reason: 'operator request',
    })

    expect(apiMocks.getEnvelope).toHaveBeenNthCalledWith(
      1,
      '/compute/provider-instances?domain=virtualization&providerKey=pve&limit=15',
    )
    expect(apiMocks.getEnvelope).toHaveBeenNthCalledWith(
      2,
      '/compute/resources/container_runtime/runtime_host/host%2Fone/relations',
    )
    expect(apiMocks.postWithHeaders).toHaveBeenNthCalledWith(
      1,
      '/compute/provider-instances/virtualization/pve/connection%2Fone/health-checks',
      { expectedGeneration: 1 },
      expect.objectContaining({
        'Idempotency-Key': expect.stringMatching(/^compute-provider-health-/),
      }),
    )
    expect(apiMocks.postWithHeaders).toHaveBeenNthCalledWith(
      2,
      '/compute/resources/virtualization/vm/vm%2Fone/actions/restart',
      { reason: 'operator request' },
      expect.objectContaining({
        'Idempotency-Key': expect.stringMatching(/^compute-resource-action-/),
      }),
    )
  })

  it('rejects legacy asynchronous health responses', async () => {
    apiMocks.postWithHeaders.mockResolvedValue({ data: { id: 'old-task', status: 'queued' } })
    await expect(
      computeApi.checkProviderHealth('virtualization', 'pve', 'connection-1', {
        expectedGeneration: 1,
      }),
    ).rejects.toThrow('server upgrade')
  })
})
