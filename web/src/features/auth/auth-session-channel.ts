const AUTH_SESSION_CHANNEL = 'soha.auth.session'

interface AuthSessionMessage {
  type: 'authenticated'
}

function openChannel() {
  if (typeof BroadcastChannel === 'undefined') return null
  try {
    return new BroadcastChannel(AUTH_SESSION_CHANNEL)
  } catch {
    return null
  }
}

export function publishAuthSessionAvailable() {
  const channel = openChannel()
  if (!channel) return
  channel.postMessage({ type: 'authenticated' } satisfies AuthSessionMessage)
  channel.close()
}

export function subscribeAuthSessionAvailable(callback: () => void) {
  const channel = openChannel()
  if (!channel) return () => undefined
  const listener = (event: MessageEvent<AuthSessionMessage>) => {
    if (event.data?.type === 'authenticated') callback()
  }
  channel.addEventListener('message', listener)
  return () => {
    channel.removeEventListener('message', listener)
    channel.close()
  }
}
