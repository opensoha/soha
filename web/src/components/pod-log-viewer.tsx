import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from 'react'
import { DeleteOutlined, ExportOutlined, ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Flex, Input, Select, Switch, Tag, Typography } from 'antd'
import { List, type ListImperativeAPI } from 'react-window'
import { ManagementState } from '@/components/management-list'
import './resource-operation-panels.css'
import { buildSameOriginStreamURL, withStreamTicket } from '@/features/auth/stream-ticket'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { downloadText } from '@/utils/download'
import { parseStreamMessage } from '@/utils/stream-message'
import type { ApiResponse, PodLogs } from '@/types'

const { Text } = Typography

const DEFAULT_HISTORY_LINES = 100
const HISTORY_INCREMENT = 100
const MAX_HISTORY_LINES = 5000
const MAX_LOG_LINES = 5000
const MAX_LOG_CHARACTERS = 2 * 1024 * 1024
const MAX_LOG_LINE_CHARACTERS = 64 * 1024
const LOG_ROW_HEIGHT = 32
const POLLING_INTERVAL_MS = 3000
const RECONNECT_DELAYS_MS = [1000, 2000, 5000, 10000, 15000]
const CLEAR_BOUNDARY_LINES = 5

interface LogMessage {
  type: string
  data?: string
  message?: string
}

interface LogRowProps {
  lines: string[]
}

function LogRow({
  ariaAttributes,
  index,
  lines,
  style,
}: LogRowProps & {
  ariaAttributes: {
    'aria-posinset': number
    'aria-setsize': number
    role: 'listitem'
  }
  index: number
  style: CSSProperties
}) {
  return (
    <div {...ariaAttributes} className="soha-log-row soha-log-row-plain" style={style}>
      <span className="soha-log-row-text">{lines[index]}</span>
    </div>
  )
}

function buildLogStreamURL({
  clusterId,
  namespace,
  podName,
  expectedUid,
  container,
}: {
  clusterId: string
  namespace: string
  expectedUid?: string
  podName: string
  container?: string
}) {
  const url = buildSameOriginStreamURL(
    `/api/v1/clusters/${encodeURIComponent(clusterId)}/workloads/pods/${encodeURIComponent(podName)}/logs/stream`,
    'ws',
  )
  url.searchParams.set('namespace', namespace)
  if (expectedUid) url.searchParams.set('expectedUid', expectedUid)
  url.searchParams.set('tailLines', '1')
  if (container) {
    url.searchParams.set('container', container)
  }
  return url.toString()
}

function splitLogContent(content: string) {
  return content
    .split('\n')
    .map((line) => line.replace(/\r$/, ''))
    .filter((line) => line.length > 0)
    .map((line) => line.slice(0, MAX_LOG_LINE_CHARACTERS))
}

function boundLogLines(lines: string[]) {
  const boundedCount = lines.length > MAX_LOG_LINES ? lines.slice(-MAX_LOG_LINES) : lines
  let characters = 0
  let start = boundedCount.length
  while (start > 0) {
    const nextLength = boundedCount[start - 1].length + 1
    if (characters + nextLength > MAX_LOG_CHARACTERS) break
    characters += nextLength
    start -= 1
  }
  return start === 0 ? boundedCount : boundedCount.slice(start)
}

function appendLineWithDedupe(current: string[], nextLine: string) {
  const boundedLine = nextLine.slice(0, MAX_LOG_LINE_CHARACTERS)
  if (!boundedLine) return current
  if (current[current.length - 1] === boundedLine) return current
  return boundLogLines([...current, boundedLine])
}

function sameLines(left: string[], right: string[]) {
  if (left === right) return true
  if (left.length !== right.length) return false
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {
      return false
    }
  }
  return true
}

