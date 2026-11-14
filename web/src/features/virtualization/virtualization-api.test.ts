import { beforeEach, describe, expect, it, vi } from 'vitest'
import { virtualizationApi } from './virtualization-api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  postWithHeaders: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('virtualizationApi', () => {
  beforeEach(() => vi.clearAllMocks())

  it('returns connection diagnostics and rejects legacy task responses', async () => {
    const result = {
      healthy: false,
      status: 'unavailable',
      checkedAt: '2026-09-22T00:00:00Z',
      message: 'Connection refused',
    }
    apiMocks.post.mockResolvedValueOnce({ data: result })
    await expect(virtualizationApi.testCluster('cluster-1')).resolves.toBe(result)
    apiMocks.post.mockResolvedValueOnce({ data: { id: 'old-task', status: 'queued' } })
    await expect(virtualizationApi.testCluster('cluster-1')).rejects.toThrow('server upgrade')
  })

  it('unwraps collection endpoints and tolerates empty envelopes', async () => {
    apiMocks.get
      .mockResolvedValueOnce({})
      .mockResolvedValueOnce({})
      .mockResolvedValueOnce({})
      .mockResolvedValueOnce({})
      .mockResolvedValueOnce({})
      .mockResolvedValueOnce({})

    await expect(virtualizationApi.platformClusters()).resolves.toEqual([])
    await expect(virtualizationApi.vms({ page: 2, pageSize: 25 })).resolves.toEqual([])
    await expect(virtualizationApi.clusters()).resolves.toEqual([])
    await expect(
      virtualizationApi.images({ provider: 'pve', category: 'storage' }),
    ).resolves.toEqual([])
    await expect(virtualizationApi.flavors()).resolves.toEqual([])
    await expect(virtualizationApi.operationLogs('op/1')).resolves.toEqual([])

    expect(apiMocks.get).toHaveBeenNthCalledWith(1, '/clusters')
    expect(apiMocks.get).toHaveBeenNthCalledWith(2, '/virtualization/vms?page=2&pageSize=25')
    expect(apiMocks.get).toHaveBeenNthCalledWith(3, '/virtualization/clusters')
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      4,
      '/virtualization/images?provider=pve&category=storage',
    )
    expect(apiMocks.get).toHaveBeenNthCalledWith(5, '/virtualization/flavors')
    expect(apiMocks.get).toHaveBeenNthCalledWith(6, '/virtualization/operations/op%2F1/logs')
  })

  it('returns detail, metrics and console domain values directly', async () => {
    const detail = { vm: { id: 'vm/1', name: 'build-vm' } }
    const metrics = { ready: true, series: [] }
    const consoleInfo = { ready: false, type: 'vnc', url: '', message: 'not available' }
    apiMocks.get
      .mockResolvedValueOnce({ data: detail })
      .mockResolvedValueOnce({ data: metrics })
      .mockResolvedValueOnce({ data: consoleInfo })

    await expect(virtualizationApi.vmDetail('vm/1')).resolves.toBe(detail)
    await expect(virtualizationApi.vmMetrics('vm/1', 15, 30)).resolves.toBe(metrics)
    await expect(virtualizationApi.vmConsoleURL('vm/1')).resolves.toBe(consoleInfo)

    expect(apiMocks.get).toHaveBeenNthCalledWith(1, '/virtualization/vms/vm%2F1/detail')
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      2,
      '/virtualization/vms/vm%2F1/metrics?rangeMinutes=15&stepSeconds=30',
    )
    expect(apiMocks.get).toHaveBeenNthCalledWith(3, '/virtualization/vms/vm%2F1/console')
  })

  it('returns mutation domain values and keeps delete results transport-free', async () => {
    const operation = { id: 'op-1', vmId: 'vm-1', status: 'queued' }
    apiMocks.post.mockResolvedValueOnce({ data: operation })
    apiMocks.delete.mockResolvedValueOnce({ data: { deleted: true } })

    await expect(virtualizationApi.powerVm('vm/1', 'restart')).resolves.toBe(operation)
    await expect(
      virtualizationApi.deleteCluster('cluster/1', { force: true }),
    ).resolves.toBeUndefined()

    expect(apiMocks.post).toHaveBeenCalledWith('/virtualization/vms/vm%2F1/power', {
      action: 'restart',
    })
    expect(apiMocks.delete).toHaveBeenCalledWith('/virtualization/clusters/cluster%2F1?force=true')
  })

  it('plans VM creation before executing it with an idempotency key', async () => {
    const input = { connectionId: 'conn-1', name: 'demo-vm' }
    const plan = {
      capability: 'virtualization.vms.create.trigger',
      target: 'demo-vm',
      ready: true,
      riskLevel: 'execute' as const,
      requiresApproval: true,
      changes: [{ action: 'create', resource: 'vm', summary: 'Create demo-vm' }],
      warnings: [],
    }
    const operation = { id: 'op-create', vmId: 'vm-1' }
    apiMocks.post.mockResolvedValueOnce({ data: plan })
    apiMocks.postWithHeaders.mockResolvedValueOnce({ data: operation })

    await expect(virtualizationApi.planVmCreate(input)).resolves.toBe(plan)
    await expect(virtualizationApi.createVm(input, 'idem-vm-1234')).resolves.toBe(operation)

    expect(apiMocks.post).toHaveBeenCalledWith('/virtualization/vms/plan', input)
    expect(apiMocks.postWithHeaders).toHaveBeenCalledWith('/virtualization/vms', input, {
      'Idempotency-Key': 'idem-vm-1234',
    })
  })

  it('normalizes operation filters into the wire query and returns the list', async () => {
    const operations = [{ id: 'op-1', status: 'failed' }]
    apiMocks.get.mockResolvedValueOnce({ data: operations })

    await expect(
      virtualizationApi.operations({
        taskKind: 'asset_sync',
        abnormal: true,
        statuses: ['failed', 'timeout'],
        connectionId: 'conn/1',
      }),
    ).resolves.toBe(operations)

    expect(apiMocks.get).toHaveBeenCalledWith(
      '/virtualization/operations?taskKind=asset_sync&abnormal=true&statuses=failed%2Ctimeout&connectionId=conn%2F1',
    )
  })
})
