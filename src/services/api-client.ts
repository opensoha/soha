import {
  API_BASE_URL,
  clearAuthSession,
  getStoredAccessToken,
  restoreAuthSession,
} from '@/features/auth/auth-api'
import type { AuthRefreshStatus } from '@/features/auth/auth-api'
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

async function refreshToken(): Promise<AuthRefreshStatus> {
  return restoreAuthSession()
}

function throwRefreshUnavailableError(): never {
  const error = createNetworkApiError(
    '/auth/refresh',
    'POST',
    new Error('Authentication refresh is temporarily unavailable'),
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

async function fetchApi(path: string, options: RequestInit, accessToken: string | null) {
  const headers = buildRequestHeaders(options, accessToken)
  const method = getRequestMethod(options)

  try {
    return await fetch(`${API_BASE_URL}${path}`, {
      ...options,
      credentials: options.credentials ?? 'include',
      headers,
    })
  } catch (cause) {
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

async function parseJsonStrictly<T>(response: Response, path: string, method: string): Promise<T> {
  try {
    return (await response.json()) as T
  } catch (cause) {
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

async function throwApiError(response: Response, path: string, method: string): Promise<never> {
  const body = await parseJsonSafely<ErrorEnvelope>(response)
  const error = createApiErrorFromResponse(response, body, { method, path })
  if (error.status === 401) {
    clearAuthSession()
  }
  emitApiError(error)
  throw error
}

async function requestResponse(path: string, options: RequestInit): Promise<Response> {
  const accessToken = getStoredAccessToken()
  const method = getRequestMethod(options)

  let res = await fetchApi(path, options, accessToken)

  if (res.status === 401 && accessToken) {
    const refreshStatus = await refreshToken()
    if (refreshStatus === 'authenticated') {
      const { accessToken: newToken } = useAuthStore.getState()
      res = await fetchApi(path, options, newToken)
    } else if (refreshStatus === 'unavailable') {
      throwRefreshUnavailableError()
    }
  }

  if (!res.ok) {
    await throwApiError(res, path, method)
  }
  return res
}

async function request<T>(
  path: string,
  options: RequestInit = {},
  normalizeBody = true,
  responseFormat: 'json' | 'text' = 'json',
): Promise<T> {
  const res = await requestResponse(path, options)
  if (res.status === 204) return undefined as T
  if (responseFormat === 'text') return (await res.text()) as T
  const body = await parseJsonStrictly<unknown>(res, path, getRequestMethod(options))
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
  const method = options.method ?? 'GET'
  const current = () => useAuthStore.getState().user?.userId === expectedUserId
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
  upload: async <T>(path: string, formData: FormData) => {
    const { accessToken } = useAuthStore.getState()
    const options: RequestInit = {
      method: 'POST',
      body: formData,
    }

    let res = await fetchApi(path, options, accessToken)
    if (res.status === 401 && accessToken) {
      const refreshStatus = await refreshToken()
      if (refreshStatus === 'authenticated') {
        const { accessToken: newToken } = useAuthStore.getState()
        res = await fetchApi(path, options, newToken)
      } else if (refreshStatus === 'unavailable') {
        throwRefreshUnavailableError()
      }
    }

    if (!res.ok) {
      await throwApiError(res, path, 'POST')
    }

    if (res.status === 204) return undefined as T
    const body = await parseJsonStrictly<unknown>(res, path, 'POST')
    return normalizeResponseBody<T>(body)
  },
}

export { ApiError }