function mergeLogLines(current: string[], incoming: string[]) {
  incoming = boundLogLines(incoming)
  if (current.length === 0) return incoming
  if (incoming.length === 0) return current

  if (incoming.length >= current.length) {
    const incomingTail = incoming.slice(-current.length)
    if (JSON.stringify(incomingTail) === JSON.stringify(current)) {
      return incoming.length === current.length ? current : incoming
    }
  }

  const maxOverlap = Math.min(current.length, incoming.length)
  for (let overlapSize = maxOverlap; overlapSize > 0; overlapSize -= 1) {
    const currentSuffix = current.slice(-overlapSize)
    const incomingPrefix = incoming.slice(0, overlapSize)
    if (JSON.stringify(currentSuffix) === JSON.stringify(incomingPrefix)) {
      return boundLogLines([...current, ...incoming.slice(overlapSize)])
    }
  }

  const merged =
    incoming.length > current.length ? incoming : boundLogLines([...current, ...incoming])
  return sameLines(merged, current) ? current : merged
}

function trimLinesAfterBoundary(incoming: string[], boundary: string[]) {
  if (boundary.length === 0 || incoming.length < boundary.length) {
    return incoming
  }
  for (let start = incoming.length - boundary.length; start >= 0; start -= 1) {
    let matched = true
    for (let index = 0; index < boundary.length; index += 1) {
      if (incoming[start + index] !== boundary[index]) {
        matched = false
        break
      }
    }
    if (matched) {
      return incoming.slice(start + boundary.length)
    }
  }
  return incoming
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
    return localeCode === 'zh_CN'
      ? '当前筛选条件下没有匹配的日志内容'
      : 'No log lines match the current filter'
  }
  if (previous) {
    return sinceSeconds > 0
      ? localeCode === 'zh_CN'
        ? '当前时间范围内没有可用的历史日志'
        : 'No historical logs are available for the selected time range'
      : localeCode === 'zh_CN'
        ? '当前没有可用的历史日志'
        : 'No historical logs are available'
  }
  return sinceSeconds > 0
    ? localeCode === 'zh_CN'
      ? '当前时间范围内没有可用的实时日志内容'
      : 'No current log lines are available for the selected time range'
    : localeCode === 'zh_CN'
      ? '当前没有可用的实时日志内容'
      : 'No current log lines are available'
}

