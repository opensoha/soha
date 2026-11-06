/** @vitest-environment jsdom */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearAuthSession, restoreAuthSession } from '@/features/auth/auth-api'
import { useAuthStore } from '@/stores/auth-store'
import type { User } from '@/types'
import { api } from './api-client'

vi.mock('@/features/auth/auth-api', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth/auth-api')>(
    '@/features/auth/auth-api',
  )
  return {
    ...actual,
    getStoredAccessToken: () => useAuthStore.getState().accessToken,
    clearAuthSession: vi.fn(),
    restoreAuthSession: vi.fn(),
  }
})
function session(id: string) {
  useAuthStore.setState({ user: { userId: id } as User, accessToken: `synthetic-token-${id}` })
}
describe('single-user sensitive requests', () => {
  beforeEach(() => {
    session('owner')
    vi.clearAllMocks()
  })
  afterEach(() => {
    useAuthStore.setState({ user: null, accessToken: null })
    vi.unstubAllGlobals()
  })
  it('refuses a stale owner before sending a body', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    await expect(
      api.scoped('/managed', 'another-user', {
        method: 'POST',
        body: { contentBase64: 'ZXhhbXBsZQ==' },
      }),
    ).rejects.toMatchObject({ status: 403 })
    expect(fetchMock).not.toHaveBeenCalled()
  })
  it('never refreshes or automatically replays a content body', async () => {
    const fetchMock = vi.fn<typeof fetch>(async () => new Response('{}', { status: 401 }))
    vi.stubGlobal('fetch', fetchMock)
    await expect(
      api.scoped('/managed', 'owner', { method: 'POST', body: { requestId: 'one-attempt' } }),
    ).rejects.toMatchObject({ status: 401 })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(restoreAuthSession).not.toHaveBeenCalled()
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({ credentials: 'omit' })
  })
  it('does not clear or replace a newer user session when an old request returns', async () => {
    const fetchMock = vi.fn<typeof fetch>(async () => {
      session('new-user')
      return new Response('{}', { status: 401 })
    })
    vi.stubGlobal('fetch', fetchMock)
    await expect(
      api.scoped('/managed', 'owner', { method: 'POST', body: { requestId: 'one-attempt' } }),
    ).rejects.toMatchObject({ status: 503 })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(clearAuthSession).not.toHaveBeenCalled()
    expect(restoreAuthSession).not.toHaveBeenCalled()
    expect(useAuthStore.getState().user?.userId).toBe('new-user')
  })
  it('does not return protected data after identity changes during response parsing', async () => {
    const response = new Response('{}', { status: 200 })
    vi.spyOn(response, 'json').mockImplementation(async () => {
      session('new-user')
      return { data: { private: 'synthetic-old-user-result' } }
    })
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => response),
    )
    await expect(api.scoped('/managed', 'owner')).rejects.toMatchObject({ status: 403 })
    expect(clearAuthSession).not.toHaveBeenCalled()
  })
})
