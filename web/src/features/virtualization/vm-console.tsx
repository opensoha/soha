import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Alert, App, Badge, Button, Card, Space, Spin } from 'antd'
import { CopyOutlined, FullscreenOutlined, FullscreenExitOutlined } from '@ant-design/icons'
import RFB from '@novnc/novnc'
import { virtualizationApi } from './virtualization-api'
import { ManagementState } from '@/components/management-list'
import { buildSameOriginStreamURL, withStreamTicket } from '@/features/auth/stream-ticket'

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
  const [status, setStatus] = useState<'connecting' | 'connected' | 'disconnected' | 'error'>('connecting')
  const [errorMessage, setErrorMessage] = useState<string>('')
  const [isFullscreen, setIsFullscreen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const { message } = App.useApp()

  const consoleQuery = useQuery({
    queryKey: ['virtualization', 'vm-console', vmId],
    queryFn: () => virtualizationApi.vmConsoleURL(vmId),
  })

  useEffect(() => {
    const consoleData = consoleQuery.data?.data
    if (!consoleData || !containerRef.current || consoleData.message) {
      return
    }

    let disposed = false
    let rfbInstance: RFB | null = null
    void (async () => {
      try {
        const baseURL = buildSameOriginStreamURL(consoleData.url, 'ws')
        const wsUrl = await withStreamTicket(baseURL)
        if (disposed || !containerRef.current) return
        rfbInstance = new RFB(containerRef.current, wsUrl, {
          credentials: { password: consoleData.token || '' },
        })

        rfbInstance.scaleViewport = true
        rfbInstance.resizeSession = true

        rfbInstance.addEventListener('connect', () => {
          setStatus('connected')
          setErrorMessage('')
        })

        rfbInstance.addEventListener('disconnect', (e: any) => {
          setStatus('disconnected')
          if (e.detail.clean === false) {
            setErrorMessage(e.detail.reason || 'Connection closed unexpectedly')
          }
        })

        rfbInstance.addEventListener('securityfailure', (e: any) => {
          setStatus('error')
          setErrorMessage(e.detail.reason || 'Security failure')
        })

        setRfb(rfbInstance)
      } catch (err) {
        setStatus('error')
        setErrorMessage(String(err))
      }
    })()

    return () => {
      disposed = true
      rfbInstance?.disconnect()
    }
  }, [consoleQuery.data, vmId])

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
    return <Spin tip="正在获取控制台信息..." />
  }

  if (consoleQuery.error) {
    return <ManagementState compact kind="error" title="获取控制台信息失败" description={String(consoleQuery.error)} />
  }

  const consoleData = consoleQuery.data?.data
  if (!consoleData?.ready) {
    return (
      <ManagementState
        compact
        kind="unsupported"
        title="控制台暂不可用"
        description={consoleData?.message || '当前 Provider 尚未提供控制台能力。'}
      />
    )
  }

  return (
    <Card
      size="small"
      title={
        <Space>
          <Badge status={STATUS_BADGE[status]} />
          <span>VNC 控制台</span>
          <span className="text-xs text-[var(--soha-text-secondary)]">{STATUS_LABEL[status]}</span>
        </Space>
      }
      extra={
        <Space>
          <Button size="small" icon={<CopyOutlined />} onClick={handlePaste} disabled={status !== 'connected'}>
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
      {status === 'disconnected' && <Alert type="warning" message="控制台已断开" className="mb-2" />}
      {status === 'error' && <Alert type="error" message="连接失败" description={errorMessage} className="mb-2" />}
      <div
        ref={containerRef}
        className="relative w-full overflow-hidden rounded border border-[var(--soha-border)] bg-black"
        style={{ height: 'calc(100vh - 320px)', minHeight: 400 }}
      />
    </Card>
  )
}
