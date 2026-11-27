import {
  lazy,
  Suspense,
  useEffect,
  useCallback,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
} from 'react'
import {
  AppstoreOutlined,
  CloseOutlined,
  CodeOutlined,
  CompressOutlined,
  DownOutlined,
  ExpandOutlined,
  FileTextOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button, Select, Spin, Tabs, Tooltip, Typography } from 'antd'
import { ManagementState } from '@/components/management-list'
import { useInlineApiError } from '@/components/request-feedback'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useClusterCapabilityForCluster } from '../cluster-capabilities'
import { useI18n } from '@/i18n'
import { podContainerTerminated } from '../workloads/pods/container-status'
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
  connectRequests: string[]
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
  validateReference?: (session: RealtimeSession, localeCode: string) => Promise<void>
}

type MultiCardCount = 1 | 2 | 3 | 4

interface SessionWorkspaceTag {
  id: string
  name: string
  cardCount: MultiCardCount
  sessionIds: string[]
}

function buildDefaultWorkspaceTags(groupByCluster: boolean): SessionWorkspaceTag[] {
  if (groupByCluster) {
    return [
      { id: 'tag-quad', name: 'AI 推理全景联调', cardCount: 4, sessionIds: [] },
      { id: 'tag-dual', name: '日志+终端双窗', cardCount: 2, sessionIds: [] },
      { id: 'tag-triple', name: '三路核心观测', cardCount: 3, sessionIds: [] },
      { id: 'tag-single', name: '单窗专注', cardCount: 1, sessionIds: [] },
    ]
  }
  return [
    { id: 'tag-quad', name: '发布全链路四窗', cardCount: 4, sessionIds: [] },
    { id: 'tag-dual', name: '灰度+Runner 双窗', cardCount: 2, sessionIds: [] },
    { id: 'tag-triple', name: '构建·灰度·迁移三窗', cardCount: 3, sessionIds: [] },
    { id: 'tag-single', name: '单窗专注', cardCount: 1, sessionIds: [] },
  ]
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
  const isPilotMock = import.meta.env.VITE_UI_PILOT_MOCK === '1'
  const preferred = window.innerHeight * (window.innerWidth <= 768 ? 0.56 : isPilotMock ? 0.56 : 0.44)
  return clampDockHeight(
    window.innerWidth <= 768
      ? preferred
      : Math.min(
          Math.max(preferred, isPilotMock ? 440 : DOCK_DESKTOP_MIN_HEIGHT),
          isPilotMock ? 640 : DOCK_DESKTOP_MAX_HEIGHT,
        ),
  )
}

