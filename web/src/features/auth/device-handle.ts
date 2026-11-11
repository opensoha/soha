const storageKey = `soha-cli-device-request:${import.meta.env.BASE_URL}`
const ttl = 10 * 60 * 1000
export interface DeviceHandle {
  value: string
  expiresAt: number
}

// Only a short-lived, tab-local request locator survives the SSO redirect. Neither the
// six-digit code nor a CLI credential goes into URLs, auth persistence or query caches.
export function captureDeviceHandle(now = Date.now()): DeviceHandle | null {
  const params = new URLSearchParams(window.location.hash.slice(1))
  const incoming = params.get('handle')
  const hasFragment = Boolean(window.location.hash)
  if (hasFragment) {
    window.history.replaceState(
      window.history.state,
      '',
      window.location.pathname + window.location.search,
    )
  }
  try {
    if (hasFragment) {
      window.sessionStorage.removeItem(storageKey)
      if (incoming === null || params.size !== 1 || !/^[A-Za-z0-9_-]{43}$/.test(incoming))
        return null
      const handle = { value: incoming, expiresAt: now + ttl }
      window.sessionStorage.setItem(storageKey, JSON.stringify(handle))
      return handle
    }
    const stored: unknown = JSON.parse(window.sessionStorage.getItem(storageKey) || 'null')
    if (
      stored &&
      typeof stored === 'object' &&
      'value' in stored &&
      'expiresAt' in stored &&
      typeof stored.value === 'string' &&
      /^[A-Za-z0-9_-]{43}$/.test(stored.value) &&
      typeof stored.expiresAt === 'number' &&
      stored.expiresAt > now &&
      stored.expiresAt <= now + ttl
    ) {
      return { value: stored.value, expiresAt: stored.expiresAt }
    }
    window.sessionStorage.removeItem(storageKey)
  } catch {
    // Blocked storage still permits this visit; the user must reopen the CLI link after SSO.
    if (incoming && /^[A-Za-z0-9_-]{43}$/.test(incoming) && params.size === 1)
      return { value: incoming, expiresAt: now + ttl }
  }
  return null
}

export function clearDeviceHandle() {
  try {
    window.sessionStorage.removeItem(storageKey)
  } catch {
    /* storage may be disabled */
  }
}
