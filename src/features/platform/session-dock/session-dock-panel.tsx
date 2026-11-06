import {
  lazy,
  Suspense,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
} from 'react'
import {
  CodeOutlined,
  CompressOutlined,
  DownOutlined,
  ExpandOutlined,
  FileTextOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button, Select, Spin, Tabs, Typography } from 'antd'
import { ManagementState } from '@/components/management-list'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useClusterCapabilityForCluster } from '../cluster-capabilities'
import { useI18n } from '@/i18n'
import { podQueries } from '../workloads/pods/queries'
import type { RealtimeSession, RealtimeSessionInput } from './types'
import './session-dock.css'

const { Text } = Typography

const PodLogViewer = lazy(async () => {
  const module = await import('@/components/pod-log-viewer')
  return { default: module.PodLogViewer }
})

const PodTerminal = lazy(async () => {
  const module = await import('@/components/pod-terminal')
  return { default: module.PodTerminal }
})

interface RealtimeSessionDockPanelProps {
  groupByCluster: boolean
  activeClusterId: string | null
  activeSessionKeys: Record<string, string>
  maximized: boolean
  onActiveSessionChange: (clusterId: string, sessionId: string) => void
  onCloseSession: (sessionId: string) => void
  onMaximize: () => void
  onMinimize: () => void
  onReplaceSession: (sessionId: string, input: RealtimeSessionInput) => void
  sessions: RealtimeSession[]
  visible: boolean
}

interface DockResizeState {
  pointerId: number
  startHeight: number
  startY: number
}

const DOCK_MIN_HEIGHT = 240
const DOCK_TOP_GAP = 64
const DOCK_KEYBOARD_STEP = 32
const DOCK_DESKTOP_MIN_HEIGHT = 300
const DOCK_DESKTOP_MAX_HEIGHT = 520

function dockHeightBounds() {
  const max = Math.max(120, window.innerHeight - DOCK_TOP_GAP)
  return { min: Math.min(DOCK_MIN_HEIGHT, max), max }
}

function clampDockHeight(height: number) {
  const { min, max } = dockHeightBounds()
  return Math.min(Math.max(height, min), max)
}

function defaultDockHeight() {
  const preferred = window.innerHeight * (window.innerWidth <= 768 ? 0.56 : 0.44)
  return clampDockHeight(
    window.innerWidth <= 768
      ? preferred
      : Math.min(Math.max(preferred, DOCK_DESKTOP_MIN_HEIGHT), DOCK_DESKTOP_MAX_HEIGHT),
  )
}

function TerminalSessionContent({
  onReplaceSession,
  session,
}: {
  onReplaceSession: RealtimeSessionDockPanelProps['onReplaceSession']
  session: RealtimeSession
}) {
  const { localeCode } = useI18n()
  const podDetailQuery = useQuery(
    podQueries.detail(
      { clusterId: session.clusterId, namespace: session.namespace },
      session.podName,
    ),
  )
  const containerOptions = (podDetailQuery.data?.containers ?? []).map(({ name }) => ({
    label: name,
    value: name,
  }))
  const containerLoadError =
    localeCode === 'zh_CN' ? '容器列表加载失败' : 'Failed to load containers'
  const reloadContainersLabel = localeCode === 'zh_CN' ? '重新加载容器' : 'Reload containers'
  if (session.container && !containerOptions.some(({ value }) => value === session.container)) {
    containerOptions.unshift({ label: session.container, value: session.container })
  }

  const replaceTerminal = (container: string | undefined, shell: string) => {
    onReplaceSession(session.id, {
      clusterId: session.clusterId,
      container,
      kind: 'terminal',
      namespace: session.namespace,
      podName: session.podName,
      shell,
    })
  }

  return (
    <PodTerminal
      clusterId={session.clusterId}
      namespace={session.namespace}
      podName={session.podName}
      container={session.container}
      shell={session.shell}
      toolbarContent={
        <>
          <div className="soha-terminal-control-group">
            <Text strong className="text-xs">
              {localeCode === 'zh_CN' ? '容器:' : 'Container:'}
            </Text>
            <Select
              allowClear
              aria-label={localeCode === 'zh_CN' ? '选择容器' : 'Select container'}
              loading={podDetailQuery.isLoading}
              options={containerOptions}
              placeholder={localeCode === 'zh_CN' ? '默认容器' : 'Default container'}
              size="small"
              status={podDetailQuery.isError ? 'error' : undefined}
              style={{ width: 220 }}
              value={session.container}
              onChange={(value) =>
                replaceTerminal(value ? String(value) : undefined, session.shell ?? '/bin/sh')
              }
            />
            {podDetailQuery.isError ? (
              <>
                <Text type="danger" className="text-xs">
                  {containerLoadError}
                </Text>
                <Button
                  aria-label={reloadContainersLabel}
                  danger
                  icon={<ReloadOutlined />}
                  loading={podDetailQuery.isFetching}
                  size="small"
                  type="text"
                  onClick={() => void podDetailQuery.refetch()}
                />
              </>
            ) : null}
          </div>
          <div className="soha-terminal-control-group">
            <Text strong className="text-xs">
              Shell:
            </Text>
            <Select
              aria-label={localeCode === 'zh_CN' ? '选择 Shell' : 'Select shell'}
              options={[
                { value: '/bin/sh', label: '/bin/sh' },
                { value: '/bin/bash', label: '/bin/bash' },
                { value: '/bin/ash', label: '/bin/ash' },
              ]}
              size="small"
              style={{ width: 180 }}
              value={session.shell ?? '/bin/sh'}
              onChange={(value) => replaceTerminal(session.container, String(value))}
            />
          </div>
        </>
      }
    />
  )
}