function TerminalSessionContent({
  beforeConnect,
  onReplaceSession,
  session,
}: {
  onReplaceSession: RealtimeSessionDockPanelProps['onReplaceSession']
  beforeConnect: () => Promise<boolean>
  session: RealtimeSession
}) {
  const { localeCode } = useI18n()
  const podDetailQuery = useQuery(
    podQueries.detail(
      { clusterId: session.clusterId, namespace: session.namespace },
      session.podName,
    ),
  )
  const containerOptions = (podDetailQuery.data?.containers ?? [])
    .filter((container) => !podContainerTerminated(container))
    .map(({ name }) => ({ label: name, value: name }))
  const containerLoadError =
    localeCode === 'zh_CN' ? '容器列表加载失败' : 'Failed to load containers'
  const reloadContainersLabel = localeCode === 'zh_CN' ? '重新加载容器' : 'Reload containers'
  const replaceTerminal = (container: string | undefined, shell: string) => {
    onReplaceSession(session.id, {
      ...session,
      container,
      shell,
    })
  }

  return (
    <PodTerminal
      expectedUid={session.podUid}
      beforeConnect={beforeConnect}
      clusterId={session.clusterId}
      namespace={session.namespace}
      podName={session.podName}
      container={session.container}
      shell={session.shell}
      toolbarContent={
        <>
          <div className="soha-terminal-control-group">
            <Text strong>
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
                <Text type="danger">
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
            <Text strong>
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
  validateReference,
}: {
  onReplaceSession: RealtimeSessionDockPanelProps['onReplaceSession']
  session: RealtimeSession
  validateReference: RealtimeSessionDockPanelProps['validateReference']
}) {
  const { localeCode } = useI18n()
  const permissions = usePermissionSnapshot()
  const isTerminal = session.kind === 'terminal'
  const permissionKey = session.kind === 'logs' ? 'platform.pods.logs' : 'platform.pods.exec'
  const action = session.kind === 'logs' ? 'logs' : 'exec'
  const permitted = hasPermission(permissions.data?.data, permissionKey) && !permissions.isError
  const capability = useClusterCapabilityForCluster(
    session.kind === 'logs' ? 'pod.logs' : 'pod.exec',
    localeCode,
    permitted ? session.clusterId : null,
  )
  const options = podQueries.detail(
    { clusterId: session.clusterId, namespace: session.namespace },
    session.podName,
  )
  const detail = useQuery({
    ...options,
    enabled: options.enabled && permitted && !session.reference,
  })
  useInlineApiError(permissions.error)
  useInlineApiError(detail.error)
  const [verified, setVerified] = useState(false)
  const [validationError, setValidationError] = useState('')
  const { refetch: refetchPermissions } = permissions
  const { refetch: refetchDetail } = detail
  const allowed = hasAllowedAction(detail.data?.allowedActions, action)
  const capabilityBlocked = capability.disabled || (isTerminal && capability.status === 'partial')
  const terminalCapabilityLost = isTerminal && !capability.isLoading && capabilityBlocked
  const terminatedContainerError =
    localeCode === 'zh_CN'
      ? '当前容器已结束，无法连接终端。'
      : 'This container has terminated. The terminal cannot connect.'
  const missingContainerError =
    localeCode === 'zh_CN'
      ? '原容器已不存在，请从当前 Pod 重新打开会话。'
      : 'The saved container no longer exists. Open the current Pod.'
  const containers = detail.data?.containers ?? []
  const selectedContainer = containers.find(({ name }) => name === session.container)
  const terminalContainerError =
    isTerminal && detail.isSuccess && session.container
      ? !selectedContainer
        ? missingContainerError
        : podContainerTerminated(selectedContainer)
          ? terminatedContainerError
          : ''
      : ''
  const accessChanged =
    !permitted || (detail.isSuccess && (!allowed || detail.data.uid !== session.podUid))
  if (verified && (accessChanged || terminalCapabilityLost || terminalContainerError)) {
    // Drop the live engine and require explicit revalidation when its target becomes unavailable.
    setVerified(false)
    setValidationError(
      accessChanged
        ? localeCode === 'zh_CN'
          ? '会话权限或 Pod 身份已变化，请重新校验。'
          : 'Session authorization or Pod identity changed. Revalidate to reconnect.'
        : terminalContainerError ||
            capability.reason ||
            (localeCode === 'zh_CN'
              ? '当前集群不支持交互终端。'
              : 'The current cluster does not support interactive terminals.'),
    )
  }
  const validateConnection = useCallback(async () => {
    setValidationError('')
    try {
      if (!session.podUid)
        throw new Error(
          localeCode === 'zh_CN'
            ? '旧记录缺少 Pod UID，请从当前 Pod 重新打开会话。'
            : 'This record has no Pod UID. Open a new session from the current Pod.',
        )
      const permission = await refetchPermissions()
      if (permission.isError || !hasPermission(permission.data?.data, permissionKey)) {
        throw new Error(
          localeCode === 'zh_CN'
            ? '当前没有会话访问权限。'
            : 'Session access is no longer permitted.',
        )
      }
      if (session.reference) {
        if (!validateReference)
          throw new Error(
            localeCode === 'zh_CN'
              ? '无法校验引用来源。'
              : 'The reference source cannot be validated.',
          )
        await validateReference(session, localeCode)
      }
      const pod = await refetchDetail()
      if (pod.isError || !pod.data)
        throw new Error(
          localeCode === 'zh_CN'
            ? '无法重新校验 Pod，请重试。'
            : 'Unable to revalidate the Pod. Retry to reconnect.',
        )
      if (pod.data.uid !== session.podUid)
        throw new Error(
          localeCode === 'zh_CN'
            ? 'Pod 已被替换或无法提供 UID，请从当前 Pod 重新打开会话。'
            : 'The Pod was replaced or its UID is unavailable. Open the current Pod.',
        )
      if (!hasAllowedAction(pod.data.allowedActions, action))
        throw new Error(
          localeCode === 'zh_CN'
            ? '当前资源不允许此操作。'
            : 'This action is not permitted on the current resource.',
        )
      const container = pod.data.containers?.find(({ name }) => name === session.container)
      if (session.container && !container) throw new Error(missingContainerError)
      if (isTerminal && podContainerTerminated(container)) throw new Error(terminatedContainerError)
      setVerified(true)
      return true
    } catch (error) {
      setVerified(false)
      setValidationError(error instanceof Error ? error.message : 'Session validation failed')
      return false
    }
  }, [
    action,
    isTerminal,
    localeCode,
    missingContainerError,
    permissionKey,
    refetchDetail,
    refetchPermissions,
    session,
    terminatedContainerError,
    validateReference,
  ])
  useEffect(() => {
    void validateConnection()
  }, [validateConnection])
  const defaultCandidates = isTerminal
    ? containers.filter((container) => !podContainerTerminated(container))
    : containers
  const defaultContainer =
    defaultCandidates.find(({ role }) => role === 'main')?.name ??
    defaultCandidates.find(({ role }) => !role || role === 'sidecar')?.name
  useEffect(() => {
    if (verified && permitted && !detail.isError && !session.container && defaultContainer) {
      onReplaceSession(session.id, { ...session, container: defaultContainer })
    }
  }, [defaultContainer, detail.isError, onReplaceSession, permitted, session, verified])
  if (validationError)
    return (
      <ManagementState
        compact
        kind="error"
        description={validationError}
        actions={
          <Button disabled={terminalCapabilityLost} onClick={() => void validateConnection()}>
            {localeCode === 'zh_CN' ? '重新校验并连接' : 'Validate and reconnect'}
          </Button>
        }
      />
    )
  if (!verified) return <ManagementState compact kind="loading" />
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
  if (!allowed) return <ManagementState compact kind="no-permission" />
  if (capabilityBlocked)
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
            expectedUid={session.podUid}
            beforeConnect={validateConnection}
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
          <TerminalSessionContent
            session={session}
            onReplaceSession={onReplaceSession}
            beforeConnect={validateConnection}
          />
        )}
      </Suspense>
    </div>
  )
}

