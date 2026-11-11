import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { DisconnectOutlined, LinkOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Select, Space, Typography } from 'antd'
import type { Terminal } from '@xterm/xterm'
import type { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import '@/components/resource-operation-panels.css'
import { withStreamTicket } from '@/features/auth'
import { localeText, useI18n } from '@/i18n'
import { readTerminalThemeColors } from '@/theme/app-theme'
import { parseStreamMessage } from '@/utils/stream-message'
import {
  dockerRuntimeWebSocketURL,
  runtimeServiceSelector,
  type DockerRuntimePanelProps,
  type TerminalMessage,
} from './shared'
import './styles.css'

const { Text } = Typography

export function DockerProjectTerminalPanel({
  enabled,
  projectId,
  serviceName,
  serviceOptions,
  servicesLoading,
  servicesError,
  onServiceChange,
}: DockerRuntimePanelProps) {
  const { localeCode } = useI18n()
  const containerRef = useRef<HTMLDivElement | null>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const socketRef = useRef<WebSocket | null>(null)
  const resizeObserverRef = useRef<ResizeObserver | null>(null)
  const terminalRunRef = useRef(0)
  const [shell, setShell] = useState('/bin/sh')
  const [connectionState, setConnectionState] = useState<
    'idle' | 'connecting' | 'connected' | 'closed' | 'error'
  >('idle')
  const [connectionError, setConnectionError] = useState('')
  const canConnectRuntime =
    enabled &&
    Boolean(
      projectId && serviceName && serviceOptions.some((option) => option.value === serviceName),
    )

  const terminalURL = useMemo(() => {
    if (!canConnectRuntime) return ''
    return dockerRuntimeWebSocketURL(projectId, 'terminal', { serviceName, shell })
  }, [canConnectRuntime, projectId, serviceName, shell])

  const disposeTerminal = useCallback(() => {
    terminalRunRef.current += 1
    resizeObserverRef.current?.disconnect()
    resizeObserverRef.current = null
    const socket = socketRef.current
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: 'close' }))
    }
    socket?.close()
    socketRef.current = null
    terminalRef.current?.dispose()
    terminalRef.current = null
    fitAddonRef.current = null
    if (containerRef.current) {
      containerRef.current.innerHTML = ''
    }
  }, [])

  const connect = useCallback(() => {
    if (!enabled || !containerRef.current || !terminalURL) {
      return
    }
    void (async () => {
      disposeTerminal()
      const runId = terminalRunRef.current + 1
      terminalRunRef.current = runId
      setConnectionState('connecting')
      setConnectionError('')
      let ticketedURL: string
      try {
        ticketedURL = await withStreamTicket(terminalURL)
      } catch {
        if (terminalRunRef.current !== runId) return
        setConnectionState('error')
        setConnectionError(
          localeText(
            localeCode,
            '获取连接凭据失败，请重试连接。',
            'Could not obtain a connection ticket; retry the connection.',
          ),
        )
        return
      }
      try {
        const [{ Terminal: XTerm }, { FitAddon: XTermFitAddon }] = await Promise.all([
          import('@xterm/xterm'),
          import('@xterm/addon-fit'),
        ])
        if (terminalRunRef.current !== runId || !containerRef.current) {
          return
        }
        const terminal = new XTerm({
          cursorBlink: true,
          convertEol: true,
          fontFamily:
            'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, monospace',
          fontSize: 13,
          theme: readTerminalThemeColors(),
        })
        const fitAddon = new XTermFitAddon()
        terminalRef.current = terminal
        fitAddonRef.current = fitAddon
        terminal.loadAddon(fitAddon)
        terminal.open(containerRef.current)
        fitAddon.fit()
        terminal.writeln('Connecting to docker service...')
        const socket = new WebSocket(ticketedURL)
        socketRef.current = socket
        socket.onopen = () => {
          if (socketRef.current !== socket) return
          setConnectionState('connected')
          terminal.writeln('Connected.')
        }
        socket.onmessage = (event) => {
          if (socketRef.current !== socket) return
          const message = parseStreamMessage<TerminalMessage>(event.data)
          if (!message) return
          if (message.type === 'stdout' || message.type === 'stderr') {
            terminal.write(message.data || '')
          }
          if (message.type === 'status' && message.message) {
            terminal.writeln(`\r\n${message.message}`)
          }
          if (message.type === 'error') {
            setConnectionState('error')
            setConnectionError(
              localeText(
                localeCode,
                'Shell 返回错误，请检查服务状态后重试连接。',
                'The Shell reported an error; check the service and retry.',
              ),
            )
            terminal.writeln(`\r\n${message.message || 'terminal error'}`)
          }
          if (message.type === 'exit') {
            setConnectionState('closed')
            terminal.writeln('\r\nSession closed.')
          }
        }
        socket.onerror = () => {
          if (socketRef.current !== socket) return
          setConnectionState('error')
          setConnectionError(
            localeText(
              localeCode,
              'Shell 连接失败，请检查主机与服务后重试。',
              'Shell connection failed; check the host and service before retrying.',
            ),
          )
        }
        socket.onclose = () => {
          if (socketRef.current !== socket) return
          setConnectionState((current) => (current === 'error' ? 'error' : 'closed'))
        }
        terminal.onData((data) => {
          if (socket.readyState === WebSocket.OPEN) {
            socket.send(JSON.stringify({ type: 'input', data }))
          }
        })
        resizeObserverRef.current = new ResizeObserver(() => fitAddon.fit())
        resizeObserverRef.current.observe(containerRef.current)
      } catch {
        if (terminalRunRef.current !== runId) return
        disposeTerminal()
        setConnectionState('error')
        setConnectionError(
          localeText(
            localeCode,
            '无法初始化 Shell，请重试连接。',
            'Could not initialize the Shell; retry the connection.',
          ),
        )
      }
    })()
  }, [disposeTerminal, enabled, terminalURL, localeCode])

  useEffect(() => {
    disposeTerminal()
    setConnectionState('idle')
    setConnectionError('')
  }, [disposeTerminal, terminalURL])

  useEffect(() => disposeTerminal, [disposeTerminal])

  if (!enabled) {
    return (
      <Card className="soha-detail-card" size="small">
        <ManagementState
          compact
          kind="no-permission"
          title={localeText(localeCode, 'Shell 不可用', 'Shell unavailable')}
          description={localeText(
            localeCode,
            'Docker 模块或当前权限不允许连接运行时 Shell。',
            'The Docker module or your permissions do not allow runtime Shell access.',
          )}
        />
      </Card>
    )
  }

  return (
    <div className="soha-pod-terminal-tab-card">
      <Card className="soha-detail-card" size="small">
        <div className="soha-terminal-toolbar">
          <div className="soha-terminal-toolbar-group">
            <div className="soha-terminal-control-group">
              <Text strong>{localeText(localeCode, '服务', 'Service')}:</Text>
              {runtimeServiceSelector({
                disabled: !enabled,
                loading: servicesLoading,
                options: serviceOptions,
                serviceName,
                localeCode,
                onChange: onServiceChange,
              })}
            </div>
            <div className="soha-terminal-control-group">
              <Text strong>Shell:</Text>
              <Select
                disabled={!enabled}
                options={[
                  { value: '/bin/sh', label: 'sh' },
                  { value: '/bin/bash', label: 'bash' },
                ]}
                popupMatchSelectWidth={false}
                size="small"
                value={shell}
                onChange={setShell}
              />
            </div>
            <StatusTag value={connectionState} />
          </div>
          <Space size={4} wrap>
            <Button
              disabled={!canConnectRuntime || connectionState === 'connecting'}
              icon={<LinkOutlined />}
              size="small"
              type="primary"
              onClick={connect}
            >
              {connectionState === 'error'
                ? localeText(localeCode, '重试连接', 'Retry connection')
                : localeText(localeCode, '连接', 'Connect')}
            </Button>
            <Button
              disabled={connectionState === 'idle' || connectionState === 'closed'}
              icon={<DisconnectOutlined />}
              size="small"
              type="text"
              onClick={() => {
                disposeTerminal()
                setConnectionState('closed')
                setConnectionError('')
              }}
            >
              {localeText(localeCode, '断开', 'Disconnect')}
            </Button>
          </Space>
        </div>
        {connectionError ? <Alert showIcon type="error" title={connectionError} /> : null}
        {serviceOptions.length === 0 ? (
          <ManagementState
            compact
            kind={servicesLoading ? 'loading' : servicesError ? 'error' : 'empty'}
            title={
              servicesLoading
                ? localeText(localeCode, '正在读取服务', 'Loading services')
                : servicesError
                  ? localeText(localeCode, '服务列表读取失败', 'Service list could not be read')
                  : localeText(localeCode, '没有可用服务', 'No available services')
            }
            description={
              servicesError && !servicesLoading
                ? localeText(
                    localeCode,
                    '请通过页面上方的“重试读取”刷新项目服务。',
                    'Retry reading project services using the action above.',
                  )
                : undefined
            }
          />
        ) : null}
        <div className="soha-terminal-shell" hidden={serviceOptions.length === 0}>
          <div ref={containerRef} className="soha-terminal-shell-inner" />
        </div>
      </Card>
    </div>
  )
}
