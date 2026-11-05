import { useAuthStore } from '@/stores/auth-store'
import type {
  ApiResponse,
  AuthProvider,
  AuthResult,
  ErrorEnvelope,
  LoginOptions,
  PermissionSnapshot,
  StreamTicket,
} from '@/types'

export const API_BASE_URL =
  (import.meta.env.VITE_API_BASE_URL as string | undefined)?.trim() || '/api/v1'

interface AuthFetchOptions extends RequestInit {
  accessToken?: string | null
}

class AuthApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'AuthApiError'
  }
}

export type AuthRefreshStatus = 'authenticated' | 'unauthenticated' | 'unavailable'

function isAuthRejected(error: unknown) {
  return error instanceof AuthApiError && (error.status === 401 || error.status === 403)
}

function buildErrorMessage(body: ErrorEnvelope | unknown | undefined, fallback: string) {
  if (!body) {
    return fallback
  }
  if (typeof body !== 'object') {
    return fallback
  }
  const envelopeError = (body as { error?: unknown }).error
  if (envelopeError && typeof envelopeError === 'object') {
    const message = (envelopeError as { message?: unknown }).message
    if (typeof message === 'string' && message.trim()) {
      return message
    }
  }
  const legacyMessage = (body as { message?: unknown }).message
  if (typeof legacyMessage === 'string' && legacyMessage.trim()) {
    return legacyMessage
  }
  if (typeof envelopeError === 'string' && envelopeError.trim()) {
    return envelopeError
  }
  return fallback
}

async function parseJsonSafely<T>(response: Response): Promise<T | undefined> {
  try {
    return (await response.json()) as T
  } catch {
    return undefined
  }
}

async function fetchAuthJSON<T>(path: string, options: AuthFetchOptions = {}): Promise<T> {
  const headers = new Headers(options.headers)
  if (!headers.has('Content-Type') && options.body && !(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  if (options.accessToken) {
    headers.set('Authorization', `Bearer ${options.accessToken}`)
  }

  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...options,
    credentials: options.credentials ?? 'include',
    headers,
  })

  if (!response.ok) {
    const body = await parseJsonSafely<ErrorEnvelope>(response)
    throw new AuthApiError(response.status, buildErrorMessage(body, response.statusText))
  }

  if (response.status === 204) {
    return undefined as T
  }

  return (await response.json()) as T
}

export function getStoredAccessToken() {
  return useAuthStore.getState().accessToken
}

export function commitAuthResult(authResult: AuthResult) {
  const { setTokens, setUser } = useAuthStore.getState()
  setTokens(authResult.tokens.accessToken)
  setUser(authResult.user)
}

export function clearAuthSession() {
  useAuthStore.getState().clearAuth()
}

export async function logoutAuthSession() {
  try {
    await fetchAuthJSON<{ status?: string }>('/auth/logout', {
      method: 'POST',
      accessToken: getStoredAccessToken(),
    })
  } catch {
    // best-effort: local state must be cleared even when the session is already gone.
  } finally {
    clearAuthSession()
  }
}

export async function fetchAuthProviders() {
  const response = await fetchAuthJSON<ApiResponse<AuthProvider[]> | { items: AuthProvider[] }>(
    '/auth/providers',
  )
  return 'data' in response ? response.data : response.items
}

export async function fetchLoginOptions() {
  const response = await fetchAuthJSON<ApiResponse<LoginOptions>>('/auth/login-options')
  return response.data
}

export async function loginWithPassword(login: string, password: string) {
  const response = await fetchAuthJSON<ApiResponse<AuthResult>>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ login, password }),
  })
  return response.data
}

export async function exchangeOIDCCode(code: string) {
  const response = await fetchAuthJSON<ApiResponse<AuthResult>>('/auth/oidc/exchange', {
    method: 'POST',
    body: JSON.stringify({ code }),
  })
  return response.data
}

export async function fetchPermissionSnapshot() {
  const response = await fetchAuthJSON<ApiResponse<PermissionSnapshot>>(
    '/access/permission-snapshot',
    {
      accessToken: getStoredAccessToken(),
    },
  )
  return response.data
}

export async function restoreAuthSession(): Promise<AuthRefreshStatus> {
  try {
    const response = await fetchAuthJSON<ApiResponse<AuthResult>>('/auth/refresh', {
      method: 'POST',
    })
    commitAuthResult(response.data)
    return 'authenticated'
  } catch (error) {
    if (isAuthRejected(error)) {
      const { accessToken, user } = useAuthStore.getState()
      // An already signed-out browser must not remount the login page on every refresh rejection.
      if (accessToken || user) clearAuthSession()
      return 'unauthenticated'
    }
    return 'unavailable'
  }
}

export async function refreshAuthSession(): Promise<boolean> {
  return (await restoreAuthSession()) === 'authenticated'
}

async function requestStreamTicket(path: string) {
  const response = await fetchAuthJSON<ApiResponse<StreamTicket>>('/auth/stream-ticket', {
    method: 'POST',
    accessToken: getStoredAccessToken(),
    body: JSON.stringify({ path }),
  })
  return response.data
}

export async function issueStreamTicket(path: string) {
  try {
    return await requestStreamTicket(path)
  } catch (error) {
    if (!(error instanceof AuthApiError) || error.status !== 401) {
      throw error
    }
    if (!(await refreshAuthSession())) {
      throw error
    }
    return requestStreamTicket(path)
  }
}
