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

function browserAccessToken(label: string, sessionId = 'browser-session-1') {
  const payload = btoa(JSON.stringify({ sid: sessionId })).replace(/=+$/, '')
  return `eyJhbGciOiJIUzI1NiJ9.${payload}.${label}`
}

function authResult(userId = 'user-1', accessToken = 'access-token'): AuthResult {
  return {
    user: { ...user, userId },
    tokens: {
      accessToken,
      refreshToken: `synthetic-refresh-${userId}`,
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

function browserSessionServer() {
  const state = {
    active: true,
    cookiePresent: true,
    userId: 'user-1',
    accessToken: browserAccessToken('server-valid-token'),
    sessionId: 'browser-session-1',
    refreshStatus: 200,
    refreshGate: undefined as Promise<void> | undefined,
    loginGate: undefined as Promise<void> | undefined,
  }
  const paths: string[] = []
  const logoutTokens: Array<string | null> = []
  const logoutRefreshTokens: Array<string | undefined> = []
  const logoutStatuses: number[] = []
  const logoutCredentials: Array<RequestCredentials | undefined> = []
  const revoked: string[] = []
  const fetchMock = vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
    const path = new URL(String(input), 'https://example.test').pathname
    paths.push(path)
    if (path.endsWith('/auth/refresh')) {
      await state.refreshGate
      const status = state.active && state.cookiePresent ? state.refreshStatus : 401
      if (status !== 200) return jsonResponse({ error: { code: 'unauthorized' } }, { status })
      state.accessToken = browserAccessToken(`refreshed-${state.userId}`, state.sessionId)
      return jsonResponse({ data: authResult(state.userId, state.accessToken) }, { status: 200 })
    }
    if (path.endsWith('/auth/login')) {
      await state.loginGate
      state.active = true
      state.cookiePresent = true
      state.userId = 'user-2'
      state.sessionId = 'browser-session-2'
      state.accessToken = browserAccessToken('new-login-token', state.sessionId)
      return jsonResponse({ data: authResult(state.userId, state.accessToken) }, { status: 200 })
    }
    if (path.endsWith('/auth/logout')) {
      const token = new Headers(options?.headers).get('Authorization')
      logoutTokens.push(token)
      logoutCredentials.push(options?.credentials)
      const refreshToken = options?.body
        ? (JSON.parse(String(options.body)) as { refreshToken?: string }).refreshToken
        : undefined
      logoutRefreshTokens.push(refreshToken)
      const status =
        state.active &&
        token === `Bearer ${state.accessToken}` &&
        (!refreshToken || refreshToken === `synthetic-refresh-${state.userId}`)
          ? 200
          : 401
      logoutStatuses.push(status)
      if (status === 200) {
        revoked.push(state.userId)
        state.active = false
        if (options?.credentials === 'include') state.cookiePresent = false
      }
      return jsonResponse({}, { status })
    }
    throw new Error(`Unexpected synthetic auth path: ${path}`)
  })
  return {
    state,
    paths,
    logoutTokens,
    logoutRefreshTokens,
    logoutCredentials,
    logoutStatuses,
    revoked,
    fetchMock,
  }
}

describe('auth session restore', () => {
  beforeEach(() => {
    useAuthStore.getState().clearAuth()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.clearAllMocks()
    useAuthStore.getState().clearAuth()
  })

  it('commits a refreshed session when refresh succeeds', async () => {
    const startingGeneration = useAuthStore.getState().sessionGeneration
    const authResult: AuthResult = {
      tokens: {
        accessToken: browserAccessToken('new-access-token'),
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

    expect(useAuthStore.getState().accessToken).toBe(browserAccessToken('new-access-token'))
    expect(useAuthStore.getState().user).toEqual(user)
    const establishedGeneration = useAuthStore.getState().sessionGeneration
    expect(establishedGeneration).toBeGreaterThan(startingGeneration)

    authResult.tokens.accessToken = browserAccessToken('rotated-access-token')
    await expect(restoreAuthSession()).resolves.toBe('authenticated')
    expect(useAuthStore.getState().accessToken).toBe(browserAccessToken('rotated-access-token'))
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
      if (established) commitAuthResult(authResult('user-1', browserAccessToken('access-token')))
      const generation = useAuthStore.getState().sessionGeneration
      const response = deferred<Response>()
      const fetchMock = vi.fn(() => response.promise)
      vi.stubGlobal('fetch', fetchMock)
      const first = restoreAuthSession()
      const second = restoreAuthSession()
      expect(first).toBe(second)
      await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce())
      response.resolve(
        jsonResponse(
          { data: authResult('user-1', browserAccessToken('fresh-token')) },
          { status: 200 },
        ),
      )
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

  it('clears logout immediately and orders its server revocation before a later login', async () => {
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

  it.each(['in-flight', 'expired'] as const)(
    'revokes the browser session with %s local credentials and cannot restore it afterward',
    async (credentials) => {
      const server = browserSessionServer()
      vi.stubGlobal('fetch', server.fetchMock)
      commitAuthResult(authResult('user-1', browserAccessToken('expired-token')))
      const refreshGate = deferred<void>()
      let restoring: Promise<unknown> | undefined
      if (credentials === 'in-flight') {
        server.state.refreshGate = refreshGate.promise
        restoring = restoreAuthSession()
        await vi.waitFor(() => expect(server.fetchMock).toHaveBeenCalledOnce())
      }
      const observed: Array<string | null> = []
      const unsubscribe = useAuthStore.subscribe((state) => observed.push(state.accessToken))
      try {
        const loggedOut = logoutAuthSession()
        expect(useAuthStore.getState().accessToken).toBeNull()
        refreshGate.resolve()
        await loggedOut
        if (restoring) await expect(restoring).resolves.toBe('superseded')
        expect(server.logoutStatuses).toEqual(credentials === 'expired' ? [401, 200] : [200])
        expect(server.logoutTokens.at(-1)).toBe(`Bearer ${browserAccessToken('refreshed-user-1')}`)
        expect(server.logoutRefreshTokens.at(-1)).toBe('synthetic-refresh-user-1')
        expect(server.revoked).toEqual(['user-1'])
        expect(server.state.active).toBe(false)
        expect(server.state.cookiePresent).toBe(true)
        expect(observed.every((token) => token === null)).toBe(true)
        await expect(restoreAuthSession()).resolves.toBe('unauthenticated')
        expect(useAuthStore.getState().accessToken).toBeNull()
      } finally {
        unsubscribe()
      }
    },
  )

  it.each([401, 503])(
    'uses the captured valid bearer if the in-flight refresh fails with %s',
    async (status) => {
      const server = browserSessionServer()
      commitAuthResult(authResult('user-1', server.state.accessToken))
      const refreshGate = deferred<void>()
      server.state.refreshGate = refreshGate.promise
      server.state.refreshStatus = status
      vi.stubGlobal('fetch', server.fetchMock)
      const restoring = restoreAuthSession()
      await vi.waitFor(() => expect(server.fetchMock).toHaveBeenCalledOnce())
      const loggedOut = logoutAuthSession()
      refreshGate.resolve()
      await loggedOut
      await expect(restoring).resolves.toBe('superseded')
      expect(server.logoutStatuses).toEqual([200])
      expect(server.logoutCredentials).toEqual(['omit'])
      expect(server.revoked).toEqual(['user-1'])
      await expect(restoreAuthSession()).resolves.toBe('unauthenticated')
    },
  )

  it('keeps a later login behind recovery and revocation of the captured session', async () => {
    const server = browserSessionServer()
    commitAuthResult(authResult('user-1', browserAccessToken('expired-token')))
    const refreshGate = deferred<void>()
    server.state.refreshGate = refreshGate.promise
    vi.stubGlobal('fetch', server.fetchMock)
    const restoring = restoreAuthSession()
    await vi.waitFor(() => expect(server.fetchMock).toHaveBeenCalledOnce())
    const loggedOut = logoutAuthSession()
    const signedIn = loginWithPassword('synthetic-user', 'synthetic-password')
    refreshGate.resolve()
    await loggedOut
    await signedIn
    await expect(restoring).resolves.toBe('superseded')
    expect(server.paths).toEqual([
      '/api/v1/auth/refresh',
      '/api/v1/auth/logout',
      '/api/v1/auth/login',
    ])
    expect(server.revoked).toEqual(['user-1'])
    expect(server.logoutTokens).toEqual([`Bearer ${browserAccessToken('refreshed-user-1')}`])
    expect(server.logoutRefreshTokens).toEqual(['synthetic-refresh-user-1'])
    expect(server.logoutCredentials).toEqual(['omit'])
    expect(useAuthStore.getState().accessToken).toBe(
      browserAccessToken('new-login-token', 'browser-session-2'),
    )
    expect(server.state.active).toBe(true)
  })

  it.each([false, true])(
    'revokes an earlier pending login without committing it over an existing session %s',
    async (established) => {
      const server = browserSessionServer()
      if (established) commitAuthResult(authResult('user-1', server.state.accessToken))
      const loginGate = deferred<void>()
      server.state.loginGate = loginGate.promise
      vi.stubGlobal('fetch', server.fetchMock)
      const signedIn = loginWithPassword('synthetic-user', 'synthetic-password')
      await vi.waitFor(() => expect(server.fetchMock).toHaveBeenCalledOnce())
      const loggedOut = logoutAuthSession()
      loginGate.resolve()
      await expect(signedIn).rejects.toMatchObject({ name: 'AbortError' })
      await loggedOut
      expect(server.revoked).toEqual(['user-2'])
      expect(server.paths).toEqual(['/api/v1/auth/login', '/api/v1/auth/logout'])
      expect(server.logoutTokens).toEqual([
        `Bearer ${browserAccessToken('new-login-token', 'browser-session-2')}`,
      ])
      expect(server.logoutRefreshTokens).toEqual(['synthetic-refresh-user-2'])
      expect(server.logoutCredentials).toEqual(['omit'])
      await expect(restoreAuthSession()).resolves.toBe('unauthenticated')
      expect(useAuthStore.getState().accessToken).toBeNull()
    },
  )

  it('does not borrow a different cookie identity when recovering from a logout 401', async () => {
    const server = browserSessionServer()
    server.state.userId = 'user-2'
    commitAuthResult(authResult('user-1', browserAccessToken('expired-token')))
    vi.stubGlobal('fetch', server.fetchMock)
    await logoutAuthSession()
    expect(server.logoutStatuses).toEqual([401])
    expect(server.logoutTokens).toEqual([`Bearer ${browserAccessToken('expired-token')}`])
    expect(server.revoked).toEqual([])
    expect(server.state.active).toBe(true)
    expect(useAuthStore.getState().accessToken).toBeNull()
  })

  it('revokes an ordinary logout with a valid bearer and leaves only an inert cookie', async () => {
    const server = browserSessionServer()
    commitAuthResult(authResult('user-1', server.state.accessToken))
    vi.stubGlobal('fetch', server.fetchMock)
    await logoutAuthSession()
    expect(server.paths).toEqual(['/api/v1/auth/logout'])
    expect(server.logoutStatuses).toEqual([200])
    expect(server.logoutCredentials).toEqual(['omit'])
    expect(server.state.cookiePresent).toBe(true)
    expect(server.revoked).toEqual(['user-1'])
    await expect(restoreAuthSession()).resolves.toBe('unauthenticated')
  })

  it('does not revoke a cookie session when only cached user metadata is available', async () => {
    const server = browserSessionServer()
    useAuthStore.getState().setUser(user)
    vi.stubGlobal('fetch', server.fetchMock)

    await logoutAuthSession()
    expect(server.fetchMock).not.toHaveBeenCalled()
    expect(server.state.active).toBe(true)
    expect(useAuthStore.getState().user).toBeNull()
  })

  it.each(['in-flight', 'expired'] as const)(
    'does not revoke a different session of the same user with an %s refresh',
    async (phase) => {
      const server = browserSessionServer()
      server.state.sessionId = 'browser-session-2'
      commitAuthResult(authResult('user-1', browserAccessToken('expired-token')))
      const gate = deferred<void>()
      server.state.refreshGate = gate.promise
      vi.stubGlobal('fetch', server.fetchMock)

      const restoring = phase === 'in-flight' ? restoreAuthSession() : undefined
      if (restoring) await vi.waitFor(() => expect(server.fetchMock).toHaveBeenCalledOnce())
      const signingOut = logoutAuthSession()
      gate.resolve()
      await signingOut
      if (restoring) await expect(restoring).resolves.toBe('superseded')

      expect(server.logoutTokens).toEqual([`Bearer ${browserAccessToken('expired-token')}`])
      expect(server.logoutCredentials).toEqual(['omit'])
      expect(server.revoked).toEqual([])
      expect(server.state.active).toBe(true)
      expect(useAuthStore.getState().accessToken).toBeNull()
    },
  )

  it('sends only the captured valid bearer when another tab has a new cookie', async () => {
    const original = authResult('user-1', browserAccessToken('original-token'))
    const activeSessions = new Set(['browser-session-1', 'browser-session-2'])
    commitAuthResult(original)
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, options?: RequestInit) => {
      if (
        new Headers(options?.headers).get('Authorization') ===
        `Bearer ${original.tokens.accessToken}`
      )
        activeSessions.delete('browser-session-1')
      if (options?.credentials === 'include') activeSessions.delete('browser-session-2')
      return jsonResponse({}, { status: 200 })
    })
    vi.stubGlobal('fetch', fetchMock)

    await logoutAuthSession()

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringMatching(/\/auth\/logout$/),
      expect.objectContaining({ credentials: 'omit', body: undefined }),
    )
    expect([...activeSessions]).toEqual(['browser-session-2'])
  })

  it.each(['headers', 'body'] as const)(
    'releases a queued login when refresh %s stall',
    async (stage) => {
      vi.useFakeTimers()
      const lateResponse = deferred<Response>()
      const lateBody = deferred<{ data: AuthResult }>()
      const response = jsonResponse({}, { status: 200 })
      const read = vi.spyOn(response, 'json').mockImplementation(() => lateBody.promise)
      const loginResult = authResult('user-2', 'new-login-token')
      const fetchMock = vi.fn((input: RequestInfo | URL, _options?: RequestInit) =>
        String(input).endsWith('/auth/refresh')
          ? stage === 'headers'
            ? lateResponse.promise
            : Promise.resolve(response)
          : Promise.resolve(jsonResponse({ data: loginResult }, { status: 200 })),
      )
      vi.stubGlobal('fetch', fetchMock)
      const restoring = restoreAuthSession()
      await vi.advanceTimersByTimeAsync(0)
      expect(fetchMock).toHaveBeenCalledOnce()
      if (stage === 'body') expect(read).toHaveBeenCalledOnce()
      const signal = fetchMock.mock.calls[0][1]?.signal
      const signingIn = loginWithPassword('synthetic-user', 'synthetic-password')
      try {
        await vi.advanceTimersByTimeAsync(14_999)
        expect(fetchMock).toHaveBeenCalledOnce()
        await vi.advanceTimersByTimeAsync(1)
        expect(signal?.aborted).toBe(true)
        expect(signal?.reason).toMatchObject({ name: 'TimeoutError' })
        await expect(restoring).resolves.toBe('superseded')
        await expect(signingIn).resolves.toEqual(loginResult)
        expect(useAuthStore.getState().accessToken).toBe(loginResult.tokens.accessToken)
      } finally {
        lateResponse.resolve(response)
        lateBody.resolve({ data: authResult() })
      }
      await vi.advanceTimersByTimeAsync(0)
      expect(useAuthStore.getState().accessToken).toBe(loginResult.tokens.accessToken)
    },
  )

  it('releases queued logout when the in-flight refresh body stalls', async () => {
    vi.useFakeTimers()
    const server = browserSessionServer()
    const original = authResult('user-1', server.state.accessToken)
    commitAuthResult(original)
    const body = deferred<unknown>()
    const response = jsonResponse({}, { status: 200 })
    const read = vi.spyOn(response, 'json').mockImplementation(() => body.promise)
    const fetchMock = vi.fn((input: RequestInfo | URL, options?: RequestInit) =>
      String(input).endsWith('/auth/refresh')
        ? Promise.resolve(response)
        : server.fetchMock(input, options),
    )
    vi.stubGlobal('fetch', fetchMock)
    const restoring = restoreAuthSession()
    await vi.advanceTimersByTimeAsync(0)
    expect(read).toHaveBeenCalledOnce()
    const signingOut = logoutAuthSession()
    try {
      await vi.advanceTimersByTimeAsync(15_000)
      await expect(restoring).resolves.toBe('superseded')
      await signingOut
      expect(server.logoutTokens).toEqual([`Bearer ${original.tokens.accessToken}`])
      expect(server.revoked).toEqual(['user-1'])
      expect(useAuthStore.getState().accessToken).toBeNull()
    } finally {
      body.resolve({ data: original })
    }
  })

  it('does not resume logout recovery after it times out and a later login succeeds', async () => {
    vi.useFakeTimers()
    const server = browserSessionServer()
    commitAuthResult(authResult('user-1', browserAccessToken('expired-token')))
    const refresh = deferred<Response>()
    const fetchMock = vi.fn((input: RequestInfo | URL, options?: RequestInit) =>
      String(input).endsWith('/auth/refresh') ? refresh.promise : server.fetchMock(input, options),
    )
    vi.stubGlobal('fetch', fetchMock)
    const signingOut = logoutAuthSession()
    await vi.advanceTimersByTimeAsync(0)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    const recoverySignal = fetchMock.mock.calls[1][1]?.signal
    const signingIn = loginWithPassword('synthetic-user', 'synthetic-password')
    try {
      await vi.advanceTimersByTimeAsync(15_000)
      expect(recoverySignal?.reason).toMatchObject({ name: 'TimeoutError' })
      await signingOut
      await signingIn
    } finally {
      const data = authResult('user-1', browserAccessToken('late-refreshed-token'))
      refresh.resolve(jsonResponse({ data }, { status: 200 }))
    }

    await vi.advanceTimersByTimeAsync(0)
    expect(server.logoutStatuses).toEqual([401])
    expect(server.revoked).toEqual([])
    expect(useAuthStore.getState().user?.userId).toBe('user-2')
  })

  it.each([
    { login: () => loginWithPassword('synthetic-user', 'synthetic-password'), status: 401 },
    { login: () => exchangeOIDCCode('synthetic-code'), status: 503 },
  ])('fences a superseded login rejection with status $status', async ({ login, status }) => {
    const response = deferred<Response>()
    const fetchMock = vi.fn(() => response.promise)
    vi.stubGlobal('fetch', fetchMock)
    const signingIn = login()
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce())
    commitAuthResult(authResult('user-2', 'new-session'))
    response.resolve(jsonResponse({ error: { message: 'Synthetic failure' } }, { status }))
    await expect(signingIn).rejects.toMatchObject({ name: 'AbortError' })
    expect(useAuthStore.getState().accessToken).toBe('new-session')
  })
})
