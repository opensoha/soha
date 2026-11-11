import { beforeEach, describe, expect, it, vi } from 'vitest'
import { storageKeys } from './keys'
import { storageDetailQuery, storageListQuery, storageYAMLQuery } from './queries'

const apiMocks = vi.hoisted(() => ({
  getStorageDetail: vi.fn(),
  getStorageYAML: vi.fn(),
  listStorageResources: vi.fn(),
}))
vi.mock('./api', () => apiMocks)

describe('storage query options', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses canonical scope keys and list guards', () => {
    const scope = { clusterId: 'cluster-a', namespace: 'team-a' }
    expect(storageListQuery('persistentvolumeclaims', scope).queryKey).toEqual(
      storageKeys.list('persistentvolumeclaims', scope),
    )
    expect(
      storageListQuery('persistentvolumeclaims', { clusterId: null, namespace: null }).enabled,
    ).toBe(false)
  })

  it('requires namespace only for namespaced detail and YAML resources', () => {
    const clusterScope = { clusterId: 'cluster-a', namespace: null }
    expect(storageDetailQuery('persistentvolumeclaims', clusterScope, 'claim', true).enabled).toBe(
      false,
    )
    expect(storageYAMLQuery('persistentvolumeclaims', clusterScope, 'claim', true).enabled).toBe(
      false,
    )
    expect(storageDetailQuery('persistentvolumes', clusterScope, 'pv', false).enabled).toBe(true)
  })
})
