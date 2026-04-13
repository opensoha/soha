import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Button, Card, Empty, Input, Select, Space, Switch, Tag, Typography } from '@douyinfe/semi-ui'
import { IconDelete, IconRefresh } from '@douyinfe/semi-icons'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { useAuthStore } from '@/stores/auth-store'
import { downloadText } from '@/utils/download'
import type { ApiResponse, PodLogs } from '@/types'

const { Text } = Typography

const DEFAULT_HISTORY_LINES = 100
const HISTORY_INCREMENT = 100
const POLLING_INTERVAL_MS = 3000
const RECONNECT_DELAYS_MS = [1000, 2000, 5000, 10000, 15000]

interface LogMessage {
  type: string
  data?: string
  message?: string
}

function buildLogStreamURL({
  clusterId,
  namespace,
  podName,
  container,
  accessToken,
}: {
  clusterId: string
  namespace: string
  podName: string
  container?: string
  accessToken?: string | null
}) {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = import.meta.env.DEV ? '127.0.0.1:8080' : window.location.host
  const url = new URL(`${protocol}//${host}/api/v1/clusters/${encodeURIComponent(clusterId)}/workloads/pods/${encodeURIComponent(podName)}/logs/stream`)
  url.searchParams.set('namespace', namespace)
  url.searchParams.set('tailLines', '1')
  if (container) {
    url.searchParams.set('container', container)
  }
  if (accessToken) {
    url.searchParams.set('access_token', accessToken)
  }
  return url.toString()
}

function splitLogContent(content: string) {
  return content
    .split('\n')
    .map((line) => line.replace(/\r$/, ''))
    .filter((line) => line.length > 0)
}

function appendLineWithDedupe(current: string[], nextLine: string) {
  if (!nextLine) return current
  if (current[current.length - 1] === nextLine) return current
  return [...current, nextLine].slice(-10000)
}

function mergeLogLines(current: string[], incoming: string[]) {
  if (current.length === 0) return incoming
  if (incoming.length === 0) return current

  if (incoming.length >= current.length) {
    const incomingTail = incoming.slice(-current.length)
    if (JSON.stringify(incomingTail) === JSON.stringify(current)) {
      return incoming
    }
  }

  const maxOverlap = Math.min(current.length, incoming.length)
  for (let overlapSize = maxOverlap; overlapSize > 0; overlapSize -= 1) {
    const currentSuffix = current.slice(-overlapSize)
    const incomingPrefix = incoming.slice(0, overlapSize)
    if (JSON.stringify(currentSuffix) === JSON.stringify(incomingPrefix)) {
      return [...current, ...incoming.slice(overlapSize)].slice(-10000)
    }
  }

  return incoming.length > current.length
    ? incoming
    : [...current, ...incoming].slice(-10000)
}

function getEmptyLogMessage({
  hasFilter,
  previous,
  sinceSeconds,
  localeCode,
}: {
  hasFilter: boolean
  previous: boolean
  sinceSeconds: number
  localeCode: 'zh_CN' | 'en_US'
}) {
  if (hasFilter) {
    return localeCode === 'zh_CN' ? '当前筛选条件下没有匹配的日志内容' : 'No log lines match the current filter'
  }
  if (previous) {
    return sinceSeconds > 0
      ? (localeCode === 'zh_CN' ? '当前时间范围内没有可用的历史日志' : 'No historical logs are available for the selected time range')
      : (localeCode === 'zh_CN' ? '当前没有可用的历史日志' : 'No historical logs are available')
  }
  return sinceSeconds > 0
    ? (localeCode === 'zh_CN' ? '当前时间范围内没有可用的实时日志内容' : 'No current log lines are available for the selected time range')
    : (localeCode === 'zh_CN' ? '当前没有可用的实时日志内容' : 'No current log lines are available')
}

