import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { DeleteOutlined, DownloadOutlined, FileOutlined, FolderOutlined, ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Input, List, Select, Space, Switch, Tag, Typography } from 'antd'
import type { Terminal } from '@xterm/xterm'
import type { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useQuery } from '@tanstack/react-query'
import { ManagementState } from '@/components/management-list'
import '@/components/resource-operation-panels.css'
import { buildSameOriginStreamURL, withStreamTicket } from '@/features/auth/stream-ticket'
import { downloadText } from '@/utils/download'
import { dockerApi } from './docker-api'
import type { DockerProjectVolumeFileEntry } from './docker-types'
import './docker-pages.css'

const { Text } = Typography
const { TextArea } = Input

interface DockerRuntimeServiceOption {
  label: string
  value: string
}

interface DockerRuntimePanelProps {
  projectId: string
  projectName?: string
  serviceName?: string
  serviceOptions: DockerRuntimeServiceOption[]
  servicesLoading?: boolean
  onServiceChange: (serviceName: string) => void
}

interface TerminalMessage {
  type: string
  data?: string
  message?: string
}

function dockerRuntimeWebSocketURL(projectId: string, path: 'logs/stream' | 'terminal', params: Record<string, string | number | undefined>) {
  const base = buildSameOriginStreamURL(`/api/v1/docker/projects/${encodeURIComponent(projectId)}/runtime/${path}`, 'ws')
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') {
      base.searchParams.set(key, String(value))
    }
  })
  return base.toString()
}

function splitLogContent(content?: string) {
  return (content || '').split(/\r?\n/).filter((line) => line.trim() !== '')
}

function appendLogLine(items: string[], line: string) {
  const next = [...items, line]
  return next.length > 2000 ? next.slice(next.length - 2000) : next
}

function runtimeServiceSelector({
  disabled,
  loading,
  onChange,
  options,
  serviceName,
}: {
  disabled?: boolean
  loading?: boolean
  onChange: (serviceName: string) => void
  options: DockerRuntimeServiceOption[]
  serviceName?: string
}) {
  return (
    <Select
      disabled={disabled}
      loading={loading}
      options={options}
      placeholder="选择服务"
      popupMatchSelectWidth={false}
      size="small"
      style={{ minWidth: 180 }}
      value={serviceName || undefined}
      onChange={onChange}
    />
  )
}

