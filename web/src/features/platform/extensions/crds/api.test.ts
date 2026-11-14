import { describe, expect, it, vi } from 'vitest'
import { deleteCustomResource } from './api'

const remove = vi.hoisted(() => vi.fn().mockResolvedValue(undefined))
vi.mock('@/services/api-client', () => ({ api: { delete: remove } }))

describe('custom resource deletion', () => {
  it('preserves namespace and version while fencing the observed UID', async () => {
    await deleteCustomResource({
      clusterId: 'cluster-a',
      namespace: 'apps',
      resourceName: 'daily',
      expectedUid: 'root/uid',
      crd: {
        name: 'workloadcronjobs.workloads.soha.io',
        group: 'workloads.soha.io',
        kind: 'WorkloadCronJob',
        plural: 'workloadcronjobs',
        version: 'v1alpha1',
        scope: 'Namespaced',
      },
    })
    const url = new URL(remove.mock.calls[0][0], 'http://localhost')
    expect(url.searchParams.get('namespace')).toBe('apps')
    expect(url.searchParams.get('version')).toBe('v1alpha1')
    expect(url.searchParams.get('expectedUid')).toBe('root/uid')
  })
})
