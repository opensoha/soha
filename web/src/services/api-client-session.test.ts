/** @vitest-environment jsdom */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  clearAuthSession,
  commitAuthResult,
  loginWithPassword,
  restoreAuthSession,
} from '@/features/auth/auth-api'
import { completeBrowserHandoff } from '@/features/auth/browser-handoff-api'
import { useAuthStore } from '@/stores/auth-store'
import type { AuthResult } from '@/types'
import { api } from './api-client'
import { API_ERROR_EVENT, type ApiErrorEventDetail } from './api-error'

function browserAccessToken(label: string, sessionId = 'session-a') {
  const payload = btoa(JSON.stringify({ sid: sessionId })).replace(/=+$/, '')
  return `eyJhbGciOiJIUzI1NiJ9.${payload}.${label}`
}

const freshAccessToken = browserAccessToken('fresh-token')

function session(userId = 'user-a', accessToken = browserAccessToken('old-token')): AuthResult {
  return {
    user: { userId, userName: userId, email: '', roles: [], teams: [], projects: [], tags: [] },
    tokens: {
      accessToken,
      refreshToken: 'synthetic-refresh',
      tokenType: 'Bearer',
      expiresIn: 3600,
      expiresAt: '2026-11-19T12:00:00Z',
    },
  }
}

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}

