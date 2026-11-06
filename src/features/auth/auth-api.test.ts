/** @vitest-environment jsdom */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { restoreAuthSession } from './auth-api'
import { useAuthStore } from '@/stores/auth-store'
import type { AuthResult, User } from '@/types'

const user = {
  userId: 'user-1',
  userName: 'opensoha',
  email: 'opensoha@soha.local',
  roles: [],
  teams: [],
  projects: [],
  tags: [],
} satisfies User

function jsonResponse(body: unknown, init: ResponseInit) {
  return new Response(JSON.stringify(body), {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...init.headers,
    },
  })
}

describe('auth session restore', () => {
  beforeEach(() => {
    useAuthStore.getState().clearAuth()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
    useAuthStore.getState().clearAuth()
  })

  it('commits a refreshed session when refresh succeeds', async () => {
    const startingGeneration = useAuthStore.getState().sessionGeneration
    const authResult: AuthResult = {
      tokens: {
        accessToken: 'new-access-token',
        refreshToken: 'refresh-token',
        tokenType: 'Bearer',
        expiresIn: 3600,
        expiresAt: '2026-07-08T13:00:00Z',
      },
      user,
    }
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse({ data: authResult }, { status: 200, statusText: 'OK' })),
    )

    await expect(restoreAuthSession()).resolves.toBe('authenticated')

    expect(useAuthStore.getState().accessToken).toBe('new-access-token')
    expect(useAuthStore.getState().user).toEqual(user)
    const establishedGeneration = useAuthStore.getState().sessionGeneration
    expect(establishedGeneration).toBeGreaterThan(startingGeneration)

    authResult.tokens.accessToken = 'rotated-access-token'
    await expect(restoreAuthSession()).resolves.toBe('authenticated')
    expect(useAuthStore.getState().accessToken).toBe('rotated-access-token')
    expect(useAuthStore.getState().sessionGeneration).toBe(establishedGeneration)
    expect(localStorage.getItem('soha-auth')).not.toContain('sessionGeneration')
  })

  it.each([null, 'access-token'])(
    'clears local auth when refresh is rejected with access token %s',
    async (accessToken) => {
      useAuthStore.getState().setUser(user)
      if (accessToken) useAuthStore.getState().setTokens(accessToken)
      const startingGeneration = useAuthStore.getState().sessionGeneration
      vi.stubGlobal(
        'fetch',
        vi.fn(async () =>
          jsonResponse(
            { error: { code: 'unauthorized', message: 'refresh token expired' } },
            { status: 401, statusText: 'Unauthorized' },
          ),
        ),
      )

      await expect(restoreAuthSession()).resolves.toBe('unauthenticated')

      expect(useAuthStore.getState().user).toBeNull()
      expect(useAuthStore.getState().accessToken).toBeNull()
      expect(useAuthStore.getState().sessionGeneration).toBe(startingGeneration + 1)
    },
  )

  it('keeps the signed-out generation stable when repeated refresh attempts are rejected', async () => {
    const startingGeneration = useAuthStore.getState().sessionGeneration
    vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 401 })))

    await expect(restoreAuthSession()).resolves.toBe('unauthenticated')
    await expect(restoreAuthSession()).resolves.toBe('unauthenticated')

    expect(useAuthStore.getState().sessionGeneration).toBe(startingGeneration)
  })

  it('advances the generation when the current user ID changes', () => {
    useAuthStore.getState().setTokens('access-token')
    useAuthStore.getState().setUser(user)
    const startingGeneration = useAuthStore.getState().sessionGeneration

    useAuthStore.getState().setUser({ ...user, userId: 'user-2' })

    expect(useAuthStore.getState().sessionGeneration).toBe(startingGeneration + 1)
  })

  it('keeps local auth when the API server is temporarily unavailable', async () => {
    useAuthStore.getState().setUser(user)
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('Failed to fetch')
      }),
    )

    await expect(restoreAuthSession()).resolves.toBe('unavailable')

    expect(useAuthStore.getState().user).toEqual(user)
  })
})
