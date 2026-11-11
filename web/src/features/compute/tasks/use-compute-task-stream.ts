import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type {
  ComputeTaskDomain,
  ComputeTaskEnvelope,
  ComputeTaskStatus,
  ComputeTaskStreamEvent,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { buildSameOriginStreamURL, withStreamTicket } from '@/features/auth'
import { taskPath } from '../api'
import { computeKeys } from '../keys'

export type ComputeTaskStreamStatus =
  | 'idle'
  | 'connecting'
  | 'live'
  | 'reconnecting'
  | 'polling'
  | 'degraded'
  | 'done'

const terminalStatuses = new Set<ComputeTaskStatus>(['succeeded', 'failed', 'canceled', 'timeout'])

export function useComputeTaskStream({
  domain,
  taskId,
  enabled = true,
}: {
  domain?: ComputeTaskDomain
  taskId?: string
  enabled?: boolean
}) {
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<ComputeTaskStreamStatus>('idle')
  const [lastEventAt, setLastEventAt] = useState<string>()
  const statusRef = useRef(status)
  statusRef.current = status

  useEffect(() => {
    if (!enabled || !domain || !taskId) {
      setStatus('idle')
      setLastEventAt(undefined)
      return
    }

    let disposed = false
    let source: EventSource | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined
    let failures = 0

    const refresh = () => {
      void queryClient.invalidateQueries({ queryKey: computeKeys.task(domain, taskId) })
      void queryClient.invalidateQueries({ queryKey: computeKeys.taskLogs(domain, taskId) })
      void queryClient.invalidateQueries({ queryKey: computeKeys.tasks() })
      void queryClient.invalidateQueries({ queryKey: computeKeys.overview() })
    }

    const connect = async () => {
      if (disposed) return
      setStatus(failures > 0 ? 'reconnecting' : 'connecting')
      try {
        const baseURL = buildSameOriginStreamURL(
          `/api/v1${taskPath(domain, taskId)}/stream`,
          'http',
        )
        const url = await withStreamTicket(baseURL)
        if (disposed) return
        source = new EventSource(url)
        source.onopen = () => !disposed && setStatus('live')
        source.onmessage = (message) => {
          if (disposed) return
          try {
            const event = JSON.parse(message.data) as ComputeTaskStreamEvent
            failures = 0
            setLastEventAt(event.observedAt)
            if (event.task) {
              queryClient.setQueryData<ComputeTaskEnvelope>(computeKeys.task(domain, taskId), {
                data: event.task,
              })
            }
            if (
              event.type === 'terminal' ||
              terminalStatuses.has(event.task?.normalizedStatus ?? 'unknown')
            ) {
              setStatus('done')
              source?.close()
              refresh()
            } else if (event.type === 'error') {
              setStatus('degraded')
            } else {
              setStatus('live')
            }
          } catch {
            setStatus('degraded')
          }
        }
        source.onerror = () => {
          if (disposed || statusRef.current === 'done') return
          source?.close()
          failures++
          setStatus(failures >= 3 ? 'polling' : 'reconnecting')
          reconnectTimer = setTimeout(connect, Math.min(1000 * 2 ** failures, 15_000))
        }
      } catch {
        if (disposed) return
        failures++
        setStatus(failures >= 3 ? 'polling' : 'reconnecting')
        reconnectTimer = setTimeout(connect, Math.min(1000 * 2 ** failures, 15_000))
      }
    }

    void connect()
    const fallback = setInterval(() => {
      if (statusRef.current !== 'live' && statusRef.current !== 'done') refresh()
    }, 15_000)
    return () => {
      disposed = true
      source?.close()
      if (reconnectTimer) clearTimeout(reconnectTimer)
      clearInterval(fallback)
    }
  }, [domain, enabled, queryClient, taskId])

  return { status, lastEventAt }
}