function SessionContent({
  requested,
  session,
  onReplaceSession,
  validateReference,
}: {
  requested: boolean
  session: RealtimeSession
  onReplaceSession: RealtimeSessionDockPanelProps['onReplaceSession']
  validateReference: RealtimeSessionDockPanelProps['validateReference']
}) {
  const { localeCode } = useI18n()
  const [started, setStarted] = useState(false)
  if (started || requested)
    return (
      <AuthorizedSessionContent
        session={session}
        onReplaceSession={onReplaceSession}
        validateReference={validateReference}
      />
    )
  return (
    <ManagementState
      compact
      kind="empty"
      title={
        localeCode === 'zh_CN' ? '会话已断开，记录已保留' : 'Disconnected · session record retained'
      }
      description={
        localeCode === 'zh_CN'
          ? session.reference
            ? '手动重连时将重新校验引用来源、Pod UID、容器和权限。'
            : '手动重连时将重新校验 Pod UID、容器和权限。'
          : session.reference
            ? 'Reconnect to revalidate the reference, Pod UID, container and permissions.'
            : 'Reconnect to revalidate the Pod UID, container and permissions.'
      }
      actions={
        <Button
          aria-label={localeCode === 'zh_CN' ? '重新连接' : 'Reconnect'}
          type="primary"
          icon={<ReloadOutlined />}
          onClick={() => setStarted(true)}
        >
          {localeCode === 'zh_CN' ? '重新连接' : 'Reconnect'}
        </Button>
      }
    />
  )
}

