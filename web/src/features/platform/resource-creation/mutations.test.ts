import { describe, expect, it, vi } from 'vitest'
import { invalidateCreatedResourceQueries } from './mutations'

describe('resource creation invalidation', () => {
  it('refreshes only affected resource families and deduplicates namespaces', async () => {
    const invalidateQueries = vi.fn(async () => undefined)
    const queryClient = { invalidateQueries } as never
    await invalidateCreatedResourceQueries(queryClient, [
      {
        clusterId: 'c1',
        apiVersion: 'v1',
        kind: 'ConfigMap',
        name: 'a',
        namespace: 'minio',
        scopeMode: 'namespace',
      },
      {
        clusterId: 'c1',
        apiVersion: 'v1',
        kind: 'ConfigMap',
        name: 'b',
        namespace: 'ops',
        scopeMode: 'namespace',
      },
      {
        clusterId: 'c1',
        apiVersion: 'v1',
        kind: 'Service',
        name: 'api',
        namespace: 'ops',
        scopeMode: 'namespace',
      },
    ])

    expect(invalidateQueries).toHaveBeenCalledTimes(2)
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['platform', 'configuration', 'configmaps'],
    })
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['platform', 'network', 'services'],
    })
  })
})
