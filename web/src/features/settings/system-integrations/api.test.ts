import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/services/api-client'
import { sourceControlApi, systemIntegrationsApi } from './api'

vi.mock('@/services/api-client', () => ({
  api: {
    delete: vi.fn(),
    get: vi.fn(),
    getEnvelope: vi.fn(),
    patch: vi.fn(),
    post: vi.fn(),
  },
}))

describe('systemIntegrationsApi', () => {
  afterEach(() => vi.clearAllMocks())

  it('lists integrations with normalized contract filters', async () => {
    vi.mocked(api.getEnvelope).mockResolvedValue({ items: [{ id: 'gitlab-main' }] })

    await expect(
      systemIntegrationsApi.list({
        category: 'source_control',
        providerType: 'gitlab',
        enabled: true,
      }),
    ).resolves.toEqual([{ id: 'gitlab-main' }])
    expect(api.getEnvelope).toHaveBeenCalledWith(
      '/system-integrations?category=source_control&providerType=gitlab&enabled=true',
    )
  })

  it('uses encoded detail, update, delete, test, and OAuth paths', async () => {
    vi.mocked(api.get).mockResolvedValue({ data: { id: 'gitlab/main' } })
    vi.mocked(api.patch).mockResolvedValue({ data: { id: 'gitlab/main', version: 2 } })
    vi.mocked(api.post).mockResolvedValue({ data: { status: 'succeeded' } })

    await systemIntegrationsApi.get('gitlab/main')
    await systemIntegrationsApi.update({
      id: 'gitlab/main',
      values: { expectedVersion: 1, enabled: false },
    })
    await systemIntegrationsApi.remove('gitlab/main')
    await systemIntegrationsApi.test('gitlab/main')
    await systemIntegrationsApi.authorizeOAuth('gitlab/main')

    expect(api.get).toHaveBeenCalledWith('/system-integrations/gitlab%2Fmain')
    expect(api.patch).toHaveBeenCalledWith('/system-integrations/gitlab%2Fmain', {
      expectedVersion: 1,
      enabled: false,
    })
    expect(api.delete).toHaveBeenCalledWith('/system-integrations/gitlab%2Fmain')
    expect(api.post).toHaveBeenCalledWith('/system-integrations/gitlab%2Fmain/test')
    expect(api.post).toHaveBeenCalledWith('/system-integrations/gitlab%2Fmain/oauth/authorize')
  })

  it('creates a GitLab integration without changing the contract payload', async () => {
    vi.mocked(api.post).mockResolvedValue({ data: { id: 'gitlab-main' } })
    const values = {
      category: 'source_control' as const,
      providerType: 'gitlab',
      name: 'GitLab',
      enabled: true,
      credentials: [{ key: 'token', value: 'secret' }],
    }

    await systemIntegrationsApi.create(values)

    expect(api.post).toHaveBeenCalledWith('/system-integrations', values)
  })

  it('reads source repositories and files through the provider-neutral contract', async () => {
    vi.mocked(api.getEnvelope)
      .mockResolvedValueOnce({ items: [{ id: 'source/main' }] })
      .mockResolvedValueOnce({ items: [{ id: 'repo/api' }] })
      .mockResolvedValueOnce({ items: [{ name: 'main' }] })
    vi.mocked(api.get).mockResolvedValue({ data: { content: 'services: {}' } })

    await sourceControlApi.connections()
    await sourceControlApi.repositories('source/main')
    await sourceControlApi.branches('source/main', 'repo/api')
    await sourceControlApi.file('source/main', 'repo/api', 'release/v1', 'deploy/compose.yaml')

    expect(api.getEnvelope).toHaveBeenNthCalledWith(1, '/source-connections')
    expect(api.getEnvelope).toHaveBeenNthCalledWith(
      2,
      '/source-connections/source%2Fmain/repositories?limit=200',
    )
    expect(api.getEnvelope).toHaveBeenNthCalledWith(
      3,
      '/source-connections/source%2Fmain/repositories/repo%2Fapi/branches',
    )
    expect(api.get).toHaveBeenCalledWith(
      '/source-connections/source%2Fmain/repositories/repo%2Fapi/files?ref=release%2Fv1&path=deploy%2Fcompose.yaml',
    )
  })
})