export function PodLogViewer({
  clusterId,
  namespace,
  podName,
  expectedUid,
  container,
  active = true,
  containerOptions,
  onOpenLogCenter,
  onContainerChange,
  streamingDisabledReason,
  beforeConnect,
  toolbarExtra,
}: {
  clusterId?: string | null
  namespace?: string | null
  expectedUid?: string
  podName: string
  container?: string
  active?: boolean
  containerOptions?: Array<{ value: string; label: string }>
  onOpenLogCenter?: () => void
  onContainerChange?: (value: string) => void
  streamingDisabledReason?: string
  beforeConnect?: () => Promise<boolean>
  toolbarExtra?: ReactNode
}) {
  const { t, localeCode } = useI18n()
  const [lines, setLines] = useState<string[]>([])
  const [connectionState, setConnectionState] = useState<
    'idle' | 'connecting' | 'connected' | 'closed' | 'error'
  >('idle')
  const [keyword, setKeyword] = useState('')
  const [autoScroll, setAutoScroll] = useState(true)
  const [sinceSeconds, setSinceSeconds] = useState(0)
  const [previous, setPrevious] = useState(false)
  const [historyLines, setHistoryLines] = useState(DEFAULT_HISTORY_LINES)
  const [loadingOlder, setLoadingOlder] = useState(false)
  const requestRef = useRef<AbortController | null>(null)
  const socketRef = useRef<WebSocket | null>(null)
  const pollingTimerRef = useRef<number | null>(null)
  const reconnectTimerRef = useRef<number | null>(null)
  const reconnectAttemptRef = useRef(0)
  const listRef = useRef<ListImperativeAPI | null>(null)
  const restoreScrollRef = useRef<{ previousHeight: number; previousTop: number } | null>(null)
  const clearBoundaryRef = useRef<string[]>([])
  const suppressClearedReplayRef = useRef(false)
  const connectionRunRef = useRef(0)

  const logsPath = useMemo(() => {
    if (!clusterId || !namespace) return ''
    const params = new URLSearchParams()
    params.set('namespace', namespace)
    if (expectedUid) params.set('expectedUid', expectedUid)
    params.set('tailLines', String(DEFAULT_HISTORY_LINES))
    if (container) params.set('container', container)
    if (sinceSeconds > 0) params.set('sinceSeconds', String(sinceSeconds))
    if (previous) params.set('previous', 'true')
    return `/clusters/${clusterId}/workloads/pods/${encodeURIComponent(podName)}/logs?${params.toString()}`
  }, [clusterId, container, namespace, podName, expectedUid, previous, sinceSeconds])

  const streamURL = useMemo(() => {
    if (!clusterId || !namespace || previous || streamingDisabledReason) return ''
    return buildLogStreamURL({
      clusterId,
      namespace,
      podName,
      expectedUid,
      container,
    })
  }, [clusterId, container, namespace, podName, expectedUid, previous, streamingDisabledReason])

  const disconnect = useCallback(() => {
    connectionRunRef.current += 1
    requestRef.current?.abort()
    requestRef.current = null
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

  const fetchSnapshot = useCallback(
    async (requestedHistoryLines: number, preserveScroll = false) => {
      if (!clusterId || !namespace) return
      const scroller = listRef.current?.element
      if (preserveScroll && scroller) {
        restoreScrollRef.current = {
          previousHeight: scroller.scrollHeight,
          previousTop: scroller.scrollTop,
        }
      }
      const params = new URLSearchParams()
      params.set('namespace', namespace)
      if (expectedUid) params.set('expectedUid', expectedUid)
      params.set('tailLines', String(requestedHistoryLines))
      if (container) params.set('container', container)
      if (sinceSeconds > 0) params.set('sinceSeconds', String(sinceSeconds))
      if (previous) params.set('previous', 'true')
      const controller = requestRef.current ?? new AbortController()
      requestRef.current = controller
      const response = await api.get<ApiResponse<PodLogs>>(
        `/clusters/${clusterId}/workloads/pods/${encodeURIComponent(podName)}/logs?${params.toString()}`,
        { signal: controller.signal },
      )
      if (controller.signal.aborted) return
      const nextLines = trimLinesAfterBoundary(
        splitLogContent(response.data?.content ?? ''),
        previous ? [] : clearBoundaryRef.current,
      )
      setLines((current) => mergeLogLines(current, nextLines))
    },
    [clusterId, container, namespace, podName, expectedUid, previous, sinceSeconds],
  )

  const startPollingSync = useCallback(() => {
    if (pollingTimerRef.current != null) return
    pollingTimerRef.current = window.setInterval(async () => {
      try {
        const controller = requestRef.current ?? new AbortController()
        requestRef.current = controller
        const response = await api.get<ApiResponse<PodLogs>>(logsPath, {
          signal: controller.signal,
        })
        if (controller.signal.aborted) return
        const nextLines = splitLogContent(response.data?.content ?? '')
        setLines((current) => mergeLogLines(current, nextLines))
        setConnectionState('connected')
      } catch {
        setConnectionState('error')
      }
    }, POLLING_INTERVAL_MS)
  }, [logsPath])

  const scheduleReconnect = useCallback(() => {
    if (previous || streamingDisabledReason || !active || reconnectTimerRef.current != null) return
    const attempt = reconnectAttemptRef.current
    const delay = RECONNECT_DELAYS_MS[Math.min(attempt, RECONNECT_DELAYS_MS.length - 1)]
    reconnectAttemptRef.current += 1
    reconnectTimerRef.current = window.setTimeout(() => {
      reconnectTimerRef.current = null
      connect()
    }, delay)
  }, [active, previous, streamingDisabledReason])

  const connect = useCallback(async () => {
    if (!streamURL) {
      if (streamingDisabledReason && !previous) {
        setConnectionState('connected')
        startPollingSync()
      }
      return
    }
    disconnect()
    const runId = connectionRunRef.current + 1
    connectionRunRef.current = runId
    if (beforeConnect && !(await beforeConnect())) return
    if (connectionRunRef.current !== runId) return
    setConnectionState('connecting')
    let ticketedURL: string
    try {
      ticketedURL = await withStreamTicket(streamURL)
    } catch {
      if (connectionRunRef.current !== runId) return
      setConnectionState('error')
      startPollingSync()
      scheduleReconnect()
      return
    }
    if (connectionRunRef.current !== runId) return
    const socket = new WebSocket(ticketedURL)
    socketRef.current = socket

    socket.onopen = () => {
      if (socketRef.current !== socket) return
      reconnectAttemptRef.current = 0
      setConnectionState('connected')
    }

    socket.onmessage = (event) => {
      if (socketRef.current !== socket) return
      const payload = parseStreamMessage<LogMessage>(event.data)
      if (!payload) return
      if (payload.type === 'log') {
        if (
          suppressClearedReplayRef.current &&
          clearBoundaryRef.current.includes(payload.data || '')
        ) {
          return
        }
        suppressClearedReplayRef.current = false
        setLines((current) => appendLineWithDedupe(current, payload.data || ''))
        return
      }
      if (payload.type === 'status' || payload.type === 'exit') {
        if (payload.type === 'exit') {
          setConnectionState('closed')
        }
      }
    }

    socket.onerror = () => {
      if (socketRef.current !== socket) return
      setConnectionState('error')
      startPollingSync()
      scheduleReconnect()
    }

    socket.onclose = () => {
      if (socketRef.current !== socket) return
      setConnectionState((current) => (current === 'error' ? 'error' : 'closed'))
      startPollingSync()
      scheduleReconnect()
    }
  }, [
    beforeConnect,
    disconnect,
    previous,
    scheduleReconnect,
    startPollingSync,
    streamURL,
    streamingDisabledReason,
  ])

  useEffect(() => {
    if (!clusterId || !namespace || !active) return
    setHistoryLines(DEFAULT_HISTORY_LINES)
    setLines([])
    setLoadingOlder(false)
    restoreScrollRef.current = null
    clearBoundaryRef.current = []
    suppressClearedReplayRef.current = false
  }, [active, clusterId, container, namespace, podName, expectedUid, previous, sinceSeconds])

  useEffect(() => {
    if (!clusterId || !namespace || !active) return
    let cancelled = false
    fetchSnapshot(DEFAULT_HISTORY_LINES)
      .then(() => {
        if (cancelled) return
        if (!previous) {
          connect()
        } else {
          disconnect()
          reconnectAttemptRef.current = 0
          setConnectionState('closed')
        }
      })
      .catch(() => {
        setConnectionState('error')
      })

    return () => {
      cancelled = true
      disconnect()
    }
  }, [active, clusterId, connect, disconnect, fetchSnapshot, namespace, previous])

  useEffect(() => {
    if (!restoreScrollRef.current || !listRef.current?.element) return
    const snapshot = restoreScrollRef.current
    requestAnimationFrame(() => {
      const scroller = listRef.current?.element
      if (!scroller) return
      scroller.scrollTop = scroller.scrollHeight - snapshot.previousHeight + snapshot.previousTop
      restoreScrollRef.current = null
    })
  }, [lines])

  const filteredLines = useMemo(
    () =>
      keyword.trim()
        ? lines.filter((line) => line.toLowerCase().includes(keyword.trim().toLowerCase()))
        : lines,
    [keyword, lines],
  )
  const emptyLogMessage = getEmptyLogMessage({
    hasFilter: keyword.trim().length > 0,
    previous,
    sinceSeconds,
    localeCode,
  })

  const timeRangeLabel =
    sinceSeconds === 0
      ? t('podLogViewer.timeAll', 'All time')
      : sinceSeconds === 300
        ? t('podLogViewer.time5m', 'Last 5 min')
        : sinceSeconds === 900
          ? t('podLogViewer.time15m', 'Last 15 min')
          : sinceSeconds === 3600
            ? t('podLogViewer.time1h', 'Last 1 hour')
            : sinceSeconds === 21600
              ? t('podLogViewer.time6h', 'Last 6 hours')
              : `${sinceSeconds}s`

  const logRowProps = useMemo(() => ({ lines: filteredLines }), [filteredLines])

  const handleExport = useCallback(
    () =>
      downloadText(
        `${podName}-${previous ? 'historical' : 'current'}-logs.txt`,
        [
          `Pod: ${podName}`,
          `Namespace: ${namespace}`,
          `Container: ${container || 'default'}`,
          `Mode: ${previous ? (localeCode === 'zh_CN' ? '历史日志' : 'historical') : localeCode === 'zh_CN' ? '当前日志' : 'current'}`,
          `Time Range: ${timeRangeLabel}`,
          `Exported At: ${new Date().toISOString()}`,
          '',
          ...filteredLines,
        ].join('\n'),
      ),
    [
      container,
      filteredLines,
      localeCode,
      namespace,
      podName,
      expectedUid,
      previous,
      timeRangeLabel,
    ],
  )

  useEffect(() => {
    if (!autoScroll || filteredLines.length === 0) return
    requestAnimationFrame(() => {
      listRef.current?.scrollToRow({ index: filteredLines.length - 1, align: 'end' })
    })
  }, [autoScroll, filteredLines])

  const handleScroll = useCallback(
    async (scroller: HTMLDivElement) => {
      const atBottom = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight <= 24
      setAutoScroll(atBottom)
      if (scroller.scrollTop > 24 || loadingOlder || historyLines >= MAX_HISTORY_LINES) return
      setLoadingOlder(true)
      const nextHistoryLines = Math.min(historyLines + HISTORY_INCREMENT, MAX_HISTORY_LINES)
      setHistoryLines(nextHistoryLines)
      try {
        await fetchSnapshot(nextHistoryLines, true)
      } finally {
        setLoadingOlder(false)
      }
    },
    [fetchSnapshot, historyLines, loadingOlder],
  )

  const handleClear = useCallback(() => {
    clearBoundaryRef.current = previous ? [] : lines.slice(-CLEAR_BOUNDARY_LINES)
    suppressClearedReplayRef.current = !previous && clearBoundaryRef.current.length > 0
    setLines([])
    restoreScrollRef.current = null
    if (previous) {
      return
    }
    reconnectAttemptRef.current = 0
    if (streamingDisabledReason) {
      void fetchSnapshot(historyLines)
      startPollingSync()
      return
    }
    disconnect()
    setConnectionState('connecting')
    connect()
  }, [
    connect,
    disconnect,
    fetchSnapshot,
    historyLines,
    lines,
    previous,
    startPollingSync,
    streamingDisabledReason,
  ])

  if (!clusterId || !namespace) {
    return (
      <ManagementState
        compact
        kind="select-scope"
        title={t(
          'podLogViewer.notReady',
          'Select a valid cluster and namespace before opening live logs',
        )}
      />
    )
  }

  if (!active) {
    return (
      <ManagementState
        compact
        title={t('podLogViewer.idle', 'Log stream has not been connected yet')}
      />
    )
  }

  return (
    <Card className="soha-detail-card soha-log-card">
      <Flex className="soha-terminal-toolbar soha-log-toolbar" align="center" gap={8} wrap>
        <Tag
          color={
            connectionState === 'connected'
              ? 'green'
              : connectionState === 'connecting'
                ? 'blue'
                : connectionState === 'error'
                  ? 'red'
                  : connectionState === 'closed'
                    ? 'orange'
                    : undefined
          }
        >
          {connectionState}
        </Tag>
        <Tag color={previous ? 'orange' : 'blue'}>
          {previous
            ? localeCode === 'zh_CN'
              ? '历史日志'
              : 'Historical logs'
            : localeCode === 'zh_CN'
              ? '当前日志'
              : 'Current logs'}
        </Tag>
        {streamingDisabledReason ? (
          <>
            <Tag color="orange">{localeCode === 'zh_CN' ? '轮询' : 'Polling'}</Tag>
            <Text className="soha-log-toolbar-status" type="secondary">
              {streamingDisabledReason}
            </Text>
          </>
        ) : null}
        {containerOptions && containerOptions.length > 0 ? (
          <Select
            aria-label={localeCode === 'zh_CN' ? '选择容器' : 'Select container'}
            size="small"
            value={container || undefined}
            onChange={(value) => onContainerChange?.(String(value ?? ''))}
            options={containerOptions}
            placeholder={t('common.container', 'Container')}
            style={{ width: 220 }}
            allowClear
          />
        ) : null}
        <Input
          size="small"
          value={keyword}
          onChange={(event) => setKeyword(event.target.value)}
          placeholder={t('podLogViewer.searchPlaceholder', 'Search log keyword')}
          style={{ width: 220 }}
        />
        <Select
          size="small"
          value={String(sinceSeconds)}
          onChange={(value) => setSinceSeconds(Number(value) || 0)}
          style={{ width: 180 }}
          options={[
            { value: '0', label: t('podLogViewer.timeAll', 'All time') },
            { value: '300', label: t('podLogViewer.time5m', 'Last 5 min') },
            { value: '900', label: t('podLogViewer.time15m', 'Last 15 min') },
            { value: '3600', label: t('podLogViewer.time1h', 'Last 1 hour') },
            { value: '21600', label: t('podLogViewer.time6h', 'Last 6 hours') },
          ]}
        />
        <div className="soha-step-inline">
          <Text type="secondary" style={{ fontSize: 12 }}>
            {t('podLogViewer.autoScroll', 'Auto scroll')}
          </Text>
          <Switch
            size="small"
            checked={autoScroll}
            onChange={(checked) => setAutoScroll(checked)}
          />
        </div>
        <div className="soha-step-inline">
          <Text type="secondary" style={{ fontSize: 12 }}>
            {localeCode === 'zh_CN' ? '历史日志' : 'Historical logs'}
          </Text>
          <Switch size="small" checked={previous} onChange={(checked) => setPrevious(checked)} />
        </div>
        <Button icon={<DeleteOutlined />} size="small" type="text" onClick={handleClear}>
          {t('podLogViewer.clear', 'Clear')}
        </Button>
        <Button
          size="small"
          type="text"
          onClick={handleExport}
          disabled={filteredLines.length === 0}
        >
          {localeCode === 'zh_CN' ? '导出日志' : 'Export Logs'}
        </Button>
        <Button
          icon={<ReloadOutlined />}
          size="small"
          type="text"
          onClick={() => fetchSnapshot(historyLines)}
        >
          {t('podLogViewer.reconnect', 'Reconnect')}
        </Button>
        {toolbarExtra}
        {onOpenLogCenter ? (
          <Button icon={<ExportOutlined />} size="small" onClick={onOpenLogCenter}>
            {localeCode === 'zh_CN' ? '在日志中心打开' : 'Open in Log Center'}
          </Button>
        ) : null}
      </Flex>
      {filteredLines.length > 0 ? (
        <List
          className="soha-log-shell"
          defaultHeight={540}
          listRef={listRef}
          onScroll={(event) => {
            void handleScroll(event.currentTarget)
          }}
          overscanCount={8}
          rowComponent={LogRow}
          rowCount={filteredLines.length}
          rowHeight={LOG_ROW_HEIGHT}
          rowProps={logRowProps}
        >
          {loadingOlder ? (
            <div className="soha-log-loading">
              {localeCode === 'zh_CN' ? '加载更早日志中...' : 'Loading older logs...'}
            </div>
          ) : null}
        </List>
      ) : (
        <div className="soha-log-shell">
          <div className="soha-log-loading">{emptyLogMessage}</div>
        </div>
      )}
    </Card>
  )
}
