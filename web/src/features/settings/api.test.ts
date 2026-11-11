import { beforeEach, describe, expect, it, vi } from 'vitest'
import { settingsApi } from './api'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  upload: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

describe('settingsApi', () => {
  beforeEach(() => vi.clearAllMocks())

  it('unwraps branding, identity, and AI response envelopes', async () => {
    apiMocks.get
      .mockResolvedValueOnce({ data: { appTitle: 'Soha' } })
      .mockResolvedValueOnce({ data: { providers: [] } })
      .mockResolvedValueOnce({ data: { skillsRegistry: [] } })
      .mockResolvedValueOnce({ data: { skillsRegistry: [{ id: 'logs' }] } })

    await expect(settingsApi.branding.get()).resolves.toEqual({ appTitle: 'Soha' })
    await expect(settingsApi.identity.get()).resolves.toEqual({ providers: [] })
    await expect(settingsApi.ai.get()).resolves.toEqual({ skillsRegistry: [] })
    await expect(settingsApi.ai.skills()).resolves.toEqual({ skillsRegistry: [{ id: 'logs' }] })

    expect(apiMocks.get.mock.calls.map(([path]) => path)).toEqual([
      '/settings/branding',
      '/settings/identity',
      '/settings/ai',
      '/settings/ai/skills',
    ])
  })

  it('uses canonical save and upload endpoints', async () => {
    apiMocks.put.mockResolvedValue(undefined)
    apiMocks.upload.mockResolvedValue({ data: { url: '/brand/logo.svg' } })
    const branding = {
      appTitle: 'OpenSoha',
      sidebarTitle: 'OpenSoha',
      loginLogoUrl: '',
      expandedLogoUrl: '',
      collapsedLogoUrl: '',
      faviconUrl: '',
    }
    const formData = new FormData()

    await settingsApi.branding.save(branding)
    await expect(settingsApi.branding.upload(formData)).resolves.toEqual({
      url: '/brand/logo.svg',
    })
    await settingsApi.identity.save({ values: { providers: [] } })

    expect(apiMocks.put).toHaveBeenNthCalledWith(1, '/settings/branding', branding)
    expect(apiMocks.upload).toHaveBeenCalledWith('/settings/branding/upload', formData)
    expect(apiMocks.put).toHaveBeenNthCalledWith(2, '/settings/identity/providers', {
      providers: [],
    })
  })
})
