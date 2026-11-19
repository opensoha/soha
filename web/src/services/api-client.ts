import {
  API_BASE_URL,
  authSessionChangePending,
  clearAuthSession,
  getStoredAccessToken,
  refreshRejectionOwner,
  restoreAuthSession,
} from '@/features/auth/auth-api'
import type { AuthRefreshStatus } from '@/features/auth/auth-api'
import {
  assertAuthSession,
  captureAuthSession,
  isCurrentAuthSession,
  withAuthCookieRequest,
  type AuthSessionOwner,
} from '@/features/auth/auth-session'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import type { ErrorEnvelope } from '@/types'
import {
  ApiError,
  createApiErrorFromResponse,
  createNetworkApiError,
  emitApiError,
} from './api-error'

function normalizeResponseBody<T>(body: unknown): T {
  if (body && typeof body === 'object' && 'items' in body && !('data' in body)) {
    return { data: (body as { items: unknown }).items } as T
  }
  return body as T
}

async function refreshToken(owner: AuthSessionOwner): Promise<AuthRefreshStatus> {
  return restoreAuthSession(owner)
}

async function throwRefreshUnavailableError(
  response: Response,
  path: string,
  method: string,
  prefetched?: { body: ErrorEnvelope | undefined },
): Promise<never> {
  const body = prefetched ? prefetched.body : await parseJsonSafely<ErrorEnvelope>(response)
  const rejected = createApiErrorFromResponse(response, body, { method, path })
  const error = new ApiError(
    503,
    usePreferencesStore.getState().localeCode === 'zh_CN'
      ? '登录状态暂时无法恢复，请稍后重试。'
      : 'Your sign-in session could not be restored right now. Please try again later.',
    {
      code: 'auth_refresh_unavailable',
      method,
      path,
      requestId: rejected.requestId,
      requestOutcome: 'rejected',
      cause: rejected,
    },
  )
  emitApiError(error)
  throw error
}

function getRequestMethod(options: RequestInit) {
  return options.method?.toUpperCase() ?? 'GET'
}