export function RealtimeSessionDockPanel({
  connectRequests,
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
  validateReference,
}: RealtimeSessionDockPanelProps) {
  const { localeCode } = useI18n()
  const dockRef = useRef<HTMLElement>(null)
  const resizeRef = useRef<DockResizeState | null>(null)
  const [dockHeight, setDockHeight] = useState<number>()
  const [workspaceTags, setWorkspaceTags] = useState<SessionWorkspaceTag[]>(() =>
    buildDefaultWorkspaceTags(groupByCluster),
  )
  const [activeTagId, setActiveTagId] = useState<string>('tag-single')
  const [soloSessionId, setSoloSessionId] = useState<string | null>(null)

  const activeWorkspaceTag = useMemo(
    () => workspaceTags.find((tag) => tag.id === activeTagId) ?? workspaceTags[0],
    [workspaceTags, activeTagId],
  )
  const requestedCardCount: MultiCardCount = activeWorkspaceTag?.cardCount ?? 1

  const setActiveTagCardCount = (count: MultiCardCount) => {
    setSoloSessionId(null)
    setWorkspaceTags((prev) =>
      prev.map((tag) => (tag.id === activeWorkspaceTag.id ? { ...tag, cardCount: count } : tag)),
    )
  }

  const handleCreateWorkspaceTag = (clusterSessions: RealtimeSession[]) => {
    const nextIndex = workspaceTags.length + 1
    const count: MultiCardCount =
      clusterSessions.length >= 3 ? 3 : clusterSessions.length >= 2 ? 2 : 1
    const picked = clusterSessions.slice(0, count).map((s) => s.id)
    const newTag: SessionWorkspaceTag = {
      id: `tag-custom-${Date.now()}`,
      name: localeCode === 'zh_CN' ? `组合视图 ${nextIndex}` : `Workspace ${nextIndex}`,
      cardCount: count,
      sessionIds: picked,
    }
    setSoloSessionId(null)
    setWorkspaceTags((prev) => [...prev, newTag])
    setActiveTagId(newTag.id)
  }

  const handleToggleSessionInTag = (sessionId: string, clusterSessions: RealtimeSession[]) => {
    setSoloSessionId(null)
    setWorkspaceTags((prev) =>
      prev.map((tag) => {
        if (tag.id !== activeWorkspaceTag.id) return tag
        const currentIds =
          tag.sessionIds.length > 0
            ? tag.sessionIds.filter((id) => clusterSessions.some((s) => s.id === id))
            : clusterSessions.slice(0, tag.cardCount).map((s) => s.id)
        const exists = currentIds.includes(sessionId)
        let nextIds: string[]
        if (exists) {
          if (currentIds.length <= 1) return tag
          nextIds = currentIds.filter((id) => id !== sessionId)
        } else {
          nextIds = [...currentIds, sessionId].slice(-4)
        }
        const nextCount = Math.min(Math.max(nextIds.length, 1), 4) as MultiCardCount
        return { ...tag, sessionIds: nextIds, cardCount: nextCount }
      }),
    )
  }

  const clusterGroups = useMemo<[string, RealtimeSession[]][]>(() => {
    if (!groupByCluster) return sessions.length ? [['application', sessions]] : []
    const groups = new Map<string, RealtimeSession[]>()
    sessions.forEach((session) => {
      groups.set(session.clusterId, [...(groups.get(session.clusterId) ?? []), session])
    })
    return Array.from(groups.entries())
  }, [groupByCluster, sessions])
  const activeClusterSessions = useMemo(
    () =>
      sessions.filter((session) => !groupByCluster || session.clusterId === activeClusterId),
    [activeClusterId, groupByCluster, sessions],
  )
  const activeClusterHasSessions = activeClusterSessions.length > 0
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
            <Text ellipsis title={activeClusterId} style={{ maxWidth: '28vw' }}>
              {activeClusterId}
            </Text>
          ) : null}
          {activeClusterSessions.length >= 2 ? (
            <div
              className="soha-session-grid-switcher"
              role="group"
              aria-label={localeCode === 'zh_CN' ? '多卡片分屏数量' : 'Multi-card split count'}
            >
              <AppstoreOutlined className="soha-session-grid-switcher__icon" />
              {([1, 2, 3, 4] as MultiCardCount[]).map((count) => {
                const disabled = count > activeClusterSessions.length && count > 1
                const active = !soloSessionId && requestedCardCount === count
                return (
                  <button
                    key={count}
                    type="button"
                    disabled={disabled}
                    className={`soha-session-grid-btn${active ? ' is-active' : ''}`}
                    onClick={() => setActiveTagCardCount(count)}
                  >
                    {localeCode === 'zh_CN' ? `${count} 卡` : `${count}x`}
                  </button>
                )
              })}
            </div>
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

          const effectiveCount: MultiCardCount = soloSessionId
            ? 1
            : (Math.min(requestedCardCount, Math.max(clusterSessions.length, 1)) as MultiCardCount)

          const configuredIds = activeWorkspaceTag.sessionIds.filter((id) =>
            clusterSessions.some((s) => s.id === id),
          )
          const visibleGridIds =
            soloSessionId && clusterSessions.some((s) => s.id === soloSessionId)
              ? [soloSessionId]
              : effectiveCount === 1
                ? activeKey
                  ? [activeKey]
                  : clusterSessions.slice(0, 1).map((s) => s.id)
                : configuredIds.length > 0
                  ? configuredIds.slice(0, effectiveCount)
                  : clusterSessions.slice(0, effectiveCount).map((s) => s.id)

          const showWorkspaceTagBar = requestedCardCount > 1 && clusterSessions.length >= 2

          return (
            <div
              key={clusterId}
              className="soha-realtime-session-cluster"
              data-cluster-id={clusterId}
              data-card-count={effectiveCount}
              hidden={groupByCluster && clusterId !== activeClusterId}
            >
              {showWorkspaceTagBar ? (
                <div className="soha-session-workspace-bar">
                  <div className="soha-session-workspace-tags">
                    {workspaceTags.map((tag) => {
                      const isTagActive = tag.id === activeWorkspaceTag.id
                      return (
                        <button
                          key={tag.id}
                          type="button"
                          className={`soha-session-workspace-tag${isTagActive ? ' is-active' : ''}`}
                          onClick={() => {
                            setSoloSessionId(null)
                            setActiveTagId(tag.id)
                          }}
                        >
                          <span className="soha-session-workspace-tag__name">{tag.name}</span>
                          <span className="soha-session-workspace-tag__badge">
                            {tag.cardCount}窗
                          </span>
                        </button>
                      )
                    })}
                    <Tooltip
                      title={
                        localeCode === 'zh_CN'
                          ? '新建单 Tag 多窗口组合视图'
                          : 'Create multi-window workspace tag'
                      }
                    >
                      <button
                        type="button"
                        className="soha-session-workspace-tag is-add"
                        onClick={() => handleCreateWorkspaceTag(clusterSessions)}
                      >
                        <PlusOutlined />
                        <span>{localeCode === 'zh_CN' ? '新建组' : 'New group'}</span>
                      </button>
                    </Tooltip>
                  </div>
                  <div className="soha-session-window-selector">
                    <span className="soha-session-window-selector__label">
                      {localeCode === 'zh_CN' ? '当前 Tag 挂载:' : 'Mounted:'}
                    </span>
                    {clusterSessions.map((session) => {
                      const mounted = visibleGridIds.includes(session.id)
                      const shortPod =
                        session.podName.length > 18
                          ? `${session.podName.slice(0, 18)}…`
                          : session.podName
                      return (
                        <button
                          key={session.id}
                          type="button"
                          className={`soha-session-mount-chip${mounted ? ' is-mounted' : ''}`}
                          onClick={() => handleToggleSessionInTag(session.id, clusterSessions)}
                        >
                          {session.kind === 'logs' ? <FileTextOutlined /> : <CodeOutlined />}
                          <span>{shortPod}</span>
                        </button>
                      )
                    })}
                  </div>
                </div>
              ) : null}
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
                  const isVisibleInGrid = visibleGridIds.includes(session.id)
                  const isSolo = soloSessionId === session.id
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
                      <div
                        className={`soha-session-window-card${isVisibleInGrid ? ' is-grid-visible' : ''}${effectiveCount > 1 || isSolo ? ' is-card-framed' : ''}`}
                      >
                        {effectiveCount > 1 || isSolo ? (
                          <div className="soha-session-window-card__head">
                            <div className="soha-session-window-card__meta">
                              <span
                                className={`soha-session-kind-pill is-${session.kind}`}
                              >
                                {session.kind === 'logs' ? <FileTextOutlined /> : <CodeOutlined />}
                                {kindLabel}
                              </span>
                              <strong
                                className="soha-session-window-card__pod"
                                title={`${session.namespace}/${session.podName}`}
                              >
                                {session.podName}
                              </strong>
                              {session.container ? (
                                <span className="soha-session-window-card__container">
                                  / {session.container}
                                </span>
                              ) : null}
                              <span className="soha-session-window-card__ns">
                                {session.namespace}
                              </span>
                            </div>
                            <div className="soha-session-window-card__tools">
                              <span className="soha-session-live-dot" title="LIVE" />
                              <Tooltip
                                title={
                                  isSolo
                                    ? localeCode === 'zh_CN'
                                      ? '返回多卡片分屏'
                                      : 'Back to multi-card grid'
                                    : localeCode === 'zh_CN'
                                      ? '单卡片放大查看'
                                      : 'Focus single card'
                                }
                              >
                                <button
                                  type="button"
                                  className="soha-session-card-tool-btn"
                                  onClick={() => {
                                    onActiveSessionChange(session.clusterId, session.id)
                                    setSoloSessionId((cur) =>
                                      cur === session.id ? null : session.id,
                                    )
                                  }}
                                >
                                  {isSolo ? <CompressOutlined /> : <ExpandOutlined />}
                                </button>
                              </Tooltip>
                              <Tooltip title={localeCode === 'zh_CN' ? '关闭此窗口' : 'Close'}>
                                <button
                                  type="button"
                                  className="soha-session-card-tool-btn is-close"
                                  onClick={() => onCloseSession(session.id)}
                                >
                                  <CloseOutlined />
                                </button>
                              </Tooltip>
                            </div>
                          </div>
                        ) : null}
                        <div className="soha-session-window-card__body">
                          <SessionContent
                            requested={connectRequests.includes(session.id)}
                            session={session}
                            onReplaceSession={onReplaceSession}
                            validateReference={validateReference}
                          />
                        </div>
                      </div>
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