function AuthorizedSessionContent({
  onReplaceSession,
  session,
}: {
  onReplaceSession: RealtimeSessionDockPanelProps['onReplaceSession']
  session: RealtimeSession
}) {
  const { localeCode } = useI18n()
  const permissions = usePermissionSnapshot()
  const permitted =
    hasPermission(
      permissions.data?.data,
      session.kind === 'logs' ? 'platform.pods.logs' : 'platform.pods.exec',
    ) && !permissions.isError
  const capability = useClusterCapabilityForCluster(
    session.kind === 'logs' ? 'pod.logs' : 'pod.exec',
    localeCode,
    permitted ? session.clusterId : null,
  )
  const options = podQueries.detail(
    { clusterId: session.clusterId, namespace: session.namespace },
    session.podName,
  )
  const detail = useQuery({ ...options, enabled: options.enabled && permitted })
  const containers = detail.data?.containers ?? []
  const defaultContainer =
    containers.find(({ role }) => role === 'main')?.name ??
    containers.find(({ role }) => !role || role === 'sidecar')?.name
  useEffect(() => {
    if (permitted && !detail.isError && !session.container && defaultContainer) {
      onReplaceSession(session.id, { ...session, container: defaultContainer })
    }
  }, [defaultContainer, detail.isError, onReplaceSession, permitted, session])
  if (!permitted)
    return <ManagementState compact kind={permissions.isLoading ? 'loading' : 'no-permission'} />
  if (detail.isError)
    return (
      <ManagementState
        compact
        kind="error"
        actions={
          <Button onClick={() => void detail.refetch()}>
            {localeCode === 'zh_CN' ? '重试' : 'Retry'}
          </Button>
        }
      />
    )
  if (detail.isPending || capability.isLoading) return <ManagementState compact kind="loading" />
  if (!hasAllowedAction(detail.data?.allowedActions, session.kind === 'logs' ? 'logs' : 'exec'))
    return <ManagementState compact kind="no-permission" />
  if (capability.disabled || (session.kind === 'terminal' && capability.status === 'partial'))
    return <ManagementState compact kind="unsupported" description={capability.reason} />
  if (!session.container)
    return (
      <ManagementState
        compact
        kind={defaultContainer ? 'loading' : 'empty'}
        title={
          defaultContainer
            ? undefined
            : localeCode === 'zh_CN'
              ? '暂无可用的主容器'
              : 'No main container available'
        }
      />
    )
  return (
    <div className="soha-realtime-session-pane">
      <Suspense
        fallback={
          <div className="soha-realtime-session-loading">
            <Spin size="large" />
          </div>
        }
      >
        {session.kind === 'logs' ? (
          <PodLogViewer
            active
            clusterId={session.clusterId}
            namespace={session.namespace}
            podName={session.podName}
            container={session.container}
            containerOptions={containers.map(({ name }) => ({ value: name, label: name }))}
            onContainerChange={(container) =>
              onReplaceSession(session.id, { ...session, container: container || undefined })
            }
            streamingDisabledReason={
              capability.status === 'partial' ? capability.reason : undefined
            }
          />
        ) : (
          <TerminalSessionContent session={session} onReplaceSession={onReplaceSession} />
        )}
      </Suspense>
    </div>
  )
}

function SessionContent({
  active,
  session,
  onReplaceSession,
}: {
  active: boolean
  session: RealtimeSession
  onReplaceSession: RealtimeSessionDockPanelProps['onReplaceSession']
}) {
  const [started, setStarted] = useState(false)
  useEffect(() => {
    if (active) setStarted(true)
  }, [active])
  return started ? (
    <AuthorizedSessionContent session={session} onReplaceSession={onReplaceSession} />
  ) : null
}