export function PodLogViewer({
  clusterId,
  namespace,
  podName,
  container,
  active = true,
  containerOptions,
  onContainerChange,
}: {
  clusterId?: string | null
  namespace?: string | null
  podName: string
  container?: string
  active?: boolean
  containerOptions?: Array<{ value: string; label: string }>
  onContainerChange?: (value: string) => void
}) {
  const { t, localeCode } = useI18n()
  const accessToken = useAuthStore((state) => state.accessToken)
  const [lines, setLines] = useState<string[]>([])
  const [connectionState, setConnectionState] = useState<'idle' | 'connecting' | 'connected' | 'closed' | 'error'>('idle')
  const [statusMessage, setStatusMessage] = useState(t('podLogViewer.idle', 'Log stream has not been connected yet'))
  const [keyword, setKeyword] = useState('')
  const [autoScroll, setAutoScroll] = useState(true)
  const [sinceSeconds, setSinceSeconds] = useState(0)
  const [previous, setPrevious] = useState(false)
  const [historyLines, setHistoryLines] = useState(DEFAULT_HISTORY_LINES)
  const [loadingOlder, setLoadingOlder] = useState(false)
  const socketRef = useRef<WebSocket | null>(null)
  const pollingTimerRef = useRef<number | null>(null)
  const reconnectTimerRef = useRef<number | null>(null)
  const reconnectAttemptRef = useRef(0)
  const scrollerRef = useRef<HTMLDivElement | null>(null)
  const restoreScrollRef = useRef<{ previousHeight: number; previousTop: number } | null>(null)

  const logsPath = useMemo(() => {
    if (!clusterId || !namespace) return ''
    const params = new URLSearchParams()
    params.set('namespace', namespace)
    params.set('tailLines', String(historyLines))
    if (container) params.set('container', container)
    if (sinceSeconds > 0) params.set('sinceSeconds', String(sinceSeconds))
    if (previous) params.set('previous', 'true')
    return `/clusters/${clusterId}/workloads/pods/${encodeURIComponent(podName)}/logs?${params.toString()}`
  }, [clusterId, container, historyLines, namespace, podName, previous, sinceSeconds])

  const streamURL = useMemo(() => {
    if (!clusterId || !namespace || previous) return ''
    return buildLogStreamURL({
      clusterId,
      namespace,
      podName,
      container,
      accessToken,
    })
  }, [accessToken, clusterId, container, namespace, podName, previous])

  const disconnect = useCallback(() => {
    if (socketRef.current?.readyState === WebSocket.OPEN) {
      socketRef.current.send(JSON.stringify({ type: 'close' }))
    }
    socketRef.current?.close()
    socketRef.current = null
    if (pollingTimerRef.current != null) {
      window.clearInterval(pollingTimerRef.current)
      pollingTimerRef.current = null
    }
    if (reconnectTimerRef.current != null) {
      window.clearTimeout(reconnectTimerRef.current)
      reconnectTimerRef.current = null
    }
  }, [])

  const fetchSnapshot = useCallback(async (requestedHistoryLines: number, preserveScroll = false) => {
    if (!clusterId || !namespace) return
    if (preserveScroll && scrollerRef.current) {
      restoreScrollRef.current = {
        previousHeight: scrollerRef.current.scrollHeight,
        previousTop: scrollerRef.current.scrollTop,
      }
    }
    const params = new URLSearchParams()
    params.set('namespace', namespace)
    params.set('tailLines', String(requestedHistoryLines))
    if (container) params.set('container', container)
    if (sinceSeconds > 0) params.set('sinceSeconds', String(sinceSeconds))
    if (previous) params.set('previous', 'true')
    const response = await api.get<ApiResponse<PodLogs>>(
      `/clusters/${clusterId}/workloads/pods/${encodeURIComponent(podName)}/logs?${params.toString()}`,
    )
    const nextLines = splitLogContent(response.data?.content ?? '')
    setLines((current) => mergeLogLines(current, nextLines))
  }, [clusterId, container, namespace, podName, previous, sinceSeconds])

  const startPollingSync = useCallback(() => {
    if (pollingTimerRef.current != null) return
    pollingTimerRef.current = window.setInterval(async () => {
      try {
        const response = await api.get<ApiResponse<PodLogs>>(logsPath)
        const nextLines = splitLogContent(response.data?.content ?? '')
        setLines((current) => mergeLogLines(current, nextLines))
        setConnectionState('connected')
        setStatusMessage(previous
          ? (localeCode === 'zh_CN' ? '当前查看历史日志' : 'Viewing historical logs')
          : (localeCode === 'zh_CN' ? '当前跟随实时日志' : 'Following live logs'))
      } catch (err) {
        setConnectionState('error')
        setStatusMessage(err instanceof Error ? err.message : t('podLogViewer.failed', 'Log stream connection failed'))
      }
    }, POLLING_INTERVAL_MS)
  }, [localeCode, logsPath, previous, t])

  const scheduleReconnect = useCallback(() => {
    if (previous || !active || reconnectTimerRef.current != null) return
    const attempt = reconnectAttemptRef.current
    const delay = RECONNECT_DELAYS_MS[Math.min(attempt, RECONNECT_DELAYS_MS.length - 1)]
    reconnectAttemptRef.current += 1
    setStatusMessage(
      localeCode === 'zh_CN'
        ? `日志流已断开，${Math.round(delay / 1000)} 秒后自动重连（第 ${reconnectAttemptRef.current} 次）`
        : `Log stream disconnected. Reconnecting in ${Math.round(delay / 1000)}s (attempt ${reconnectAttemptRef.current})`,
    )
    reconnectTimerRef.current = window.setTimeout(() => {
      reconnectTimerRef.current = null
      connect()
    }, delay)
  }, [active, localeCode, previous])

  const connect = useCallback(() => {
    if (!streamURL) return
    disconnect()
    setConnectionState('connecting')
    setStatusMessage(t('podLogViewer.connecting', 'Connecting log stream...'))
    const socket = new WebSocket(streamURL)
    socketRef.current = socket

    socket.onopen = () => {
      reconnectAttemptRef.current = 0
      setConnectionState('connected')
      setStatusMessage(previous
        ? (localeCode === 'zh_CN' ? '当前查看历史日志' : 'Viewing historical logs')
        : (localeCode === 'zh_CN' ? '当前跟随实时日志' : 'Following live logs'))
    }

    socket.onmessage = (event) => {
      const payload = JSON.parse(String(event.data)) as LogMessage
      if (payload.type === 'log') {
        setLines((current) => appendLineWithDedupe(current, payload.data || ''))
        return
      }
      if (payload.type === 'status' || payload.type === 'exit') {
        if (payload.message) {
          setStatusMessage(payload.message)
        }
        if (payload.type === 'exit') {
          setConnectionState('closed')
        }
      }
    }

    socket.onerror = () => {
      setConnectionState('error')
      setStatusMessage(t('podLogViewer.failed', 'Log stream connection failed'))
      startPollingSync()
      scheduleReconnect()
    }

    socket.onclose = () => {
      setConnectionState((current) => current === 'error' ? 'error' : 'closed')
      startPollingSync()
      scheduleReconnect()
    }
  }, [disconnect, localeCode, previous, scheduleReconnect, startPollingSync, streamURL, t])

  useEffect(() => {
    if (!clusterId || !namespace || !active) return
    setHistoryLines(DEFAULT_HISTORY_LINES)
    setLines([])
  }, [active, clusterId, container, namespace, podName, previous, sinceSeconds])

  useEffect(() => {
    if (!clusterId || !namespace || !active) return
    fetchSnapshot(historyLines)
      .then(() => {
        if (!previous) {
          startPollingSync()
          connect()
        } else {
          disconnect()
          reconnectAttemptRef.current = 0
          setConnectionState('closed')
          setStatusMessage(localeCode === 'zh_CN' ? '当前查看历史日志' : 'Viewing historical logs')
        }
      })
      .catch((err) => {
        setConnectionState('error')
        setStatusMessage(err instanceof Error ? err.message : t('podLogViewer.failed', 'Log stream connection failed'))
      })

    return () => disconnect()
  }, [active, clusterId, connect, disconnect, fetchSnapshot, historyLines, localeCode, namespace, previous, startPollingSync, t])

  useEffect(() => {
    if (!restoreScrollRef.current || !scrollerRef.current) return
    const snapshot = restoreScrollRef.current
    requestAnimationFrame(() => {
      if (!scrollerRef.current) return
      scrollerRef.current.scrollTop = scrollerRef.current.scrollHeight - snapshot.previousHeight + snapshot.previousTop
      restoreScrollRef.current = null
    })
  }, [lines])

  const filteredLines = useMemo(
    () => (keyword.trim() ? lines.filter((line) => line.toLowerCase().includes(keyword.trim().toLowerCase())) : lines),
    [keyword, lines],
  )
  const emptyLogMessage = getEmptyLogMessage({
    hasFilter: keyword.trim().length > 0,
    previous,
    sinceSeconds,
    localeCode,
  })

  const timeRangeLabel = sinceSeconds === 0
    ? t('podLogViewer.timeAll', 'All available')
    : sinceSeconds === 300
      ? t('podLogViewer.time5m', 'Last 5 min')
      : sinceSeconds === 900
        ? t('podLogViewer.time15m', 'Last 15 min')
        : sinceSeconds === 3600
          ? t('podLogViewer.time1h', 'Last 1 hour')
          : sinceSeconds === 21600
            ? t('podLogViewer.time6h', 'Last 6 hours')
            : `${sinceSeconds}s`

  const exportLogContent = useMemo(() => [
    `Pod: ${podName}`,
    `Namespace: ${namespace}`,
    `Container: ${container || 'default'}`,
    `Mode: ${previous ? (localeCode === 'zh_CN' ? '历史日志' : 'historical') : (localeCode === 'zh_CN' ? '当前日志' : 'current')}`,
    `Time Range: ${timeRangeLabel}`,
    `Exported At: ${new Date().toISOString()}`,
    '',
    ...filteredLines,
  ].join('\n'), [container, filteredLines, localeCode, namespace, podName, previous, timeRangeLabel])

  useEffect(() => {
    if (!autoScroll || filteredLines.length === 0 || !scrollerRef.current) return
    requestAnimationFrame(() => {
      if (!scrollerRef.current) return
      scrollerRef.current.scrollTop = scrollerRef.current.scrollHeight
    })
  }, [autoScroll, filteredLines])

  const handleScroll = useCallback(async () => {
    if (!scrollerRef.current || loadingOlder) return
    if (scrollerRef.current.scrollTop > 24) return
    setLoadingOlder(true)
    const nextHistoryLines = historyLines + HISTORY_INCREMENT
    setHistoryLines(nextHistoryLines)
    try {
      await fetchSnapshot(nextHistoryLines, true)
    } finally {
      setLoadingOlder(false)
    }
  }, [fetchSnapshot, historyLines, loadingOlder])

  if (!clusterId || !namespace) {
    return <Empty description={t('podLogViewer.notReady', 'Select a valid cluster and namespace before opening live logs')} />
  }

  if (!active) {
    return <Empty description={t('podLogViewer.idle', 'Log stream has not been connected yet')} />
  }

  return (
    <Card className="kc-detail-card">
      <div className="kc-terminal-toolbar">
        <Space>
          <Tag color={connectionState === 'connected' ? 'green' : connectionState === 'connecting' ? 'blue' : connectionState === 'error' ? 'red' : 'grey'}>
            {connectionState}
          </Tag>
          <Tag color={previous ? 'orange' : 'blue'}>
            {previous
              ? (localeCode === 'zh_CN' ? '历史日志' : 'Historical logs')
              : (localeCode === 'zh_CN' ? '当前日志' : 'Current logs')}
          </Tag>
          <Text type="tertiary" size="small">{statusMessage}</Text>
        </Space>
        <Space>
          {containerOptions && containerOptions.length > 0 ? (
            <Select
              value={container || undefined}
              onChange={(value) => onContainerChange?.(String(value || ''))}
              optionList={containerOptions}
              placeholder={t('common.container', 'Container')}
              style={{ width: 220 }}
              showClear
            />
          ) : null}
          <Input value={keyword} onChange={setKeyword} placeholder={t('podLogViewer.searchPlaceholder', 'Search log keyword')} style={{ width: 220 }} />
          <Select
            value={String(sinceSeconds)}
            onChange={(value) => setSinceSeconds(Number(value) || 0)}
            style={{ width: 180 }}
            optionList={[
              { value: '0', label: t('podLogViewer.timeAll', 'All available') },
              { value: '300', label: t('podLogViewer.time5m', 'Last 5 min') },
              { value: '900', label: t('podLogViewer.time15m', 'Last 15 min') },
              { value: '3600', label: t('podLogViewer.time1h', 'Last 1 hour') },
              { value: '21600', label: t('podLogViewer.time6h', 'Last 6 hours') },
            ]}
          />
          <div className="kc-step-inline">
            <Text type="tertiary" size="small">{t('podLogViewer.autoScroll', 'Auto scroll')}</Text>
            <Switch checked={autoScroll} onChange={setAutoScroll} />
          </div>
          <div className="kc-step-inline">
            <Text type="tertiary" size="small">{localeCode === 'zh_CN' ? '历史日志' : 'Historical logs'}</Text>
            <Switch checked={previous} onChange={(value) => setPrevious(Boolean(value))} />
          </div>
          <Button icon={<IconDelete />} theme="borderless" onClick={() => setLines([])}>{t('podLogViewer.clear', 'Clear')}</Button>
          <Button
            theme="borderless"
            onClick={() => downloadText(
              `${podName}-${previous ? 'historical' : 'current'}-logs.txt`,
              exportLogContent,
            )}
            disabled={filteredLines.length === 0}
          >
            {localeCode === 'zh_CN' ? '导出日志' : 'Export Logs'}
          </Button>
          <Button icon={<IconRefresh />} size="small" theme="borderless" onClick={() => fetchSnapshot(historyLines)}>{t('podLogViewer.reconnect', 'Reconnect')}</Button>
        </Space>
      </div>
      <div ref={scrollerRef} className="kc-log-shell" onScroll={() => { void handleScroll() }}>
        {loadingOlder ? (
          <div className="kc-log-loading">{localeCode === 'zh_CN' ? '加载更早日志中...' : 'Loading older logs...'}</div>
        ) : null}
        {filteredLines.length > 0 ? (
          filteredLines.map((line, index) => (
            <div key={`${index}:${line.slice(0, 32)}`} className="kc-log-row kc-log-row-plain">
              <span className="kc-log-row-text">{line}</span>
            </div>
          ))
        ) : (
          <div className="kc-log-loading">{emptyLogMessage}</div>
        )}
      </div>
    </Card>
  )
}
