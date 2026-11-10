import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Tag, Typography } from 'antd'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { ManagementState } from '@/components/management-list'
import './resource-operation-panels.css'
import { buildSameOriginStreamURL, withStreamTicket } from '@/features/auth/stream-ticket'
import { useI18n } from '@/i18n'
import { readTerminalThemeColors } from '@/theme/app-theme'
import { parseStreamMessage } from '@/utils/stream-message'

const { Text } = Typography
const TERMINAL_RESIZE_SETTLE_MS = 150

interface TerminalMessage {
  type: string
  data?: string
  message?: string
  cols?: number
  rows?: number
}

function buildTerminalWebSocketURL({
  clusterId,
  namespace,
  podName,
  expectedUid,
  container,
  shell,
}: {
  clusterId: string
  namespace: string
  expectedUid?: string
  podName: string
  container?: string
  shell: string
}) {
  const url = buildSameOriginStreamURL(
    `/api/v1/clusters/${encodeURIComponent(clusterId)}/workloads/pods/${encodeURIComponent(podName)}/terminal`,
    'ws',
  )
  url.searchParams.set('namespace', namespace)
  if (expectedUid) url.searchParams.set('expectedUid', expectedUid)
  url.searchParams.set('shell', shell)
  if (container) {
    url.searchParams.set('container', container)
  }
  return url.toString()
}

