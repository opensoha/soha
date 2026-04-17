import { useAuthStore } from '@/stores/auth-store'

const BASE_URL = import.meta.env.DEV ? 'http://127.0.0.1:8080/api/v1' : '/api/v1'

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
  const { refreshToken: token, setTokens, setUser, clearAuth } = useAuthStore.getState()
  if (!token) return false

  try {
    const res = await fetch(`${BASE_URL}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refreshToken: token }),
    })
    if (!res.ok) {
      clearAuth()
      return false
    }
    const body = await res.json()
    const tokens = body?.data?.tokens
    if (!tokens?.accessToken || !tokens?.refreshToken) {
      clearAuth()
      return false
    }
    setTokens(tokens.accessToken, tokens.refreshToken)
    if (body?.data?.user) {
      setUser(body.data.user)
    }
    return true
  } catch {
    clearAuth()
    return false
  }
}

async function request<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const { accessToken } = useAuthStore.getState()

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string>),
  }

  if (accessToken) {
    headers['Authorization'] = `Bearer ${accessToken}`
  }

  let res = await fetch(`${BASE_URL}${path}`, { ...options, headers })

  if (res.status === 401 && accessToken) {
    const refreshed = await refreshToken()
    if (refreshed) {
      const { accessToken: newToken } = useAuthStore.getState()
      headers['Authorization'] = `Bearer ${newToken}`
      res = await fetch(`${BASE_URL}${path}`, { ...options, headers })
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
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
  upload: <T>(path: string, formData: FormData) => {
    const { accessToken } = useAuthStore.getState()
    const headers: Record<string, string> = {}
    if (accessToken) {
      headers['Authorization'] = `Bearer ${accessToken}`
    }
    return fetch(`${BASE_URL}${path}`, {
      method: 'POST',
      headers,
      body: formData,
    }).then(async (res) => {
      if (!res.ok) {
        const body = await res.json().catch(() => ({ message: res.statusText })) as { message?: string; error?: string | { message?: string } }
        const message = typeof body.message === 'string' ? body.message : typeof body.error === 'string' ? body.error : (body.error as { message?: string })?.message || res.statusText
        throw new ApiError(res.status, message)
      }
      const body = await res.json()
      return normalizeResponseBody<T>(body)
    })
  },
}
