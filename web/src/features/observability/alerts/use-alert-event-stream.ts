import { buildSameOriginStreamURL, withStreamTicket } from '@/features/auth'
import { queryAccessDenied } from '@/utils/query-feedback'
import type { AlertEventStreamSignal } from '@opensoha/contracts/gen/ts/sohaapi'
import { useEffect, useRef, useState } from 'react'

export type AlertEventStreamStatus =
  | 'idle'
  | 'connecting'
  | 'live'
  | 'reconnecting'
  | 'polling'
  | 'degraded'

interface AlertEventStreamOptions {
  clusterId?: string | null
  enabled?: boolean
  fallbackIntervalMs?: number
  onFallback?: () => void
  onSignal: (signal: AlertEventStreamSignal) => void
}

export function useAlertEventStream({
  clusterId,
  enabled = true,
  fallbackIntervalMs = 15_000,
  onFallback,
  onSignal,
}: AlertEventStreamOptions) {
  const [status, setStatus] = useState<AlertEventStreamStatus>('idle')
  const [lastEventAt, setLastEventAt] = useState<string>()
  const [error, setError] = useState<Error | null>(null)
  const [revision, setRevision] = useState(0)
  const onFallbackRef = useRef(onFallback)
  const onSignalRef = useRef(onSignal)
  const statusRef = useRef(status)

  onFallbackRef.current = onFallback
  onSignalRef.current = onSignal
  statusRef.current = status

  useEffect(() => {
    if (!enabled) {
      setStatus('idle')
      setError(null)
      return
    }
    setError(null)
    setLastEventAt(undefined)
    let stopped = false
    let socket: WebSocket | undefined
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined
    let attempt = 0

    const connect = async () => {
      setStatus(attempt === 0 ? 'connecting' : 'reconnecting')
      try {
        const url = buildSameOriginStreamURL('/api/v1/alert-events/stream', 'ws')
        if (clusterId?.trim()) url.searchParams.set('clusterId', clusterId.trim())
        const ticketedURL = await withStreamTicket(url)
        if (stopped) return
        socket = new WebSocket(ticketedURL)
        socket.onopen = () => {
          if (stopped) return
          attempt = 0
          setStatus('connecting')
        }
        socket.onmessage = (message) => {
          if (stopped) return
          try {
            const signal = JSON.parse(String(message.data)) as AlertEventStreamSignal
            setLastEventAt(signal.observedAt)
            setStatus(signal.type === 'error' ? 'degraded' : 'live')
            if (signal.type !== 'status') onSignalRef.current(signal)
          } catch {
            setStatus('degraded')
          }
        }
        socket.onerror = () => {
          if (!stopped) setStatus('degraded')
        }
        socket.onclose = () => {
          if (stopped) return
          attempt += 1
          setStatus(attempt > 3 ? 'polling' : 'reconnecting')
          reconnectTimer = setTimeout(connect, Math.min(15_000, 1000 * 2 ** Math.min(attempt, 4)))
        }
      } catch (error) {
        if (stopped) return
        if (error instanceof Error && queryAccessDenied({ error })) {
          stopped = true
          setError(error)
          setStatus('degraded')
          clearInterval(fallbackTimer)
          return
        }
        attempt += 1
        setStatus(attempt > 3 ? 'polling' : 'reconnecting')
        reconnectTimer = setTimeout(connect, Math.min(15_000, 1000 * 2 ** Math.min(attempt, 4)))
      }
    }

    const fallbackTimer = setInterval(() => {
      if (!stopped && statusRef.current !== 'live') onFallbackRef.current?.()
    }, fallbackIntervalMs)
    void connect()
    return () => {
      stopped = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      clearInterval(fallbackTimer)
      socket?.close()
    }
  }, [clusterId, enabled, fallbackIntervalMs, revision])

  return {
    status,
    lastEventAt,
    live: status === 'live',
    error,
    reconnect: () => setRevision((value) => value + 1),
  }
}
