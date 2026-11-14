/** @vitest-environment jsdom */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearAuthSession, restoreAuthSession } from '@/features/auth/auth-api'
import { usePreferencesStore } from '@/stores/preferences-store'
import { useAuthStore } from '@/stores/auth-store'
import { api } from './api-client'
import { API_ERROR_EVENT, type ApiErrorEventDetail } from './api-error'

vi.mock('@/features/auth/auth-api', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth/auth-api')>(
    '@/features/auth/auth-api',
  )
  return {
    ...actual,
    clearAuthSession: vi.fn(),
    getStoredAccessToken: vi.fn(() => 'expired-token'),
    restoreAuthSession: vi.fn(async () => 'unauthenticated'),
  }
})

function jsonResponse(body: unknown, init: ResponseInit) {
  return new Response(JSON.stringify(body), {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...init.headers,
    },
  })
}

describe('api client error handling', () => {
  const events: ApiErrorEventDetail[] = []
  let apiErrorListener: EventListener

  beforeEach(() => {
    events.length = 0
    apiErrorListener = ((event: CustomEvent<ApiErrorEventDetail>) => {
      events.push(event.detail)
    }) as EventListener
    window.addEventListener(API_ERROR_EVENT, apiErrorListener)
    vi.mocked(clearAuthSession).mockClear()
    vi.mocked(restoreAuthSession).mockClear()
    vi.mocked(restoreAuthSession).mockResolvedValue('unauthenticated')
    usePreferencesStore.setState({ localeCode: 'zh_CN' })
  })

  afterEach(() => {
    window.removeEventListener(API_ERROR_EVENT, apiErrorListener)
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('keeps the browser session when an MFA response is incorrect', async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse(
        { error: { code: 'mfa_verification_failed', message: 'Verification failed' } },
        { status: 401 },
      ),
    )
    vi.stubGlobal('fetch', fetchMock)
    await expect(
      api.post('/identity/mfa/challenges/test/verify', { response: '000000' }),
    ).rejects.toMatchObject({ status: 401, code: 'mfa_verification_failed', kind: 'client' })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(restoreAuthSession).not.toHaveBeenCalled()
    expect(clearAuthSession).not.toHaveBeenCalled()
    expect(events).toEqual([
      expect.objectContaining({ kind: 'client', code: 'mfa_verification_failed' }),
    ])
    expect(JSON.stringify(events)).not.toContain('000000')
  })

  it('accepts an empty successful response only for an explicit void operation', async () => {
    const fetchMock = vi.fn<typeof fetch>(async () => new Response(null, { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    await expect(api.postVoid('/auth/cli/sessions/session-1/revoke')).resolves.toBeUndefined()
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({ method: 'POST' })
  })

  it('refreshes an expired token before returning an unread download response', async () => {
    const archive = new Response('archive-bytes', {
      status: 200,
      headers: { 'content-type': 'application/x-tar', 'x-content-sha256': 'receipt' },
    })
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse({ error: { message: 'expired' } }, { status: 401 }))
      .mockResolvedValueOnce(archive)
    vi.stubGlobal('fetch', fetchMock)
    vi.mocked(restoreAuthSession).mockImplementationOnce(async () => {
      useAuthStore.setState({ accessToken: 'refreshed-token' })
      return 'authenticated'
    })
    const controller = new AbortController()
    const body = { assistantId: 'assistant', environmentVersion: 2 }
    const response = await api.postResponse(
      '/copilot/assistant/environment/export',
      body,
      controller.signal,
    )
    expect(response).toBe(archive)
    expect(response.bodyUsed).toBe(false)
    expect(response.headers.get('x-content-sha256')).toBe('receipt')
    expect(await response.text()).toBe('archive-bytes')
    expect(restoreAuthSession).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls[1]?.[1]).toMatchObject({
      method: 'POST',
      body: JSON.stringify(body),
      signal: controller.signal,
    })
    expect(new Headers(fetchMock.mock.calls[1]?.[1]?.headers).get('Authorization')).toBe(
      'Bearer refreshed-token',
    )
    expect(clearAuthSession).not.toHaveBeenCalled()
  })

  it.each(['unavailable', 'unauthenticated'] as const)(
    'retains shared download error handling when refresh is %s',
    async (status) => {
      vi.mocked(restoreAuthSession).mockResolvedValueOnce(status)
      const fetchMock = vi.fn(async () =>
        jsonResponse({ error: { message: 'expired' } }, { status: 401 }),
      )
      vi.stubGlobal('fetch', fetchMock)
      await expect(
        api.postResponse('/export', {}, new AbortController().signal),
      ).rejects.toMatchObject({
        status: status === 'unavailable' ? 0 : 401,
      })
      expect(fetchMock).toHaveBeenCalledTimes(1)
      expect(restoreAuthSession).toHaveBeenCalledTimes(1)
      expect(clearAuthSession).toHaveBeenCalledTimes(status === 'unavailable' ? 0 : 1)
    },
  )

  it('does not retry or clear authentication when a download is aborted', async () => {
    const aborted = new DOMException('Aborted', 'AbortError')
    const fetchMock = vi.fn(async () => {
      throw aborted
    })
    vi.stubGlobal('fetch', fetchMock)
    await expect(api.postResponse('/export', {}, new AbortController().signal)).rejects.toBe(
      aborted,
    )
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(restoreAuthSession).not.toHaveBeenCalled()
    expect(clearAuthSession).not.toHaveBeenCalled()
  })

  it('reads XML as text while retaining shared authentication and errors', async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response('<EntityDescriptor />', {
          status: 200,
          headers: { 'content-type': 'application/samlmetadata+xml' },
        }),
    )
    vi.stubGlobal('fetch', fetchMock)
    await expect(api.getText('/saml2/idp/provider/metadata')).resolves.toBe('<EntityDescriptor />')
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ error: { code: 'access_denied', message: 'denied' } }, { status: 403 }),
    )
    await expect(api.getText('/saml2/idp/provider/metadata')).rejects.toMatchObject({ status: 403 })
    expect(clearAuthSession).not.toHaveBeenCalled()
  })

  it('preserves contract envelopes for exact SDK consumers', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        jsonResponse({ items: [{ id: 'task-1' }], nextCursor: 'next' }, { status: 200 }),
      ),
    )

    await expect(api.getEnvelope('/compute/tasks')).resolves.toEqual({
      items: [{ id: 'task-1' }],
      nextCursor: 'next',
    })
  })

  it('sends a guarded DELETE body and preserves bodyless calls', async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(null, { status: 204 }),
    )
    vi.stubGlobal('fetch', fetchMock)
    const input = { expectedGeneration: 3, disposition: 'keep' }
    await api.delete('/delivery/template-sources/source-1', input)
    await api.delete('/clusters/cluster-1')
    expect(fetchMock.mock.calls[0][1]).toMatchObject({
      method: 'DELETE',
      body: JSON.stringify(input),
    })
    expect(fetchMock.mock.calls[1][1]?.body).toBeUndefined()
  })

  it('normalizes list envelopes for ordinary API consumers', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse({ items: [{ id: 'event-1' }] }, { status: 200 })),
    )

    await expect(api.get<{ data: Array<{ id: string }> }>('/directory/events')).resolves.toEqual({
      data: [{ id: 'event-1' }],
    })
  })

  it('sends the current interface language with API requests', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) =>
      jsonResponse({ data: {} }, { status: 200 }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await api.get('/clusters')

    const headers = new Headers(fetchMock.mock.calls[0]?.[1]?.headers)
    expect(headers.get('Accept-Language')).toBe('zh-CN')
  })

  it('clears auth and emits a typed event when refresh cannot recover a 401', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        jsonResponse(
          { error: { code: 'token_expired', message: 'token expired', request_id: 'req-401' } },
          {
            status: 401,
            statusText: 'Unauthorized',
            headers: { 'x-request-id': 'req-401' },
          },
        ),
      ),
    )

    await expect(api.get('/clusters')).rejects.toMatchObject({
      code: 'token_expired',
      kind: 'auth',
      message: 'token expired',
      requestId: 'req-401',
      status: 401,
    })

    expect(restoreAuthSession).toHaveBeenCalledTimes(1)
    expect(clearAuthSession).toHaveBeenCalledTimes(1)
    expect(events).toEqual([
      expect.objectContaining({
        code: 'token_expired',
        kind: 'auth',
        method: 'GET',
        path: '/clusters',
        requestId: 'req-401',
        status: 401,
      }),
    ])
  })

  it('keeps auth state when token refresh is temporarily unavailable', async () => {
    vi.mocked(restoreAuthSession).mockResolvedValueOnce('unavailable')
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        jsonResponse(
          { error: { code: 'token_expired', message: 'token expired', request_id: 'req-401' } },
          {
            status: 401,
            statusText: 'Unauthorized',
            headers: { 'x-request-id': 'req-401' },
          },
        ),
      ),
    )

    await expect(api.get('/clusters')).rejects.toMatchObject({
      kind: 'network',
      message: 'Authentication refresh is temporarily unavailable',
      path: '/auth/refresh',
      status: 0,
    })

    expect(restoreAuthSession).toHaveBeenCalledTimes(1)
    expect(clearAuthSession).not.toHaveBeenCalled()
    expect(events).toEqual([
      expect.objectContaining({
        kind: 'network',
        method: 'POST',
        path: '/auth/refresh',
        status: 0,
      }),
    ])
  })

  it('classifies forbidden and server errors for global notification handling', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        jsonResponse(
          { error: { code: 'access_denied', message: 'denied' } },
          {
            status: 403,
            statusText: 'Forbidden',
          },
        ),
      ),
    )

    await expect(api.delete('/clusters/prod')).rejects.toMatchObject({
      code: 'access_denied',
      kind: 'forbidden',
      message: 'denied',
      status: 403,
    })

    expect(events[events.length - 1]).toMatchObject({
      code: 'access_denied',
      kind: 'forbidden',
      method: 'DELETE',
      path: '/clusters/prod',
      status: 403,
    })

    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        jsonResponse(
          { error: { code: 'internal_error', message: 'database unavailable' } },
          {
            status: 503,
            statusText: 'Service Unavailable',
          },
        ),
      ),
    )

    await expect(api.post('/applications', { name: 'demo' })).rejects.toMatchObject({
      code: 'internal_error',
      kind: 'server',
      message: 'database unavailable',
      status: 503,
    })

    expect(events[events.length - 1]).toMatchObject({
      code: 'internal_error',
      kind: 'server',
      method: 'POST',
      path: '/applications',
      status: 503,
    })
  })

  it('normalizes network failures into api errors', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('Failed to fetch')
      }),
    )

    await expect(api.get('/clusters')).rejects.toMatchObject({
      kind: 'network',
      message: 'Failed to fetch',
      status: 0,
    })

    expect(events[events.length - 1]).toMatchObject({
      kind: 'network',
      method: 'GET',
      path: '/clusters',
      status: 0,
    })
  })

  it('keeps legacy error payload compatibility while preferring the contract schema', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        jsonResponse(
          { message: 'legacy message' },
          {
            status: 409,
            statusText: 'Conflict',
          },
        ),
      ),
    )

    await expect(api.put('/applications/app-1', { name: 'demo' })).rejects.toMatchObject({
      code: undefined,
      kind: 'client',
      message: 'legacy message',
      status: 409,
    })

    expect(events[events.length - 1]).toMatchObject({
      code: undefined,
      kind: 'client',
      message: 'legacy message',
      method: 'PUT',
      path: '/applications/app-1',
      status: 409,
    })
  })

  it('rejects successful responses with invalid JSON bodies', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response('<html>not json</html>', {
            status: 200,
            statusText: 'OK',
            headers: {
              'content-type': 'text/html',
              'x-request-id': 'req-json',
            },
          }),
      ),
    )

    await expect(api.get('/clusters')).rejects.toMatchObject({
      kind: 'client',
      message: expect.stringContaining('Invalid JSON response'),
      requestId: 'req-json',
      status: 200,
    })

    expect(events[events.length - 1]).toMatchObject({
      kind: 'client',
      method: 'GET',
      path: '/clusters',
      requestId: 'req-json',
      status: 200,
    })
  })
})
