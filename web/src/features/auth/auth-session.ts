import { useAuthStore } from '@/stores/auth-store'

let revision = 0
let cookieRequests: Promise<unknown> = Promise.resolve()

export function captureAuthSession() {
  return { generation: useAuthStore.getState().sessionGeneration, revision }
}

export type AuthSessionOwner = ReturnType<typeof captureAuthSession>

export function advanceAuthSession() {
  revision += 1
  return captureAuthSession()
}

export function isCurrentAuthSession(owner: AuthSessionOwner) {
  return (
    owner.revision === revision && owner.generation === useAuthStore.getState().sessionGeneration
  )
}

export function assertAuthSession(owner: AuthSessionOwner, response?: Response) {
  if (!isCurrentAuthSession(owner)) {
    void response?.body?.cancel().catch(() => undefined)
    throw new DOMException(
      'Authentication context changed; this request was not replayed.',
      'AbortError',
    )
  }
}

export function withAuthCookieRequest<T>(path: string, method: string, send: () => Promise<T>) {
  const pathname = path.split('?')[0]
  if (
    method.toUpperCase() !== 'POST' ||
    !(
      ['/auth/login', '/auth/logout', '/auth/refresh', '/auth/oidc/exchange'].includes(pathname) ||
      /^\/auth\/desktop\/attempts\/[^/]+\/exchange$/.test(pathname) ||
      /^\/auth\/browser-handoffs\/[^/]+\/complete$/.test(pathname)
    )
  )
    return send()

  // Set-Cookie applies before JavaScript can reject a stale response.
  const request = cookieRequests.then(send, send)
  cookieRequests = request.then(
    () => undefined,
    () => undefined,
  )
  return request
}
