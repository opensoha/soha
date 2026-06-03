import {
  API_BASE_URL,
  clearAuthSession,
  getStoredAccessToken,
  refreshAuthSession,
} from '@/features/auth/auth-api'
import { useAuthStore } from '@/stores/auth-store'

interface ErrorEnvelope {
  message?: string
  error?: {
    code?: string
    message?: string
    request_id?: string
  } | string
}

function normalizeResponseBody<T>(body: unknown): T {
  if (body && typeof body === 'object' && 'items' in body && !('data' in body)) {
    return { data: (body as { items: unknown }).items } as T
  }
  return body as T
}

class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

async function refreshToken(): Promise<boolean> {
  return refreshAuthSession()
}

async function request<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const accessToken = getStoredAccessToken()

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string>),
  }

  if (accessToken) {
    headers['Authorization'] = `Bearer ${accessToken}`
  }

  let res = await fetch(`${API_BASE_URL}${path}`, {
    ...options,
    credentials: options.credentials ?? 'include',
    headers,
  })

  if (res.status === 401 && accessToken) {
    const refreshed = await refreshToken()
    if (refreshed) {
      const { accessToken: newToken } = useAuthStore.getState()
      headers['Authorization'] = `Bearer ${newToken}`
      res = await fetch(`${API_BASE_URL}${path}`, {
        ...options,
        credentials: options.credentials ?? 'include',
        headers,
      })
    }
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({ message: res.statusText })) as ErrorEnvelope
    const message =
      typeof body.message === 'string'
        ? body.message
        : typeof body.error === 'string'
          ? body.error
          : body.error?.message || res.statusText
    throw new ApiError(res.status, message)
  }

  if (res.status === 204) return undefined as T
  const body = await res.json()
  return normalizeResponseBody<T>(body)
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'POST', body: body ? JSON.stringify(body) : undefined }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PUT', body: body ? JSON.stringify(body) : undefined }),
  patch: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PATCH', body: body ? JSON.stringify(body) : undefined }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
  upload: <T>(path: string, formData: FormData) => {
    const { accessToken } = useAuthStore.getState()
    const headers: Record<string, string> = {}
    if (accessToken) {
      headers['Authorization'] = `Bearer ${accessToken}`
    }
    return fetch(`${API_BASE_URL}${path}`, {
      method: 'POST',
      credentials: 'include',
      headers,
      body: formData,
    }).then(async (res) => {
      if (!res.ok) {
        if (res.status === 401) {
          clearAuthSession()
        }
        const body = await res.json().catch(() => ({ message: res.statusText })) as { message?: string; error?: string | { message?: string } }
        const message = typeof body.message === 'string' ? body.message : typeof body.error === 'string' ? body.error : (body.error as { message?: string })?.message || res.statusText
        throw new ApiError(res.status, message)
      }
      const body = await res.json()
      return normalizeResponseBody<T>(body)
    })
  },
}