export function RealtimeSessionDockPanel({
  groupByCluster,
  activeClusterId,
  activeSessionKeys,
  maximized,
  onActiveSessionChange,
  onCloseSession,
  onMaximize,
  onMinimize,
  onReplaceSession,
  sessions,
  visible,
}: RealtimeSessionDockPanelProps) {
  const { localeCode } = useI18n()
  const dockRef = useRef<HTMLElement>(null)
  const resizeRef = useRef<DockResizeState | null>(null)
  const [dockHeight, setDockHeight] = useState<number>()
  const clusterGroups = useMemo<[string, RealtimeSession[]][]>(() => {
    if (!groupByCluster) return sessions.length ? [['application', sessions]] : []
    const groups = new Map<string, RealtimeSession[]>()
    sessions.forEach((session) => {
      groups.set(session.clusterId, [...(groups.get(session.clusterId) ?? []), session])
    })
    return Array.from(groups.entries())
  }, [groupByCluster, sessions])
  const activeClusterHasSessions = Boolean(
    sessions.some((session) => !groupByCluster || session.clusterId === activeClusterId),
  )
  const title = localeCode === 'zh_CN' ? '实时会话' : 'Live sessions'
  const resizeLabel = localeCode === 'zh_CN' ? '调整实时会话高度' : 'Resize live sessions'

  const currentDockHeight = () => {
    const measuredHeight = dockRef.current?.getBoundingClientRect().height
    return dockHeight ?? (measuredHeight ? measuredHeight : defaultDockHeight())
  }

  const handleResizePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (maximized || event.button !== 0) return
    event.preventDefault()
    event.currentTarget.setPointerCapture(event.pointerId)
    resizeRef.current = {
      pointerId: event.pointerId,
      startHeight: currentDockHeight(),
      startY: event.clientY,
    }
  }

  const handleResizePointerEnd = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (resizeRef.current?.pointerId === event.pointerId) resizeRef.current = null
  }

  const handleResizeKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    const { min, max } = dockHeightBounds()
    const nextHeight =
      event.key === 'ArrowUp'
        ? currentDockHeight() + DOCK_KEYBOARD_STEP
        : event.key === 'ArrowDown'
          ? currentDockHeight() - DOCK_KEYBOARD_STEP
          : event.key === 'Home'
            ? min
            : event.key === 'End'
              ? max
              : null
    if (nextHeight === null) return
    event.preventDefault()
    setDockHeight(clampDockHeight(nextHeight))
  }

  useEffect(() => {
    if (!visible) return undefined
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      if (maximized) onMaximize()
      else onMinimize()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [maximized, onMaximize, onMinimize, visible])

  useEffect(() => {
    if (!visible || maximized) return undefined
    const handlePointerMove = (event: PointerEvent) => {
      const resize = resizeRef.current
      if (!resize || resize.pointerId !== event.pointerId) return
      event.preventDefault()
      setDockHeight(clampDockHeight(resize.startHeight + resize.startY - event.clientY))
    }
    const handlePointerEnd = (event: PointerEvent) => {
      if (resizeRef.current?.pointerId === event.pointerId) resizeRef.current = null
    }
    window.addEventListener('pointermove', handlePointerMove, { passive: false })
    window.addEventListener('pointerup', handlePointerEnd)
    window.addEventListener('pointercancel', handlePointerEnd)
    return () => {
      resizeRef.current = null
      window.removeEventListener('pointermove', handlePointerMove)
      window.removeEventListener('pointerup', handlePointerEnd)
      window.removeEventListener('pointercancel', handlePointerEnd)
    }
  }, [maximized, visible])

  useEffect(() => {
    window.dispatchEvent(new Event('soha:session-dock-layout'))
  }, [dockHeight, maximized, visible])

  return (
    <section
      ref={dockRef}
      aria-label={title}
      className={`soha-realtime-session-dock${maximized ? ' is-maximized' : ''}`}
      hidden={!visible}
      style={
        dockHeight === undefined
          ? undefined
          : ({ '--soha-realtime-session-height': `${dockHeight}px` } as CSSProperties)
      }
    >
      {!maximized ? (
        <div
          aria-label={resizeLabel}
          aria-orientation="horizontal"
          aria-valuemax={dockHeightBounds().max}
          aria-valuemin={dockHeightBounds().min}
          aria-valuenow={Math.round(currentDockHeight())}
          className="soha-realtime-session-dock__resize-handle"
          role="separator"
          tabIndex={0}
          onKeyDown={handleResizeKeyDown}
          onLostPointerCapture={handleResizePointerEnd}
          onPointerDown={handleResizePointerDown}
        />
      ) : null}
      <div className="soha-realtime-session-dock__header">
        <div className="soha-realtime-session-dock__identity">
          <CodeOutlined />
          <strong>{title}</strong>
          {groupByCluster && activeClusterId ? (
            <Text ellipsis title={activeClusterId} style={{ maxWidth: '36vw' }}>
              {activeClusterId}
            </Text>
          ) : null}
        </div>
        <div className="soha-realtime-session-dock__actions">
          <Button
            aria-label={
              maximized
                ? localeCode === 'zh_CN'
                  ? '还原实时会话'
                  : 'Restore live sessions'
                : localeCode === 'zh_CN'
                  ? '最大化实时会话'
                  : 'Maximize live sessions'
            }
            icon={maximized ? <CompressOutlined /> : <ExpandOutlined />}
            size="small"
            type="text"
            onClick={onMaximize}
          />
          <Button
            aria-label={localeCode === 'zh_CN' ? '收起实时会话' : 'Minimize live sessions'}
            icon={<DownOutlined />}
            size="small"
            type="text"
            onClick={onMinimize}
          />
        </div>
      </div>
      <div className="soha-realtime-session-dock__body">
        {clusterGroups.map(([clusterId, clusterSessions]) => {
          const requestedActiveKey =
            activeSessionKeys[groupByCluster ? clusterId : activeClusterId || '']
          const activeKey = clusterSessions.some((session) => session.id === requestedActiveKey)
            ? requestedActiveKey
            : clusterSessions[clusterSessions.length - 1]?.id
          return (
            <div
              key={clusterId}
              className="soha-realtime-session-cluster"
              data-cluster-id={clusterId}
              hidden={groupByCluster && clusterId !== activeClusterId}
            >
              <Tabs
                activeKey={activeKey}
                animated={false}
                classNames={{
                  body: 'soha-realtime-session-tabs-body',
                  content: 'soha-realtime-session-tab-content',
                }}
                destroyOnHidden={false}
                hideAdd
                items={clusterSessions.map((session) => {
                  const kindLabel =
                    session.kind === 'logs'
                      ? localeCode === 'zh_CN'
                        ? '日志'
                        : 'Logs'
                      : localeCode === 'zh_CN'
                        ? '终端'
                        : 'Terminal'
                  const fullLabel = [
                    kindLabel,
                    session.clusterId,
                    session.namespace,
                    session.podName,
                    session.container,
                  ]
                    .filter(Boolean)
                    .join(' / ')
                  return {
                    key: session.id,
                    label: (
                      <span className="soha-realtime-session-tab-label" title={fullLabel}>
                        {session.kind === 'logs' ? <FileTextOutlined /> : <CodeOutlined />}
                        <span>{kindLabel}</span>
                        <span className="soha-realtime-session-tab-label__pod">
                          {session.podName}
                        </span>
                        {session.container ? (
                          <span className="soha-realtime-session-tab-label__container">
                            / {session.container}
                          </span>
                        ) : null}
                      </span>
                    ),
                    forceRender: true,
                    children: (
                      <SessionContent
                        active={
                          visible &&
                          (!groupByCluster || clusterId === activeClusterId) &&
                          session.id === activeKey
                        }
                        session={session}
                        onReplaceSession={onReplaceSession}
                      />
                    ),
                  }
                })}
                size="small"
                type="editable-card"
                onChange={(sessionId) => {
                  const session = clusterSessions.find((item) => item.id === sessionId)
                  if (session) onActiveSessionChange(session.clusterId, sessionId)
                }}
                onEdit={(targetKey, action) => {
                  if (action === 'remove' && typeof targetKey === 'string') {
                    onCloseSession(targetKey)
                  }
                }}
              />
            </div>
          )
        })}
        {!activeClusterHasSessions ? (
          <div className="soha-realtime-session-empty" role="status">
            <CodeOutlined />
            <span>
              {!groupByCluster
                ? localeCode === 'zh_CN'
                  ? '暂无实时会话'
                  : 'No live sessions'
                : activeClusterId
                  ? localeCode === 'zh_CN'
                    ? '当前集群暂无实时会话'
                    : 'No live sessions in the current cluster'
                  : localeCode === 'zh_CN'
                    ? '尚未选择集群'
                    : 'No cluster selected'}
            </span>
          </div>
        ) : null}
      </div>
    </section>
  )
}