describe('API session ownership', () => {
  const events: ApiErrorEventDetail[] = []
  const listener = ((event: CustomEvent<ApiErrorEventDetail>) => {
    events.push(event.detail)
  }) as EventListener

  beforeEach(() => {
    clearAuthSession()
    commitAuthResult(session())
    events.length = 0
    window.addEventListener(API_ERROR_EVENT, listener)
  })

  afterEach(() => {
    window.removeEventListener(API_ERROR_EVENT, listener)
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    clearAuthSession()
  })

  it.each([
    { name: 'read', send: () => api.get('/synthetic-resource') },
    { name: 'write', send: () => api.post('/synthetic-resource', { value: 'draft' }) },
    { name: 'upload', send: () => api.upload('/synthetic-resource', new FormData()) },
  ])('does not refresh or replay an old $name 401 after another login', async ({ send }) => {
    const response = deferred<Response>()
    const fetchMock = vi.fn(() => response.promise)
    vi.stubGlobal('fetch', fetchMock)
    const request = send()
    commitAuthResult(session('user-b', 'new-session'))
    response.resolve(json({ error: { code: 'token_expired' } }, 401))
    await expect(request).rejects.toMatchObject({ name: 'AbortError' })
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(useAuthStore.getState().accessToken).toBe('new-session')
    expect(events).toEqual([])
  })

  it('does not replay when another login supersedes an in-flight refresh', async () => {
    const refresh = deferred<Response>()
    const fetchMock = vi.fn((input: RequestInfo | URL) =>
      String(input).endsWith('/auth/refresh') ? refresh.promise : Promise.resolve(json({}, 401)),
    )
    vi.stubGlobal('fetch', fetchMock)
    const request = api.post('/synthetic-resource', { value: 'draft' })
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    commitAuthResult(session('user-b', 'new-session'))
    refresh.resolve(json({ data: session('user-a', freshAccessToken) }))
    await expect(request).rejects.toMatchObject({ name: 'AbortError' })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(useAuthStore.getState().accessToken).toBe('new-session')
    expect(events).toEqual([])
  })

  it.each([false, true])(
    'does not let a 401 clear a pending login with an existing session %s',
    async (established) => {
      if (!established) clearAuthSession()
      const login = deferred<Response>()
      const fetchMock = vi.fn((input: RequestInfo | URL) =>
        String(input).endsWith('/auth/login') ? login.promise : Promise.resolve(json({}, 401)),
      )
      vi.stubGlobal('fetch', fetchMock)
      const signedIn = loginWithPassword('synthetic-user', 'synthetic-password')
      await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce())
      await expect(api.get('/synthetic-resource')).rejects.toMatchObject({ name: 'AbortError' })
      login.resolve(json({ data: session('user-b', 'new-session') }))
      await signedIn
      expect(useAuthStore.getState().accessToken).toBe('new-session')
      expect(fetchMock).toHaveBeenCalledTimes(2)
      expect(events).toEqual([])
    },
  )

  it('does not clear a newer login when an already-replayed request returns 401', async () => {
    const replay = deferred<Response>()
    let resourceCalls = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      if (String(input).endsWith('/auth/refresh'))
        return Promise.resolve(json({ data: session('user-a', freshAccessToken) }))
      resourceCalls += 1
      return resourceCalls === 1 ? Promise.resolve(json({}, 401)) : replay.promise
    })
    vi.stubGlobal('fetch', fetchMock)
    const request = api.get('/synthetic-resource')
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    commitAuthResult(session('user-b', 'new-session'))
    replay.resolve(json({}, 401))
    await expect(request).rejects.toMatchObject({ name: 'AbortError' })
    expect(useAuthStore.getState().accessToken).toBe('new-session')
    expect(events).toEqual([])
  })

  it('coalesces simultaneous 401 refreshes and reuses that token for a delayed same-session 401', async () => {
    const refresh = deferred<Response>()
    const late = deferred<Response>()
    const generation = useAuthStore.getState().sessionGeneration
    const calls: Array<{ url: string; token: string | null }> = []
    const fetchMock = vi.fn((input: RequestInfo | URL, options?: RequestInit) => {
      const url = String(input)
      const token = new Headers(options?.headers).get('Authorization')
      calls.push({ url, token })
      if (url.endsWith('/auth/refresh')) return refresh.promise
      if (token === `Bearer ${freshAccessToken}`) return Promise.resolve(json({ data: url }))
      return url.endsWith('/late') ? late.promise : Promise.resolve(json({}, 401))
    })
    vi.stubGlobal('fetch', fetchMock)
    const first = api.get('/first')
    const second = api.get('/second')
    const third = api.get('/late')
    await vi.waitFor(() =>
      expect(calls.filter((call) => call.url.endsWith('/auth/refresh'))).toHaveLength(1),
    )
    refresh.resolve(json({ data: session('user-a', freshAccessToken) }))
    await Promise.all([first, second])
    late.resolve(json({}, 401))
    await third
    expect(calls.filter((call) => call.url.endsWith('/auth/refresh'))).toHaveLength(1)
    expect(calls.filter((call) => call.token === `Bearer ${freshAccessToken}`)).toHaveLength(3)
    expect(useAuthStore.getState().sessionGeneration).toBe(generation)
  })

  it('does not replay a write when the shared cookie belongs to a new session of the same user', async () => {
    const generation = useAuthStore.getState().sessionGeneration
    const accessToken = browserAccessToken('fresh-token', 'session-b')
    const fetchMock = vi.fn(async (input: RequestInfo | URL) =>
      String(input).endsWith('/auth/refresh')
        ? json({ data: session('user-a', accessToken) })
        : json({}, 401),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(api.post('/synthetic-resource', { value: 'draft' })).rejects.toMatchObject({
      name: 'AbortError',
    })

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(useAuthStore.getState()).toMatchObject({
      accessToken,
      sessionGeneration: generation + 1,
    })
    expect(events).toEqual([])
  })

  it('rejects a decoded result if its originating session changed while the body was read', async () => {
    const body = deferred<unknown>()
    const response = json({ data: 'old-data' })
    const read = vi.spyOn(response, 'json').mockImplementation(() => body.promise)
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => response),
    )
    const request = api.get('/synthetic-resource')
    await vi.waitFor(() => expect(read).toHaveBeenCalledOnce())
    commitAuthResult(session('user-b', 'new-session'))
    body.resolve({ data: 'old-data' })
    await expect(request).rejects.toMatchObject({ name: 'AbortError' })
  })

  it.each(['json', 'text'] as const)(
    'suppresses a stale %s body failure after another login',
    async (format) => {
      const body = deferred<string>()
      const response = new Response('old body', { headers: { 'x-request-id': 'old-request' } })
      const read =
        format === 'json'
          ? vi.spyOn(response, 'json').mockImplementation(() => body.promise)
          : vi.spyOn(response, 'text').mockImplementation(() => body.promise)
      vi.stubGlobal(
        'fetch',
        vi.fn(async () => response),
      )

      const request =
        format === 'json' ? api.get('/synthetic-resource') : api.getText('/synthetic-resource')
      await vi.waitFor(() => expect(read).toHaveBeenCalledOnce())
      commitAuthResult(session('user-b', 'new-session'))
      body.reject(new SyntaxError('old malformed body'))

      await expect(request).rejects.toMatchObject({ name: 'AbortError' })
      expect(events).toEqual([])
      expect(useAuthStore.getState().accessToken).toBe('new-session')
    },
  )

  it('still reports invalid JSON for the current session', async () => {
    const response = new Response('{', { headers: { 'x-request-id': 'current-request' } })
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => response),
    )

    await expect(api.get('/synthetic-resource')).rejects.toMatchObject({
      name: 'ApiError',
      requestId: 'current-request',
    })
    expect(events).toEqual([expect.objectContaining({ requestId: 'current-request' })])
  })

  it('preserves original expiry details and auth signaling after the refresh itself clears the session', async () => {
    const generation = useAuthStore.getState().sessionGeneration
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) =>
        String(input).endsWith('/auth/refresh')
          ? json({ error: { code: 'unauthorized', message: 'refresh rejected' } }, 401)
          : new Response(JSON.stringify({ error: { code: 'token_expired', message: 'expired' } }), {
              status: 401,
              headers: { 'Content-Type': 'application/json', 'x-request-id': 'synthetic-request' },
            }),
      ),
    )
    await expect(api.get('/synthetic-resource')).rejects.toMatchObject({
      status: 401,
      kind: 'auth',
      code: 'token_expired',
      requestId: 'synthetic-request',
    })
    expect(useAuthStore.getState().accessToken).toBeNull()
    expect(useAuthStore.getState().sessionGeneration).toBe(generation + 1)
    expect(events).toEqual([
      expect.objectContaining({ kind: 'auth', requestId: 'synthetic-request' }),
    ])
  })

  it('does not send an old expiry notification after a newer login replaces the refresh-cleared session', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => json({}, 401)),
    )
    const unsubscribe = useAuthStore.subscribe((state) => {
      if (!state.accessToken) commitAuthResult(session('user-b', 'new-session'))
    })
    try {
      await expect(api.get('/synthetic-resource')).rejects.toMatchObject({ name: 'AbortError' })
      expect(useAuthStore.getState().accessToken).toBe('new-session')
      expect(events).toEqual([])
    } finally {
      unsubscribe()
    }
  })

  it('serializes cookie-setting handoff completion with refresh through the shared API transport', async () => {
    const refresh = deferred<Response>()
    const calls: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = String(input)
        calls.push(url)
        return url.endsWith('/auth/refresh')
          ? refresh.promise
          : Promise.resolve(json({ data: { destinationUrl: '/' } }))
      }),
    )
    const restoring = restoreAuthSession()
    await vi.waitFor(() => expect(calls).toHaveLength(1))
    const completion = completeBrowserHandoff('synthetic-handoff')
    await Promise.resolve()
    expect(calls).toHaveLength(1)
    refresh.resolve(json({ data: session('user-a', freshAccessToken) }))
    await restoring
    await completion
    expect(calls.map((url) => new URL(url, 'https://example.test').pathname)).toEqual([
      '/api/v1/auth/refresh',
      '/api/v1/auth/browser-handoffs/synthetic-handoff/complete',
    ])
  })

  it('aborts a stalled handoff response body before a queued login can send', async () => {
    vi.useFakeTimers()
    const lateBody = deferred<ArrayBuffer>()
    const handoff = json({ data: { destinationUrl: '/' } })
    const read = vi.spyOn(handoff, 'arrayBuffer').mockImplementation(() => lateBody.promise)
    const loginResult = session('user-b', 'new-session')
    const fetchMock = vi.fn(async (input: RequestInfo | URL, _options?: RequestInit) =>
      String(input).endsWith('/complete') ? handoff : json({ data: loginResult }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const completion = completeBrowserHandoff('synthetic-handoff')
    const result = expect(completion).rejects.toMatchObject({ name: 'AbortError' })
    await vi.advanceTimersByTimeAsync(0)
    const handoffSignal = fetchMock.mock.calls[0][1]?.signal
    expect(read).toHaveBeenCalledOnce()
    const login = loginWithPassword('synthetic-user', 'synthetic-password')
    try {
      await vi.advanceTimersByTimeAsync(14_999)
      expect(fetchMock).toHaveBeenCalledOnce()
      await vi.advanceTimersByTimeAsync(1)
      expect(handoffSignal?.reason).toMatchObject({ name: 'TimeoutError' })
      await result
      await expect(login).resolves.toEqual(loginResult)
      expect(events).toEqual([])
    } finally {
      lateBody.resolve(new TextEncoder().encode('{"data":{"destinationUrl":"/"}}').buffer)
    }
  })
})
