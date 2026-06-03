import { issueStreamTicket } from '@/features/auth/auth-api'

export function buildSameOriginStreamURL(path: string, protocol: 'http' | 'ws') {
  const scheme = protocol === 'ws'
    ? (window.location.protocol === 'https:' ? 'wss:' : 'ws:')
    : window.location.protocol
  return new URL(path, `${scheme}//${window.location.host}`)
}

export async function withStreamTicket(url: URL | string) {
  const next = typeof url === 'string' ? new URL(url) : new URL(url.toString())
  const ticket = await issueStreamTicket(next.pathname)
  next.searchParams.set('stream_ticket', ticket.ticket)
  return next.toString()
}