export function PodTerminal({
  clusterId,
  namespace,
  podName,
  expectedUid,
  container,
  shell = '/bin/sh',
  beforeConnect,
  toolbarContent,
}: {
  clusterId?: string | null
  namespace?: string | null
  expectedUid?: string
  podName: string
  container?: string
  shell?: string
  beforeConnect?: () => Promise<boolean>
  toolbarContent?: ReactNode
}) {
  const { t } = useI18n()
  const containerRef = useRef<HTMLDivElement | null>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const socketRef = useRef<WebSocket | null>(null)
  const resizeObserverRef = useRef<ResizeObserver | null>(null)
  const resizeSendTimerRef = useRef<number | null>(null)
  const lastSentSizeRef = useRef<{ cols: number; rows: number } | null>(null)
  const previousContainerRef = useRef<string | undefined>(container)
  const previousShellRef = useRef<string>(shell)
  const sessionRef = useRef(0)
  const [connectionState, setConnectionState] = useState<
    'idle' | 'connecting' | 'connected' | 'closed' | 'error'
  >('idle')
  const [, setLastMessage] = useState(t('podTerminal.idle', 'Terminal has not been connected yet'))
  const [reconnectNotice, setReconnectNotice] = useState('')

  const canConnect = Boolean(clusterId && namespace)

  const terminalURL = useMemo(() => {
    if (!clusterId || !namespace) return ''
    return buildTerminalWebSocketURL({
      clusterId,
      namespace,
      podName,
      expectedUid,
      container,
      shell,
    })
  }, [clusterId, container, namespace, podName, expectedUid, shell])

  const sendResize = useCallback(() => {
    const terminal = terminalRef.current
    const socket = socketRef.current
    if (!terminal || !socket || socket.readyState !== WebSocket.OPEN) {
      return
    }
    const lastSentSize = lastSentSizeRef.current
    if (lastSentSize?.cols === terminal.cols && lastSentSize.rows === terminal.rows) return
    lastSentSizeRef.current = { cols: terminal.cols, rows: terminal.rows }
    socket.send(JSON.stringify({ type: 'resize', cols: terminal.cols, rows: terminal.rows }))
  }, [])

  const disposeTerminal = useCallback(() => {
    if (resizeSendTimerRef.current !== null) {
      window.clearTimeout(resizeSendTimerRef.current)
      resizeSendTimerRef.current = null
    }
    lastSentSizeRef.current = null
    resizeObserverRef.current?.disconnect()
    resizeObserverRef.current = null
    if (socketRef.current && socketRef.current.readyState === WebSocket.OPEN) {
      socketRef.current.send(JSON.stringify({ type: 'close' }))
    }
    socketRef.current?.close()
    socketRef.current = null
    terminalRef.current?.dispose()
    terminalRef.current = null
    fitAddonRef.current = null
    if (containerRef.current) {
      containerRef.current.innerHTML = ''
    }
  }, [])

  const connect = useCallback(async () => {
    if (!containerRef.current || !terminalURL) {
      return
    }
    const sessionId = ++sessionRef.current
    disposeTerminal()
    if (containerRef.current) {
      containerRef.current.innerHTML = ''
    }
    if (beforeConnect && !(await beforeConnect())) return
    if (sessionRef.current !== sessionId || !containerRef.current) return
    setConnectionState('connecting')
    setLastMessage(t('podTerminal.connecting', 'Connecting terminal...'))

    const terminal = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily:
        'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, monospace',
      theme: {
        ...readTerminalThemeColors(),
      },
      convertEol: false,
      scrollback: 3000,
    })
    const fitAddon = new FitAddon()
    terminal.loadAddon(fitAddon)
    terminal.open(containerRef.current)
    fitAddon.fit()
    terminalRef.current = terminal
    fitAddonRef.current = fitAddon

    let ticketedURL: string
    try {
      ticketedURL = await withStreamTicket(terminalURL)
    } catch {
      if (sessionRef.current !== sessionId) return
      setConnectionState('error')
      setLastMessage(t('podTerminal.failed', 'Terminal connection failed'))
      terminal.writeln('\r\n[error] terminal authorization failed')
      return
    }
    if (sessionRef.current !== sessionId) return
    const socket = new WebSocket(ticketedURL)
    socketRef.current = socket

    socket.onopen = () => {
      if (sessionRef.current !== sessionId) return
      setConnectionState('connected')
      if (reconnectNotice) {
        setLastMessage(reconnectNotice)
        setReconnectNotice('')
      } else {
        setLastMessage(t('podTerminal.connected', 'Terminal connected'))
      }
      terminal.focus()
      fitAddon.fit()
      sendResize()
    }
    socket.onmessage = (event) => {
      if (sessionRef.current !== sessionId) return
      const payload = parseStreamMessage<TerminalMessage>(event.data)
      if (!payload) return
      switch (payload.type) {
        case 'stdout':
        case 'stderr':
          terminal.write(payload.data || '')
          break
        case 'status':
          if (payload.message) {
            setLastMessage(payload.message)
          }
          break
        case 'exit':
          setConnectionState('closed')
          setLastMessage(payload.message || t('podTerminal.closed', 'Terminal session ended'))
          terminal.writeln(`\r\n[session] ${payload.message || 'terminal session closed'}`)
          break
      }
    }
    socket.onerror = () => {
      if (sessionRef.current !== sessionId) return
      setConnectionState('error')
      setLastMessage(t('podTerminal.failed', 'Terminal connection failed'))
      terminal.writeln('\r\n[error] terminal connection failed')
    }
    socket.onclose = () => {
      if (sessionRef.current !== sessionId) return
      setConnectionState((current) => (current === 'error' ? 'error' : 'closed'))
    }

    terminal.onData((data) => {
      if (sessionRef.current !== sessionId) return
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: 'input', data }))
      }
    })

    resizeObserverRef.current = new ResizeObserver(() => {
      if (sessionRef.current !== sessionId) return
      fitAddon.fit()
      if (resizeSendTimerRef.current !== null) {
        window.clearTimeout(resizeSendTimerRef.current)
      }
      resizeSendTimerRef.current = window.setTimeout(() => {
        resizeSendTimerRef.current = null
        if (sessionRef.current === sessionId) sendResize()
      }, TERMINAL_RESIZE_SETTLE_MS)
    })
    resizeObserverRef.current.observe(containerRef.current)
  }, [beforeConnect, disposeTerminal, sendResize, t, terminalURL])

  useEffect(() => {
    if (!canConnect) return
    const frame = window.requestAnimationFrame(() => {
      connect()
    })
    return () => {
      window.cancelAnimationFrame(frame)
      sessionRef.current += 1
      disposeTerminal()
    }
  }, [canConnect, connect, disposeTerminal])

  useEffect(() => {
    if (!canConnect) return
    const previousContainer = previousContainerRef.current
    const previousShell = previousShellRef.current
    const containerChanged = previousContainer !== undefined && previousContainer !== container
    const shellChanged = previousShell !== shell

    if (containerChanged || shellChanged) {
      const nextNotice = containerChanged
        ? t(
            'podTerminal.containerReconnected',
            'Container changed, terminal reconnected automatically',
          )
        : t('podTerminal.shellReconnected', 'Shell changed, terminal reconnected automatically')
      setReconnectNotice(nextNotice)
    }

    previousContainerRef.current = container
    previousShellRef.current = shell
  }, [canConnect, container, shell, t])

  if (!canConnect) {
    return (
      <ManagementState
        compact
        kind="select-scope"
        title={t(
          'podTerminal.notReady',
          'Select a valid cluster and namespace before connecting the terminal',
        )}
      />
    )
  }

  return (
    <Card className="soha-detail-card">
      <div className="soha-terminal-toolbar">
        <div className="soha-terminal-toolbar-group">
          {toolbarContent ?? (
            <Text strong>
              {container
                ? `${t('common.container', 'Container')}: ${container}`
                : t('podTerminal.defaultContainer', 'Container: default')}
            </Text>
          )}
          <TagByState state={connectionState} />
        </div>
        <Button icon={<ReloadOutlined />} size="small" type="text" onClick={connect}>
          {t('podTerminal.reconnect', 'Reconnect')}
        </Button>
      </div>
      <div className="soha-terminal-shell">
        <div ref={containerRef} className="soha-terminal-shell-inner" />
      </div>
    </Card>
  )
}

function TagByState({
  state,
}: {
  state: 'idle' | 'connecting' | 'connected' | 'closed' | 'error'
}) {
  const { localeCode } = useI18n()
  const mapping = {
    idle: { color: undefined, label: localeCode === 'zh_CN' ? '空闲' : 'Idle' },
    connecting: { color: 'blue', label: localeCode === 'zh_CN' ? '连接中' : 'Connecting' },
    connected: { color: 'green', label: localeCode === 'zh_CN' ? '已连接' : 'Connected' },
    closed: { color: 'orange', label: localeCode === 'zh_CN' ? '已关闭' : 'Closed' },
    error: { color: 'red', label: localeCode === 'zh_CN' ? '错误' : 'Error' },
  } as const
  const current = mapping[state]
  return <Tag color={current.color}>{current.label}</Tag>
}