export function DockerProjectLogsPanel({
  projectId,
  projectName,
  serviceName,
  serviceOptions,
  servicesLoading,
  onServiceChange,
}: DockerRuntimePanelProps) {
  const socketRef = useRef<WebSocket | null>(null)
  const streamRunRef = useRef(0)
  const [tailLines, setTailLines] = useState(200)
  const [streaming, setStreaming] = useState(true)
  const [connectionState, setConnectionState] = useState<'idle' | 'connecting' | 'connected' | 'closed' | 'error'>('idle')
  const [lines, setLines] = useState<string[]>([])

  const logsQuery = useQuery({
    enabled: Boolean(projectId && serviceName && !streaming),
    queryKey: ['docker', 'project-runtime-logs', projectId, serviceName, tailLines],
    queryFn: () => dockerApi.projectLogs(projectId, { serviceName, tailLines }),
  })

  useEffect(() => {
    if (logsQuery.data?.data) {
      setLines(splitLogContent(logsQuery.data.data.content))
    }
  }, [logsQuery.data])

  const streamURL = useMemo(() => {
    if (!projectId || !serviceName) return ''
    return dockerRuntimeWebSocketURL(projectId, 'logs/stream', { serviceName, tailLines })
  }, [projectId, serviceName, tailLines])

  const closeStream = useCallback(() => {
    streamRunRef.current += 1
    const socket = socketRef.current
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: 'close' }))
    }
    socket?.close()
    socketRef.current = null
  }, [])

  useEffect(() => {
    closeStream()
    if (!streaming || !streamURL) {
      setConnectionState('idle')
      return undefined
    }
    const runId = streamRunRef.current + 1
    streamRunRef.current = runId
    setConnectionState('connecting')
    setLines([])
    void (async () => {
      let ticketedURL = ''
      try {
        ticketedURL = await withStreamTicket(streamURL)
      } catch {
        if (streamRunRef.current !== runId) return
        setConnectionState('error')
        return
      }
      if (streamRunRef.current !== runId) return
      const socket = new WebSocket(ticketedURL)
      socketRef.current = socket
      socket.onopen = () => {
        if (socketRef.current !== socket) return
        setConnectionState('connected')
      }
      socket.onmessage = (event) => {
        if (socketRef.current !== socket) return
        const message = JSON.parse(event.data) as TerminalMessage
        if (message.type === 'log' && message.data !== undefined) {
          setLines((current) => appendLogLine(current, message.data || ''))
        }
        if (message.type === 'exit') {
          setConnectionState('closed')
        }
        if (message.type === 'error') {
          setConnectionState('error')
          if (message.message) {
            setLines((current) => appendLogLine(current, `[error] ${message.message}`))
          }
        }
      }
      socket.onerror = () => {
        if (socketRef.current !== socket) return
        setConnectionState('error')
      }
      socket.onclose = () => {
        if (socketRef.current !== socket) return
        setConnectionState((current) => current === 'error' ? 'error' : 'closed')
      }
    })()
    return () => {
      closeStream()
    }
  }, [closeStream, streamURL, streaming])

  const statusColor = connectionState === 'connected' ? 'green' : connectionState === 'error' ? 'red' : 'default'

  if (serviceOptions.length === 0 && !servicesLoading) {
    return (
      <Card className="soha-docker-runtime-card" size="small">
        <ManagementState compact kind="empty" title="没有可用服务" description="该项目还没有同步到可用于运行时访问的服务记录。" />
      </Card>
    )
  }

  return (
    <Card
      className="soha-docker-runtime-card soha-log-card"
      size="small"
      title="日志"
      extra={(
        <Space size={8} wrap>
          {runtimeServiceSelector({ loading: servicesLoading, options: serviceOptions, serviceName, onChange: onServiceChange })}
          <Select
            options={[100, 200, 500, 1000, 2000].map((value) => ({ label: `${value} 行`, value }))}
            popupMatchSelectWidth={false}
            size="small"
            value={tailLines}
            onChange={setTailLines}
          />
          <Switch checked={streaming} checkedChildren="实时" size="small" unCheckedChildren="快照" onChange={setStreaming} />
          <Button icon={<ReloadOutlined />} loading={logsQuery.isFetching} size="small" onClick={() => streaming ? setLines([]) : logsQuery.refetch()}>刷新</Button>
          <Button icon={<DownloadOutlined />} size="small" onClick={() => downloadText(`${projectName || projectId}-${serviceName || 'service'}.log`, lines.join('\n'))}>下载</Button>
        </Space>
      )}
    >
      <Space className="soha-terminal-toolbar" size={8}>
        <Tag color={statusColor}>{streaming ? connectionState : 'snapshot'}</Tag>
        <Text type="secondary">{serviceName || '未选择服务'}</Text>
      </Space>
      <div className="soha-log-shell">
        {lines.length === 0 ? (
          <div className="soha-log-loading">{logsQuery.isFetching || connectionState === 'connecting' ? '正在读取日志...' : '暂无日志'}</div>
        ) : lines.map((line, index) => (
          <div className="soha-log-row soha-log-row-plain" key={`${index}-${line.slice(0, 32)}`}>
            <span className="soha-log-row-text">{line}</span>
          </div>
        ))}
      </div>
    </Card>
  )
}

