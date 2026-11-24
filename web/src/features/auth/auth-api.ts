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
let loginInFlight: { owner: AuthSessionOwner; result: Promise<AuthResult> } | undefined
let refreshInFlight:
  | { owner: AuthSessionOwner; result: Promise<AuthResult>; promise: Promise<AuthRefreshStatus> }
  | undefined
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

function sendAuthRequest(
  path: string,
  options: AuthFetchOptions,
  owner: AuthSessionOwner | null,
): Promise<Response> {
  const headers = new Headers(options.headers)
  if (!headers.has('Content-Type') && options.body && !(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  if (options.accessToken) {
    headers.set('Authorization', `Bearer ${options.accessToken}`)
  }

  if (owner) assertAuthSession(owner)
  options.signal?.throwIfAborted()
  return fetch(`${API_BASE_URL}${path}`, {
    ...options,
    credentials: options.credentials ?? 'include',
    headers,
  })
}

async function readAuthJSON<T>(response: Response, owner: AuthSessionOwner | null): Promise<T> {
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

async function fetchAuthJSON<T>(
  path: string,
  options: AuthFetchOptions = {},
  owner: AuthSessionOwner | null = captureAuthSession(),
): Promise<T> {
  return withAuthCookieRequest(
    path,
    options.method ?? 'GET',
    async (signal) => {
      const response = await sendAuthRequest(path, { ...options, signal: signal ?? options.signal }, owner)
      return readAuthJSON<T>(response, owner)
    },
    options.signal,
  )
}

function requestAuthResult(path: string, options: AuthFetchOptions, owner: AuthSessionOwner) {
  return withAuthCookieRequest(path, options.method ?? 'POST', async (signal) => {
    const response = await sendAuthRequest(path, { ...options, signal }, owner)
    // A queued logout may need these credentials even after the local owner is signed out.
    return (await readAuthJSON<ApiResponse<AuthResult>>(response, null)).data
  })
}

function accessTokenSessionId(token: string): string | undefined {
  const parts = token.split('.')
  const payload = parts[1]
  if (parts.length !== 3 || !parts[0] || !payload || !parts[2]) return
  try {
    const bytes = Uint8Array.from(atob(payload.replace(/-/g, '+').replace(/_/g, '/')), (byte) =>
      byte.charCodeAt(0),
    )
    const { sid } = JSON.parse(new TextDecoder().decode(bytes)) as { sid?: unknown }
    return typeof sid === 'string' && sid ? sid : undefined
  } catch {
    return undefined
  }
}

function isSameBrowserSession(
  accessToken: string | null,
  userId: string | undefined,
  result: AuthResult,
) {
  if (!accessToken || userId !== result.user.userId) return false
  if (accessToken === result.tokens.accessToken) return true
  // Core validates the token. The signed sid correlates its auth responses across token rotation.
  const sessionId = accessTokenSessionId(accessToken)
  return Boolean(sessionId && sessionId === accessTokenSessionId(result.tokens.accessToken))
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
  const originalOwner = captureAuthSession()
  const accessToken = getStoredAccessToken()
  const userId = useAuthStore.getState().user?.userId
  const sameOwner = (candidate: AuthSessionOwner) =>
    candidate.generation === originalOwner.generation &&
    candidate.revision === originalOwner.revision
  const preceding =
    loginInFlight && sameOwner(loginInFlight.owner)
      ? { result: loginInFlight.result, login: true }
      : refreshInFlight && sameOwner(refreshInFlight.owner)
        ? { result: refreshInFlight.result, login: false }
        : undefined
  const response = withAuthCookieRequest('/auth/logout', 'POST', (signal) =>
    revokeBrowserSession(accessToken, userId, preceding, signal),
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

async function revokeBrowserSession(
  capturedToken: string | null,
  userId: string | undefined,
  preceding: { result: Promise<AuthResult>; login: boolean } | undefined,
  signal?: AbortSignal,
) {
  const refreshed = await preceding?.result.catch(() => undefined)
  const ownedResult =
    refreshed && (preceding?.login || isSameBrowserSession(capturedToken, userId, refreshed))
      ? refreshed
      : undefined
  const sendLogout = async (accessToken: string, refreshToken?: string) =>
    readAuthJSON(
      await sendAuthRequest(
        '/auth/logout',
        {
          method: 'POST',
          // Core can revoke both supplied credentials. An ambient cookie may belong to another tab's session.
          credentials: 'omit',
          accessToken,
          body: refreshToken ? JSON.stringify({ refreshToken }) : undefined,
          signal,
        },
        null,
      ),
      null,
    )
  const accessToken = ownedResult?.tokens.accessToken ?? capturedToken
  if (!accessToken) return
  try {
    await sendLogout(accessToken, ownedResult?.tokens.refreshToken)
    return
  } catch (error) {
    if (!(error instanceof AuthApiError) || error.status !== 401) throw error
  }
  if (refreshed && !ownedResult) return
  // Already inside the logout cookie slot: recovery must not enqueue behind itself.
  const response = await readAuthJSON<ApiResponse<AuthResult>>(
    await sendAuthRequest('/auth/refresh', { method: 'POST', signal }, null),
    null,
  )
  if (isSameBrowserSession(accessToken, ownedResult?.user.userId ?? userId, response.data)) {
    await sendLogout(response.data.tokens.accessToken, response.data.tokens.refreshToken)
  }
}

async function requestNewSession(path: string, body: unknown) {
  const owner = advanceAuthSession()
  pendingSessionChange = owner
  const result = requestAuthResult(path, { method: 'POST', body: JSON.stringify(body) }, owner)
  const request = { owner, result }
  loginInFlight = request
  try {
    const authResult = await result
    assertAuthSession(owner)
    commitAuthResult(authResult)
    return authResult
  } catch (error) {
    assertAuthSession(owner)
    throw error
  } finally {
    if (pendingSessionChange === owner) pendingSessionChange = undefined
    if (loginInFlight === request) loginInFlight = undefined
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

async function restoreOwnedSession(
  owner: AuthSessionOwner,
  refreshResult: Promise<AuthResult>,
): Promise<AuthRefreshStatus> {
  try {
    const result = await refreshResult
    assertAuthSession(owner)
    useAuthStore.setState((state) => ({
      accessToken: result.tokens.accessToken,
      user: result.user,
      sessionGeneration: isSameBrowserSession(state.accessToken, state.user?.userId, result)
        ? state.sessionGeneration
        : state.sessionGeneration + 1,
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
  const result = requestAuthResult('/auth/refresh', { method: 'POST' }, owner)
  const promise = restoreOwnedSession(owner, result).finally(() => {
    if (refreshInFlight?.promise === promise) refreshInFlight = undefined
  })
  refreshInFlight = { owner, result, promise }
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
