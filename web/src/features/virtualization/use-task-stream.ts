import { useEffect, useRef, useState } from 'react'
import { buildSameOriginStreamURL, withStreamTicket } from '@/features/auth/stream-ticket'
import type { VirtualizationOperation } from './virtualization-types'

const TERMINAL_STATUSES = new Set(['completed', 'failed', 'canceled', 'callback_timeout'])

export function useTaskStream(taskId: string | null) {
  const [task, setTask] = useState<VirtualizationOperation | null>(null)
  const [status, setStatus] = useState<'idle' | 'streaming' | 'done' | 'error'>('idle')
  const sourceRef = useRef<EventSource | null>(null)

  useEffect(() => {
    if (!taskId) {
      setTask(null)
      setStatus('idle')
      return
    }

    setStatus('streaming')

    let cancelled = false
    void (async () => {
      try {
        const baseURL = buildSameOriginStreamURL(`/api/v1/virtualization/operations/${encodeURIComponent(taskId)}/stream`, 'http')
        const url = await withStreamTicket(baseURL)
        if (cancelled) return
        const es = new EventSource(url)
        sourceRef.current = es

        es.onmessage = (event) => {
          try {
            const data = JSON.parse(event.data) as VirtualizationOperation
            setTask(data)
            if (data.status && TERMINAL_STATUSES.has(data.status)) {
              setStatus('done')
              es.close()
            }
          } catch {
            // ignore parse errors
          }
        }

        es.onerror = () => {
          setStatus('error')
          es.close()
        }
      } catch {
        if (!cancelled) {
          setStatus('error')
        }
      }
    })()

    return () => {
      cancelled = true
      sourceRef.current?.close()
      sourceRef.current = null
    }
  }, [taskId])

  return { task, status }
}
