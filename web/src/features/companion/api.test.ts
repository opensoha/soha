/** @vitest-environment jsdom */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAuthStore } from '@/stores/auth-store'
import { fetchCompanionAsset } from './api'

const auth = vi.hoisted(() => ({
  getStoredAccessToken: vi.fn<() => string | null>(),
  refreshAuthSession: vi.fn<() => Promise<boolean>>(),
}))

vi.mock('@/features/auth/auth-api', async (original) => ({
  ...(await original<typeof import('@/features/auth/auth-api')>()),
  getStoredAccessToken: auth.getStoredAccessToken,
  refreshAuthSession: auth.refreshAuthSession,
}))

describe('companion asset authentication', () => {
  beforeEach(() => {
    auth.getStoredAccessToken.mockReset()
    auth.refreshAuthSession.mockReset()
    auth.getStoredAccessToken.mockReturnValue('old-token')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    useAuthStore.getState().clearAuth()
  })

  it('refreshes and retries an asset within its originating session', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(new Response(null, { status: 401 }))
      .mockResolvedValueOnce(new Response('asset-bytes'))
    vi.stubGlobal('fetch', fetchMock)
    auth.getStoredAccessToken.mockReturnValueOnce('old-token').mockReturnValue('fresh-token')
    auth.refreshAuthSession.mockResolvedValue(true)
    const asset = await fetchCompanionAsset('plugin-id', 'folder/asset name.png')
    expect(asset.size).toBe('asset-bytes'.length)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(String(fetchMock.mock.calls[1][0])).toContain(
      '/plugins/plugin-id/assets/folder/asset%20name.png',
    )
    expect(new Headers(fetchMock.mock.calls[1][1]?.headers).get('Authorization')).toBe(
      'Bearer fresh-token',
    )
  })

  it.each(['response', 'refresh'] as const)(
    'does not retry an old asset after the session changes during its %s',
    async (stage) => {
      let respond!: (value: Response) => void
      let refreshed!: (value: boolean) => void
      const response = new Promise<Response>((resolve) => {
        respond = resolve
      })
      const refresh = new Promise<boolean>((resolve) => {
        refreshed = resolve
      })
      const fetchMock = vi.fn(() => response)
      vi.stubGlobal('fetch', fetchMock)
      auth.refreshAuthSession.mockReturnValue(refresh)
      const request = fetchCompanionAsset('plugin-id', 'asset.png')
      if (stage === 'refresh') {
        respond(new Response(null, { status: 401 }))
        await vi.waitFor(() => expect(auth.refreshAuthSession).toHaveBeenCalledOnce())
      }
      useAuthStore.getState().clearAuth()
      if (stage === 'response') respond(new Response(null, { status: 401 }))
      else refreshed(true)
      await expect(request).rejects.toMatchObject({ name: 'AbortError' })
      expect(fetchMock).toHaveBeenCalledOnce()
      if (stage === 'response') expect(auth.refreshAuthSession).not.toHaveBeenCalled()
    },
  )
})
