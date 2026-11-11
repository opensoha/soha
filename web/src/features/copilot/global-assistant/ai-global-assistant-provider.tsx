import { hasPermission } from '@/features/auth'
import { systemKeys } from '@/features/system'
import { api } from '@/services/api-client'
import { usePreferencesStore } from '@/stores/preferences-store'
import type { ApiResponse, PermissionSnapshot } from '@/types'
import type {
  WorkbenchGlobalAssistantOpenRequest,
  WorkbenchSendMessageStreamRequest,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQueryClient } from '@tanstack/react-query'
import { Alert, App as AntdApp, Button, Select, Space } from 'antd'
import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import {
  isRunningExternalAgentRun,
  sortedAgentRunReplayEvents,
} from '../workbench/agent-run-replay'
import { preferredExternalAgent } from '../workbench/agent-selection'
import { workbenchApi } from '../workbench/api'
import { workbenchKeys } from '../workbench/keys'
import { getAIWorkbenchPathForMode } from '../workbench/navigation'
import { workbenchQueries } from '../workbench/queries'
import {
  createWorkbenchStreamState,
  reduceWorkbenchStreamState,
  streamWorkbenchMessage,
  workbenchStreamEventKey,
  type WorkbenchStreamState,
} from '../workbench/stream'
import type { WorkbenchSession } from '../workbench/types'
import { watchAgentRun } from '../workbench/watch-agent-run'
import {
  contextIdentityKey,
  decodeAIContextFromElement,
  inferSelectionKind,
  mergeAIPageContext,
  pinnedContextFromAIContext,
  sanitizeSelectionText,
  workbenchScopeFromAIContext,
  type AIGlobalAssistantAction,
  type AIGlobalAssistantLaunchRequest,
  type AIGlobalAssistantMessage,
  type AIPageContext,
  type AISelectionContext,
} from './ai-context'
import { AIContextMenu, type AIContextMenuState } from './ai-context-menu'
import { AIPageContextRegistry, type AIPageContextRegistryValue } from './ai-context-provider'
import { AIFloatButton } from './ai-float-button'
import { buildGlobalAssistantPrompt, buildGlobalAssistantTitle } from './ai-prompts'
import { AISelectionToolbar, type AISelectionToolbarState } from './ai-selection-toolbar'
import './global-assistant.css'

const AIFloatingAssistantPanel = lazy(async () => {
  const module = await import('./ai-floating-assistant-panel')
  return { default: module.AIFloatingAssistantPanel }
})

interface AIPageContextRegistration {
  context: AIPageContext
  id: string
  key: string
}

export interface GlobalAssistantCompanionRenderProps {
  disabled: boolean
  messages: AIGlobalAssistantMessage[]
  nativeWindow: boolean
  onAction: (action: AIGlobalAssistantAction) => void
  onOpenAssistant: () => void
  onOpenWorkbench: () => void
  panelOpen: boolean
  running: boolean
}

interface GlobalAIAssistantProviderProps {
  children: ReactNode
  enabled?: boolean
  nativeCompanionWindow?: boolean
  permissionSnapshot?: PermissionSnapshot
  renderCompanion?: (props: GlobalAssistantCompanionRenderProps) => ReactNode
}

interface PendingAssistantCancellation {
  sessionId: string
  runId?: string
  cancelAttempted: boolean
}

interface SharedAssistantSession {
  contextKey: string
  session: WorkbenchSession
}

const sharedSessionStorageKey = 'soha.ai.global-assistant.session'
const sharedSessionChannelName = 'soha-ai-global-assistant'

function readSharedSession(): SharedAssistantSession | null {
  try {
    const raw = localStorage.getItem(sharedSessionStorageKey)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<SharedAssistantSession>
    if (!parsed.session?.id || typeof parsed.contextKey !== 'string') return null
    return parsed as SharedAssistantSession
  } catch {
    return null
  }
}

function writeSharedSession(value: SharedAssistantSession | null) {
  try {
    if (value) localStorage.setItem(sharedSessionStorageKey, JSON.stringify(value))
    else localStorage.removeItem(sharedSessionStorageKey)
    if (typeof BroadcastChannel !== 'undefined') {
      const channel = new BroadcastChannel(sharedSessionChannelName)
      channel.postMessage(value)
      channel.close()
    }
  } catch {
    // The active provider state remains authoritative when browser storage is unavailable.
  }
}

