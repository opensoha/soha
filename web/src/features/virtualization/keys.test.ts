import { describe, expect, it } from 'vitest'
import {
  normalizeVirtualizationImageListParams,
  normalizeVirtualizationListParams,
  normalizeVirtualizationOperationParams,
  virtualizationKeys,
  virtualizationMutationKeys,
} from './keys'

describe('virtualizationKeys', () => {
  it('builds hierarchical VM keys with normalized list and runtime parameters', () => {
    expect(
      virtualizationKeys.vmList({
        status: '',
        provider: 'pve',
        pageSize: 20,
        page: 2,
      }),
    ).toEqual(['virtualization', 'vms', 'list', { page: 2, pageSize: 20, provider: 'pve' }])
    expect(virtualizationKeys.vmDetail(' vm-1 ')).toEqual([
      'virtualization',
      'vms',
      'detail',
      'vm-1',
    ])
    expect(virtualizationKeys.vmMetrics('vm-1')).toEqual([
      'virtualization',
      'vms',
      'detail',
      'vm-1',
      'metrics',
      { rangeMinutes: 60, stepSeconds: 60 },
    ])
    expect(virtualizationKeys.vmConsole('vm-1')).toEqual([
      'virtualization',
      'vms',
      'detail',
      'vm-1',
      'console',
    ])
  })

  it('canonicalizes operation filters without mutating caller input', () => {
    const statuses = ['running', 'failed', 'running']
    const params = { statuses, abnormal: false, pending: true, search: '' }

    expect(normalizeVirtualizationOperationParams(params)).toEqual({
      pending: true,
      statuses: ['failed', 'running'],
    })
    expect(statuses).toEqual(['running', 'failed', 'running'])
    expect(virtualizationKeys.operationList(params)).toEqual([
      'virtualization',
      'operations',
      'list',
      { pending: true, statuses: ['failed', 'running'] },
    ])
  })

  it('provides resource prefixes for exact invalidation and stable mutation keys', () => {
    expect(virtualizationKeys.clusterList()).toEqual(['virtualization', 'clusters', 'list'])
    expect(virtualizationKeys.imageOptions()).toEqual([
      'virtualization',
      'images',
      'list',
      'options',
    ])
    expect(virtualizationKeys.operationLogs('op-1')).toEqual([
      'virtualization',
      'operations',
      'detail',
      'op-1',
      'logs',
    ])
    expect(virtualizationMutationKeys.cluster('sync-many')).toEqual([
      'virtualization',
      'mutation',
      'cluster',
      'sync-many',
    ])
  })

  it('drops only parameters that the API query serializer also omits', () => {
    expect(
      normalizeVirtualizationListParams({
        search: '',
        connectionId: '',
        status: 'running',
        page: 0,
      }),
    ).toEqual({ page: 0, status: 'running' })
    expect(normalizeVirtualizationImageListParams({ search: '', category: 'storage' })).toEqual({
      category: 'storage',
    })
  })
})
