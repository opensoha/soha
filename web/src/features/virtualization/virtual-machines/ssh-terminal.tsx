import { useCallback, useEffect, useRef, useState } from 'react'
import { Alert, Button, Space } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { Terminal } from '@xterm/xterm'
import type {
  VirtualMachineAccessBinding,
  VirtualMachineSSHFrame,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { buildSameOriginStreamURL, withStreamTicket } from '@/features/auth'
import { readTerminalThemeColors } from '@/theme/app-theme'
import { virtualizationMutations } from '../mutations'
import { virtualizationQueries } from '../queries'
import { virtualizationApi } from '../virtualization-api'
import '@xterm/xterm/css/xterm.css'

export function VMSSHTerminal({
  binding,
  enabled,
}: {
  binding: VirtualMachineAccessBinding
  enabled: boolean
}) {
  const container = useRef<HTMLDivElement>(null)
  const terminal = useRef<Terminal | null>(null)
  const socket = useRef<WebSocket | null>(null)
  const observer = useRef<ResizeObserver | null>(null)
  const currentSession = useRef('')
  const generation = useRef(0)
  const [sessionId, setSessionId] = useState('')
  const [state, setState] = useState('idle')
  const [error, setError] = useState('')
  const client = useQueryClient()
  const create = useMutation(virtualizationMutations.createSSHSession())
  const close = useMutation(virtualizationMutations.closeSSHSession(client))
  const receipt = useQuery(virtualizationQueries.sshSession(sessionId))
  const uncertainDisconnect = state === 'closed' && receipt.data?.status === 'unknown'

  const dispose = useCallback(() => {
    generation.current++
    observer.current?.disconnect()
    observer.current = null
    socket.current?.close()
    socket.current = null
    terminal.current?.dispose()
    terminal.current = null
  }, [])
  useEffect(
    () => () => {
      dispose()
      const id = currentSession.current
      if (id) void virtualizationApi.closeSSHSession(id).catch(() => undefined)
    },
    [dispose],
  )
  useEffect(() => {
    if (!enabled) {
      dispose()
      const id = currentSession.current
      if (id) void virtualizationApi.closeSSHSession(id).catch(() => undefined)
    }
  }, [enabled, dispose])

  async function connect() {
    if (!enabled || !container.current) return
    dispose()
    const run = generation.current
    setError('')
    setState('connecting')
    let id = ''
    try {
      const [{ Terminal: XTerm }, { FitAddon }] = await Promise.all([
        import('@xterm/xterm'),
        import('@xterm/addon-fit'),
      ])
      if (run !== generation.current || !container.current) return
      const term = new XTerm({ cursorBlink: true, fontSize: 13, theme: readTerminalThemeColors() })
      const fit = new FitAddon()
      term.loadAddon(fit)
      term.open(container.current)
      fit.fit()
      terminal.current = term
      const session = await create.mutateAsync({
        id: binding.vmId,
        input: {
          bindingRevision: binding.revision,
          cols: Math.max(1, Math.min(term.cols, 512)),
          rows: Math.max(1, Math.min(term.rows, 512)),
        },
      })
      id = session.id
      if (run !== generation.current) {
        await virtualizationApi.closeSSHSession(id)
        return
      }
      currentSession.current = id
      setSessionId(id)
      const url = await withStreamTicket(
        buildSameOriginStreamURL(
          `/api/v1/virtualization/ssh-sessions/${encodeURIComponent(id)}/stream`,
          'ws',
        ).toString(),
      )
      if (run !== generation.current) {
        await virtualizationApi.closeSSHSession(id)
        return
      }
      const connection = new WebSocket(url)
      socket.current = connection
      connection.onopen = () => {
        if (run === generation.current) {
          setState('active')
          term.focus()
        }
      }
      connection.onmessage = (event) => {
        if (run !== generation.current) return
        try {
          const frame = JSON.parse(String(event.data)) as VirtualMachineSSHFrame
          if (frame.type === 'output') {
            if (frame.dataEncoding === 'base64')
              term.write(Uint8Array.from(atob(frame.data ?? ''), (byte) => byte.charCodeAt(0)))
            else term.write(frame.data ?? '')
          }
          if (frame.type === 'error') setError(frame.message ?? 'SSH 连接失败')
          if (frame.type === 'close') {
            setState('closed')
            connection.close()
          }
        } catch {
          setError('无效的终端消息')
          connection.close()
        }
      }
      connection.onerror = () => {
        if (run === generation.current) setError('SSH 通道不可用，请检查绑定、执行端与 host key')
      }
      connection.onclose = () => {
        if (run === generation.current) {
          setState('closed')
          void client.invalidateQueries({ queryKey: virtualizationQueries.sshSession(id).queryKey })
        }
      }
      term.onData((data) => {
        if (connection.readyState === WebSocket.OPEN)
          connection.send(
            JSON.stringify({
              type: 'input',
              dataEncoding: 'base64',
              data: btoa(
                Array.from(new TextEncoder().encode(data), (byte) =>
                  String.fromCharCode(byte),
                ).join(''),
              ),
            }),
          )
      })
      term.onResize(({ cols, rows }) => {
        if (connection.readyState === WebSocket.OPEN)
          connection.send(
            JSON.stringify({
              type: 'resize',
              cols: Math.max(1, Math.min(cols, 512)),
              rows: Math.max(1, Math.min(rows, 512)),
            }),
          )
      })
      observer.current = new ResizeObserver(() => fit.fit())
      observer.current.observe(container.current)
    } catch (failure) {
      if (id) void virtualizationApi.closeSSHSession(id).catch(() => undefined)
      if (run === generation.current) {
        setError(failure instanceof Error ? failure.message : 'SSH 连接失败')
        setState('closed')
        dispose()
      }
    }
  }

  async function disconnect() {
    dispose()
    setState('closing')
    try {
      if (currentSession.current) await close.mutateAsync(currentSession.current)
      setState('closed')
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : '关闭会话失败，请重试')
      setState('closed')
    }
  }

  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Space>
        <Button
          type="primary"
          disabled={!enabled || state === 'active' || state === 'connecting' || state === 'closing'}
          loading={create.isPending}
          onClick={() => void connect()}
        >
          连接 Shell
        </Button>
        <Button disabled={!sessionId} loading={close.isPending} onClick={() => void disconnect()}>
          关闭会话
        </Button>
        <span>
          {uncertainDisconnect ? '连接已断开 · 执行结果待核验' : (receipt.data?.status ?? state)}
        </span>
      </Space>
      {error ? <Alert type="error" showIcon title={error} /> : null}
      {receipt.data?.reason ? (
        <Alert
          showIcon
          type={receipt.data.status === 'unknown' ? 'warning' : 'info'}
          title={
            uncertainDisconnect
              ? '连接已断开；guest 中已启动的命令或后台进程是否结束仍需核验。'
              : receipt.data.reason
          }
        />
      ) : null}
      <div
        ref={container}
        role="region"
        aria-label="VM SSH 终端"
        style={{ height: 360, width: '100%' }}
      />
    </Space>
  )
}
