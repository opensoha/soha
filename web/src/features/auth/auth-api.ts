import { useAuthStore } from '@/stores/auth-store'
import {
  advanceAuthSession,
  assertAuthSession,
  captureAuthSession,
  isCurrentAuthSession,
  withAuthCookieRequest,
  type AuthSessionOwner,
} from './auth-session'
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
  (import.meta.env.VITE_API_BASE_URL as string | undefined)?.trim() ||
  `${import.meta.env.BASE_URL}api/v1`

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

export type AuthRefreshStatus = 'authenticated' | 'unauthenticated' | 'unavailable' | 'superseded'

let pendingSessionChange: AuthSessionOwner | undefined
let refreshInFlight: { owner: AuthSessionOwner; promise: Promise<AuthRefreshStatus> } | undefined
let rejectedRefresh: { owner: AuthSessionOwner; clearedOwner: AuthSessionOwner } | undefined

export function authSessionChangePending(owner: AuthSessionOwner) {
  return (
    pendingSessionChange?.generation === owner.generation &&
    pendingSessionChange.revision === owner.revision
  )
}

export function refreshRejectionOwner(owner: AuthSessionOwner) {
  return rejectedRefresh?.owner.generation === owner.generation &&
    rejectedRefresh.owner.revision === owner.revision &&
    isCurrentAuthSession(rejectedRefresh.clearedOwner)
    ? rejectedRefresh.clearedOwner
    : undefined
}

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

async function fetchAuthJSON<T>(
  path: string,
  options: AuthFetchOptions = {},
  owner: AuthSessionOwner | null = captureAuthSession(),
): Promise<T> {
  const headers = new Headers(options.headers)
  if (!headers.has('Content-Type') && options.body && !(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  if (options.accessToken) {
    headers.set('Authorization', `Bearer ${options.accessToken}`)
  }

  const response = await withAuthCookieRequest(path, options.method ?? 'GET', () => {
    if (owner) assertAuthSession(owner)
    return fetch(`${API_BASE_URL}${path}`, {
      ...options,
      credentials: options.credentials ?? 'include',
      headers,
    })
  })
  if (owner) assertAuthSession(owner, response)

  if (!response.ok) {
    const body = await parseJsonSafely<ErrorEnvelope>(response)
    if (owner) assertAuthSession(owner)
    throw new AuthApiError(response.status, buildErrorMessage(body, response.statusText))
  }

  if (response.status === 204) {
    return undefined as T
  }

  const body = (await response.json()) as T
  if (owner) assertAuthSession(owner)
  return body
}

export function getStoredAccessToken() {
  return useAuthStore.getState().accessToken
}

export function commitAuthResult(authResult: AuthResult) {
  advanceAuthSession()
  useAuthStore.setState((state) => ({
    accessToken: authResult.tokens.accessToken,
    user: authResult.user,
    sessionGeneration: state.sessionGeneration + 1,
  }))
}

export function clearAuthSession() {
  const owner = advanceAuthSession()
  const clearedOwner = { ...owner, generation: owner.generation + 1 }
  useAuthStore.getState().clearAuth()
  return clearedOwner
}

export async function logoutAuthSession() {
  const accessToken = getStoredAccessToken()
  const response = fetchAuthJSON<{ status?: string }>(
    '/auth/logout',
    { method: 'POST', accessToken },
    null,
  )
  const owner = clearAuthSession()
  if (isCurrentAuthSession(owner)) pendingSessionChange = owner
  try {
    await response
  } catch {
    // best-effort: local state must be cleared even when the session is already gone.
  } finally {
    if (pendingSessionChange === owner) pendingSessionChange = undefined
  }
}

async function requestNewSession(path: string, body: unknown) {
  const owner = advanceAuthSession()
  pendingSessionChange = owner
  try {
    const response = await fetchAuthJSON<ApiResponse<AuthResult>>(
      path,
      { method: 'POST', body: JSON.stringify(body) },
      owner,
    )
    assertAuthSession(owner)
    commitAuthResult(response.data)
    return response.data
  } finally {
    if (pendingSessionChange === owner) pendingSessionChange = undefined
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
  return requestNewSession('/auth/login', { login, password })
}

export async function exchangeOIDCCode(code: string) {
  return requestNewSession('/auth/oidc/exchange', { code })
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

async function restoreOwnedSession(owner: AuthSessionOwner): Promise<AuthRefreshStatus> {
  try {
    const response = await fetchAuthJSON<ApiResponse<AuthResult>>(
      '/auth/refresh',
      { method: 'POST' },
      owner,
    )
    assertAuthSession(owner)
    const result = response.data
    useAuthStore.setState((state) => ({
      accessToken: result.tokens.accessToken,
      user: result.user,
      sessionGeneration:
        !state.accessToken || state.user?.userId !== result.user.userId
          ? state.sessionGeneration + 1
          : state.sessionGeneration,
    }))
    return 'authenticated'
  } catch (error) {
    if (!isCurrentAuthSession(owner)) return 'superseded'
    if (isAuthRejected(error)) {
      const { accessToken, user } = useAuthStore.getState()
      // An already signed-out browser must not remount the login page on every refresh rejection.
      const clearedOwner = accessToken || user ? clearAuthSession() : owner
      rejectedRefresh = { owner, clearedOwner }
      return 'unauthenticated'
    }
    return 'unavailable'
  }
}

export function restoreAuthSession(owner = captureAuthSession()): Promise<AuthRefreshStatus> {
  if (!isCurrentAuthSession(owner) || authSessionChangePending(owner))
    return Promise.resolve('superseded')
  if (
    refreshInFlight?.owner.generation === owner.generation &&
    refreshInFlight.owner.revision === owner.revision
  )
    return refreshInFlight.promise
  const promise = restoreOwnedSession(owner).finally(() => {
    if (refreshInFlight?.promise === promise) refreshInFlight = undefined
  })
  refreshInFlight = { owner, promise }
  return promise
}

export async function refreshAuthSession(owner = captureAuthSession()): Promise<boolean> {
  return (await restoreAuthSession(owner)) === 'authenticated'
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
  const owner = captureAuthSession()
  try {
    const ticket = await requestStreamTicket(path)
    assertAuthSession(owner)
    return ticket
  } catch (error) {
    assertAuthSession(owner)
    if (!(error instanceof AuthApiError) || error.status !== 401) {
      throw error
    }
    if (!(await refreshAuthSession(owner))) {
      throw error
    }
    assertAuthSession(owner)
    const ticket = await requestStreamTicket(path)
    assertAuthSession(owner)
    return ticket
  }
}
