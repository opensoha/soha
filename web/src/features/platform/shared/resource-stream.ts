import { useEffect, useRef, useState } from 'react'
import type { KubernetesResourceStreamEvent } from '@opensoha/contracts/gen/ts/sohaapi'
import { buildSameOriginStreamURL, withStreamTicket } from '@/features/auth'

export type KubernetesResourceStreamStatus =
  | 'idle'
  | 'connecting'
  | 'live'
  | 'reconnecting'
  | 'polling'
  | 'degraded'

interface KubernetesResourceStreamOptions {
  clusterId?: string | null
  namespace?: string | null
  kinds: string[]
  enabled?: boolean
  fallbackIntervalMs?: number
  onEvent: (event: KubernetesResourceStreamEvent) => void
  onFallback?: () => void
  onResyncRequired?: () => unknown
}

export function useKubernetesResourceStream({
  clusterId,
  namespace,
  kinds,
  enabled = true,
  fallbackIntervalMs = 15_000,
  onEvent,
  onFallback,
  onResyncRequired,
}: KubernetesResourceStreamOptions) {
  const [status, setStatus] = useState<KubernetesResourceStreamStatus>('idle')
  const [lastEventAt, setLastEventAt] = useState<string>()
  const onEventRef = useRef(onEvent)
  const onFallbackRef = useRef(onFallback)
  const onResyncRequiredRef = useRef(onResyncRequired)
  const statusRef = useRef(status)
  const kindsKey = kinds
    .map((kind) => kind.trim())
    .filter(Boolean)
    .sort()
    .join(',')

  onEventRef.current = onEvent
  onFallbackRef.current = onFallback
  onResyncRequiredRef.current = onResyncRequired
  statusRef.current = status

  useEffect(() => {
    const normalizedClusterId = clusterId?.trim()
    if (!enabled || !normalizedClusterId) {
      setStatus('idle')
      return
    }
    let stopped = false
    let socket: WebSocket | undefined
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined
    let attempt = 0

    const connect = async () => {
      setStatus(attempt === 0 ? 'connecting' : 'reconnecting')
      try {
        const url = buildSameOriginStreamURL(
          `/api/v1/clusters/${encodeURIComponent(normalizedClusterId)}/resources/stream`,
          'ws',
        )
        if (namespace?.trim()) url.searchParams.set('namespace', namespace.trim())
        if (kindsKey) url.searchParams.set('kinds', kindsKey)
        const ticketedURL = await withStreamTicket(url)
        if (stopped) return
        socket = new WebSocket(ticketedURL)
        socket.onopen = () => {
          attempt = 0
          setStatus('connecting')
        }
        socket.onmessage = (message) => {
          try {
            const event = JSON.parse(String(message.data)) as KubernetesResourceStreamEvent
            setLastEventAt(event.observedAt)
            if (event.type === 'status') {
              setStatus(
                event.cacheStatus === 'live'
                  ? 'live'
                  : event.cacheStatus === 'warming'
                    ? 'connecting'
                    : 'degraded',
              )
              return
            }
            if (event.type === 'reset' || event.type === 'error' || event.resyncRequired) {
              setStatus('degraded')
              const resync = onResyncRequiredRef.current
              if (!resync) {
                onEventRef.current(event)
                return
              }
              const resyncResult = resync()
              if (event.type !== 'error') {
                void Promise.resolve(resyncResult).then(
                  () => {
                    if (!stopped) setStatus('live')
                  },
                  () => {
                    if (!stopped) setStatus('degraded')
                  },
                )
              }
              return
            }
            onEventRef.current(event)
          } catch {
            setStatus('degraded')
          }
        }
        socket.onerror = () => setStatus('degraded')
        socket.onclose = () => {
          if (stopped) return
          attempt += 1
          setStatus(attempt > 3 ? 'polling' : 'reconnecting')
          reconnectTimer = setTimeout(connect, Math.min(15_000, 1000 * 2 ** Math.min(attempt, 4)))
        }
      } catch {
        if (stopped) return
        attempt += 1
        setStatus(attempt > 3 ? 'polling' : 'reconnecting')
        reconnectTimer = setTimeout(connect, Math.min(15_000, 1000 * 2 ** Math.min(attempt, 4)))
      }
    }

    const fallbackTimer = setInterval(() => {
      if (statusRef.current !== 'live') onFallbackRef.current?.()
    }, fallbackIntervalMs)
    void connect()
    return () => {
      stopped = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      clearInterval(fallbackTimer)
      socket?.close()
    }
  }, [clusterId, enabled, fallbackIntervalMs, kindsKey, namespace])

  return { status, lastEventAt, live: status === 'live' }
}
