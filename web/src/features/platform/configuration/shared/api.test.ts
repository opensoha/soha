import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createConfigurationResource,
  deleteConfigurationResource,
  getConfigurationDetail,
  getConfigurationYAML,
  listConfigurationReferences,
  listConfigurationResources,
  updateConfigurationData,
  updateConfigurationYAML,
} from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const scope = { clusterId: 'cluster-a', namespace: 'team/a' }
const target = { scope, name: 'app/config' }

describe('configuration api', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('unwraps list, detail, references, and YAML envelopes', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: [{ name: 'app/config' }] })
      .mockResolvedValueOnce({ data: { name: 'app/config', immutable: false } })
      .mockResolvedValueOnce({ data: [{ kind: 'Deployment', name: 'api' }] })
      .mockResolvedValueOnce({ data: { content: 'kind: ConfigMap' } })

    await expect(listConfigurationResources('configmaps', scope)).resolves.toEqual([
      { name: 'app/config' },
    ])
    await expect(getConfigurationDetail('configmaps', target)).resolves.toMatchObject({
      name: 'app/config',
    })
    await expect(listConfigurationReferences('configmaps', target)).resolves.toHaveLength(1)
    await expect(getConfigurationYAML('configmaps', target)).resolves.toMatchObject({
      content: 'kind: ConfigMap',
    })
  })

  it('preserves create, update data, update YAML, and delete wire payloads', async () => {
    apiMocks.post.mockResolvedValueOnce({ data: { content: 'kind: ConfigMap' } })
    apiMocks.put
      .mockResolvedValueOnce({ data: { name: 'app/config', data: { key: 'next' } } })
      .mockResolvedValueOnce({ data: { content: 'kind: ConfigMap\ndata: {}' } })
    apiMocks.delete.mockResolvedValueOnce({ data: null })

    await createConfigurationResource('configmaps', { scope, content: 'kind: ConfigMap' })
    await updateConfigurationData('configmaps', target, {
      data: { key: 'next' },
      binaryData: {},
    })
    await updateConfigurationYAML('configmaps', { ...target, content: 'kind: ConfigMap\ndata: {}' })
    await deleteConfigurationResource('configmaps', target)

    expect(apiMocks.post).toHaveBeenCalledWith(
      '/clusters/cluster-a/configuration/configmaps?namespace=team%2Fa',
      { content: 'kind: ConfigMap', namespace: 'team/a' },
    )
    expect(apiMocks.put).toHaveBeenNthCalledWith(
      1,
      '/clusters/cluster-a/configuration/configmaps/app%2Fconfig/data?namespace=team%2Fa',
      { data: { key: 'next' }, binaryData: {} },
    )
    expect(apiMocks.put).toHaveBeenNthCalledWith(
      2,
      '/clusters/cluster-a/configuration/configmaps/app%2Fconfig/yaml?namespace=team%2Fa',
      { content: 'kind: ConfigMap\ndata: {}' },
    )
    expect(apiMocks.delete).toHaveBeenCalledWith(
      '/clusters/cluster-a/configuration/configmaps/app%2Fconfig?namespace=team%2Fa',
    )
  })
})