function buildRequestHeaders(options: RequestInit, accessToken: string | null) {
  const headers = new Headers(options.headers)
  if (!headers.has('Accept-Language')) {
    headers.set(
      'Accept-Language',
      usePreferencesStore.getState().localeCode === 'zh_CN' ? 'zh-CN' : 'en-US',
    )
  }
  if (!headers.has('Content-Type') && options.body && !(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  if (accessToken) {
    headers.set('Authorization', `Bearer ${accessToken}`)
  }
  return headers
}

async function fetchApi(
  path: string,
  options: RequestInit,
  accessToken: string | null,
  owner: AuthSessionOwner | null = captureAuthSession(),
) {
  const headers = buildRequestHeaders(options, accessToken)
  const method = getRequestMethod(options)

  try {
    const response = await withAuthCookieRequest(path, method, () => {
      if (owner) assertAuthSession(owner)
      return fetch(`${API_BASE_URL}${path}`, {
        ...options,
        credentials: options.credentials ?? 'include',
        headers,
      })
    })
    if (owner) assertAuthSession(owner, response)
    return response
  } catch (cause) {
    if (owner) assertAuthSession(owner)
    if (cause instanceof DOMException && cause.name === 'AbortError') throw cause
    const error = createNetworkApiError(path, method, cause)
    emitApiError(error)
    throw error
  }
}

async function parseJsonSafely<T>(response: Response): Promise<T | undefined> {
  try {
    return (await response.json()) as T
  } catch {
    return undefined
  }
}

function responseRequestId(response: Response) {
  return (
    response.headers.get('x-request-id') ||
    response.headers.get('x-correlation-id') ||
    response.headers.get('x-trace-id') ||
    undefined
  )
}

async function parseJsonStrictly<T>(
  response: Response,
  path: string,
  method: string,
  owner: AuthSessionOwner,
): Promise<T> {
  try {
    return (await response.json()) as T
  } catch (cause) {
    assertAuthSession(owner)
    const reason = cause instanceof Error && cause.message ? `: ${cause.message}` : ''
    const error = new ApiError(response.status, `Invalid JSON response${reason}`, {
      cause,
      method,
      path,
      requestId: responseRequestId(response),
    })
    emitApiError(error)
    throw error
  }
}

async function throwApiError(
  response: Response,
  path: string,
  method: string,
  owner: AuthSessionOwner,
  prefetched?: { body: ErrorEnvelope | undefined },
  clearSession = true,
): Promise<never> {
  const body = prefetched ? prefetched.body : await parseJsonSafely<ErrorEnvelope>(response)
  assertAuthSession(owner)
  const error = createApiErrorFromResponse(response, body, { method, path })
  if (error.kind === 'auth' && clearSession) {
    clearAuthSession()
  }
  emitApiError(error)
  throw error
}

async function requestResponse(
  path: string,
  options: RequestInit,
  owner = captureAuthSession(),
): Promise<Response> {
  const accessToken = getStoredAccessToken()
  const method = getRequestMethod(options)

  let res = await fetchApi(path, options, accessToken, owner)

  let rejected: { body: ErrorEnvelope | undefined } | undefined
  if (res.status === 401) rejected = { body: await parseJsonSafely<ErrorEnvelope>(res) }
  assertAuthSession(owner)
  if (res.status === 401 && authSessionChangePending(owner))
    throw new DOMException(
      'Authentication is changing; this request was not replayed.',
      'AbortError',
    )
  if (
    res.status === 401 &&
    accessToken &&
    rejected?.body?.error?.code !== 'mfa_verification_failed'
  ) {
    const refreshStatus =
      useAuthStore.getState().accessToken !== accessToken
        ? 'authenticated'
        : await refreshToken(owner)
    const clearedOwner =
      refreshStatus === 'unauthenticated' ? refreshRejectionOwner(owner) : undefined
    if (clearedOwner) await throwApiError(res, path, method, clearedOwner, rejected, false)
    assertAuthSession(owner)
    if (refreshStatus === 'authenticated') {
      const { accessToken: newToken } = useAuthStore.getState()
      res = await fetchApi(path, options, newToken, owner)
      rejected = undefined
    } else if (refreshStatus === 'unavailable') {
      await throwRefreshUnavailableError(res, path, method, rejected)
    }
  }

  if (!res.ok) {
    await throwApiError(res, path, method, owner, rejected)
  }
  return res
}

async function request<T>(
  path: string,
  options: RequestInit = {},
  normalizeBody = true,
  responseFormat: 'json' | 'text' = 'json',
): Promise<T> {
  const owner = captureAuthSession()
  const res = await requestResponse(path, options, owner)
  if (res.status === 204) return undefined as T
  if (responseFormat === 'text') {
    const body = await res.text().catch((cause: unknown) => {
      assertAuthSession(owner)
      throw cause
    })
    assertAuthSession(owner)
    return body as T
  }
  const body = await parseJsonStrictly<unknown>(res, path, getRequestMethod(options), owner)
  assertAuthSession(owner)
  return normalizeBody ? normalizeResponseBody<T>(body) : (body as T)
}

// Sensitive managed transfers capture one owner's token. They never replay a
// body after refresh, mix it with a newer cookie or return another user's data.
async function scopedRequest<T>(
  path: string,
  expectedUserId: string,
  options: { method?: 'GET' | 'POST' | 'PUT'; body?: unknown } = {},
): Promise<T> {
  const accessToken = getStoredAccessToken()
  const owner = captureAuthSession()
  const method = options.method ?? 'GET'
  const current = () =>
    isCurrentAuthSession(owner) && useAuthStore.getState().user?.userId === expectedUserId
  const changed = () =>
    new ApiError(
      method === 'GET' ? 403 : 503,
      'Authentication context changed; this request was not replayed. Inspect its original request ID.',
      { method, path },
    )
  if (!expectedUserId || !accessToken || !current())
    throw new ApiError(
      403,
      'Authentication context changed before submission; no content was sent.',
      { method, path },
    )
  const response = await fetchApi(
    path,
    {
      method,
      credentials: 'omit',
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    },
    accessToken,
    null,
  )
  if (
    !current() ||
    (response.status === 401 && useAuthStore.getState().accessToken !== accessToken)
  ) {
    await response.body?.cancel().catch(() => undefined)
    throw changed()
  }
  if (!response.ok) {
    const errorBody = await parseJsonSafely<ErrorEnvelope>(response)
    if (
      !current() ||
      (response.status === 401 && useAuthStore.getState().accessToken !== accessToken)
    )
      throw changed()
    const error = createApiErrorFromResponse(response, errorBody, { method, path })
    if (response.status === 401) clearAuthSession()
    emitApiError(error)
    throw error
  }
  const body = await parseJsonSafely<unknown>(response)
  if (!current()) throw changed()
  if (!body || typeof body !== 'object' || !('data' in body) || body.data === null) {
    throw new ApiError(502, 'Invalid managed response; inspect the original request record.', {
      method,
      path,
    })
  }
  return normalizeResponseBody<T>(body)
}

export const api = {
  scoped: scopedRequest,
  serviceScope: () => new URL(API_BASE_URL, window.location.origin).toString(),
  postResponse: (path: string, body: unknown, signal: AbortSignal) =>
    requestResponse(path, { method: 'POST', body: JSON.stringify(body), signal }),
  getText: (path: string) => request<string>(path, {}, false, 'text'),
  get: <T>(path: string, options?: Pick<RequestInit, 'signal'>) => request<T>(path, options),
  getEnvelope: <T>(path: string) => request<T>(path, {}, false),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: 'POST',
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  postVoid: async (path: string, body?: unknown): Promise<void> => {
    await requestResponse(path, {
      method: 'POST',
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  },
  postWithSignal: <T>(path: string, body: unknown, signal: AbortSignal) =>
    request<T>(path, {
      method: 'POST',
      body: JSON.stringify(body),
      signal,
    }),
  postWithHeaders: <T>(path: string, body: unknown, headers: HeadersInit) =>
    request<T>(path, {
      method: 'POST',
      body: JSON.stringify(body),
      headers,
    }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: 'PUT',
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  patch: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: 'PATCH',
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  delete: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: 'DELETE',
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  upload: <T>(path: string, formData: FormData) =>
    request<T>(path, {
      method: 'POST',
      body: formData,
    }),
}

export { ApiError }
