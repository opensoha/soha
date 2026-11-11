import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Alert, App, Badge, Button, Card, Space, Spin } from 'antd'
import {
  CopyOutlined,
  FullscreenOutlined,
  FullscreenExitOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import RFB from '@novnc/novnc'
import { virtualizationQueries } from './queries'
import { ManagementState } from '@/components/management-list'
import { buildSameOriginStreamURL, withStreamTicket } from '@/features/auth'

const STATUS_BADGE: Record<string, 'success' | 'processing' | 'warning' | 'error' | 'default'> = {
  connecting: 'processing',
  connected: 'success',
  disconnected: 'warning',
  error: 'error',
}

const STATUS_LABEL: Record<string, string> = {
  connecting: '连接中',
  connected: '已连接',
  disconnected: '已断开',
  error: '连接失败',
}

export function VMConsole({ vmId }: { vmId: string }) {
  const [rfb, setRfb] = useState<RFB | null>(null)
  const [status, setStatus] = useState<'connecting' | 'connected' | 'disconnected' | 'error'>(
    'connecting',
  )
  const [errorMessage, setErrorMessage] = useState<string>('')
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [requestedVmId, setRequestedVmId] = useState('')
  const containerRef = useRef<HTMLDivElement>(null)
  const { message } = App.useApp()

  const consoleQuery = useQuery({
    ...virtualizationQueries.vmConsole(vmId),
    enabled: requestedVmId !== vmId,
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })

  useEffect(() => {
    // VM mutations invalidate the detail key and its console child. Only explicit entry/reconnect acquires another ticket.
    if (consoleQuery.isFetched) setRequestedVmId(vmId)
  }, [consoleQuery.isFetched, vmId])

  useEffect(() => {
    const consoleData = consoleQuery.data
    if (
      !consoleData?.ready ||
      !containerRef.current ||
      consoleData.message ||
      consoleQuery.isFetching ||
      consoleQuery.isError
    ) {
      return
    }

    let disposed = false
    let disconnected = false
    let rfbInstance: RFB | null = null
    void (async () => {
      try {
        setStatus('connecting')
        setErrorMessage('')
        const baseURL = buildSameOriginStreamURL(consoleData.url, 'ws')
        const wsUrl = await withStreamTicket(baseURL)
        if (disposed || !containerRef.current) return
        rfbInstance = new RFB(containerRef.current, wsUrl, {
          credentials: { password: consoleData.token || '' },
        })

        rfbInstance.scaleViewport = true
        rfbInstance.resizeSession = true

        rfbInstance.addEventListener('connect', () => {
          if (disposed) return
          setStatus('connected')
          setErrorMessage('')
        })

        rfbInstance.addEventListener('disconnect', (e: any) => {
          if (disposed) return
          disconnected = true
          setStatus('disconnected')
          if (e.detail.clean === false) {
            setErrorMessage(e.detail.reason || 'Connection closed unexpectedly')
          }
        })

        rfbInstance.addEventListener('securityfailure', (e: any) => {
          if (disposed) return
          setStatus('error')
          setErrorMessage(e.detail.reason || 'Security failure')
        })

        setRfb(rfbInstance)
      } catch (err) {
        if (disposed) return
        setStatus('error')
        setErrorMessage(String(err))
      }
    })()

    return () => {
      disposed = true
      if (!disconnected) {
        rfbInstance?.disconnect()
      }
    }
    // An explicit reconnect may return the same URL/token; the read timestamp still starts a new session.
  }, [
    consoleQuery.data,
    consoleQuery.dataUpdatedAt,
    consoleQuery.isFetching,
    consoleQuery.isError,
    vmId,
  ])

  useEffect(() => {
    const handler = () => setIsFullscreen(Boolean(document.fullscreenElement))
    document.addEventListener('fullscreenchange', handler)
    return () => document.removeEventListener('fullscreenchange', handler)
  }, [])

  const toggleFullscreen = () => {
    if (!containerRef.current) return

    if (!document.fullscreenElement) {
      containerRef.current.requestFullscreen?.()
    } else {
      document.exitFullscreen?.()
    }
  }

  const sendCtrlAltDel = () => {
    if (rfb) {
      rfb.sendCtrlAltDel()
    }
  }

  const handlePaste = async () => {
    if (!rfb) return
    try {
      const text = await navigator.clipboard.readText()
      if (!text) {
        message.info('剪贴板为空')
        return
      }
      rfb.clipboardPasteFrom(text)
      message.success('已粘贴到控制台')
    } catch (err) {
      message.error(`读取剪贴板失败: ${String(err)}`)
    }
  }

  if (consoleQuery.isLoading) {
    return (
      <Card size="small" className="soha-vm-console" title="VNC 控制台">
        <Spin description="正在获取控制台信息..." />
      </Card>
    )
  }

  if (consoleQuery.error) {
    return (
      <Card size="small" className="soha-vm-console" title="VNC 控制台">
        <ManagementState
          compact
          kind="error"
          title="获取控制台信息失败"
          description={String(consoleQuery.error)}
          actions={<Button onClick={() => void consoleQuery.refetch()}>重试获取连接信息</Button>}
        />
      </Card>
    )
  }

  const consoleData = consoleQuery.data
  if (!consoleData?.ready) {
    return (
      <Card size="small" className="soha-vm-console" title="VNC 控制台">
        <ManagementState
          compact
          kind="unsupported"
          title="控制台暂不可用"
          description={consoleData?.message || '当前 Provider 尚未提供控制台能力。'}
          actions={<Button onClick={() => void consoleQuery.refetch()}>重新检查控制台</Button>}
        />
      </Card>
    )
  }

  return (
    <Card
      size="small"
      className="soha-vm-console"
      classNames={{ header: 'soha-vm-console-header' }}
      title={
        <Space wrap>
          <Badge status={STATUS_BADGE[status]} />
          <span>VNC 控制台</span>
          <span className="text-xs text-[var(--soha-text-secondary)]">{STATUS_LABEL[status]}</span>
        </Space>
      }
      extra={
        <Space wrap>
          {status === 'disconnected' || status === 'error' ? (
            <Button
              size="small"
              icon={<ReloadOutlined />}
              loading={consoleQuery.isFetching}
              onClick={() => void consoleQuery.refetch()}
            >
              重新连接
            </Button>
          ) : null}
          <Button
            size="small"
            icon={<CopyOutlined />}
            onClick={handlePaste}
            disabled={status !== 'connected'}
          >
            粘贴
          </Button>
          <Button size="small" onClick={sendCtrlAltDel} disabled={status !== 'connected'}>
            Ctrl+Alt+Del
          </Button>
          <Button
            size="small"
            icon={isFullscreen ? <FullscreenExitOutlined /> : <FullscreenOutlined />}
            onClick={toggleFullscreen}
          >
            {isFullscreen ? '退出全屏' : '全屏'}
          </Button>
        </Space>
      }
    >
      {status === 'disconnected' && (
        <Alert
          type="warning"
          title="控制台已断开"
          description={errorMessage || '连接已关闭，请重新进入控制台后重试。'}
          className="mb-2"
        />
      )}
      {status === 'error' && (
        <Alert type="error" title="连接失败" description={errorMessage} className="mb-2" />
      )}
      <div
        ref={containerRef}
        className="relative w-full overflow-hidden rounded border border-[var(--soha-terminal-border)] bg-[var(--soha-terminal-bg)]"
        style={{ height: 'calc(100vh - 320px)', minHeight: 400 }}
      />
    </Card>
  )
}
