import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createAccessControlResource,
  deleteAccessControlResource,
  getAccessControlDetail,
  getAccessControlYAML,
  listAccessControlResources,
  updateAccessControlYAML,
} from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const namespacedScope = { clusterId: 'cluster-a', namespace: 'team/a' }
const clusterScope = { clusterId: 'cluster-a', namespace: null }

describe('access-control API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('unwraps list, detail, and YAML responses', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: [{ name: 'reader' }] })
      .mockResolvedValueOnce({ data: { name: 'reader', rules: 1 } })
      .mockResolvedValueOnce({ data: { content: 'kind: Role' } })

    await expect(listAccessControlResources('roles', namespacedScope)).resolves.toEqual([
      { name: 'reader' },
    ])
    await expect(
      getAccessControlDetail('roles', { scope: namespacedScope, name: 'reader' }),
    ).resolves.toMatchObject({ name: 'reader' })
    await expect(
      getAccessControlYAML('roles', { scope: namespacedScope, name: 'reader' }),
    ).resolves.toMatchObject({ content: 'kind: Role' })
  })

  it('preserves namespaced and cluster-scoped create, update, and delete wires', async () => {
    apiMocks.post.mockResolvedValue({ data: { content: '' } })
    apiMocks.put.mockResolvedValue({ data: { content: 'kind: ClusterRole' } })
    apiMocks.delete.mockResolvedValue({ data: null })

    await createAccessControlResource('roles', {
      scope: namespacedScope,
      content: 'kind: Role',
    })
    await createAccessControlResource('clusterroles', {
      scope: clusterScope,
      content: 'kind: ClusterRole',
    })
    await updateAccessControlYAML('clusterroles', {
      scope: clusterScope,
      name: 'viewer/all',
      content: 'kind: ClusterRole',
    })
    await deleteAccessControlResource('roles', {
      scope: namespacedScope,
      name: 'reader/all',
    })

    expect(apiMocks.post).toHaveBeenNthCalledWith(
      1,
      '/clusters/cluster-a/access-control/roles?namespace=team%2Fa',
      { content: 'kind: Role', namespace: 'team/a' },
    )
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      2,
      '/clusters/cluster-a/access-control/clusterroles',
      { content: 'kind: ClusterRole' },
    )
    expect(apiMocks.put).toHaveBeenCalledWith(
      '/clusters/cluster-a/access-control/clusterroles/viewer%2Fall/yaml',
      { content: 'kind: ClusterRole' },
    )
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster-a/access-control/roles/reader%2Fall?namespace=team%2Fa',
    )
  })

  it('passes ServiceAccount subject filters through list requests', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: [{ name: 'builders', roleRef: 'Role/edit' }] })
    await expect(
      listAccessControlResources('rolebindings', namespacedScope, {
        subjectKind: ' ServiceAccount ',
        subjectName: ' build/bot ',
        subjectNamespace: ' team/a ',
      }),
    ).resolves.toHaveLength(1)
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/clusters/cluster-a/access-control/rolebindings?namespace=team%2Fa&subjectKind=ServiceAccount&subjectName=build%2Fbot&subjectNamespace=team%2Fa',
    )
  })
})