export function DockerProjectTerminalPanel({
  projectId,
  serviceName,
  serviceOptions,
  servicesLoading,
  onServiceChange,
}: DockerRuntimePanelProps) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const socketRef = useRef<WebSocket | null>(null)
  const resizeObserverRef = useRef<ResizeObserver | null>(null)
  const terminalRunRef = useRef(0)
  const [shell, setShell] = useState('/bin/sh')
  const [connectionState, setConnectionState] = useState<'idle' | 'connecting' | 'connected' | 'closed' | 'error'>('idle')

  const terminalURL = useMemo(() => {
    if (!projectId || !serviceName) return ''
    return dockerRuntimeWebSocketURL(projectId, 'terminal', { serviceName, shell })
  }, [projectId, serviceName, shell])

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
    if (!containerRef.current || !terminalURL) {
      return
    }
    void (async () => {
      disposeTerminal()
      const runId = terminalRunRef.current + 1
      terminalRunRef.current = runId
      setConnectionState('connecting')
      let ticketedURL = ''
      try {
        ticketedURL = await withStreamTicket(terminalURL)
      } catch {
        if (terminalRunRef.current !== runId) return
        setConnectionState('error')
        return
      }
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
        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, monospace',
        fontSize: 13,
        theme: { background: '#0b1220', foreground: '#e5edf5' },
      })
      const fitAddon = new XTermFitAddon()
      terminal.loadAddon(fitAddon)
      terminal.open(containerRef.current)
      fitAddon.fit()
      terminal.writeln('Connecting to docker service...')
      terminalRef.current = terminal
      fitAddonRef.current = fitAddon

      const socket = new WebSocket(ticketedURL)
      socketRef.current = socket
      socket.onopen = () => {
        if (socketRef.current !== socket) return
        setConnectionState('connected')
        terminal.writeln('Connected.')
      }
      socket.onmessage = (event) => {
        if (socketRef.current !== socket) return
        const message = JSON.parse(event.data) as TerminalMessage
        if (message.type === 'stdout' || message.type === 'stderr') {
          terminal.write(message.data || '')
        }
        if (message.type === 'status' && message.message) {
          terminal.writeln(`\r\n${message.message}`)
        }
        if (message.type === 'error') {
          setConnectionState('error')
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
      }
      socket.onclose = () => {
        if (socketRef.current !== socket) return
        setConnectionState((current) => current === 'error' ? 'error' : 'closed')
      }
      terminal.onData((data) => {
        if (socket.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({ type: 'input', data }))
        }
      })
      resizeObserverRef.current = new ResizeObserver(() => fitAddon.fit())
      resizeObserverRef.current.observe(containerRef.current)
    })()
  }, [disposeTerminal, terminalURL])

  useEffect(() => disposeTerminal, [disposeTerminal])

  const statusColor = connectionState === 'connected' ? 'green' : connectionState === 'error' ? 'red' : 'default'

  return (
    <Card
      className="soha-docker-runtime-card"
      size="small"
      title="Shell"
      extra={(
        <Space size={8} wrap>
          {runtimeServiceSelector({ loading: servicesLoading, options: serviceOptions, serviceName, onChange: onServiceChange })}
          <Select
            options={[{ value: '/bin/sh', label: 'sh' }, { value: '/bin/bash', label: 'bash' }]}
            popupMatchSelectWidth={false}
            size="small"
            value={shell}
            onChange={setShell}
          />
          <Button disabled={!serviceName || connectionState === 'connecting'} size="small" type="primary" onClick={connect}>连接</Button>
          <Button icon={<DeleteOutlined />} size="small" onClick={disposeTerminal}>断开</Button>
        </Space>
      )}
    >
      <Space className="soha-terminal-toolbar" size={8}>
        <Tag color={statusColor}>{connectionState}</Tag>
        <Text type="secondary">{serviceName || '未选择服务'}</Text>
      </Space>
      <div className="soha-terminal-shell">
        <div ref={containerRef} className="soha-terminal-shell-inner" />
      </div>
    </Card>
  )
}

