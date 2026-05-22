import { useEffect, useRef, useState } from 'react'
import { useAuthStore } from '@/stores/auth-store'
import type { VirtualizationOperation } from './virtualization-types'

const TERMINAL_STATUSES = new Set(['completed', 'failed', 'canceled', 'callback_timeout'])

export function useTaskStream(taskId: string | null) {
  const [task, setTask] = useState<VirtualizationOperation | null>(null)
  const [status, setStatus] = useState<'idle' | 'streaming' | 'done' | 'error'>('idle')
  const accessToken = useAuthStore((state) => state.accessToken)
  const sourceRef = useRef<EventSource | null>(null)

  useEffect(() => {
    if (!taskId) {
      setTask(null)
      setStatus('idle')
      return
    }

    const host = import.meta.env.DEV ? '//127.0.0.1:8080' : ''
    const url = `${host}/api/v1/virtualization/operations/${encodeURIComponent(taskId)}/stream?access_token=${accessToken}`
    const es = new EventSource(url)
    sourceRef.current = es
    setStatus('streaming')

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

    return () => {
      es.close()
      sourceRef.current = null
    }
  }, [taskId, accessToken])

  return { task, status }
}