const platformRoutePrefixes = [
  '/clusters',
  '/cluster-resources',
  '/workloads',
  '/configuration',
  '/network',
  '/storage',
  '/platform-access-control',
  '/resource-creation',
  '/manifests',
  '/extensions',
  '/helm',
] as const

export function sourceWorkbenchFromPath(pathname: string): AIPageContext['sourceWorkbench'] {
  if (
    pathname === '/' ||
    pathname.startsWith('/platform') ||
    platformRoutePrefixes.some((prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`))
  ) {
    return 'platform'
  }
  if (pathname.startsWith('/monitoring-workbench')) return 'monitoring'
  if (pathname.startsWith('/delivery') || pathname.startsWith('/applications')) return 'delivery'
  if (pathname.startsWith('/compute')) return 'compute'
  return 'ai'
}

function recordGlobalAssistantEvent(
  action: WorkbenchGlobalAssistantOpenRequest['action'],
  context: AIPageContext,
  sessionId?: string,
) {
  const body: WorkbenchGlobalAssistantOpenRequest = {
    action,
    launchContext: context,
    sessionId,
    source: 'global-assistant',
  }
  void api.post('/copilot/global-assistant/events', body).catch(() => undefined)
}

function routeContext(pathname: string, search: string): AIPageContext {
  return {
    sourceWorkbench: sourceWorkbenchFromPath(pathname),
    sourceRoute: `${pathname}${search}`,
    sourceTitle:
      pathname === '/' ? 'Soha Overview' : pathname.split('/').filter(Boolean).join(' / '),
  }
}

function canUseWorkspaceSelection(target: EventTarget | null) {
  if (!(target instanceof Element)) return false
  if (!target.closest('.soha-pro-content-host')) return false
  return !target.closest(
    [
      'input',
      'textarea',
      'select',
      'button',
      'a',
      '[contenteditable="true"]',
      '[role="textbox"]',
      '.monaco-editor',
      '.ant-modal',
      '.ant-drawer',
      '.ant-dropdown',
    ].join(','),
  )
}

function selectionAnchorElement(selection: Selection) {
  const node = selection.anchorNode
  if (!node) return null
  return node instanceof Element ? node : node.parentElement
}

function selectionSnapshotFromWindow(): {
  context: AISelectionContext
  toolbar: AISelectionToolbarState
} | null {
  const selection = window.getSelection()
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return null
  const anchorElement = selectionAnchorElement(selection)
  if (!canUseWorkspaceSelection(anchorElement)) return null

  const text = sanitizeSelectionText(selection.toString())
  if (text.length < 2) return null

  const range = selection.getRangeAt(0)
  const rect = range.getBoundingClientRect()
  const fallbackRect = range.getClientRects()[0]
  const activeRect = rect.width || rect.height ? rect : fallbackRect
  const left = Math.min(
    Math.max((activeRect?.left ?? 16) + (activeRect?.width ?? 0) / 2, 16),
    window.innerWidth - 16,
  )
  const top = Math.max((activeRect?.top ?? 48) - 10, 16)

  return {
    context: {
      text,
      kind: inferSelectionKind(text),
      sourceElementLabel:
        anchorElement?.getAttribute('aria-label') ??
        anchorElement?.textContent?.trim().slice(0, 80),
    },
    toolbar: {
      left,
      top,
      text,
    },
  }
}

function streamMessageStatus(state: WorkbenchStreamState): AIGlobalAssistantMessage['status'] {
  if (state.error || state.agentStatus?.status === 'failed') return 'error'
  if (state.agentStatus?.status === 'cancelled') return 'abort'
  if (state.done || state.message.done) return 'success'
  return 'loading'
}

function streamContent(state: WorkbenchStreamState, fallback: string) {
  if (state.message.content) return state.message.content
  if (state.error) return state.error.message
  if (state.agentStatus?.status === 'failed') return 'Agent 执行失败。'
  if (state.agentStatus?.status === 'cancelled') return '已取消本次回复。'
  return fallback
}

function nextMessageId(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

class WorkbenchStreamEventError extends Error {
  code?: string
  retryable?: boolean

  constructor(error: NonNullable<WorkbenchStreamState['error']>) {
    super(error.message)
    this.name = 'WorkbenchStreamEventError'
    this.code = error.code
    this.retryable = error.retryable
  }
}

export function GlobalAIAssistantProvider({
  children,
  enabled = true,
  nativeCompanionWindow = false,
  permissionSnapshot,
  renderCompanion,
}: GlobalAIAssistantProviderProps) {
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = AntdApp.useApp()
  const companionMode = usePreferencesStore((state) => state.companionMode)
  const canUseChat = hasPermission(permissionSnapshot, 'observe.ai.chat')
  const routeFallbackContext = useMemo(
    () => routeContext(location.pathname, location.search),
    [location.pathname, location.search],
  )
  const [registrations, setRegistrations] = useState<AIPageContextRegistration[]>([])
  const pageContext = registrations[registrations.length - 1]?.context ?? routeFallbackContext
  const [panelOpen, setPanelOpen] = useState(false)
  const [assistantInput, setAssistantInput] = useState('')
  const [messages, setMessages] = useState<AIGlobalAssistantMessage[]>([])
  const [streamState, setStreamState] = useState<WorkbenchStreamState>(() =>
    createWorkbenchStreamState(),
  )
  const initialSharedSession = useMemo(() => readSharedSession(), [])
  const [currentSession, setCurrentSession] = useState<WorkbenchSession | null>(
    initialSharedSession?.session ?? null,
  )
  const [currentSessionContextKey, setCurrentSessionContextKey] = useState<string | null>(
    initialSharedSession?.contextKey ?? null,
  )
  const [running, setRunning] = useState(false)
  const [selectionContext, setSelectionContext] = useState<AISelectionContext | null>(null)
  const [selectionToolbar, setSelectionToolbar] = useState<AISelectionToolbarState | null>(null)
  const [contextMenu, setContextMenu] = useState<AIContextMenuState | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const externalRunRef = useRef<{ sessionId: string; runId?: string } | null>(null)
  const cancellationRef = useRef<PendingAssistantCancellation | null>(null)
  const cancellationController = useRef<AbortController | null>(null)
  const [cancellation, setCancellation] = useState<PendingAssistantCancellation | null>(null)
  const [cancellationError, setCancellationError] = useState<Error | null>(null)
  const [cancellationRuns, setCancellationRuns] = useState<{ id: string; status: string }[]>([])
  const [checkingCancellation, setCheckingCancellation] = useState(false)
  const chatAllowedRef = useRef(canUseChat && enabled)
  chatAllowedRef.current = canUseChat && enabled
  const selectionTimerRef = useRef<number | null>(null)

  const activeContext = useMemo(() => mergeAIPageContext(pageContext), [pageContext])

  useEffect(() => {
    if (typeof BroadcastChannel === 'undefined') return undefined
    const channel = new BroadcastChannel(sharedSessionChannelName)
    channel.onmessage = (event: MessageEvent<SharedAssistantSession | null>) => {
      if (!chatAllowedRef.current) return
      const shared = event.data
      setCurrentSession(shared?.session ?? null)
      setCurrentSessionContextKey(shared?.contextKey ?? null)
    }
    return () => channel.close()
  }, [])

  const registerPageContext = useCallback((id: string, context: AIPageContext, key: string) => {
    setRegistrations((items) => {
      const existing = items.find((item) => item.id === id)
      if (existing?.key === key) return items
      const next = { id, context, key }
      if (!existing) return [...items, next]
      return items.map((item) => (item.id === id ? next : item))
    })
    return () => {
      setRegistrations((items) => items.filter((item) => item.id !== id))
    }
  }, [])

  const ensureSession = useCallback(
    async (context: AIPageContext, action: AIGlobalAssistantAction, signal: AbortSignal) => {
      const key = contextIdentityKey(context)
      if (currentSession && (nativeCompanionWindow || currentSessionContextKey === key)) {
        return { session: currentSession, created: false }
      }

      const mode =
        action.startsWith('troubleshoot') || action === 'analyze-page' ? 'root_cause' : 'general'
      const catalog = await queryClient.fetchQuery(workbenchQueries.catalog())
      const provider = preferredExternalAgent(catalog.data.agentProviders ?? [], mode)
      if (!provider) throw new Error('没有支持当前任务的就绪助手，请先配置 Agent 插件。')
      const response = await api.postWithSignal<ApiResponse<WorkbenchSession>>(
        '/copilot/sessions',
        {
          title: buildGlobalAssistantTitle(action, context),
          mode,
          agentProviderId: provider.id,
          scope: workbenchScopeFromAIContext(context),
          pinnedContext: pinnedContextFromAIContext(context),
          source: 'global-assistant',
          tags: ['global-assistant'],
        },
        signal,
      )
      if (signal.aborted) {
        void api.delete(`/copilot/sessions/${response.data.id}`).catch(() => undefined)
        throw new DOMException('The operation was aborted.', 'AbortError')
      }
      setCurrentSession(response.data)
      setCurrentSessionContextKey(key)
      writeSharedSession({ session: response.data, contextKey: key })
      await queryClient.invalidateQueries({ queryKey: workbenchKeys.sessions.all() })
      return { session: response.data, created: true }
    },
    [currentSession, currentSessionContextKey, nativeCompanionWindow, queryClient],
  )

  const readCancellation = useCallback(
    async (target: PendingAssistantCancellation, requestCancel = false) => {
      if (cancellationController.current || !chatAllowedRef.current) return
      const controller = new AbortController()
      cancellationController.current = controller
      setCheckingCancellation(true)
      setCancellationError(null)
      try {
        const response = await workbenchApi.agentRuns.session(target.sessionId)
        if (controller.signal.aborted) return
        if (!target.runId) {
          setCancellationRuns(
            response.data
              .filter((run) => !run.sessionId || run.sessionId === target.sessionId)
              .map(({ id, status }) => ({ id, status })),
          )
          throw new Error(
            '尚未取得本次任务 ID。请选择与本次请求对应的任务，或打开完整工作台核对；不会自动取消其他任务。',
          )
        }
        let run = response.data.find(
          (item) =>
            item.id === target.runId && (!item.sessionId || item.sessionId === target.sessionId),
        )
        if (!run) throw new Error('暂未读到对应任务，请稍后重新读取状态。')
        if (requestCancel && isRunningExternalAgentRun(run)) {
          target.cancelAttempted = true
          const receipt = await workbenchApi.agentRuns.cancel(target.runId)
          if (controller.signal.aborted) return
          if (
            receipt.data.id !== target.runId ||
            (receipt.data.sessionId && receipt.data.sessionId !== target.sessionId)
          )
            throw new Error('取消回执与原任务不一致，请读取状态核对。')
          run = receipt.data
        }
        if (isRunningExternalAgentRun(run) && !requestCancel)
          throw new Error('任务仍在运行，可继续读取状态或明确请求取消。')
        if (isRunningExternalAgentRun(run)) {
          run = await watchAgentRun(
            target.sessionId,
            target.runId,
            controller.signal,
            () => undefined,
          )
        }
        if (controller.signal.aborted || cancellationRef.current !== target) return
        setMessages((items) =>
          items.concat({
            id: nextMessageId('ai-cancel'),
            role: 'system',
            content: `任务 ${run.id} 已确认终态：${run.status}`,
            status: 'success',
          }),
        )
        cancellationRef.current = null
        setCancellation(null)
        await queryClient.invalidateQueries({ queryKey: workbenchKeys.agentRuns.all() })
      } catch (error) {
        if (!controller.signal.aborted && cancellationRef.current === target)
          setCancellationError(error instanceof Error ? error : new Error('任务状态未确认'))
      } finally {
        if (cancellationController.current === controller) cancellationController.current = null
        setCheckingCancellation(false)
      }
    },
    [queryClient],
  )

  const sendPrompt = useCallback(
    async (
      action: AIGlobalAssistantAction,
      context: AIPageContext,
      prompt: string,
      selection?: AISelectionContext,
    ) => {
      if (!canUseChat) {
        void message.warning('当前账号没有 AI 助手权限')
        return
      }
      if (running || abortRef.current || cancellationRef.current) {
        void message.warning('当前回复仍在生成')
        return
      }

      setPanelOpen(true)
      setContextMenu(null)
      setSelectionToolbar(null)

      const userMessageId = nextMessageId('ai-user')
      const assistantMessageId = nextMessageId('ai-assistant')
      setMessages((items) =>
        items.concat(
          { id: userMessageId, role: 'user', content: prompt, status: 'success' },
          { id: assistantMessageId, role: 'assistant', content: '', status: 'loading' },
        ),
      )
      setStreamState(createWorkbenchStreamState())
      setRunning(true)

      const controller = new AbortController()
      abortRef.current = controller
      let createdSessionId: string | null = null
      let submitted = false
      let terminalConfirmed = false

      try {
        const ensured = await ensureSession(context, action, controller.signal)
        const session = ensured.session
        externalRunRef.current = { sessionId: session.id }
        createdSessionId = ensured.created ? session.id : null
        let nextStreamState = createWorkbenchStreamState()
        const seenEvents = new Set<string>()
        const request: WorkbenchSendMessageStreamRequest = {
          content: prompt,
          mode:
            action === 'freeform'
              ? session.metadata?.mode || 'general'
              : action.startsWith('troubleshoot') || action === 'analyze-page'
                ? 'root_cause'
                : 'general',
          agentProviderId: session.metadata?.agentProviderId,
          scopeOverrides: workbenchScopeFromAIContext(context),
          source: 'global-assistant',
          launchContext: context,
          selectionContext: selection,
          pinnedContext: pinnedContextFromAIContext(context),
        }

        if (controller.signal.aborted)
          throw new DOMException('The operation was aborted.', 'AbortError')
        submitted = true
        await streamWorkbenchMessage(
          `/copilot/sessions/${session.id}/messages/stream`,
          request,
          (event) => {
            if (controller.signal.aborted || !chatAllowedRef.current) return
            if (
              event.type === 'agent.status' &&
              event.status === 'queued' &&
              event.runId &&
              event.providerId !== 'internal'
            ) {
              externalRunRef.current = { sessionId: session.id, runId: event.runId }
            }
            const eventKey = workbenchStreamEventKey(event)
            if (seenEvents.has(eventKey)) return
            seenEvents.add(eventKey)
            nextStreamState = reduceWorkbenchStreamState(nextStreamState, event)
            setStreamState(nextStreamState)
            setMessages((items) =>
              items.map((item) => {
                if (item.id !== assistantMessageId) return item
                return {
                  ...item,
                  id: item.id,
                  content: streamContent(nextStreamState, item.content),
                  status: streamMessageStatus(nextStreamState),
                }
              }),
            )
            if (nextStreamState.error) {
              throw new WorkbenchStreamEventError(nextStreamState.error)
            }
          },
          controller.signal,
        )

        if (externalRunRef.current?.runId) {
          nextStreamState = createWorkbenchStreamState()
          const run = await watchAgentRun(
            session.id,
            externalRunRef.current.runId,
            controller.signal,
            (snapshot) => {
              if (controller.signal.aborted || !chatAllowedRef.current) return
              for (const event of sortedAgentRunReplayEvents(snapshot, session.id)) {
                const key = workbenchStreamEventKey(event)
                if (seenEvents.has(key)) continue
                seenEvents.add(key)
                nextStreamState = reduceWorkbenchStreamState(nextStreamState, event)
              }
              setStreamState(nextStreamState)
              setMessages((items) =>
                items.map((item) =>
                  item.id === assistantMessageId
                    ? {
                        ...item,
                        content: nextStreamState.message.content || '助手正在处理当前请求。',
                        status: isRunningExternalAgentRun(snapshot) ? 'loading' : 'success',
                      }
                    : item,
                ),
              )
            },
          )
          terminalConfirmed = true
          if (run.status !== 'completed' && run.status !== 'succeeded')
            throw new Error(
              run.status === 'canceled' || run.status === 'cancelled'
                ? '已取消本次回复。'
                : '助手运行失败，请查看运行记录。',
            )
          const content =
            typeof run.output?.summary === 'string'
              ? run.output.summary
              : nextStreamState.message.content
          setMessages((items) =>
            items.map((item) =>
              item.id === assistantMessageId ? { ...item, content, status: 'success' } : item,
            ),
          )
        }
        if (controller.signal.aborted || !chatAllowedRef.current) return
        setCurrentSession((current) =>
          current?.id === session.id
            ? {
                ...current,
                metadata: {
                  ...current.metadata,
                  mode: request.mode as NonNullable<WorkbenchSession['metadata']>['mode'],
                },
              }
            : current,
        )
        await queryClient.invalidateQueries({
          queryKey: workbenchKeys.sessions.messages(session.id),
        })
        await queryClient.invalidateQueries({
          queryKey: workbenchKeys.sessions.detail(session.id),
        })
        await queryClient.invalidateQueries({ queryKey: workbenchKeys.sessions.all() })
        await queryClient.invalidateQueries({ queryKey: workbenchKeys.agentRuns.all() })
        await queryClient.invalidateQueries({ queryKey: systemKeys.audit.all })
      } catch (error) {
        const isAbort = error instanceof DOMException && error.name === 'AbortError'
        if (isAbort && createdSessionId && !submitted) {
          const sessionId = createdSessionId
          setCurrentSession((current) => (current?.id === sessionId ? null : current))
          setCurrentSessionContextKey(null)
          writeSharedSession(null)
          await api.delete(`/copilot/sessions/${sessionId}`).catch(() => undefined)
          await queryClient.invalidateQueries({ queryKey: workbenchKeys.sessions.all() })
        }
        if (
          submitted &&
          !terminalConfirmed &&
          !(error instanceof WorkbenchStreamEventError) &&
          externalRunRef.current &&
          !cancellationRef.current
        ) {
          const target = { ...externalRunRef.current, cancelAttempted: false }
          cancellationRef.current = target
          setCancellation(target)
          setCancellationError(
            new Error('输出已中断，后台任务状态尚未确认。请读取状态或在完整工作台核对。'),
          )
        }
        if (chatAllowedRef.current)
          setMessages((items) =>
            items.map((item) =>
              item.id === assistantMessageId
                ? {
                    ...item,
                    content: isAbort
                      ? submitted
                        ? '输出已停止，后台任务状态待确认。'
                        : '已停止本次请求。'
                      : error instanceof Error
                        ? error.message
                        : 'AI 请求失败。',
                    status: isAbort ? 'abort' : 'error',
                  }
                : item,
            ),
          )
      } finally {
        abortRef.current = null
        externalRunRef.current = null
        setRunning(false)
      }
    },
    [canUseChat, ensureSession, message, queryClient, running],
  )

  const launchAssistant = useCallback(
    async (request: AIGlobalAssistantLaunchRequest) => {
      if (!enabled) return
      const context = mergeAIPageContext(activeContext, request.contextOverride)
      const selection =
        request.selection ??
        (request.action.includes('selection') ? (selectionContext ?? undefined) : undefined)
      if (request.action === 'open') {
        setPanelOpen(true)
        recordGlobalAssistantEvent('open', context, currentSession?.id)
        return
      }
      const prompt = buildGlobalAssistantPrompt(request.action, context, selection, request.prompt)
      await sendPrompt(request.action, context, prompt, selection)
    },
    [activeContext, currentSession?.id, enabled, selectionContext, sendPrompt],
  )

  const openWorkbench = useCallback(() => {
    const params = new URLSearchParams()
    const scope = workbenchScopeFromAIContext(activeContext)
    Object.entries(scope).forEach(([key, value]) => {
      if (value !== undefined && value !== null && value !== '') params.set(key, String(value))
    })
    ;(['service', 'pod', 'node', 'entityKind', 'entityName', 'sourceWorkbench'] as const).forEach(
      (key) => {
        const value = activeContext[key]
        if (value) params.set(key, String(value))
      },
    )
    if (currentSession?.id) params.set('session', currentSession.id)
    recordGlobalAssistantEvent('open-workbench', activeContext, currentSession?.id)
    navigate(getAIWorkbenchPathForMode('general', params))
  }, [activeContext, currentSession?.id, navigate])

  const cancelStream = useCallback(() => {
    const active = externalRunRef.current
    abortRef.current?.abort()
    if (!active || cancellationRef.current) return
    const target = { ...active, cancelAttempted: false }
    cancellationRef.current = target
    setCancellation(target)
    void readCancellation(target, true)
  }, [readCancellation])

  const closeAssistant = useCallback(() => setPanelOpen(false), [])

  useEffect(
    () => () => {
      abortRef.current?.abort()
      cancellationController.current?.abort()
    },
    [],
  )

  useEffect(() => {
    if (enabled && canUseChat) return
    abortRef.current?.abort()
    cancellationController.current?.abort()
    if (externalRunRef.current && !cancellationRef.current) {
      const target = { ...externalRunRef.current, cancelAttempted: false }
      cancellationRef.current = target
      setCancellation(target)
    }
    setMessages([])
    setCancellationRuns([])
    setCancellationError(new Error('权限已变化，重新取得权限后请读取任务状态。'))
    setAssistantInput('')
    setStreamState(createWorkbenchStreamState())
    setCurrentSession(null)
    setCurrentSessionContextKey(null)
    writeSharedSession(null)
    if (selectionTimerRef.current !== null) {
      window.clearTimeout(selectionTimerRef.current)
      selectionTimerRef.current = null
    }
    setPanelOpen(false)
    setSelectionContext(null)
    setSelectionToolbar(null)
    setContextMenu(null)
  }, [enabled, canUseChat])

  useEffect(() => {
    if (!enabled || !canUseChat) {
      setSelectionToolbar(null)
      return undefined
    }

    const updateSelection = () => {
      if (selectionTimerRef.current !== null) window.clearTimeout(selectionTimerRef.current)
      selectionTimerRef.current = window.setTimeout(() => {
        selectionTimerRef.current = null
        const snapshot = selectionSnapshotFromWindow()
        setSelectionContext(snapshot?.context ?? null)
        setSelectionToolbar(snapshot?.toolbar ?? null)
      }, 0)
    }
    const clearSelection = () => {
      setSelectionToolbar(null)
      setContextMenu(null)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') clearSelection()
    }

    document.addEventListener('mouseup', updateSelection)
    document.addEventListener('keyup', updateSelection)
    document.addEventListener('selectionchange', updateSelection)
    document.addEventListener('keydown', onKeyDown)
    window.addEventListener('scroll', clearSelection, true)
    return () => {
      if (selectionTimerRef.current !== null) {
        window.clearTimeout(selectionTimerRef.current)
        selectionTimerRef.current = null
      }
      document.removeEventListener('mouseup', updateSelection)
      document.removeEventListener('keyup', updateSelection)
      document.removeEventListener('selectionchange', updateSelection)
      document.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('scroll', clearSelection, true)
    }
  }, [canUseChat, enabled])

  useEffect(() => {
    if (!enabled || !canUseChat) return undefined

    const onContextMenu = (event: MouseEvent) => {
      if (!canUseWorkspaceSelection(event.target)) return
      event.preventDefault()
      const snapshot = selectionSnapshotFromWindow()
      const contextElement =
        event.target instanceof Element ? event.target.closest('[data-ai-context]') : null
      if (snapshot) {
        setSelectionContext(snapshot.context)
      }
      setContextMenu({
        contextOverride: decodeAIContextFromElement(contextElement),
        left: Math.min(Math.max(event.clientX, 12), window.innerWidth - 220),
        top: Math.min(Math.max(event.clientY, 12), window.innerHeight - 180),
        hasSelection: Boolean(snapshot?.context.text || selectionContext?.text),
      })
      setSelectionToolbar(null)
    }
    const closeMenu = () => setContextMenu(null)

    document.addEventListener('contextmenu', onContextMenu)
    document.addEventListener('click', closeMenu)
    return () => {
      document.removeEventListener('contextmenu', onContextMenu)
      document.removeEventListener('click', closeMenu)
    }
  }, [canUseChat, enabled, selectionContext?.text])

  const contextValue = useMemo<AIPageContextRegistryValue>(
    () => ({
      currentContext: activeContext,
      launchAssistant,
      openAssistant: () => {
        if (enabled) setPanelOpen(true)
      },
      openWorkbench,
      registerPageContext,
    }),
    [activeContext, enabled, launchAssistant, openWorkbench, registerPageContext],
  )

  return (
    <AIPageContextRegistry.Provider value={contextValue}>
      {children}
      {enabled && renderCompanion && (nativeCompanionWindow || companionMode === 'companion')
        ? renderCompanion({
            disabled: !canUseChat,
            messages,
            panelOpen,
            running,
            nativeWindow: nativeCompanionWindow,
            onAction: (action) => {
              void launchAssistant({ action })
            },
            onOpenAssistant: () => setPanelOpen(true),
            onOpenWorkbench: openWorkbench,
          })
        : null}
      {enabled && !nativeCompanionWindow && companionMode === 'icon' ? (
        <AIFloatButton
          disabled={!canUseChat}
          hasSelection={Boolean(selectionContext?.text)}
          running={running}
          onAction={(action) => {
            void launchAssistant({ action })
          }}
          onOpenAssistant={() => setPanelOpen(true)}
          onOpenWorkbench={openWorkbench}
        />
      ) : null}
      {enabled && canUseChat && panelOpen ? (
        <Suspense fallback={null}>
          <AIFloatingAssistantPanel
            context={activeContext}
            disabled={!canUseChat || Boolean(cancellation)}
            feedback={
              cancellation ? (
                <Alert
                  showIcon
                  type="warning"
                  title="后台任务取消待确认"
                  description={
                    <Space orientation="vertical">
                      <span>{cancellation.runId || cancellation.sessionId}</span>
                      <span>{cancellationError?.message || '正在核对后台任务终态'}</span>
                      {!cancellation.runId && cancellationRuns.length ? (
                        <Select
                          aria-label="待核对助手任务"
                          style={{ width: '100%' }}
                          placeholder="选择本次请求对应的任务 ID"
                          options={cancellationRuns.map((run) => ({
                            value: run.id,
                            label: `${run.id} · ${run.status}`,
                          }))}
                          onChange={(runId) => {
                            const target = { ...cancellation, runId }
                            cancellationRef.current = target
                            setCancellation(target)
                            setCancellationError(null)
                          }}
                        />
                      ) : null}
                    </Space>
                  }
                  action={
                    <Space orientation="vertical">
                      <Button
                        size="small"
                        loading={checkingCancellation}
                        onClick={() => void readCancellation(cancellation)}
                      >
                        读取状态
                      </Button>
                      {cancellation.runId && !checkingCancellation ? (
                        <Button
                          size="small"
                          onClick={() => void readCancellation(cancellation, true)}
                        >
                          请求取消
                        </Button>
                      ) : null}
                    </Space>
                  }
                />
              ) : undefined
            }
            inputValue={assistantInput}
            messages={messages}
            open
            running={running}
            sources={streamState.sources}
            thinkingSummary={streamState.thinking?.summary}
            toolCalls={streamState.toolCalls}
            onCancel={cancelStream}
            onClose={closeAssistant}
            onInputChange={setAssistantInput}
            onOpenWorkbench={openWorkbench}
            onQuickPrompt={(prompt) => {
              void launchAssistant({ action: 'freeform', prompt })
            }}
            onSubmit={(value) => {
              const prompt = value.trim()
              if (!prompt) return
              setAssistantInput('')
              void launchAssistant({ action: 'freeform', prompt })
            }}
          />
        </Suspense>
      ) : null}
      {enabled ? (
        <>
          <AISelectionToolbar
            disabled={!canUseChat}
            state={selectionToolbar}
            onAction={(action) => {
              void launchAssistant({ action })
            }}
            onClose={() => setSelectionToolbar(null)}
          />
          <AIContextMenu
            disabled={!canUseChat}
            state={contextMenu}
            onAction={(action) => {
              void launchAssistant({ action, contextOverride: contextMenu?.contextOverride })
            }}
            onClose={() => setContextMenu(null)}
            onOpenWorkbench={openWorkbench}
          />
        </>
      ) : null}
    </AIPageContextRegistry.Provider>
  )
}