export function DockerProjectVolumesPanel({
  projectId,
  serviceName,
  serviceOptions,
  servicesLoading,
  onServiceChange,
}: DockerRuntimePanelProps) {
  const [target, setTarget] = useState('')
  const [currentPath, setCurrentPath] = useState('/')
  const [previewPath, setPreviewPath] = useState('')
  const volumesQuery = useQuery({
    enabled: Boolean(projectId && serviceName),
    queryKey: ['docker', 'project-runtime-volumes', projectId, serviceName],
    queryFn: () => dockerApi.projectVolumes(projectId, { serviceName }),
  })
  const volumes = volumesQuery.data?.data ?? []
  const filesQuery = useQuery({
    enabled: Boolean(projectId && serviceName && target),
    queryKey: ['docker', 'project-runtime-volume-files', projectId, serviceName, target, currentPath],
    queryFn: () => dockerApi.projectVolumeFiles(projectId, { serviceName, target, path: currentPath, limit: 300 }),
  })
  const fileQuery = useQuery({
    enabled: Boolean(projectId && serviceName && target && previewPath),
    queryKey: ['docker', 'project-runtime-volume-file', projectId, serviceName, target, previewPath],
    queryFn: () => dockerApi.projectVolumeFile(projectId, { serviceName, target, path: previewPath, limitBytes: 262144 }),
  })

  useEffect(() => {
    const firstTarget = volumes[0]?.target || ''
    setTarget((current) => current || firstTarget)
  }, [volumes])

  useEffect(() => {
    setTarget('')
    setCurrentPath('/')
    setPreviewPath('')
  }, [serviceName])

  const selectedVolume = volumes.find((item) => item.target === target)
  const entries = filesQuery.data?.data.items ?? []
  const preview = fileQuery.data?.data
  const goParent = () => {
    const clean = currentPath.replace(/\/+$/, '')
    const parent = clean.includes('/') ? clean.slice(0, clean.lastIndexOf('/')) || '/' : '/'
    setCurrentPath(parent)
    setPreviewPath('')
  }
  const openEntry = (entry: DockerProjectVolumeFileEntry) => {
    if (entry.kind === 'directory') {
      setCurrentPath(entry.path || '/')
      setPreviewPath('')
      return
    }
    setPreviewPath(entry.path)
  }

  return (
    <Card
      className="soha-docker-runtime-card"
      size="small"
      title="卷文件"
      extra={(
        <Space size={8} wrap>
          {runtimeServiceSelector({ loading: servicesLoading, options: serviceOptions, serviceName, onChange: onServiceChange })}
          <Select
            disabled={volumes.length === 0}
            loading={volumesQuery.isFetching}
            options={volumes.map((volume) => ({ label: volume.target, value: volume.target }))}
            placeholder="选择卷"
            popupMatchSelectWidth={false}
            size="small"
            style={{ minWidth: 180 }}
            value={target || undefined}
            onChange={(value) => {
              setTarget(value)
              setCurrentPath('/')
              setPreviewPath('')
            }}
          />
          <Button icon={<ReloadOutlined />} loading={filesQuery.isFetching || volumesQuery.isFetching} size="small" onClick={() => {
            volumesQuery.refetch()
            filesQuery.refetch()
          }}>刷新</Button>
        </Space>
      )}
    >
      {volumes.length === 0 && !volumesQuery.isFetching ? (
        <ManagementState compact kind="empty" title="没有可浏览的卷" description="该服务没有声明可从容器内浏览的卷挂载。" />
      ) : (
        <>
          <div className="soha-docker-volume-toolbar">
            <Button disabled={currentPath === '/'} size="small" onClick={goParent}>上级</Button>
            <Input size="small" value={currentPath} onChange={(event) => setCurrentPath(event.target.value || '/')} />
            {selectedVolume ? (
              <Space size={6} wrap>
                {selectedVolume.readOnly ? <Tag>只读</Tag> : null}
                {selectedVolume.source ? <Text type="secondary">{selectedVolume.source}</Text> : null}
              </Space>
            ) : null}
          </div>
          <div className="soha-docker-volume-browser">
            <div className="soha-docker-volume-list">
              <List
                dataSource={entries}
                loading={filesQuery.isFetching}
                locale={{ emptyText: '暂无文件' }}
                renderItem={(entry) => (
                  <List.Item className="soha-docker-volume-file-row" onClick={() => openEntry(entry)}>
                    <Space size={8}>
                      {entry.kind === 'directory' ? <FolderOutlined /> : <FileOutlined />}
                      <Text>{entry.name}</Text>
                    </Space>
                    <Text type="secondary">{entry.kind === 'directory' ? '-' : `${entry.sizeBytes ?? 0} B`}</Text>
                  </List.Item>
                )}
              />
            </div>
            <div className="soha-docker-volume-preview">
              <div className="soha-docker-volume-preview-toolbar">
                <Text type="secondary">{fileQuery.isFetching ? '正在读取文件...' : previewPath || '选择文件预览'}</Text>
                <Button
                  disabled={!preview}
                  icon={<DownloadOutlined />}
                  size="small"
                  onClick={() => preview && downloadText(preview.path.split('/').pop() || 'volume-file.txt', preview.content)}
                >
                  下载
                </Button>
              </div>
              <TextArea
                readOnly
                rows={18}
                spellCheck={false}
                value={preview ? `${preview.truncated ? '[内容已截断]\n' : ''}${preview.content}` : ''}
              />
            </div>
          </div>
        </>
      )}
    </Card>
  )
}
