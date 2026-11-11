import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createStorageResource,
  deleteStorageResource,
  getStorageDetail,
  getStorageYAML,
  listStorageResources,
  updateStorageYAML,
} from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))
vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const scope = { clusterId: 'cluster/a', namespace: 'team/a' }
const target = { scope, name: 'data/claim' }

describe('storage api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps list, detail, and YAML responses with encoded names and scope', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: [{ name: 'data/claim' }] })
      .mockResolvedValueOnce({ data: { name: 'data/claim', status: 'Bound' } })
      .mockResolvedValueOnce({ data: { name: 'data/claim', content: 'kind: PVC' } })

    await expect(listStorageResources('persistentvolumeclaims', scope)).resolves.toEqual([
      { name: 'data/claim' },
    ])
    await expect(getStorageDetail('persistentvolumeclaims', target)).resolves.toMatchObject({
      status: 'Bound',
    })
    await expect(getStorageYAML('persistentvolumeclaims', target)).resolves.toMatchObject({
      content: 'kind: PVC',
    })
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      1,
      '/clusters/cluster/a/storage/persistentvolumeclaims?namespace=team%2Fa',
    )
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      2,
      '/clusters/cluster/a/storage/persistentvolumeclaims/data%2Fclaim/detail?namespace=team%2Fa',
    )
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      3,
      '/clusters/cluster/a/storage/persistentvolumeclaims/data%2Fclaim/yaml?namespace=team%2Fa',
    )
  })

  it('preserves create and YAML update wire bodies', async () => {
    apiMocks.post.mockResolvedValueOnce({ data: { content: 'created' } })
    apiMocks.put.mockResolvedValueOnce({ data: { content: 'updated' } })

    await expect(
      createStorageResource('persistentvolumeclaims', { scope, content: 'kind: PVC' }),
    ).resolves.toMatchObject({ content: 'created' })
    await expect(
      updateStorageYAML('persistentvolumeclaims', { ...target, content: 'kind: PVC v2' }),
    ).resolves.toMatchObject({ content: 'updated' })
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/clusters/cluster/a/storage/persistentvolumeclaims?namespace=team%2Fa',
      { content: 'kind: PVC', namespace: 'team/a' },
    )
    expect(apiMocks.put).toHaveBeenCalledWith(
      '/clusters/cluster/a/storage/persistentvolumeclaims/data%2Fclaim/yaml?namespace=team%2Fa',
      { content: 'kind: PVC v2' },
    )
  })

  it('keeps cluster-scoped resources free of namespace query and returns void on delete', async () => {
    apiMocks.delete.mockResolvedValueOnce({ data: { deleted: true } })
    const clusterScope = { clusterId: 'cluster-a', namespace: null }

    await expect(
      deleteStorageResource('persistentvolumes', { scope: clusterScope, name: 'pv-a' }),
    ).resolves.toBeUndefined()
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster-a/storage/persistentvolumes/pv-a',
    )
  })
})
