import { useAuthStore } from '@/stores/auth-store'
import { applyBrandingSettings, normalizeBrandingSettings } from '@/utils/branding'
import type { ApiResponse, AuthResult, BrandingSettings, PermissionSnapshot } from '@/types'

export const API_BASE_URL = import.meta.env.DEV ? 'http://127.0.0.1:8080/api/v1' : '/api/v1'

export interface AuthProvider {
  enabled: boolean
  loginURL?: string
  name: string
  type: string
}

export interface BootstrapResponse {
  currentUser?: API.CurrentUser
  user?: API.CurrentUser
  permissionSnapshot?: PermissionSnapshot
  branding?: BrandingSettings
}

export interface BootstrapState {
  branding: BrandingSettings
  currentUser?: API.CurrentUser
  permissionSnapshot?: PermissionSnapshot
}

interface AuthFetchOptions extends RequestInit {
  accessToken?: string | null
}

interface ErrorEnvelope {
  message?: string
  error?:
    | {
        code?: string
        message?: string
        request_id?: string
      }
    | string
}

export class AuthApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'AuthApiError'
  }
}

function buildErrorMessage(body: ErrorEnvelope | undefined, fallback: string) {
  if (!body) {
    return fallback
  }
  if (typeof body.message === 'string') {
    return body.message
  }
  if (typeof body.error === 'string') {
    return body.error
  }
  return body.error?.message || fallback
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
  setTokens(authResult.tokens.accessToken, authResult.tokens.refreshToken)
  setUser(authResult.user)
}

export function clearAuthSession() {
  useAuthStore.getState().clearAuth()
}

export async function fetchBootstrapState(accessToken?: string | null): Promise<BootstrapState> {
  const result = await fetchAuthJSON<ApiResponse<BootstrapResponse>>('/auth/bootstrap', {
    accessToken,
  })
  const payload = result?.data ?? {}
  const branding = normalizeBrandingSettings(payload.branding)
  applyBrandingSettings(branding)

  return {
    currentUser: payload.currentUser ?? payload.user,
    permissionSnapshot: payload.permissionSnapshot,
    branding,
  }
}

export async function fetchAuthProviders() {
  const response = await fetchAuthJSON<ApiResponse<AuthProvider[]>>('/auth/providers')
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

export async function refreshAuthSession(): Promise<boolean> {
  const { refreshToken } = useAuthStore.getState()
  if (!refreshToken) {
    return false
  }

  try {
    const response = await fetchAuthJSON<ApiResponse<AuthResult>>('/auth/refresh', {
      method: 'POST',
      body: JSON.stringify({ refreshToken }),
    })
    commitAuthResult(response.data)
    return true
  } catch {
    clearAuthSession()
    return false
  }
}
