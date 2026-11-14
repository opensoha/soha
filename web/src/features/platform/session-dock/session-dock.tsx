import {
  createContext,
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { flushSync } from 'react-dom'
import { useStore } from 'zustand'
import { createSessionDockStore } from './store'
import { Badge } from 'antd'
import { CodeOutlined } from '@ant-design/icons'
import { HeaderActionButton } from '@/components/header-action-button'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import {
  normalizeRealtimeSession,
  type RealtimeSession,
  type RealtimeSessionInput,
  type RealtimeSessionWorkbench,
} from './types'

const RealtimeSessionDockPanel = lazy(async () => {
  const module = await import('./session-dock-panel')
  return { default: module.RealtimeSessionDockPanel }
})

interface RealtimeSessionDockContextValue {
  workbench: RealtimeSessionWorkbench
  activeClusterId: string | null
  activeSessionKeys: Record<string, string>
  closeSession: (sessionId: string) => void
  dockOpen: boolean
  maximizeDock: () => void
  maximized: boolean
  minimizeDock: () => void
  openSession: (input: RealtimeSessionInput) => void
  sessions: RealtimeSession[]
  setActiveSession: (clusterId: string, sessionId: string) => void
  toggleDock: () => void
}

const RealtimeSessionDockContext = createContext<RealtimeSessionDockContextValue | null>(null)

export function useRealtimeSessionDock() {
  const context = useContext(RealtimeSessionDockContext)
  if (!context) {
    throw new Error('useRealtimeSessionDock must be used inside RealtimeSessionDockProvider')
  }
  return context
}

export function RealtimeSessionDockProvider({
  children,
  visible,
  ownerKey,
  workbench,
  scopeKey,
}: {
  children: ReactNode
  visible: boolean
  ownerKey?: string
  workbench: RealtimeSessionWorkbench
  scopeKey?: string
}) {
  return (
    <SessionDockOwner
      key={ownerKey || 'anonymous'}
      ownerKey={ownerKey}
      visible={visible}
      workbench={workbench}
      scopeKey={scopeKey}
    >
      {children}
    </SessionDockOwner>
  )
}

function SessionDockOwner({
  children,
  visible,
  ownerKey,
  workbench,
  scopeKey,
}: {
  children: ReactNode
  visible: boolean
  ownerKey?: string
  workbench: RealtimeSessionWorkbench
  scopeKey?: string
}) {
  const platformClusterId = usePlatformScopeStore((state) => state.clusterId)
  const store = useMemo(
    () => createSessionDockStore(workbench, ownerKey, scopeKey),
    [workbench, ownerKey, scopeKey],
  )
  const { sessions, activeSessionKeys, selectedClusterId } = useStore(store)
  const activeClusterId = workbench === 'platform' ? platformClusterId : selectedClusterId
  const setSessions = useCallback(
    (value: RealtimeSession[] | ((current: RealtimeSession[]) => RealtimeSession[])) => {
      store.setState((state) => ({
        sessions: typeof value === 'function' ? value(state.sessions) : value,
      }))
    },
    [store],
  )
  const setActiveSessionKeys = useCallback(
    (value: (current: Record<string, string>) => Record<string, string>) => {
      store.setState((state) => ({ activeSessionKeys: value(state.activeSessionKeys) }))
    },
    [store],
  )
  const [openedScope, setOpenedScope] = useState<string | undefined>()
  const [isOpen, setIsOpen] = useState(false)
  const dockOpen = isOpen && openedScope === scopeKey
  const [maximized, setMaximized] = useState(false)
  const lifecycleKey = JSON.stringify([workbench, scopeKey, visible])
  const [lifecycle, setLifecycle] = useState(lifecycleKey)
  const [connectRequests, setConnectRequests] = useState<string[]>([])
  const [pageSuspended, setPageSuspended] = useState(false)
  // Reset before committing children: returning to an earlier scope never restores connection intent.
  if (lifecycle !== lifecycleKey) {
    setLifecycle(lifecycleKey)
    setConnectRequests([])
    setIsOpen(false)
    setMaximized(false)
  }
  useEffect(() => {
    const suspend = () =>
      flushSync(() => {
        setPageSuspended(true)
        setConnectRequests([])
      })
    const resume = () => setPageSuspended(false)
    window.addEventListener('pagehide', suspend)
    window.addEventListener('pageshow', resume)
    return () => {
      window.removeEventListener('pagehide', suspend)
      window.removeEventListener('pageshow', resume)
    }
  }, [])

  const openSession = useCallback(
    (input: RealtimeSessionInput) => {
      const session = normalizeRealtimeSession(input)
      if (workbench === 'delivery' && !scopeKey) return
      if (!session || (workbench === 'platform' && session.clusterId !== platformClusterId)) return

      setSessions((current) => {
        const existing = current.findIndex((item) => item.id === session.id)
        if (existing < 0) return [...current, session]
        return current.map((item, index) => (index === existing ? session : item))
      })
      setActiveSessionKeys((current) => ({ ...current, [session.clusterId]: session.id }))
      if (workbench === 'delivery') store.setState({ selectedClusterId: session.clusterId })
      setConnectRequests((current) =>
        current.includes(session.id) ? current : [...current, session.id],
      )
      setOpenedScope(scopeKey)
      setIsOpen(true)
    },
    [setSessions, setActiveSessionKeys, store, workbench, platformClusterId, scopeKey],
  )

  const replaceSession = useCallback(
    (sessionId: string, input: RealtimeSessionInput) => {
      const replacement = normalizeRealtimeSession(input)
      if (!replacement) return

      setConnectRequests((current) => current.map((id) => (id === sessionId ? replacement.id : id)))
      setSessions((current) => {
        const index = current.findIndex((session) => session.id === sessionId)
        if (index < 0) return current
        const next = current.filter(
          (session) => session.id !== sessionId && session.id !== replacement.id,
        )
        next.splice(Math.min(index, next.length), 0, replacement)
        return next
      })
      setActiveSessionKeys((current) => ({
        ...current,
        [replacement.clusterId]: replacement.id,
      }))
    },
    [setSessions, setActiveSessionKeys],
  )

  const closeSession = useCallback(
    (sessionId: string) => {
      const closing = sessions.find((session) => session.id === sessionId)
      if (!closing) return
      setConnectRequests((current) => current.filter((id) => id !== sessionId))
      const remaining = sessions.filter((session) => session.id !== sessionId)
      setSessions(remaining)
      setActiveSessionKeys((current) => {
        if (current[closing.clusterId] !== sessionId) return current
        const clusterSessions = remaining.filter(
          (session) => session.clusterId === closing.clusterId,
        )
        const replacement = clusterSessions[clusterSessions.length - 1]
        const next = { ...current }
        if (replacement) next[closing.clusterId] = replacement.id
        else delete next[closing.clusterId]
        return next
      })
      if (
        workbench === 'delivery' &&
        activeClusterId === closing.clusterId &&
        !remaining.some((session) => session.clusterId === closing.clusterId)
      ) {
        store.setState({ selectedClusterId: remaining[remaining.length - 1]?.clusterId ?? null })
      }
      if (remaining.length === 0) {
        setIsOpen(false)
        setMaximized(false)
      }
    },
    [sessions, setSessions, setActiveSessionKeys, activeClusterId, store, workbench],
  )

  const setActiveSession = useCallback(
    (clusterId: string, sessionId: string) => {
      if (workbench === 'platform' && clusterId !== platformClusterId) return
      if (!sessions.some((session) => session.clusterId === clusterId && session.id === sessionId))
        return
      if (workbench === 'delivery') store.setState({ selectedClusterId: clusterId })
      setActiveSessionKeys((current) => ({ ...current, [clusterId]: sessionId }))
    },
    [setActiveSessionKeys, store, workbench, platformClusterId, sessions],
  )

  const toggleDock = useCallback(() => {
    if (workbench === 'delivery' && !scopeKey) return
    if (dockOpen) setMaximized(false)
    setOpenedScope(scopeKey)
    setIsOpen(!dockOpen)
  }, [dockOpen, scopeKey, workbench])

  const minimizeDock = useCallback(() => {
    setIsOpen(false)
    setMaximized(false)
  }, [])

  const maximizeDock = useCallback(() => {
    setOpenedScope(scopeKey)
    setIsOpen(true)
    setMaximized((current) => !current)
  }, [scopeKey])

  const contextValue = useMemo<RealtimeSessionDockContextValue>(
    () => ({
      workbench,
      activeClusterId,
      activeSessionKeys,
      closeSession,
      dockOpen,
      maximizeDock,
      maximized,
      minimizeDock,
      openSession,
      sessions,
      setActiveSession,
      toggleDock,
    }),
    [
      workbench,
      activeClusterId,
      activeSessionKeys,
      closeSession,
      dockOpen,
      maximizeDock,
      maximized,
      minimizeDock,
      openSession,
      sessions,
      setActiveSession,
      toggleDock,
    ],
  )

  return (
    <RealtimeSessionDockContext.Provider value={contextValue}>
      {children}
      {visible && !pageSuspended && (sessions.length > 0 || dockOpen) ? (
        <Suspense fallback={null}>
          <RealtimeSessionDockPanel
            key={JSON.stringify([workbench, scopeKey])}
            connectRequests={connectRequests}
            groupByCluster={workbench === 'platform'}
            activeClusterId={activeClusterId}
            activeSessionKeys={activeSessionKeys}
            maximized={maximized}
            sessions={sessions}
            visible={visible && dockOpen}
            onActiveSessionChange={setActiveSession}
            onCloseSession={closeSession}
            onMaximize={maximizeDock}
            onMinimize={minimizeDock}
            onReplaceSession={replaceSession}
          />
        </Suspense>
      ) : null}
    </RealtimeSessionDockContext.Provider>
  )
}

export function RealtimeSessionDockTrigger() {
  const { localeCode } = useI18n()
  const { activeClusterId, dockOpen, sessions, toggleDock, workbench } = useRealtimeSessionDock()
  const sessionCount =
    workbench === 'platform'
      ? sessions.filter((session) => session.clusterId === activeClusterId).length
      : sessions.length
  const label = localeCode === 'zh_CN' ? '实时会话' : 'Live sessions'
  const ariaLabel = sessionCount > 0 ? `${label} (${sessionCount})` : label

  return (
    <HeaderActionButton
      ariaLabel={ariaLabel}
      className="soha-header-realtime-sessions"
      icon={
        <Badge count={sessionCount} overflowCount={99} size="small">
          <CodeOutlined />
        </Badge>
      }
      label={label}
      title={ariaLabel}
      pressed={dockOpen}
      onClick={toggleDock}
    />
  )
}
