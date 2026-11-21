import { beforeEach, describe, expect, it, vi } from 'vitest'
import { pluginApi } from './plugin-api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  getEnvelope: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('pluginApi', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps marketplace lists and serializes only normalized filters', async () => {
    const plugins = [{ id: 'plugin-1', name: 'Plugin 1' }]
    apiMocks.get.mockResolvedValue({ data: plugins })

    await expect(
      pluginApi.marketplace({
        query: ' agent ',
        type: 'skill',
        publisher: '',
        sourceId: ' catalog/a ',
      }),
    ).resolves.toBe(plugins)

    expect(apiMocks.get).toHaveBeenCalledWith(
      '/plugins/marketplace?q=agent&type=skill&sourceId=catalog%2Fa',
    )
  })

  it('retains source status from the canonical envelope and accepts legacy fixtures', async () => {
    const items = [{ id: 'plugin-1' }]
    const meta = {
      sources: [{ sourceId: 'remote', status: 'unavailable', code: 'source_unavailable' }],
    }
    apiMocks.getEnvelope
      .mockResolvedValueOnce({ items, meta })
      .mockResolvedValueOnce({ data: items })
    await expect(pluginApi.marketplaceWithMeta()).resolves.toEqual({ items, meta })
    await expect(pluginApi.marketplaceWithMeta()).resolves.toEqual({ items })
  })

  it('unwraps detail and extension responses while encoding path identifiers', async () => {
    const detail = { id: 'plugin/a', name: 'Plugin A' }
    const extensions = [{ id: 'ext-1', pluginId: 'plugin/a' }]
    apiMocks.get.mockResolvedValueOnce({ data: detail }).mockResolvedValueOnce({ data: extensions })

    await expect(
      pluginApi.marketplaceDetail('plugin/a', {
        marketplaceUrl: 'https://catalog.example/plugins',
        version: '1.2.0',
      }),
    ).resolves.toBe(detail)
    await expect(pluginApi.extensions('runtime/ui')).resolves.toBe(extensions)

    expect(apiMocks.get).toHaveBeenNthCalledWith(
      1,
      '/plugins/marketplace/plugin%2Fa?marketplaceUrl=https%3A%2F%2Fcatalog.example%2Fplugins&version=1.2.0',
    )
    expect(apiMocks.get).toHaveBeenNthCalledWith(2, '/extensions/runtime%2Fui')
  })

  it('unwraps mutation responses, including delete envelopes', async () => {
    const installed = { id: 'plugin/a', name: 'Plugin A' }
    apiMocks.post.mockResolvedValueOnce({ data: installed })
    apiMocks.put.mockResolvedValueOnce({ data: installed })
    apiMocks.delete.mockResolvedValueOnce({ data: { status: 'removed' } })

    await expect(pluginApi.enable('plugin/a')).resolves.toBe(installed)
    await expect(pluginApi.configure('plugin/a', { metadata: { region: 'cn' } })).resolves.toBe(
      installed,
    )
    await expect(pluginApi.remove('plugin/a')).resolves.toEqual({ status: 'removed' })

    expect(apiMocks.post).toHaveBeenCalledWith('/plugins/plugin%2Fa/enable')
    expect(apiMocks.put).toHaveBeenCalledWith('/plugins/plugin%2Fa/config', {
      metadata: { region: 'cn' },
    })
    expect(apiMocks.delete).toHaveBeenCalledWith('/plugins/plugin%2Fa')
  })

  it('uses the repository preview, install and same-source upgrade routes', async () => {
    const preview = { previewToken: 'token-1', manifest: { id: 'plugin/a' } }
    const installed = { id: 'plugin/a', name: 'Plugin A' }
    apiMocks.post
      .mockResolvedValueOnce({ data: preview })
      .mockResolvedValueOnce({ data: installed })
      .mockResolvedValueOnce({ data: installed })

    await expect(
      pluginApi.repositoryPreview({
        repositoryUrl: 'https://github.com/example/plugin',
        ref: 'v2.0.0',
      }),
    ).resolves.toBe(preview)
    await expect(pluginApi.repositoryInstall({ previewToken: 'token-1' })).resolves.toBe(installed)
    await expect(
      pluginApi.repositoryUpgrade('plugin/a', {
        previewToken: 'token-2',
        confirmPermissionIncrease: true,
      }),
    ).resolves.toBe(installed)

    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/plugins/repository/preview', {
      repositoryUrl: 'https://github.com/example/plugin',
      ref: 'v2.0.0',
    })
    expect(apiMocks.post).toHaveBeenNthCalledWith(2, '/plugins/repository/install', {
      previewToken: 'token-1',
    })
    expect(apiMocks.post).toHaveBeenNthCalledWith(3, '/plugins/plugin%2Fa/repository/upgrade', {
      previewToken: 'token-2',
      confirmPermissionIncrease: true,
    })
  })
})
