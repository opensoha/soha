import { useAuthStore } from '@/stores/auth-store'

const COOKIE_REQUEST_TIMEOUT_MS = 15_000

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

export function withAuthCookieRequest<T>(
  path: string,
  method: string,
  send: (signal?: AbortSignal) => Promise<T>,
  signal?: AbortSignal | null,
) {
  const pathname = path.split('?')[0]
  if (
    method.toUpperCase() !== 'POST' ||
    !(
      ['/auth/login', '/auth/logout', '/auth/refresh', '/auth/oidc/exchange'].includes(pathname) ||
      pathname === '/auth/refresh/application-workbench' ||
      /^\/auth\/desktop\/attempts\/[^/]+\/exchange$/.test(pathname) ||
      /^\/auth\/browser-handoffs\/[^/]+\/complete$/.test(pathname)
    )
  )
    return send()

  // Set-Cookie applies before JavaScript can reject a stale response.
  const request = cookieRequests.then(async () => {
    signal?.throwIfAborted()
    const controller = new AbortController()
    let cancel!: (reason: unknown) => void
    const interrupted = new Promise<never>((_, reject) => {
      cancel = (reason) => {
        controller.abort(reason)
        reject(reason)
      }
    })
    const onAbort = () => cancel(signal?.reason)
    signal?.addEventListener('abort', onAbort, { once: true })
    const timer = setTimeout(
      () => cancel(new DOMException('The authentication request timed out.', 'TimeoutError')),
      COOKIE_REQUEST_TIMEOUT_MS,
    )
    try {
      return await Promise.race([send(controller.signal), interrupted])
    } finally {
      clearTimeout(timer)
      signal?.removeEventListener('abort', onAbort)
    }
  })
  cookieRequests = request.then(
    () => undefined,
    () => undefined,
  )
  return request
}
