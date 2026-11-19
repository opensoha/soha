/** @vitest-environment jsdom */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  clearAuthSession,
  commitAuthResult,
  exchangeOIDCCode,
  fetchAuthProviders,
  issueStreamTicket,
  loginWithPassword,
  logoutAuthSession,
  restoreAuthSession,
} from './auth-api'
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

function authResult(userId = 'user-1', accessToken = 'access-token'): AuthResult {
  return {
    user: { ...user, userId },
    tokens: {
      accessToken,
      refreshToken: 'synthetic-refresh',
      tokenType: 'Bearer',
      expiresIn: 3600,
      expiresAt: '2026-11-19T12:00:00Z',
    },
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

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
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(null, { status: 401 })),
    )

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

  it.each([200, 401, 403, 503])(
    'ignores a deferred refresh status %s after a newer explicit login',
    async (status) => {
      commitAuthResult(authResult())
      const response = deferred<Response>()
      const fetchMock = vi.fn(() => response.promise)
      vi.stubGlobal('fetch', fetchMock)
      const restore = restoreAuthSession()
      await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce())
      commitAuthResult(authResult('user-2', 'second-session'))
      const generation = useAuthStore.getState().sessionGeneration
      response.resolve(jsonResponse({ data: authResult() }, { status }))
      await expect(restore).resolves.toBe('superseded')
      expect(useAuthStore.getState()).toMatchObject({
        user: { userId: 'user-2' },
        accessToken: 'second-session',
        sessionGeneration: generation,
      })
    },
  )

  it('ignores a successful old refresh after local logout', async () => {
    commitAuthResult(authResult())
    const response = deferred<Response>()
    const fetchMock = vi.fn(() => response.promise)
    vi.stubGlobal('fetch', fetchMock)
    const restore = restoreAuthSession()
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce())
    clearAuthSession()
    response.resolve(jsonResponse({ data: authResult() }, { status: 200 }))
    await expect(restore).resolves.toBe('superseded')
    expect(useAuthStore.getState().accessToken).toBeNull()
  })

  it.each([false, true])(
    'coalesces refresh with an established session %s',
    async (established) => {
      if (established) commitAuthResult(authResult())
      const generation = useAuthStore.getState().sessionGeneration
      const response = deferred<Response>()
      const fetchMock = vi.fn(() => response.promise)
      vi.stubGlobal('fetch', fetchMock)
      const first = restoreAuthSession()
      const second = restoreAuthSession()
      expect(first).toBe(second)
      await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce())
      response.resolve(jsonResponse({ data: authResult('user-1', 'fresh-token') }, { status: 200 }))
      await expect(Promise.all([first, second])).resolves.toEqual([
        'authenticated',
        'authenticated',
      ])
      expect(useAuthStore.getState().sessionGeneration).toBe(generation + (established ? 0 : 1))
    },
  )

  it('establishes each explicit login atomically even for the same user', () => {
    commitAuthResult(authResult())
    const generation = useAuthStore.getState().sessionGeneration
    const observed: Array<{ token: string | null; userId?: string }> = []
    const unsubscribe = useAuthStore.subscribe((state) => {
      observed.push({ token: state.accessToken, userId: state.user?.userId })
    })
    commitAuthResult(authResult('user-2', 'new-user-token'))
    unsubscribe()
    expect(observed).toEqual([{ token: 'new-user-token', userId: 'user-2' }])
    commitAuthResult(authResult('user-2', 'same-user-new-session'))
    expect(useAuthStore.getState().sessionGeneration).toBe(generation + 2)
  })

  it.each([
    { path: '/auth/login', login: () => loginWithPassword('synthetic-user', 'synthetic-password') },
    { path: '/auth/oidc/exchange', login: () => exchangeOIDCCode('synthetic-code') },
  ])(
    'orders $path after refresh while unrelated auth reads remain concurrent',
    async ({ path, login }) => {
      const refreshResponse = deferred<Response>()
      const loginResponse = deferred<Response>()
      const calls: string[] = []
      const fetchMock = vi.fn((input: RequestInfo | URL) => {
        const url = String(input)
        calls.push(url)
        if (url.endsWith('/auth/providers'))
          return Promise.resolve(jsonResponse({ items: [] }, { status: 200 }))
        return url.endsWith('/auth/refresh') ? refreshResponse.promise : loginResponse.promise
      })
      vi.stubGlobal('fetch', fetchMock)
      const restore = restoreAuthSession()
      await vi.waitFor(() => expect(calls).toHaveLength(1))
      const signedIn = login()
      await expect(fetchAuthProviders()).resolves.toEqual([])
      expect(calls.some((url) => url.endsWith(path))).toBe(false)
      refreshResponse.resolve(jsonResponse({ data: authResult() }, { status: 200 }))
      await expect(restore).resolves.toBe('superseded')
      await vi.waitFor(() => expect(calls.some((url) => url.endsWith(path))).toBe(true))
      expect(useAuthStore.getState().accessToken).toBeNull()
      loginResponse.resolve(
        jsonResponse({ data: authResult('user-2', 'new-session') }, { status: 200 }),
      )
      await signedIn
      expect(useAuthStore.getState()).toMatchObject({
        user: { userId: 'user-2' },
        accessToken: 'new-session',
      })
    },
  )

  it('clears logout immediately and serializes its cookie expiry before a later login', async () => {
    commitAuthResult(authResult())
    const logoutResponse = deferred<Response>()
    const loginResponse = deferred<Response>()
    const calls: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = String(input)
        calls.push(url)
        return url.endsWith('/auth/logout') ? logoutResponse.promise : loginResponse.promise
      }),
    )
    const loggedOut = logoutAuthSession()
    expect(useAuthStore.getState().accessToken).toBeNull()
    await expect(restoreAuthSession()).resolves.toBe('superseded')
    await vi.waitFor(() => expect(calls).toHaveLength(1))
    const signedIn = loginWithPassword('synthetic-user', 'synthetic-password')
    await Promise.resolve()
    expect(calls).toHaveLength(1)
    logoutResponse.resolve(jsonResponse({}, { status: 200 }))
    await loggedOut
    await vi.waitFor(() => expect(calls).toHaveLength(2))
    loginResponse.resolve(
      jsonResponse({ data: authResult('user-2', 'new-session') }, { status: 200 }),
    )
    await signedIn
    expect(useAuthStore.getState().accessToken).toBe('new-session')
    expect(calls.map((url) => new URL(url, 'https://example.test').pathname)).toEqual([
      '/api/v1/auth/logout',
      '/api/v1/auth/login',
    ])
  })

  it('does not commit a pending login after a newer logout', async () => {
    const loginResponse = deferred<Response>()
    const fetchMock = vi.fn((input: RequestInfo | URL) =>
      String(input).endsWith('/auth/login')
        ? loginResponse.promise
        : Promise.resolve(jsonResponse({}, { status: 200 })),
    )
    vi.stubGlobal('fetch', fetchMock)
    const signedIn = loginWithPassword('synthetic-user', 'synthetic-password')
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce())
    const loggedOut = logoutAuthSession()
    loginResponse.resolve(jsonResponse({ data: authResult() }, { status: 200 }))
    await expect(signedIn).rejects.toMatchObject({ name: 'AbortError' })
    await loggedOut
    expect(useAuthStore.getState().accessToken).toBeNull()
  })

  it('does not refresh or retry an old stream-ticket rejection after a session switch', async () => {
    commitAuthResult(authResult())
    const response = deferred<Response>()
    const fetchMock = vi.fn(() => response.promise)
    vi.stubGlobal('fetch', fetchMock)
    const ticket = issueStreamTicket('/synthetic-stream')
    commitAuthResult(authResult('user-2', 'new-session'))
    response.resolve(jsonResponse({}, { status: 401 }))
    await expect(ticket).rejects.toMatchObject({ name: 'AbortError' })
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(useAuthStore.getState().accessToken).toBe('new-session')
  })
})
