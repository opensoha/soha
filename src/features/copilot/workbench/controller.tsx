import { ManagementState } from '@/components/management-list'
import { MutationFeedback, ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { observabilitySignalQueries } from '@/features/observability'
import { systemKeys } from '@/features/system'
import { useAuthStore } from '@/stores/auth-store'
import { downloadBlob } from '@/utils/download'
import { queryAccessDenied, readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import {
  BookOutlined,
  CommentOutlined,
  CopyOutlined,
  DeleteOutlined,
  EditOutlined,
  LinkOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { Bubble, Conversations, Prompts, Sources, ThoughtChain, Welcome } from '@ant-design/x'
import type { BubbleListRef } from '@ant-design/x/es/bubble/interface'
import type { SenderRef } from '@ant-design/x/es/sender'
import type {
  AgentRunEventPage,
  WorkbenchMessageWindow,
  WorkbenchSendMessageStreamRequest,
  WorkbenchSource,
  WorkbenchTaskIntent,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  App,
  Button,
  Card,
  Flex,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import '../copilot-pages.css'
import { evaluationLifecycleApi } from '../evaluation-lifecycle/api'
import { observeKeys } from '../observe/keys'
import { observeQueries } from '../observe/queries'
import {
  agentRunReplayMessage,
  agentRunTimestamp,
  isDurableAgentRun,
  isExternalAgentRunReplayMessage,
  isFinalAgentRunMessage,
  isNativeAgentRun,
  isRunningAgentRun,
  messageAgentRunRefs,
  sortedAgentRunReplayEvents,
} from './agent-run-replay'
import { agentUnavailableReason, taskAgentUnavailableReason } from './agent-selection'
import { workbenchApi } from './api'
import {
  artifactContextLinks,
  artifactMeta,
  artifactSnapshotText,
  artifactTitle,
  graphNodeLabel,
  isStaticArtifact,
  staticArtifactLabel,
  type ArtifactContextLink,
  type WorkbenchArtifactEntry,
} from './artifacts'
import './assistant.css'
import { AnimatedAssistantIcon } from './components/animated-icon'
import { AssistantAvatar } from './components/assistant-avatar'
import { AssistantChatResult, UserMessageContent } from './components/assistant-chat-result'
import { AssistantPanel, type AssistantTab } from './components/assistant-panel'
import { ChatMemory } from './components/chat-memory'
import { WorkbenchComposer } from './components/composer'
import { ChatContextSelection, ChatContextTags } from './components/context-selection'
import { LazyWorkbenchGraphView } from './components/graph-view-loader'
import { WorkbenchMarkdown, safeSourceURL } from './components/markdown'
import { MessageActivity } from './components/message-activity'
import { MessageProgress } from './components/message-progress'
import { MessageSearch, rememberChatVisit } from './components/message-search'
import { PersonalFeed } from './components/personal-feed'
import { SessionReferenceReader } from './components/session-reference-reader'
import { MobileAssistantNav, WorkbenchPanel, WorkbenchShell } from './components/shell'
import { StaticArtifactView } from './components/static-artifact'
import { CHAT_STARTERS } from './composer-commands'
import { contextSnapshotSummary } from './context-selection'
import {
  assistantReactionForMessage,
  bubbleItems,
  isLegacyPlatformContextMessage,
  mergeConversationMessages,
  modelStatusDetail,
  pendingConversationMessages,
  type ConversationMessage,
  type WorkbenchBubbleStatus,
} from './conversation'
import { useChatDraft } from './hooks/use-chat-draft'
import { useNativeRequestRecovery } from './hooks/use-native-request-recovery'
import { useWorkbenchRouteState } from './hooks/use-workbench-route-state'
import { workbenchKeys } from './keys'
import {
  RUNNABLE_ANALYSIS_MODE_OPTIONS,
  defaultAnalysisQuestion,
  modeDescription,
  modeLabel,
} from './mode'
import { displayWorkbenchSessionTitle } from './model'
import { workbenchMutations } from './mutations'
import {
  getAIModelSettingsPath,
  getAIOperationsPath,
  getAIWorkbenchPathForMode,
  getAIWorkbenchPathForSession,
} from './navigation'
import { workbenchQueries } from './queries'
import { PINNED_SESSION_TAG, sessionTagsWithPin } from './session-organization'
import {
  canonicalWorkbenchAgentStatus,
  createWorkbenchStreamState,
  isTerminalWorkbenchAgentStatus,
  reduceWorkbenchStreamState,
  streamWorkbenchMessage,
  workbenchStreamEventKey,
  type WorkbenchStreamState,
} from './stream'
import {
  WorkbenchStreamEventError,
  agentStatusLabel,
  isRetryableWorkbenchStreamError,
  metadataAgentStatus,
  metadataSources,
  metadataThinkingSummary,
  replayArtifactsForMessage,
  sourceItemsForArtifactEntry,
  streamBubbleStatus,
  streamFallbackContent,
  streamMessageMetadata,
  thoughtChainStatus,
  toolCallSummaryText,
  workbenchStreamErrorMessage,
  type ThoughtChainStatus,
} from './stream-presentation'
import {
  TOOLSET_BUDGET_FIELDS,
  buildDisabledToolOptions,
  canonicalDisabledToolNames,
  cleanToolsetPayload,
  countObjectKeys,
  numberRecord,
  recommendedAdapterIds,
  scopeOverrideState,
} from './toolset'
import type { AssistantChatTask, WorkbenchMode, WorkbenchSessionScope } from './types'

const { Paragraph, Text } = Typography

function combineNativeEventPages(
  results: Array<{
    data?: { runId: string; afterSequence: number; page: AgentRunEventPage }
    isError: boolean
    error: Error | null
    isFetching: boolean
    refetch: () => Promise<unknown>
  }>,
) {
  return {
    pages: results.flatMap((result) => {
      const data = readableQueryData(result)
      return data ? [data] : []
    }),
    isError: results.some((result) => result.isError),
    error: results.find((result) => result.isError)?.error ?? null,
    isFetching: results.some((result) => result.isFetching),
    refetch: () => Promise.all(results.map((result) => result.refetch())),
  }
}

type InspectorView = 'evidence' | 'hypotheses' | 'actions'
type TaskCapability = NonNullable<WorkbenchTaskIntent['capabilities']>[number]
type WorkbenchStreamRetryInput = {
  sessionId: string
  request: WorkbenchSendMessageStreamRequest & { assistantTask?: AssistantChatTask }
  closeAnalysisOnSuccess?: boolean
  navigateMode?: WorkbenchMode
}
type WorkbenchStreamSubmission = WorkbenchStreamRetryInput & {
  controller: AbortController
  pendingMessages: { user: ConversationMessage; assistant: ConversationMessage }
}
function formatSessionTimestamp(value?: string) {
  if (!value) return '刚刚'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

function buildScopeSummary(scope?: WorkbenchSessionScope) {
  if (!scope) return '未固定上下文'
  return (
    [
      scope.clusterId,
      scope.namespace,
      scope.workload || scope.service || scope.pod || scope.node,
      scope.alertId,
    ]
      .filter(Boolean)
      .join(' / ') || '未固定上下文'
  )
}

export function AIWorkbenchController() {
  const { message, modal } = App.useApp()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const {
    personal,
    feedView,
    messageAnchor,
    messageSearchQuery,
    draftScope,
    isExplicitRouteMode,
    location,
    pathMode,
    requestedSessionId,
    searchParams,
    setSearchParams,
    updateSearchParams,
  } = useWorkbenchRouteState()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canUseChat = hasPermission(permissionSnapshotQuery.data?.data, 'observe.ai.chat')
  const canCreateInspection = hasPermission(
    permissionSnapshotQuery.data?.data,
    'observe.ai.inspection.create',
  )
  const canCreateInspectionTask = canUseChat && canCreateInspection
  const canRunRootCause = hasPermission(
    permissionSnapshotQuery.data?.data,
    'observe.ai.root-cause.run',
  )
  const canViewMonitoring = hasPermission(
    permissionSnapshotQuery.data?.data,
    'observe.monitoring.view',
  )
  const canViewAI = hasPermission(permissionSnapshotQuery.data?.data, 'observe.ai.view')
  const canInvokeGateway = hasPermission(permissionSnapshotQuery.data?.data, 'ai.gateway.invoke')
  const autoSessionScopeKeyRef = useRef<string>('')
  const profileFocusPending = useRef(false)
  const senderRef = useRef<SenderRef>(null)
  const focusAfterFeed = useRef(false)
  useEffect(() => {
    if (feedView || !focusAfterFeed.current) return
    focusAfterFeed.current = false
    senderRef.current?.focus()
  }, [feedView])
  const activeSessionRef = useRef(requestedSessionId)
  useEffect(() => {
    activeSessionRef.current = requestedSessionId
  }, [requestedSessionId])
  const streamAbortRef = useRef<Record<string, AbortController>>({})
  const [streamingSessions, setStreamingSessions] = useState<string[]>([])
  const currentStreamPending = streamingSessions.includes(requestedSessionId ?? '')
  const externalRunReplayRef = useRef<
    Record<
      string,
      { state: WorkbenchStreamState; seenEventKeys: Set<string>; afterSequence?: number }
    >
  >({})
  const terminalRunRefreshRef = useRef<Record<string, string>>({})

  const initialMode = pathMode

  const [renameOpen, setRenameOpen] = useState(false)
  const [renameValue, setRenameValue] = useState('')
  const [renamePinned, setRenamePinned] = useState(false)
  const [renameTargetId, setRenameTargetId] = useState<string>()
  const [panel, setPanel] = useState<
    | 'assistant'
    | 'settings'
    | 'toolset'
    | 'thinking'
    | 'inspector'
    | 'history'
    | 'reference'
    | 'source'
    | null
  >(() => (window.matchMedia('(min-width: 992px)').matches && personal ? 'assistant' : null))
  const [assistantTab, setAssistantTab] = useState<AssistantTab>('activity')
  const [sideOpen, setSideOpen] = useState(false)
  const [sideTitle, setSideTitle] = useState('')
  // ponytail: reactions stay in this page; add durable storage when cross-device sync is needed.
  const [reactions, setReactions] = useState<Record<string, string>>({})
  const [selectedSource, setSelectedSource] = useState<WorkbenchSource>()
  const openSource = useCallback((source: WorkbenchSource) => {
    setSelectedSource(source)
    setPanel('source')
  }, [])
  const [searchOpen, setSearchOpen] = useState(false)
  const bubbleRef = useRef<BubbleListRef>(null)
  const historyKey = `${requestedSessionId}:${messageAnchor}`
  const [history, setHistory] = useState<{
    key: string
    before: WorkbenchMessageWindow[]
    after: WorkbenchMessageWindow[]
  }>({ key: '', before: [], after: [] })
  const [referenceSessionId, setReferenceSessionId] = useState('')
  const [inspectorView, setInspectorView] = useState<InspectorView>('evidence')
  const [draftMode, setDraftMode] = useState<WorkbenchMode>(initialMode)
  const [analysisOpen, setAnalysisOpen] = useState(false)
  const [analysisCapabilities, setAnalysisCapabilities] = useState<TaskCapability[]>([])
  const analysisMode: WorkbenchMode = analysisCapabilities[0] ?? 'root_cause'
  const [analysisIntent, setAnalysisIntent] = useState<WorkbenchTaskIntent>({})
  const [analysisAgentProviderId, setAnalysisAgentProviderId] = useState('internal')
  const [analysisQuestion, setAnalysisQuestion] = useState('')
  const [selectedAgentProviderId, setSelectedAgentProviderId] = useState('internal')
  const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([])
  const [selectedAdapterIds, setSelectedAdapterIds] = useState<string[]>([])
  const [disabledToolNames, setDisabledToolNames] = useState<string[]>([])
  const [budgetOverrides, setBudgetOverrides] = useState<Record<string, number>>({})
  const [scopeOverrides, setScopeOverrides] = useState<Partial<WorkbenchSessionScope>>({})
  const [memoryDraft, setMemoryDraft] = useState<{ fact?: string; sourceRefs?: string[] }>()
  const {
    heldValue,
    assistantTask,
    senderValue,
    contextSelection,
    knowledgeBaseIds,
    modelDraft,
    setSenderValue,
    setContextSelection,
    setKnowledgeBaseIds,
    updateDraft,
  } = useChatDraft(requestedSessionId)
  const owner = useAuthStore((state) => state.user?.userId)
  const [contextParsing, setContextParsing] = useState(false)
  const [selectedArtifactKey, setSelectedArtifactKey] = useState<string>()
  const [personalArtifactEntry, setPersonalArtifactEntry] = useState<WorkbenchArtifactEntry>()
  const selectArtifact = (key: string) => {
    setPersonalArtifactEntry(undefined)
    setSelectedArtifactKey(key)
  }
  const selectedGraphScope = useRef<string | null>(null)
  const [selectedGraphNodeId, setSelectedGraphNodeId] = useState<string | null>(null)
  const [senderResetVersion, setSenderResetVersion] = useState(0)
  const [localMessages, setLocalMessages] = useState<ConversationMessage[]>([])
  const [retryInputs, setRetryInputs] = useState<
    Record<string, WorkbenchStreamRetryInput | undefined>
  >({})
  const lastRetryableInput = retryInputs[requestedSessionId ?? '']

  useEffect(
    () => () => {
      Object.values(streamAbortRef.current).forEach((controller) => controller.abort())
      streamAbortRef.current = {}
    },
    [],
  )

  useEffect(() => {
    externalRunReplayRef.current = {}
    terminalRunRefreshRef.current = {}
  }, [requestedSessionId])

  const sessionsQueryResult = useQuery({
    ...workbenchQueries.sessions.all(canUseChat, personal, owner),
    refetchInterval: 5000,
  })
  const sessionsQuery = { ...sessionsQueryResult, data: readableQueryData(sessionsQueryResult) }
  const catalogQueryResult = useQuery({ ...workbenchQueries.catalog(), refetchInterval: 15000 })
  const catalogQuery = { ...catalogQueryResult, data: readableQueryData(catalogQueryResult) }
  const metricCatalogQueryResult = useQuery({
    ...observabilitySignalQueries.metricCatalog(),
    enabled:
      analysisOpen &&
      analysisAgentProviderId === 'internal' &&
      canViewMonitoring &&
      analysisCapabilities.includes('performance'),
  })
  const metricCatalogQuery = {
    ...metricCatalogQueryResult,
    data: readableQueryData(metricCatalogQueryResult),
  }
  const inspectionRunsQueryResult = useQuery({
    ...observeQueries.operations.runs(),
    enabled:
      analysisOpen &&
      analysisAgentProviderId === 'internal' &&
      canViewAI &&
      analysisCapabilities.includes('inspection_review'),
  })
  const inspectionRunsQuery = {
    ...inspectionRunsQueryResult,
    data: readableQueryData(inspectionRunsQueryResult),
  }
  const sessionDetailQueryResult = useQuery(
    workbenchQueries.sessions.detail(requestedSessionId, owner),
  )
  const sessionDetailQuery = {
    ...sessionDetailQueryResult,
    data: readableQueryData(sessionDetailQueryResult),
  }
  const matchesWorkspace = Boolean(
    sessionDetailQuery.data?.data &&
    personal ===
      Boolean(sessionDetailQuery.data.data.metadata?.conversationKind?.startsWith('personal_')),
  )
  const requestRecovery = useNativeRequestRecovery(
    owner,
    requestedSessionId,
    canUseChat && matchesWorkspace && !currentStreamPending,
  )
  const messagesQueryResult = useQuery({
    ...workbenchQueries.sessions.messages(requestedSessionId, owner),
    enabled: matchesWorkspace && !!requestedSessionId && !messageAnchor,
  })
  const messagesQuery = { ...messagesQueryResult, data: readableQueryData(messagesQueryResult) }
  const messageWindowQueryResult = useQuery(
    workbenchQueries.messageWindow(
      owner,
      matchesWorkspace ? requestedSessionId : undefined,
      messageAnchor,
    ),
  )
  const messageWindowQuery = {
    ...messageWindowQueryResult,
    data: readableQueryData(messageWindowQueryResult),
  }
  const feedReferenceQueryResult = useQuery(
    workbenchQueries.feedItem(owner, contextSelection.feedItemId || ''),
  )
  const feedReferenceQuery = {
    ...feedReferenceQueryResult,
    data: readableQueryData(feedReferenceQueryResult),
  }
  const conversationDenied =
    queryAccessDenied(sessionDetailQuery) ||
    queryAccessDenied(messagesQuery) ||
    queryAccessDenied(messageWindowQuery)
  useEffect(() => {
    if (!conversationDenied) return
    Object.values(streamAbortRef.current).forEach((controller) => controller.abort())
    setHistory({ key: historyKey, before: [], after: [] })
    setLocalMessages([])
    setMemoryDraft(undefined)
    setRenameOpen(false)
    setRenameValue('')
    setSideOpen(false)
    setSideTitle('')
    updateDraft({ value: '', context: {} })
  }, [conversationDenied, historyKey])
  useEffect(() => {
    setMemoryDraft(undefined)
  }, [owner])
  const historyWindows = !conversationDenied && history.key === historyKey ? history : undefined
  const beforeCursor = historyWindows?.before.length
    ? historyWindows.before[0].beforeCursor
    : messageWindowQuery.data?.data?.beforeCursor
  const afterCursor = historyWindows?.after.length
    ? historyWindows.after.at(-1)?.afterCursor
    : messageWindowQuery.data?.data?.afterCursor
  const windowMutation = useOperationMutation(
    {
      ...workbenchMutations.messageWindow(),
      onSuccess: (response, input) =>
        setHistory((current) => {
          if (input.historyKey !== historyKey) return current
          const base =
            current.key === historyKey ? current : { key: historyKey, before: [], after: [] }
          if (!response.data) return base
          return input.selector.before
            ? { ...base, before: [response.data, ...base.before] }
            : { ...base, after: [...base.after, response.data] }
        }),
    },
    {
      label: ['读取历史消息', 'Read message history'],
      target: (input) => input.sessionId,
      readOnly: true,
    },
  )
  const agentRunsQueryResult = useQuery({
    ...workbenchQueries.agentRuns.session(requestedSessionId),
    enabled: matchesWorkspace && Boolean(requestedSessionId),
    refetchInterval: (query) => {
      const runs = query.state.data?.data ?? []
      return runs.some(
        (run) => run.sessionId === requestedSessionId && !run.parentRunId && isRunningAgentRun(run),
      )
        ? 2500
        : false
    },
  })
  const agentRunsQuery = { ...agentRunsQueryResult, data: readableQueryData(agentRunsQueryResult) }

  const visibleSessions = useMemo(() => sessionsQuery.data?.data ?? [], [sessionsQuery.data?.data])
  const currentSession =
    sessionDetailQuery.data?.data ?? visibleSessions.find((item) => item.id === requestedSessionId)
  const modelPreferences = modelDraft ?? currentSession?.metadata?.modelPreferences ?? {}
  const isMainChat = ['main', 'personal_main'].includes(
    currentSession?.metadata?.conversationKind || '',
  )
  const currentAlertId = currentSession?.metadata?.scope?.alertId
  const currentSessionTitle =
    isMainChat || !currentSession ? '主要聊天' : displayWorkbenchSessionTitle(currentSession.title)
  const serverMessages = useMemo(
    () =>
      messageAnchor
        ? [
            ...(historyWindows?.before.flatMap((page) => page.messages) ?? []),
            ...(messageWindowQuery.data?.data?.messages ?? []),
            ...(historyWindows?.after.flatMap((page) => page.messages) ?? []),
          ]
        : (messagesQuery.data?.data ?? []),
    [messageAnchor, historyWindows, messageWindowQuery.data?.data, messagesQuery.data?.data],
  )
  useEffect(() => {
    if (owner && currentSession?.id) rememberChatVisit(owner, currentSession.id, personal)
  }, [owner, currentSession?.id, personal])
  useEffect(() => {
    if (!messageAnchor || feedView || !messageWindowQuery.data?.data) return
    const frame = requestAnimationFrame(() =>
      bubbleRef.current?.scrollTo({ key: messageAnchor, block: 'center', behavior: 'instant' }),
    )
    return () => cancelAnimationFrame(frame)
  }, [messageAnchor, requestedSessionId, messageWindowQuery.data?.data, feedView])
  const finalAgentRunIds = useMemo(
    () =>
      serverMessages.reduce((ids, item) => {
        if (!isFinalAgentRunMessage(item)) return ids
        for (const runId of messageAgentRunRefs(item)) {
          ids.add(runId)
        }
        return ids
      }, new Set<string>()),
    [serverMessages],
  )
  const runningAgentRuns = useMemo(() => {
    return (agentRunsQuery.data?.data ?? [])
      .filter(
        (run) =>
          run.sessionId === requestedSessionId &&
          !run.parentRunId &&
          isRunningAgentRun(run) &&
          !finalAgentRunIds.has(run.id),
      )
      .sort(
        (left, right) =>
          agentRunTimestamp(left) - agentRunTimestamp(right) || left.id.localeCompare(right.id),
      )
  }, [agentRunsQuery.data?.data, finalAgentRunIds, requestedSessionId])
  const nativeEventPages = useQueries({
    queries: runningAgentRuns
      .filter(isNativeAgentRun)
      .map((run) =>
        workbenchQueries.agentRuns.events(
          run.id,
          externalRunReplayRef.current[run.id]?.afterSequence ?? 0,
        ),
      ),
    combine: combineNativeEventPages,
  })
  const legacyPlatformMessages = useMemo(
    () => serverMessages.filter(isLegacyPlatformContextMessage),
    [serverMessages],
  )
  const messages = useMemo(
    () => mergeConversationMessages(serverMessages, localMessages, requestedSessionId),
    [localMessages, requestedSessionId, serverMessages],
  )
  const adapters = useMemo(
    () => catalogQuery.data?.data?.adapters ?? [],
    [catalogQuery.data?.data?.adapters],
  )
  const dataSources = useMemo(
    () => catalogQuery.data?.data?.dataSources ?? [],
    [catalogQuery.data?.data?.dataSources],
  )
  const globalSkills = useMemo(
    () => catalogQuery.data?.data?.skillsRegistry ?? [],
    [catalogQuery.data?.data?.skillsRegistry],
  )
  const analysisProfiles = useMemo(
    () => catalogQuery.data?.data?.analysisProfiles ?? [],
    [catalogQuery.data?.data?.analysisProfiles],
  )
  const agentProviders = useMemo(
    () => catalogQuery.data?.data?.agentProviders ?? [],
    [catalogQuery.data?.data?.agentProviders],
  )
  const agentCapabilities = useMemo(
    () => catalogQuery.data?.data?.capabilities ?? [],
    [catalogQuery.data?.data?.capabilities],
  )
  const defaultAgentProviderId = useMemo(() => {
    return (
      agentProviders.find((item) => item.id === 'internal' && item.enabled)?.id ??
      agentProviders.find((item) => item.default && item.enabled)?.id ??
      agentProviders.find((item) => item.enabled)?.id ??
      'internal'
    )
  }, [agentProviders])
  const defaultInspectionProfileId = useMemo(() => {
    return analysisProfiles.find((item) => item.enabled && item.mode === 'inspection')?.id
  }, [analysisProfiles])

  useEffect(() => {
    if (!requestedSessionId) return
    if (runningAgentRuns.length === 0 && Object.keys(externalRunReplayRef.current).length === 0) {
      return
    }
    const activeRunIds = new Set(runningAgentRuns.map((run) => run.id))
    const replayMessages = runningAgentRuns.map((run) => {
      const replay = externalRunReplayRef.current[run.id] ?? {
        state: createWorkbenchStreamState(),
        seenEventKeys: new Set<string>(),
        afterSequence: 0,
      }
      let state = replay.state
      const page = nativeEventPages.pages.find(
        (item) => item.runId === run.id && item.afterSequence === (replay.afterSequence ?? 0),
      )
      if (page?.page.resyncRequired) {
        state = createWorkbenchStreamState()
        replay.seenEventKeys.clear()
        replay.afterSequence = 0
        void queryClient.invalidateQueries({
          queryKey: workbenchKeys.sessions.messages(requestedSessionId, owner),
        })
      }
      const events = [
        ...sortedAgentRunReplayEvents(run, requestedSessionId),
        ...(page && !page.page.resyncRequired ? page.page.items : []),
      ]
      for (const event of events) {
        const eventKey = workbenchStreamEventKey(event)
        if (replay.seenEventKeys.has(eventKey)) continue
        replay.seenEventKeys.add(eventKey)
        state = reduceWorkbenchStreamState(state, event)
      }
      if (page && !page.page.resyncRequired) replay.afterSequence = page.page.nextSequence
      replay.state = state
      externalRunReplayRef.current[run.id] = replay
      return agentRunReplayMessage(run, state)
    })

    for (const runId of Object.keys(externalRunReplayRef.current)) {
      if (!activeRunIds.has(runId)) {
        delete externalRunReplayRef.current[runId]
      }
    }

    setLocalMessages((items) => {
      let changed = false
      const next = items.filter((item) => {
        if (!isExternalAgentRunReplayMessage(item)) return true
        const agentRunId = artifactSnapshotText(item.metadata, 'agentRunId', 'agentRuntimeId')
        const keep = agentRunId ? activeRunIds.has(agentRunId) : false
        if (!keep) changed = true
        return keep
      })
      const indexById = new Map(next.map((item, index) => [item.id, index]))
      for (const replayMessage of replayMessages) {
        const index = indexById.get(replayMessage.id)
        if (index === undefined) {
          indexById.set(replayMessage.id, next.length)
          next.push(replayMessage)
          changed = true
        } else {
          const updated = {
            ...replayMessage,
            createdAt: next[index].createdAt,
          }
          if (
            next[index].content !== updated.content ||
            next[index].deliveryStatus !== updated.deliveryStatus ||
            next[index].metadata !== updated.metadata
          ) {
            next[index] = updated
            changed = true
          }
        }
      }
      return changed ? next : items
    })
  }, [requestedSessionId, runningAgentRuns, nativeEventPages.pages, queryClient])

  useEffect(() => {
    if (!requestedSessionId) return
    const terminalRuns = (agentRunsQuery.data?.data ?? []).filter(
      (run) =>
        run.sessionId === requestedSessionId &&
        !run.parentRunId &&
        isDurableAgentRun(run) &&
        isTerminalWorkbenchAgentStatus(run.status) &&
        !finalAgentRunIds.has(run.id),
    )
    if (terminalRuns.length === 0) return
    let shouldRefresh = false
    for (const run of terminalRuns) {
      const status = canonicalWorkbenchAgentStatus(run.status)
      const refreshKey = `${run.id}:${status}:${run.updatedAt ?? ''}:${run.completedAt ?? ''}`
      if (terminalRunRefreshRef.current[run.id] === refreshKey) continue
      terminalRunRefreshRef.current[run.id] = refreshKey
      delete externalRunReplayRef.current[run.id]
      shouldRefresh = true
    }
    if (!shouldRefresh) return
    void queryClient.invalidateQueries({
      queryKey: workbenchKeys.sessions.messages(requestedSessionId, owner),
    })
    void queryClient.invalidateQueries({ queryKey: workbenchKeys.sessions.all(personal) })
    void queryClient.invalidateQueries({
      queryKey: workbenchKeys.sessions.detail(requestedSessionId, owner),
    })
    void queryClient.invalidateQueries({ queryKey: systemKeys.audit.all })
  }, [agentRunsQuery.data?.data, finalAgentRunIds, queryClient, requestedSessionId])

  const mainSessionQueryResult = useQuery(
    workbenchQueries.sessions.main(
      defaultAgentProviderId,
      owner,
      canUseChat && !personal && !catalogQuery.isPending,
    ),
  )
  const mainSessionQuery = {
    ...mainSessionQueryResult,
    data: readableQueryData(mainSessionQueryResult),
  }
  const assistantQueryResult = useQuery(workbenchQueries.assistant(owner, canUseChat && personal))
  const assistantQuery = { ...assistantQueryResult, data: readableQueryData(assistantQueryResult) }
  const personalMainQueryResult = useQuery(
    workbenchQueries.sessions.detail(
      personal ? assistantQuery.data?.data?.mainSessionId : undefined,
      owner,
    ),
  )
  const personalMainQuery = {
    ...personalMainQueryResult,
    data: readableQueryData(personalMainQueryResult),
  }
  const mainSession = personal ? personalMainQuery.data?.data : mainSessionQuery.data?.data
  const personalTasksQueryResult = useQuery(
    workbenchQueries.personalTasks(owner, canUseChat && personal),
  )
  const personalTasksQuery = {
    ...personalTasksQueryResult,
    data: readableQueryData(personalTasksQueryResult),
  }
  const personalEnvironmentQueryResult = useQuery(
    workbenchQueries.personalEnvironment(
      owner,
      assistantQuery.data?.data?.id,
      canUseChat && personal && panel === 'assistant' && assistantTab === 'identity',
    ),
  )
  const personalEnvironmentQuery = {
    ...personalEnvironmentQueryResult,
    data: queryAccessDenied(assistantQuery)
      ? undefined
      : readableQueryData(personalEnvironmentQueryResult),
  }
  const personalArtifactsQueryResult = useQuery(
    workbenchQueries.personalArtifacts(
      owner,
      canUseChat && personal && panel === 'assistant' && assistantTab === 'results',
    ),
  )
  const personalArtifactsQuery = {
    ...personalArtifactsQueryResult,
    data: readableQueryData(personalArtifactsQueryResult),
  }
  const environmentTemplatesQueryResult = useQuery(
    workbenchQueries.personalEnvironmentTemplates(
      owner,
      assistantQuery.data?.data?.id,
      canUseChat &&
        personal &&
        panel === 'assistant' &&
        assistantTab === 'identity' &&
        !personalEnvironmentQuery.isError &&
        personalEnvironmentQuery.data?.data?.state === 'unprovisioned',
    ),
  )
  const environmentTemplatesQuery = {
    ...environmentTemplatesQueryResult,
    data: readableQueryData(environmentTemplatesQueryResult),
  }
  const [deletionReconciled, setDeletionReconciled] = useState('')
  const environmentMutation = useMutation({
    mutationFn: (input: Parameters<typeof workbenchApi.assistant.changeEnvironment>[1]) =>
      workbenchApi.assistant.changeEnvironment(assistantQuery.data!.data!.id, input),
    retry: false,
    onSuccess: (result, input) => {
      if (input.action === 'reconcile' && result.data?.state === 'deletion_unknown') {
        setDeletionReconciled(`${result.data.assistantId}:${result.data.version}`)
      }
      if (
        input.action === 'reconcile' &&
        result.data?.assistantId === assistantQuery.data?.data?.id
      ) {
        environmentDeletion.reset()
      }
    },
    onSettled: () =>
      queryClient.invalidateQueries({ queryKey: workbenchKeys.personalEnvironmentRoot() }),
  })
  const environmentDeletion = useMutation({
    mutationFn: workbenchApi.assistant.deleteEnvironmentData,
    retry: false,
    onSettled: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: workbenchKeys.personalEnvironmentRoot() }),
        queryClient.invalidateQueries({ queryKey: workbenchKeys.assistant(owner) }),
      ])
    },
  })
  const environmentExportAbort = useRef<AbortController | null>(null)
  useEffect(
    () => () => environmentExportAbort.current?.abort(),
    [owner, assistantQuery.data?.data?.id],
  )
  const environmentExport = useMutation({
    mutationFn: (input: Parameters<typeof workbenchApi.assistant.exportEnvironmentData>[0]) => {
      const controller = new AbortController()
      environmentExportAbort.current = controller
      return workbenchApi.assistant.exportEnvironmentData(input, controller.signal)
    },
    retry: false,
    onSuccess: (blob) => {
      if (!environmentExportAbort.current?.signal.aborted) downloadBlob('personal-data.tar', blob)
    },
    onSettled: () =>
      queryClient.invalidateQueries({ queryKey: workbenchKeys.personalEnvironmentRoot() }),
  })

  const hasScopedEntry = Boolean(
    draftScope.alertId ||
    draftScope.clusterId ||
    draftScope.namespace ||
    draftScope.workload ||
    draftScope.service ||
    draftScope.pod ||
    draftScope.node,
  )
  useEffect(() => {
    if (requestedSessionId || hasScopedEntry) return
    if (isExplicitRouteMode) {
      if (visibleSessions[0]?.id) updateSearchParams({ session: visibleSessions[0].id })
    } else if (mainSession?.id) {
      updateSearchParams({ session: mainSession.id })
    }
  }, [
    requestedSessionId,
    hasScopedEntry,
    isExplicitRouteMode,
    mainSession?.id,
    visibleSessions,
    searchParams,
    setSearchParams,
  ])

  useEffect(() => {
    setDraftMode(pathMode)
  }, [pathMode])

  const patchSessionMutation = useOperationMutation(
    {
      ...workbenchMutations.sessions.patch(),
      onSuccess: async (_response, payload) => {
        await queryClient.invalidateQueries({ queryKey: workbenchKeys.sessions.all(personal) })
        await queryClient.invalidateQueries({
          queryKey: workbenchKeys.sessions.detail(payload.sessionId, owner),
        })
      },
    },
    { label: ['保存会话', 'Save session'], target: (input) => input.sessionId },
  )

  const createSessionMutation = useOperationMutation(
    {
      ...workbenchMutations.sessions.create(),
      mutationFn: personal ? workbenchApi.personalAgent.create : workbenchApi.sessions.create,
      onSuccess: async (response) => {
        await queryClient.invalidateQueries({ queryKey: workbenchKeys.sessions.all(personal) })
        navigate(getAIWorkbenchPathForSession(response.data))
        setSideOpen(false)
        setSideTitle('')
      },
    },
    { label: ['开启旁聊', 'Open side conversation'], target: (input) => input.title || '旁聊' },
  )
  const messageFeedbackMutation = useOperationMutation(
    {
      mutationFn: evaluationLifecycleApi.feedback.create,
    },
    {
      label: ['记录消息反馈', 'Record message feedback'],
      target: (input) => input.messageId || input.id,
    },
  )
  const submitMessageFeedback = (
    messageId: string,
    traceRef: string,
    disposition: 'accepted' | 'rejected',
  ) => {
    if (!currentSession) return
    modal.confirm({
      title: disposition === 'accepted' ? '记录有帮助的反馈？' : '记录需要改进的反馈？',
      content: '仅保存你可访问的任务引用和反馈结果，不复制私人对话正文。',
      okText: '提交反馈',
      cancelText: '取消',
      onOk: async () => {
        try {
          await messageFeedbackMutation.mutateAsync({
            id: crypto.randomUUID(),
            traceRef,
            sessionId: currentSession.id,
            messageId,
            disposition,
          })
        } catch {
          /* Keep the persistent failure visible outside the confirmation. */
        }
      },
    })
  }
  const branchFromMessage = (messageId: string) => {
    if (!currentSession) return
    const index = serverMessages.findIndex((item) => item.id === messageId)
    if (index < 0) return
    const messageIds = serverMessages
      .slice(0, index + 1)
      .filter((item) => item.role === 'user' || item.role === 'assistant')
      .slice(-20)
      .map((item) => item.id)
    createSessionMutation.mutate({
      title: `旁聊 · ${displayWorkbenchSessionTitle(currentSession.title)}`,
      mode: 'general',
      agentProviderId: currentSession.metadata?.agentProviderId ?? selectedAgentProviderId,
      scope: {},
      tags: sessionTagsWithPin(currentSession.metadata?.tags, false),
      pinnedContext: {
        branchReference: {
          kind: 'session',
          name: `分支背景 · ${displayWorkbenchSessionTitle(currentSession.title)}`,
          sessionId: currentSession.id,
          messageIds,
        },
      },
    })
  }
  const createSession = (payload?: { title?: string; scope?: WorkbenchSessionScope }) => {
    if (catalogQuery.isPending || createSessionMutation.blocked) return
    createSessionMutation.mutate({
      title: payload?.title?.trim() || '新的旁聊',
      mode: 'general',
      agentProviderId: currentSession?.metadata?.agentProviderId || defaultAgentProviderId,
      scope: payload?.scope || draftScope,
      tags: [],
    })
  }

  useEffect(() => {
    const scopeKey = JSON.stringify(draftScope)
    const hasScopedEntry = Boolean(
      draftScope.alertId ||
      draftScope.clusterId ||
      draftScope.namespace ||
      draftScope.workload ||
      draftScope.service ||
      draftScope.pod ||
      draftScope.node,
    )
    if (
      !hasScopedEntry ||
      requestedSessionId ||
      !canUseChat ||
      catalogQuery.isPending ||
      createSessionMutation.blocked ||
      autoSessionScopeKeyRef.current === scopeKey
    ) {
      return
    }
    const provider = agentProviders.find((item) => item.id === defaultAgentProviderId)
    if (!provider) return
    autoSessionScopeKeyRef.current = scopeKey
    createSessionMutation.mutate({
      title: draftScope.alertId
        ? `Alert ${draftScope.alertId}`
        : draftScope.workload || draftScope.service || draftScope.pod || draftScope.node
          ? `${draftScope.workload || draftScope.service || draftScope.pod || draftScope.node} 分析`
          : '新的会话',
      mode: draftMode,
      agentProviderId: provider.id,
      scope: draftScope,
      tags: [],
    })
  }, [
    agentProviders,
    canUseChat,
    catalogQuery.isPending,
    createSessionMutation,
    draftMode,
    draftScope,
    requestedSessionId,
  ])

  const deleteSessionMutation = useOperationMutation(
    {
      ...workbenchMutations.sessions.archive(),
      onSuccess: async (_response, sessionId) => {
        if (requestedSessionId === sessionId) {
          updateSearchParams({ session: undefined })
        }
        await queryClient.invalidateQueries({ queryKey: workbenchKeys.sessions.all(personal) })
      },
    },
    { label: ['归档会话', 'Archive session'], target: (id) => id },
  )

  const buildWorkbenchStreamRequest = (
    content: string,
    mode: WorkbenchMode,
    agentProviderId = selectedAgentProviderId || defaultAgentProviderId,
    taskIntent?: WorkbenchTaskIntent,
  ): WorkbenchSendMessageStreamRequest & { assistantTask?: AssistantChatTask } => {
    const requestScopeOverrides = scopeOverrideState(scopeOverrides)
    return {
      requestId: crypto.randomUUID(),
      assistantTask,
      content,
      mode,
      ...(agentProviderId === 'internal'
        ? { taskIntent: taskIntent ?? { capabilities: mode === 'general' ? [] : [mode] } }
        : {}),
      ...(mode === 'general' || agentProviderId === 'internal'
        ? {
            contextSelection,
            knowledgeContext: { enabled: knowledgeBaseIds.length > 0, knowledgeBaseIds },
          }
        : {}),
      ...(agentProviderId === 'internal' ? { modelPreferences } : {}),
      agentProviderId,
      toolset: cleanToolsetPayload({
        enabledAdapterIds: selectedAdapterIds,
        enabledSkillIds: selectedSkillIds,
        disabledToolNames: canonicalDisabledToolNames(disabledToolNames, adapters),
        budgetOverrides: numberRecord(budgetOverrides),
        scopeOverrides: requestScopeOverrides,
      }),
      scopeOverrides: requestScopeOverrides,
    }
  }

  const sendMessageMutation = useMutation({
    mutationFn: async (payload: WorkbenchStreamSubmission) => {
      const controller = payload.controller
      const seenEvents = new Set<string>()
      let streamState = createWorkbenchStreamState()
      await streamWorkbenchMessage(
        `/copilot/sessions/${payload.sessionId}/messages/stream`,
        payload.request,
        (event) => {
          const eventKey = workbenchStreamEventKey(event)
          if (seenEvents.has(eventKey)) return
          seenEvents.add(eventKey)
          streamState = reduceWorkbenchStreamState(streamState, event)
          setLocalMessages((items) =>
            items.map((item) => {
              if (item.id === payload.pendingMessages.user.id) {
                return { ...item, deliveryStatus: 'success' as WorkbenchBubbleStatus }
              }
              if (item.id !== payload.pendingMessages.assistant.id) {
                return item
              }
              return {
                ...item,
                id:
                  streamState.message.done && streamState.message.id
                    ? streamState.message.id
                    : item.id,
                content: streamFallbackContent(streamState, item.content),
                deliveryStatus: streamBubbleStatus(streamState),
                metadata: streamMessageMetadata(item.metadata, streamState),
              }
            }),
          )
          if (streamState.error) {
            throw new WorkbenchStreamEventError(streamState.error)
          }
        },
        controller.signal,
      )
      return streamState
    },
    onSuccess: async (streamState, payload) => {
      if (
        payload.request.requestId &&
        streamState.agentStatus &&
        streamState.agentStatus.providerKind !== 'native' &&
        isTerminalWorkbenchAgentStatus(streamState.agentStatus.status)
      )
        requestRecovery.acknowledge(payload.sessionId, payload.request.requestId)
      if (
        streamState.toolCalls.some(
          (call) => call.toolName === 'assistant.avatar' && call.status === 'error',
        )
      )
        updateDraft(
          (draft) => ({
            value: draft.value || payload.request.content,
            context: {
              ...draft.context,
              images: draft.context.images || payload.request.contextSelection?.images,
            },
            task:
              draft.task ||
              (payload.request as { assistantTask?: AssistantChatTask }).assistantTask,
          }),
          payload.sessionId,
        )
      if (
        streamState.toolCalls.some(
          (call) =>
            ['assistant.rename', 'assistant.avatar'].includes(call.toolName) &&
            call.status === 'success',
        )
      ) {
        await queryClient.invalidateQueries({ queryKey: workbenchKeys.assistant(owner) })
        updateDraft(
          (draft) => ({ value: draft.value || draft.heldValue || '', heldValue: undefined }),
          payload.sessionId,
        )
      }
      delete streamAbortRef.current[payload.sessionId]
      setStreamingSessions((current) => current.filter((id) => id !== payload.sessionId))
      setRetryInputs((current) => ({ ...current, [payload.sessionId]: undefined }))
      await queryClient.invalidateQueries({
        queryKey: workbenchKeys.sessions.messages(payload.sessionId, owner),
      })
      await queryClient.invalidateQueries({ queryKey: workbenchKeys.sessions.all(personal) })
      await queryClient.invalidateQueries({
        queryKey: workbenchKeys.sessions.detail(payload.sessionId, owner),
      })
      await queryClient.invalidateQueries({ queryKey: workbenchKeys.agentRuns.all() })
      await queryClient.invalidateQueries({ queryKey: systemKeys.audit.all })
      setLocalMessages((items) =>
        items.filter(
          (item) =>
            item.id !== payload.pendingMessages.user.id &&
            !(
              item.sessionId === payload.sessionId &&
              item.metadata?.source === 'agent-runtime-queued'
            ) &&
            (item.id !== payload.pendingMessages.assistant.id ||
              (!streamState.message.id && streamState.agentStatus?.providerKind !== 'native')),
        ),
      )
      if (payload.navigateMode && payload.sessionId === activeSessionRef.current) {
        navigate(
          getAIWorkbenchPathForMode(
            payload.navigateMode,
            new URLSearchParams({ session: payload.sessionId }),
          ),
        )
      }
      if (payload.closeAnalysisOnSuccess && payload.sessionId === activeSessionRef.current) {
        setAnalysisOpen(false)
      }
    },
    onError: (err: Error, payload) => {
      if (
        payload.request.agentProviderId === 'internal' &&
        payload.request.requestId &&
        err instanceof WorkbenchStreamEventError &&
        err.code === 'agent_unsupported'
      )
        requestRecovery.acknowledge(payload.sessionId, payload.request.requestId)
      if ((payload.request as { assistantTask?: AssistantChatTask }).assistantTask)
        void queryClient.invalidateQueries({ queryKey: workbenchKeys.assistant(owner) })
      delete streamAbortRef.current[payload.sessionId]
      setStreamingSessions((current) => current.filter((id) => id !== payload.sessionId))
      const aborted = err.name === 'AbortError'
      const errorMessage = aborted
        ? '已停止接收回复，后台任务停止状态待确认。'
        : workbenchStreamErrorMessage(err)
      setRetryInputs((current) => ({
        ...current,
        [payload.sessionId]:
          !aborted && isRetryableWorkbenchStreamError(err)
            ? {
                sessionId: payload.sessionId,
                request: payload.request,
                closeAnalysisOnSuccess: payload.closeAnalysisOnSuccess,
                navigateMode: payload.navigateMode,
              }
            : undefined,
      }))
      setLocalMessages((items) =>
        items.map((item) =>
          item.id === payload.pendingMessages.user.id
            ? {
                ...item,
                deliveryStatus: aborted ? 'abort' : 'error',
                metadata: {
                  ...(item.metadata ?? {}),
                  error: errorMessage,
                  errorRetryable: isRetryableWorkbenchStreamError(err),
                },
              }
            : item.id === payload.pendingMessages.assistant.id
              ? {
                  ...item,
                  content: errorMessage,
                  deliveryStatus: aborted ? 'abort' : 'error',
                  metadata: {
                    ...(item.metadata ?? {}),
                    error: errorMessage,
                    errorCode: err instanceof WorkbenchStreamEventError ? err.code : undefined,
                    errorRetryable: isRetryableWorkbenchStreamError(err),
                  },
                }
              : item,
        ),
      )
      if (!aborted || (payload.request as { assistantTask?: AssistantChatTask }).assistantTask) {
        updateDraft(
          (draft) => ({
            value: draft.value || payload.request.content,
            context: {
              ...draft.context,
              images: draft.context.images || payload.request.contextSelection?.images,
            },
            task:
              draft.task ||
              (payload.request as { assistantTask?: AssistantChatTask }).assistantTask,
          }),
          payload.sessionId,
        )
        if (!aborted) void message.error(errorMessage)
      }
    },
  })

  const submitWorkbenchStream = (input: WorkbenchStreamRetryInput) => {
    if (streamAbortRef.current[input.sessionId]) return
    if (
      input.sessionId === requestedSessionId &&
      (requestRecovery.error || !requestRecovery.canRetry(input.request.requestId))
    )
      return
    if (
      input.request.agentProviderId === 'internal' &&
      !input.request.assistantTask &&
      input.request.requestId
    ) {
      try {
        requestRecovery.remember(input.sessionId, input.request.requestId)
      } catch (error) {
        void message.error(error instanceof Error ? error.message : '无法保存任务恢复标识')
        return
      }
    }
    const controller = new AbortController()
    streamAbortRef.current[input.sessionId] = controller
    setStreamingSessions((current) => [...current, input.sessionId])
    setRetryInputs((current) => ({ ...current, [input.sessionId]: undefined }))
    const pendingMessages = pendingConversationMessages(input.sessionId, input.request.content)
    if (input.request.contextSelection?.images?.length)
      pendingMessages.user.metadata = {
        ...pendingMessages.user.metadata,
        images: input.request.contextSelection.images,
      }
    const appearance = (input.request as { assistantTask?: AssistantChatTask }).assistantTask
    if (appearance?.image)
      pendingMessages.user.metadata = {
        ...pendingMessages.user.metadata,
        assistantImage: appearance.image,
      }
    setLocalMessages((items) => [...items, pendingMessages.user, pendingMessages.assistant])
    sendMessageMutation.mutate({ ...input, pendingMessages, controller })
  }

  const submitMessage = (value: string) => {
    const content = value.trim()
    if (
      !content ||
      !canUseChat ||
      !currentSession ||
      !requestedSessionId ||
      currentStreamPending ||
      requestRecovery.pending ||
      requestRecovery.error ||
      runningAgentRuns.length > 0
    ) {
      return
    }
    const reason = agentUnavailableReason(
      agentProviders.find((item) => item.id === selectedAgentProviderId),
      pathMode,
    )
    if (reason) {
      void message.warning(reason)
      return
    }
    updateSearchParams({ message: undefined, q: undefined })
    updateDraft({
      value: '',
      task: undefined,
      context: { ...contextSelection, images: undefined, feedItemId: undefined },
    })
    senderRef.current?.clear()
    setSenderResetVersion((version) => version + 1)
    submitWorkbenchStream({
      sessionId: requestedSessionId,
      request: buildWorkbenchStreamRequest(content, pathMode),
    })
  }

  const cancelAgentRunsMutation = useMutation({
    mutationFn: async (sessionId: string) => {
      const response = await workbenchApi.agentRuns.session(sessionId)
      await Promise.all(
        response.data.filter(isRunningAgentRun).map((run) => workbenchApi.agentRuns.cancel(run.id)),
      )
    },
    onSuccess: async (_, sessionId) => {
      await queryClient.invalidateQueries({ queryKey: workbenchKeys.agentRuns.all() })
      await queryClient.invalidateQueries({
        queryKey: workbenchKeys.sessions.messages(sessionId, owner),
      })
    },
  })
  const cancelMessageStream = () => {
    const sessionId = requestedSessionId
    if (!sessionId) return
    modal.confirm({
      title: '停止当前任务？',
      content: '将终止当前回答及其专业子任务。已提交的审批申请保持原状态。',
      okText: '停止任务',
      cancelText: '返回',
      onOk: () => {
        try {
          requestRecovery.cancel(sessionId)
        } catch (error) {
          void message.error(
            error instanceof Error ? error.message : '取消意图保存失败，请保持页面打开并重试',
          )
        }
        streamAbortRef.current[sessionId]?.abort()
        cancelAgentRunsMutation.mutate(sessionId)
      },
    })
  }

  const createInspectionFromSessionMutation = useOperationMutation(
    {
      ...workbenchMutations.sessions.createInspectionTask(),
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: observeKeys.operations.tasks() })
        navigate(getAIOperationsPath(location.search))
      },
    },
    { label: ['生成巡检任务', 'Create inspection task'], target: (input) => input.sessionId },
  )
  const createInspectionFromSession = () => {
    if (!requestedSessionId) return
    createInspectionFromSessionMutation.mutate({
      sessionId: requestedSessionId,
      body: {
        title: `${currentSessionTitle || '会话'} 巡检模板`,
        scopeType: currentSession?.metadata?.scope?.namespace
          ? 'namespace'
          : currentSession?.metadata?.scope?.clusterId
            ? 'cluster'
            : 'platform',
        clusterId: currentSession?.metadata?.scope?.clusterId,
        namespace: currentSession?.metadata?.scope?.namespace,
        checks: ['cluster_health', 'alert_pressure', 'audit_denials'],
        enabled: true,
        intervalMinutes: 30,
        metadata: {
          ...(defaultInspectionProfileId ? { analysisProfileId: defaultInspectionProfileId } : {}),
        },
      },
    })
  }

  useEffect(() => {
    setContextParsing(false)
  }, [requestedSessionId])

  useEffect(() => {
    setSelectedSkillIds(currentSession?.metadata?.toolset?.enabledSkillIds ?? [])
    setSelectedAdapterIds(currentSession?.metadata?.toolset?.enabledAdapterIds ?? [])
    setDisabledToolNames(
      canonicalDisabledToolNames(
        currentSession?.metadata?.toolset?.disabledToolNames ?? [],
        adapters,
      ),
    )
    setBudgetOverrides(numberRecord(currentSession?.metadata?.toolset?.budgetOverrides))
    setScopeOverrides(scopeOverrideState(currentSession?.metadata?.toolset?.scopeOverrides))
  }, [
    adapters,
    currentSession?.id,
    currentSession?.metadata?.toolset?.enabledSkillIds,
    currentSession?.metadata?.toolset?.enabledAdapterIds,
    currentSession?.metadata?.toolset?.disabledToolNames,
    currentSession?.metadata?.toolset?.budgetOverrides,
    currentSession?.metadata?.toolset?.scopeOverrides,
  ])

  useEffect(() => {
    setSelectedAgentProviderId(
      currentSession
        ? currentSession.metadata?.agentProviderId || 'internal'
        : defaultAgentProviderId,
    )
  }, [currentSession?.id, currentSession?.metadata?.agentProviderId, defaultAgentProviderId])

  const artifactEntries = useMemo<WorkbenchArtifactEntry[]>(() => {
    const entries: WorkbenchArtifactEntry[] = []
    for (const item of [...messages].reverse()) {
      if (item.role !== 'assistant') continue
      const artifacts = replayArtifactsForMessage(item)
      artifacts.forEach((artifact, index) => {
        entries.push({
          key: `${item.id}:${artifact.runId || artifact.kind}:${index}`,
          artifact,
          message: item,
          index,
        })
      })
    }
    return entries
  }, [messages])

  const artifactsByMessage = useMemo(() => {
    const grouped = new Map<string, WorkbenchArtifactEntry[]>()
    for (const entry of artifactEntries) {
      const entries = grouped.get(entry.message.id) ?? []
      entries.push(entry)
      grouped.set(entry.message.id, entries)
    }
    return grouped
  }, [artifactEntries])

  useEffect(() => {
    if (artifactEntries.length === 0) {
      if (selectedArtifactKey) setSelectedArtifactKey(undefined)
      return
    }
    if (!selectedArtifactKey || !artifactEntries.some((item) => item.key === selectedArtifactKey)) {
      setSelectedArtifactKey(artifactEntries[0].key)
    }
  }, [artifactEntries, selectedArtifactKey])

  const activeArtifactEntry =
    personalArtifactEntry ??
    artifactEntries.find((item) => item.key === selectedArtifactKey) ??
    artifactEntries[0]
  const activeArtifact = activeArtifactEntry?.artifact
  const activeArtifactLinks = activeArtifactEntry
    ? artifactContextLinks(
        activeArtifactEntry,
        personalArtifactEntry
          ? visibleSessions.find((item) => item.id === personalArtifactEntry.message.sessionId)
          : currentSession,
      )
    : []
  const chainArtifactEntry = activeArtifactEntry
  const toolCalls = chainArtifactEntry?.artifact.toolExecutions ?? []
  const chainThinkingSummary =
    metadataThinkingSummary(chainArtifactEntry?.message.metadata) ||
    chainArtifactEntry?.artifact.summary ||
    ''
  const chainAgentStatus = metadataAgentStatus(chainArtifactEntry?.message.metadata)
  const chainToolCallSummary = toolCallSummaryText(toolCalls)
  const activeGraph = activeArtifact?.graph
  const activeMode = pathMode
  const latestAssistantMessage = [...messages].reverse().find((item) => item.role === 'assistant')
  const promptItems = CHAT_STARTERS.map((item) => ({
    key: item.value,
    label: item.label,
    description: item.description,
    icon: <span className="soha-ai-workbench__starter-icon">{item.icon}</span>,
  }))
  const conversationItems = visibleSessions
    .filter((item) => !['main', 'personal_main'].includes(item.metadata?.conversationKind || ''))
    .sort(
      (left, right) =>
        Number(right.metadata?.tags?.includes(PINNED_SESSION_TAG) ?? false) -
        Number(left.metadata?.tags?.includes(PINNED_SESSION_TAG) ?? false),
    )

    .map((item) => {
      const title = displayWorkbenchSessionTitle(item.title)
      const scopeSummary = buildScopeSummary(item.metadata?.scope)
      const timeText = formatSessionTimestamp(item.updatedAt)
      const isArchiving =
        deleteSessionMutation.isPending && deleteSessionMutation.variables === item.id
      return {
        key: item.id,
        icon: <CommentOutlined />,
        label: (
          <div
            className="soha-ai-workbench__conversation-label"
            title={`${title} · ${timeText} · ${scopeSummary}`}
          >
            <span className="soha-ai-workbench__conversation-label-main">
              <span className="soha-ai-workbench__conversation-label-title">
                {item.metadata?.tags?.includes(PINNED_SESSION_TAG) ? '置顶 · ' : ''}
                {title}
              </span>
              {item.activity ? (
                <Tag color={item.activity === 'waiting_approval' ? 'warning' : 'processing'}>
                  {item.activity === 'waiting_approval'
                    ? '待审批'
                    : item.activity === 'queued'
                      ? '排队中'
                      : '运行中'}
                </Tag>
              ) : null}
              <span className="soha-ai-workbench__conversation-label-meta">{timeText}</span>
            </span>
            <span className="soha-ai-workbench__conversation-label-actions">
              <Button
                aria-label={`重命名 ${title}`}
                className="soha-ai-workbench__conversation-action"
                icon={<EditOutlined />}
                size="small"
                type="text"
                onClick={(event) => {
                  event.preventDefault()
                  event.stopPropagation()
                  setRenameTargetId(item.id)
                  setRenameValue(title)
                  setRenamePinned(item.metadata?.tags?.includes(PINNED_SESSION_TAG) ?? false)
                  setRenameOpen(true)
                }}
              />
              <span onClick={(event) => event.stopPropagation()}>
                <Popconfirm
                  title="确认归档此会话？"
                  description="归档后会话将从当前工作台列表移除。"
                  okText="归档"
                  cancelText="取消"
                  okButtonProps={{ danger: true, loading: isArchiving }}
                  onConfirm={() => deleteSessionMutation.mutate(item.id)}
                >
                  <Button
                    aria-label={`归档 ${title}`}
                    className="soha-ai-workbench__conversation-action soha-ai-workbench__conversation-action--danger"
                    danger
                    disabled={deleteSessionMutation.blocked}
                    icon={<DeleteOutlined />}
                    loading={isArchiving}
                    size="small"
                    type="text"
                  />
                </Popconfirm>
              </span>
            </span>
          </div>
        ),
      }
    })
  const enabledDataSources = dataSources.filter((item) => item.enabled)
  const enabledAgentProviders = agentProviders.filter((item) => item.enabled)
  const providerOptions = enabledAgentProviders.map((item) => ({
    value: item.id,
    label: `${item.name}${item.supportsAsync ? ' / async' : ' / inline'}`,
  }))
  const analysisProfileOptions = analysisProfiles
    .filter((item) => item.enabled && item.mode === 'root_cause')
    .map((item) => ({ value: item.id, label: item.name }))
  const selectedAnalysisProfile = analysisProfiles.find(
    (item) => item.id === analysisIntent.rootCauseProfileId,
  )
  const activeAgentProvider =
    agentProviders.find((item) => item.id === selectedAgentProviderId) ??
    agentProviders.find((item) => item.id === currentSession?.metadata?.agentProviderId) ??
    agentProviders.find((item) => item.id === defaultAgentProviderId)
  const activeCapability = agentCapabilities.find(
    (item) => (item.analysisKinds ?? []).includes(activeMode) || item.id === activeMode,
  )
  const disabledToolOptions = useMemo(() => buildDisabledToolOptions(adapters), [adapters])
  const cleanedBudgetOverrides = useMemo(() => numberRecord(budgetOverrides), [budgetOverrides])
  const cleanedScopeOverrides = useMemo(() => scopeOverrideState(scopeOverrides), [scopeOverrides])
  const activeDataSourceAdapters = [
    ...new Set(enabledDataSources.map((item) => item.mcpAdapter).filter(Boolean)),
  ]
  const unavailableSelectedAdapterIds = selectedAdapterIds.filter(
    (adapterId) =>
      adapterId !== 'platform-native.v1' && !activeDataSourceAdapters.includes(adapterId),
  )
  const toolsetPolicySummary = [
    {
      label: 'Agent Provider',
      value: activeAgentProvider?.name || selectedAgentProviderId || 'Auto',
      detail:
        activeAgentProvider?.description ||
        '会话级 provider 决定显式分析由内置分析还是外部 agent runner 执行。',
    },
    {
      label: 'Capability',
      value: activeCapability?.name || activeMode,
      detail: activeCapability?.toolRefs?.length
        ? activeCapability.toolRefs.join(', ')
        : '按当前分析模式自动选择 capability 与工具绑定。',
    },
    {
      label: 'Adapter',
      value: selectedAdapterIds.length > 0 ? `${selectedAdapterIds.length} selected` : 'Auto',
      detail:
        selectedAdapterIds.length > 0
          ? selectedAdapterIds.join(', ')
          : '默认允许已注册 adapter，运行时按数据源可用性选择。',
    },
    {
      label: 'Disabled Tools',
      value: disabledToolNames.length,
      detail: disabledToolNames.length > 0 ? disabledToolNames.join(', ') : '没有屏蔽具体工具。',
    },
    {
      label: 'Budget',
      value: countObjectKeys(cleanedBudgetOverrides),
      detail:
        countObjectKeys(cleanedBudgetOverrides) > 0
          ? Object.entries(cleanedBudgetOverrides)
              .map(([key, value]) => `${key}=${value}`)
              .join(', ')
          : '沿用 profile 和数据源默认预算。',
    },
    {
      label: 'Scope Override',
      value: countObjectKeys(cleanedScopeOverrides as Record<string, unknown>),
      detail:
        countObjectKeys(cleanedScopeOverrides as Record<string, unknown>) > 0
          ? buildScopeSummary(cleanedScopeOverrides)
          : '沿用会话固定范围。',
    },
  ]
  const canRunExplicitAnalysis = canUseChat && (canRunRootCause || canViewMonitoring || canViewAI)
  const explicitAnalysisTitle = canRunExplicitAnalysis
    ? undefined
    : !canUseChat
      ? '缺少 observe.ai.chat 权限'
      : '缺少专业分析权限'
  const createInspectionTitle = canCreateInspectionTask
    ? undefined
    : !canUseChat
      ? '缺少 observe.ai.chat 权限'
      : '缺少 observe.ai.inspection.create 权限'
  const selectedGraphNode = useMemo(
    () => activeGraph?.nodes?.find((item) => item.id === selectedGraphNodeId) ?? null,
    [activeGraph?.nodes, selectedGraphNodeId],
  )
  const graphFitKey = useMemo(
    () =>
      `${requestedSessionId}::${activeGraph?.nodes?.map((item) => item.id).join(',') || ''}::${activeGraph?.edges?.map((item) => item.id).join(',') || ''}`,
    [activeGraph?.edges, activeGraph?.nodes, requestedSessionId],
  )

  useEffect(() => {
    const nodes = activeGraph?.nodes
    if (!nodes?.length) {
      setSelectedGraphNodeId(null)
      return
    }
    const changedScope = selectedGraphScope.current !== (requestedSessionId ?? '')
    selectedGraphScope.current = requestedSessionId ?? ''
    setSelectedGraphNodeId((current) =>
      !changedScope && (current === null || nodes.some((node) => node.id === current))
        ? current
        : (activeGraph?.focusNodeId ?? nodes[0]?.id ?? null),
    )
  }, [activeGraph?.focusNodeId, activeGraph?.nodes, requestedSessionId])

  const hasAnalysisParameters = Object.values(analysisIntent).some(
    (value) => value !== undefined && value !== '',
  )
  const analysisUnavailableReason = taskAgentUnavailableReason(
    agentProviders.find((item) => item.id === analysisAgentProviderId),
    analysisCapabilities,
    hasAnalysisParameters,
  )
  const nativeAnalysis = analysisAgentProviderId === 'internal'
  const analysisCapabilityAllowed = (kind: WorkbenchMode) =>
    (kind === 'root_cause'
      ? canRunRootCause
      : kind === 'inspection_review'
        ? canViewAI
        : canViewMonitoring) &&
    (!nativeAnalysis || canInvokeGateway)
  const analysisSources = (kind: string) =>
    enabledDataSources.filter(
      (source) =>
        source.sourceKind === kind &&
        source.mcpAdapter === `${kind}.v1` &&
        (!currentSession?.metadata?.toolset?.enabledAdapterIds?.length ||
          currentSession.metadata.toolset.enabledAdapterIds.includes(source.mcpAdapter)) &&
        (!selectedAdapterIds.length || selectedAdapterIds.includes(source.mcpAdapter)),
    )
  const performanceSources = analysisSources('metrics')
  const traceSources = analysisSources('traces')
  const completedInspectionRuns = (inspectionRunsQuery.data ?? []).filter(
    (run) => run.completedAt && run.status !== 'running',
  )
  const analysisInputReason = !nativeAnalysis
    ? ''
    : analysisCapabilities.includes('performance') &&
        (!analysisIntent.metricKey ||
          !(metricCatalogQuery.data ?? []).some(
            (metric) => metric.key === analysisIntent.metricKey && metric.available,
          ) ||
          !performanceSources.some(
            (source) => source.id === analysisIntent.performanceDataSourceId,
          ))
      ? '请选择可用指标和指标数据源。'
      : analysisCapabilities.includes('trace') &&
          !traceSources.some((source) => source.id === analysisIntent.traceDataSourceId)
        ? '请选择链路数据源。'
        : analysisCapabilities.includes('inspection_review') &&
            (!analysisIntent.inspectionRunIds?.length ||
              !analysisIntent.inspectionRunIds.every((id) =>
                completedInspectionRuns.some((run) => run.id === id),
              ))
          ? '请选择已完成的真实巡检记录。'
          : ''
  const canSubmitExplicitAnalysis =
    canUseChat &&
    analysisCapabilities.length > 0 &&
    analysisCapabilities.every(analysisCapabilityAllowed) &&
    !analysisUnavailableReason &&
    !analysisInputReason
  const handleSessionChange = (sessionId: string) => {
    setPersonalArtifactEntry(undefined)
    const selected = visibleSessions.find((item) => item.id === sessionId)
    if (selected) {
      const params = new URLSearchParams(searchParams)
      for (const key of ['view', 'message', 'q']) params.delete(key)
      navigate(getAIWorkbenchPathForSession(selected, params))
      return
    }
    updateSearchParams({ session: sessionId, view: undefined, message: undefined, q: undefined })
  }
  const openArtifactLink = (link: ArtifactContextLink) => {
    navigate(link.path)
    if (link.kind === 'inspection') {
      return
    }
    if (link.kind === 'root_cause' || link.kind === 'agent') {
      setPanel('thinking')
    }
  }
  const openExplicitAnalysis = () => {
    if (!currentSession || !canRunExplicitAnalysis) return
    const nextMode = activeMode
    const runnableMode =
      RUNNABLE_ANALYSIS_MODE_OPTIONS.some((item) => item.value === nextMode) &&
      analysisCapabilityAllowed(nextMode)
        ? nextMode
        : canRunRootCause
          ? 'root_cause'
          : canViewMonitoring
            ? 'performance'
            : 'inspection_review'
    setAnalysisCapabilities([runnableMode as TaskCapability])
    setAnalysisIntent({})
    setAnalysisAgentProviderId(selectedAgentProviderId || defaultAgentProviderId)
    setAnalysisQuestion(defaultAnalysisQuestion(runnableMode))
    setPanel(null)
    setAnalysisOpen(true)
  }
  const submitExplicitAnalysis = () => {
    if (!currentSession || !canSubmitExplicitAnalysis || currentStreamPending) return
    const question = analysisQuestion.trim() || defaultAnalysisQuestion(analysisMode)
    submitWorkbenchStream({
      sessionId: currentSession.id,
      request: buildWorkbenchStreamRequest(question, analysisMode, analysisAgentProviderId, {
        capabilities: analysisCapabilities,
        ...(analysisCapabilities.includes('root_cause') && analysisIntent.rootCauseProfileId
          ? { rootCauseProfileId: analysisIntent.rootCauseProfileId }
          : {}),
        ...(analysisCapabilities.includes('performance')
          ? {
              performanceDataSourceId: analysisIntent.performanceDataSourceId,
              metricKey: analysisIntent.metricKey,
            }
          : {}),
        ...(analysisCapabilities.includes('trace')
          ? {
              traceDataSourceId: analysisIntent.traceDataSourceId,
              traceId: analysisIntent.traceId,
              minDurationMs: analysisIntent.minDurationMs ?? 0,
            }
          : {}),
        ...(analysisCapabilities.includes('inspection_review')
          ? { inspectionRunIds: analysisIntent.inspectionRunIds }
          : {}),
      }),
      closeAnalysisOnSuccess: true,
      navigateMode: isExplicitRouteMode ? analysisMode : undefined,
    })
  }
  const openInspector = (view: InspectorView) => {
    setInspectorView(view)
    setPanel('inspector')
  }
  const setBudgetOverrideValue = (key: string, value: number | string | null) => {
    setBudgetOverrides((current) => {
      const next = { ...current }
      const numberValue = Number(value)
      if (Number.isFinite(numberValue) && numberValue > 0) {
        next[key] = numberValue
      } else {
        delete next[key]
      }
      return next
    })
  }
  const setScopeOverrideValue = (key: keyof WorkbenchSessionScope, value: string) => {
    setScopeOverrides((current) => {
      const next = { ...current }
      const trimmed = value.trim()
      if (trimmed) {
        next[key] = trimmed as never
      } else {
        delete next[key]
      }
      return next
    })
  }
  const setScopeOverrideNumberValue = (
    key: keyof WorkbenchSessionScope,
    value: number | string | null,
  ) => {
    setScopeOverrides((current) => {
      const next = { ...current }
      const numberValue = Number(value)
      if (Number.isFinite(numberValue) && numberValue > 0) {
        next[key] = numberValue as never
      } else {
        delete next[key]
      }
      return next
    })
  }
  const saveToolset = () => {
    if (!currentSession) return
    const payload = cleanToolsetPayload({
      enabledAdapterIds: selectedAdapterIds,
      enabledSkillIds: selectedSkillIds,
      disabledToolNames: canonicalDisabledToolNames(disabledToolNames, adapters),
      budgetOverrides: cleanedBudgetOverrides,
      scopeOverrides: cleanedScopeOverrides,
    })
    patchSessionMutation.mutate(
      {
        sessionId: currentSession.id,
        body: {
          toolset: payload,
          agentProviderId: selectedAgentProviderId || defaultAgentProviderId,
        },
      },
      {},
    )
  }
  const applyRecommendedToolset = () => {
    setSelectedAdapterIds(recommendedAdapterIds(adapters, dataSources))
    setSelectedSkillIds(globalSkills.filter((item) => item.enabled).map((item) => item.id))
    setDisabledToolNames([])
    setBudgetOverrides({ timeoutSeconds: 60, maxEvidenceItems: 20 })
    setScopeOverrides({})
  }
  const clearToolset = () => {
    setSelectedAdapterIds([])
    setSelectedSkillIds([])
    setDisabledToolNames([])
    setBudgetOverrides({})
    setScopeOverrides({})
  }

  const latestContextMessage = [...serverMessages].reverse().find((item) => item.role !== 'user')
  const usedContextSummary = contextSnapshotSummary(
    latestContextMessage?.metadata?.contextSnapshot ??
      latestContextMessage?.metadata?.contextEnvelope,
  )
  const contextUsage = latestContextMessage?.metadata?.contextUsage
  const nextContextSummary = [
    {
      label: '已选背景',
      value:
        [
          ...(contextSelection.memoryIds ?? []).map((id) => `记忆 ${id}`),
          ...knowledgeBaseIds.map((id) => `知识库 ${id}`),
          ...(contextSelection.references ?? []).map((item) => item.name),
          ...(contextSelection.attachments ?? []).map((item) => item.name),
        ].join('、') || '无',
    },
    {
      label: '资源范围',
      value: buildScopeSummary(currentSession?.metadata?.scope) || '未限定',
    },
    { label: '可用历史消息', value: `${serverMessages.length} 条` },
    {
      label: '已选工具适配器',
      value: selectedAdapterIds.length ? selectedAdapterIds.join('、') : '默认',
    },
    {
      label: '已选技能',
      value: selectedSkillIds.length ? selectedSkillIds.join('、') : '默认',
    },
    {
      label: '固定背景',
      value: Object.keys(currentSession?.metadata?.pinnedContext ?? {}).join('、') || '无',
    },
  ]

  const renderInspectorBody = () => {
    if (inspectorView === 'evidence') {
      const sources = sourceItemsForArtifactEntry(activeArtifactEntry)
      return activeArtifact ? (
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          {(activeArtifact.evidence ?? []).length === 0 && sources.length === 0 ? (
            <ManagementState
              bordered={false}
              compact
              title="暂无证据"
              description="当前分析工件还没有结构化证据。"
            />
          ) : (
            <>
              {sources.length > 0 ? <Sources title="证据来源" items={sources} /> : null}
              {(activeArtifact.evidence ?? []).map((item) => (
                <Card key={item.id} size="small">
                  <Flex justify="space-between">
                    <Text strong>{item.title}</Text>
                    {item.severity ? <StatusTag value={item.severity} /> : null}
                  </Flex>
                  <Paragraph type="secondary" style={{ margin: '8px 0 0' }}>
                    {item.summary}
                  </Paragraph>
                </Card>
              ))}
            </>
          )}
        </Space>
      ) : (
        <ManagementState
          bordered={false}
          compact
          title="暂无分析工件"
          description="会话产生分析结果后这里会展示证据。"
        />
      )
    }

    if (inspectorView === 'hypotheses') {
      return activeArtifact ? (
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          {(activeArtifact.hypotheses ?? []).length === 0 ? (
            <ManagementState
              bordered={false}
              compact
              title="暂无假设"
              description="当前分析工件还没有形成候选根因。"
            />
          ) : (
            (activeArtifact.hypotheses ?? []).map((item) => (
              <Card key={item.id} size="small">
                <Flex justify="space-between">
                  <Text strong>{item.title}</Text>
                  <Tag color="gold">{item.confidence}%</Tag>
                </Flex>
                <Paragraph type="secondary" style={{ margin: '8px 0 0' }}>
                  {item.summary}
                </Paragraph>
              </Card>
            ))
          )}
        </Space>
      ) : (
        <ManagementState
          bordered={false}
          compact
          title="暂无假设"
          description="选择一个分析工件后查看候选根因。"
        />
      )
    }

    return activeArtifact ? (
      <Space orientation="vertical" size={8} style={{ width: '100%' }}>
        {(activeArtifact.recommendations ?? []).length === 0 ? (
          <ManagementState
            bordered={false}
            compact
            title="暂无建议动作"
            description="当前分析工件还没有生成下一步操作。"
          />
        ) : (
          (activeArtifact.recommendations ?? []).map((item) => (
            <Card key={item} size="small">
              <Paragraph style={{ marginBottom: 0 }}>{item}</Paragraph>
            </Card>
          ))
        )}
      </Space>
    ) : (
      <ManagementState
        bordered={false}
        compact
        title="暂无建议"
        description="选择一个分析工件后查看建议动作。"
      />
    )
  }

  if (conversationDenied)
    return (
      <>
        <ReadFeedback query={sessionDetailQuery} label="当前会话" />
        <ReadFeedback query={messagesQuery} label="会话消息" />
        <ReadFeedback query={messageWindowQuery} label="消息窗口" />
        <ReadFeedback query={agentRunsQuery} label="任务运行记录" />
        <ReadFeedback
          query={{
            ...nativeEventPages,
            data: nativeEventPages.pages.length ? nativeEventPages.pages : undefined,
          }}
          label="任务事件"
        />
        <ManagementState
          title="当前会话内容不可访问"
          description="已隐藏保护内容。恢复访问后再打开会话。"
        />
      </>
    )

  if (
    currentSession &&
    personal !== Boolean(currentSession.metadata?.conversationKind?.startsWith('personal_'))
  ) {
    return (
      <ManagementState
        title="此会话属于另一个入口"
        description="普通聊天与 Heya 个人会话分别管理。"
        actions={
          <Button onClick={() => navigate(getAIWorkbenchPathForSession(currentSession))}>
            打开所属会话
          </Button>
        }
      />
    )
  }

  return (
    <>
      <h1 className="soha-ai-sr-only">
        {personal ? assistantQuery.data?.data?.name || 'heya' : 'AI 工作台'} ·{' '}
        {currentSessionTitle || '主要聊天'}
      </h1>
      <WorkbenchShell
        personal={personal}
        onSearch={() => setSearchOpen(true)}
        chatActive={!feedView}
        onChat={() => updateSearchParams({ view: undefined })}
        panelOpen={panel !== null}
        assistantPanelOpen={panel === 'assistant'}
        mobileTab={
          feedView
            ? 'feed'
            : panel === 'assistant'
              ? assistantTab === 'activity'
                ? 'tasks'
                : assistantTab === 'results'
                  ? 'results'
                  : 'me'
              : 'chat'
        }
        onMobileTab={
          personal
            ? (value) => {
                if (value === 'chat' || value === 'feed') {
                  setPanel(null)
                  updateSearchParams({ view: value === 'feed' ? 'feed' : undefined })
                } else {
                  setAssistantTab(
                    value === 'tasks' ? 'activity' : value === 'results' ? 'results' : 'identity',
                  )
                  setPanel('assistant')
                }
              }
            : undefined
        }
        railActions={
          personal ? (
            <>
              <Tooltip title="动态" placement="right">
                <Button
                  type="text"
                  aria-label="动态"
                  className="soha-ai-rail-action soha-ai-icon-trigger"
                  aria-pressed={feedView}
                  icon={<AnimatedAssistantIcon name="tasks" />}
                  onClick={() => {
                    setPanel(null)
                    updateSearchParams({ view: 'feed' })
                  }}
                />
              </Tooltip>
              <Tooltip title="成果" placement="right">
                <Button
                  type="text"
                  aria-label="成果"
                  className="soha-ai-rail-action soha-ai-icon-trigger"
                  aria-pressed={panel === 'assistant' && assistantTab === 'results'}
                  icon={<AnimatedAssistantIcon name="results" />}
                  onClick={() => {
                    setAssistantTab('results')
                    setPanel('assistant')
                  }}
                />
              </Tooltip>
              <Tooltip title="即将到来" placement="right">
                <Button
                  type="text"
                  aria-label="即将到来"
                  className="soha-ai-rail-action soha-ai-icon-trigger"
                  aria-pressed={panel === 'assistant' && assistantTab === 'upcoming'}
                  icon={<AnimatedAssistantIcon name="calendar" />}
                  onClick={() => {
                    setAssistantTab('upcoming')
                    setPanel('assistant')
                  }}
                />
              </Tooltip>
              <Tooltip title="助手身份" placement="right">
                <Button
                  type="text"
                  aria-label="助手身份"
                  className="soha-ai-rail-action soha-ai-icon-trigger"
                  aria-pressed={panel === 'assistant' && assistantTab === 'identity'}
                  icon={<AnimatedAssistantIcon name="identity" />}
                  onClick={() => {
                    setAssistantTab('identity')
                    setPanel('assistant')
                  }}
                />
              </Tooltip>
            </>
          ) : undefined
        }
        onClosePanel={() => setPanel(null)}
        sidebar={
          <aside className="soha-ai-workbench-sidebar">
            <Button
              type="text"
              block
              icon={<AnimatedAssistantIcon name="search" />}
              onClick={() => setSearchOpen(true)}
            >
              搜索对话
            </Button>
            <Button
              aria-label="主要聊天"
              className={`soha-ai-main-chat soha-ai-icon-trigger${isMainChat ? ' is-active' : ''}`}
              type="text"
              icon={<AnimatedAssistantIcon name="chat" />}
              disabled={!mainSession}
              onClick={() => mainSession && navigate(getAIWorkbenchPathForSession(mainSession))}
            >
              主要聊天
            </Button>
            <div className="soha-ai-side-heading">
              <Text type="secondary">旁聊</Text>
              <Button
                type="text"
                size="small"
                aria-label="新增旁聊"
                icon={<PlusOutlined />}
                disabled={!canUseChat || createSessionMutation.blocked}
                onClick={() => setSideOpen(true)}
              />
            </div>

            <Conversations
              items={conversationItems}
              activeKey={currentSession?.id}
              onActiveChange={(value) => handleSessionChange(String(value))}
              className="soha-ai-workbench__conversations"
            />
          </aside>
        }
        railFooter={
          <Tooltip title="会话设置" placement="right">
            <Button
              type="text"
              className="soha-ai-rail-action soha-ai-icon-trigger"
              aria-label="会话设置"
              aria-pressed={panel === 'settings'}
              icon={<AnimatedAssistantIcon name="settings" />}
              onClick={() => setPanel('settings')}
            />
          </Tooltip>
        }
        alerts={
          <>
            <ReadFeedback query={sessionDetailQuery} label="当前会话" />
            <ReadFeedback query={messagesQuery} label="会话消息" />
            <ReadFeedback query={messageWindowQuery} label="消息窗口" />
            <ReadFeedback query={agentRunsQuery} label="任务运行记录" />
            <ReadFeedback
              query={{
                ...nativeEventPages,
                data: nativeEventPages.pages.length ? nativeEventPages.pages : undefined,
              }}
              label="任务事件"
            />
            <ReadFeedback query={sessionsQuery} label="会话目录" />
            <ReadFeedback query={catalogQuery} label="模型目录" />
            <ReadFeedback query={feedReferenceQuery} label="动态引用" />
            {!renameOpen && panel !== 'toolset' ? patchSessionMutation.feedback : null}
            {deleteSessionMutation.feedback}
            {createInspectionFromSessionMutation.feedback}
            {messageFeedbackMutation.feedback}
            {!sideOpen ? createSessionMutation.feedback : null}
            {windowMutation.feedback}
            <MutationFeedback
              operations={[
                {
                  mutation: cancelAgentRunsMutation,
                  label: '停止任务',
                  target: cancelAgentRunsMutation.variables,
                  receipt: { message: '取消请求已返回，请核对任务终态。' },
                },
              ]}
            />
            {requestRecovery.pending && !currentStreamPending ? (
              <Alert
                type="warning"
                title={
                  requestRecovery.cancelling
                    ? '正在停止任务，等待后台确认'
                    : runningAgentRuns.length > 0
                      ? '后台任务正在执行'
                      : '任务提交结果未确认'
                }
                description={
                  requestRecovery.cancelling
                    ? '正在查回原任务并请求停止。请保持页面打开；关闭后将在返回时继续确认，未确认前后台任务可能仍在运行。'
                    : '正在读取原任务状态。关闭页面不会停止已经接收的任务。'
                }
                action={<Button onClick={requestRecovery.recheck}>重新读取</Button>}
              />
            ) : null}
            {requestRecovery.error ? (
              <Alert
                type="error"
                title={requestRecovery.error}
                action={<Button onClick={cancelMessageStream}>重试停止</Button>}
              />
            ) : null}
            {!canUseChat ? (
              <Alert
                type="warning"
                showIcon
                title="当前账号缺少 observe.ai.chat 权限，无法发送消息或创建会话。"
              />
            ) : null}

            {lastRetryableInput ? (
              <Alert
                type="warning"
                showIcon
                title="上一次 Workbench 流式调用可重试"
                description={
                  <Space size={10} wrap>
                    <Text>{lastRetryableInput.request.content}</Text>
                    <Button
                      type="primary"
                      size="small"
                      icon={<ReloadOutlined />}
                      onClick={() => submitWorkbenchStream(lastRetryableInput)}
                      disabled={currentStreamPending}
                    >
                      重试
                    </Button>
                  </Space>
                }
              />
            ) : null}
          </>
        }
      >
        {feedView ? (
          <main className="soha-ai-workbench__canvas is-feed">
            <PersonalFeed
              owner={owner}
              onDiscuss={(item) => {
                focusAfterFeed.current = true
                setContextSelection((current) => ({ ...current, feedItemId: item.id }))
                queryClient.setQueryData(workbenchKeys.feedItem(owner, item.id), { data: item })
                setPanel(null)
                updateSearchParams({ view: undefined, message: undefined, q: undefined })
              }}
            />
          </main>
        ) : null}
        <main className="soha-ai-workbench__canvas" hidden={feedView}>
          <div className="soha-ai-workbench__dialog-shell">
            {!currentSession ? (
              <div className="soha-ai-workbench__empty-state">
                <Welcome
                  variant="borderless"
                  icon={<img src="/logo.svg" alt="" className="soha-ai-workbench__welcome-logo" />}
                  title={mainSessionQuery.isError ? '主要聊天加载失败' : '正在准备对话'}
                  description={
                    mainSessionQuery.isError
                      ? '请重试，或从左侧打开已有旁聊。'
                      : '正在同步你的会话，请稍候。'
                  }
                  extra={
                    <Space wrap>
                      <Button
                        type="primary"
                        loading={mainSessionQuery.isFetching}
                        onClick={() => void mainSessionQuery.refetch()}
                        disabled={!canUseChat}
                      >
                        重试
                      </Button>
                    </Space>
                  }
                />
              </div>
            ) : (
              <div className="soha-ai-workbench__conversation-card">
                {messageAnchor ? (
                  <div className="soha-message-history" aria-label="搜索定位">
                    <Typography.Text type="secondary">正在查看搜索命中的消息</Typography.Text>
                    <Button
                      size="small"
                      onClick={() => updateSearchParams({ message: undefined, q: undefined })}
                    >
                      回到最新消息
                    </Button>
                    {beforeCursor ? (
                      <Button
                        size="small"
                        loading={windowMutation.isPending}
                        onClick={() =>
                          windowMutation.mutate({
                            sessionId: requestedSessionId!,
                            selector: { before: beforeCursor },
                            historyKey,
                          })
                        }
                      >
                        更早消息
                      </Button>
                    ) : null}
                    {afterCursor ? (
                      <Button
                        size="small"
                        loading={windowMutation.isPending}
                        onClick={() =>
                          windowMutation.mutate({
                            sessionId: requestedSessionId!,
                            selector: { after: afterCursor },
                            historyKey,
                          })
                        }
                      >
                        后续消息
                      </Button>
                    ) : null}
                  </div>
                ) : null}
                <div className="soha-ai-workbench__conversation-scroll">
                  {messageWindowQuery.isLoading && messageAnchor ? (
                    <Typography.Text>正在定位消息…</Typography.Text>
                  ) : null}
                  {messages.length === 0 ? (
                    <div className="soha-ai-workbench__welcome">
                      <Welcome
                        variant="borderless"
                        icon={
                          <img src="/logo.svg" alt="" className="soha-ai-workbench__welcome-logo" />
                        }
                        title={isMainChat ? '今天，想一起完成什么？' : currentSessionTitle}
                        description={
                          isMainChat
                            ? '聊想法、查资源，或把一件事交给 Soha。需要专注讨论时，可以开启旁聊。'
                            : '这是一个独立话题。消息和背景单独保存，也可以随时回到主要聊天。'
                        }
                        styles={{ title: { fontSize: 24 }, description: { fontSize: 14 } }}
                      />
                      <Prompts
                        styles={{ list: { display: 'grid' } }}
                        classNames={{
                          list: 'soha-ai-workbench__welcome-list',
                          item: 'soha-ai-workbench__welcome-item',
                        }}
                        items={promptItems}
                        onItemClick={({ data }) => {
                          const starter = CHAT_STARTERS.find((item) => item.value === data.key)
                          if (starter) {
                            setSenderValue(starter.prompt)
                            senderRef.current?.focus()
                          }
                        }}
                      />
                    </div>
                  ) : (
                    <Bubble.List
                      ref={bubbleRef}
                      autoScroll={!messageAnchor}
                      items={bubbleItems(messages).map((item, index) => ({
                        ...item,
                        className:
                          String(item.key) === messageAnchor ? 'soha-message-hit' : undefined,
                        content:
                          item.role === 'ai' ? (
                            <>
                              {item.content ? (
                                <WorkbenchMarkdown
                                  content={item.content}
                                  searchQuery={
                                    String(item.key) === messageAnchor
                                      ? messageSearchQuery
                                      : undefined
                                  }
                                  streaming={['loading', 'updating'].includes(item.status)}
                                  sources={metadataSources(messages[index].metadata)}
                                  onSource={openSource}
                                />
                              ) : null}
                              <MessageProgress message={messages[index]} />
                            </>
                          ) : item.role === 'user' ? (
                            <UserMessageContent
                              message={messages[index]}
                              searchQuery={
                                String(item.key) === messageAnchor ? messageSearchQuery : undefined
                              }
                              reaction={assistantReactionForMessage(messages, messages[index])}
                            />
                          ) : (
                            item.content
                          ),
                        footer: (
                          <>
                            {personal && (
                              <AssistantChatResult
                                metadata={messages[index].metadata}
                                loading={currentStreamPending}
                                onContinue={() => {
                                  updateDraft((draft) => ({
                                    value: draft.value
                                      ? `${draft.value}\n调整你的头像：`
                                      : '调整你的头像：',
                                    task: { kind: 'avatar', useCurrentAvatar: true },
                                  }))
                                  senderRef.current?.focus({ cursor: 'end' })
                                }}
                                onUndo={() =>
                                  submitWorkbenchStream({
                                    sessionId: requestedSessionId!,
                                    request: {
                                      ...buildWorkbenchStreamRequest('撤销这次形象更改', 'general'),
                                      assistantTask: { kind: 'undo', messageId: String(item.key) },
                                    },
                                  })
                                }
                              />
                            )}
                            <MessageActivity
                              feedbackBlocked={messageFeedbackMutation.blocked}
                              branchBlocked={createSessionMutation.blocked}
                              onFeedback={
                                hasPermission(
                                  permissionSnapshotQuery.data?.data,
                                  'ai.evaluations.feedback.curate',
                                ) && typeof messages[index].metadata?.agentRunId === 'string'
                                  ? (disposition) =>
                                      submitMessageFeedback(
                                        String(item.key),
                                        String(messages[index].metadata?.agentRunId),
                                        disposition,
                                      )
                                  : undefined
                              }
                              onSaveMemory={
                                serverMessages.some((message) => message.id === String(item.key)) &&
                                hasPermission(
                                  permissionSnapshotQuery.data?.data,
                                  'ai.memory.update',
                                )
                                  ? () =>
                                      setMemoryDraft({
                                        fact: messages[index].content,
                                        sourceRefs: [
                                          `soha://session/${encodeURIComponent(currentSession.id)}/message/${encodeURIComponent(String(item.key))}`,
                                        ],
                                      })
                                  : undefined
                              }
                              onBranch={
                                serverMessages.some((message) => message.id === String(item.key))
                                  ? () => branchFromMessage(String(item.key))
                                  : undefined
                              }
                              reaction={reactions[String(item.key)]}
                              onReaction={(emoji) =>
                                setReactions((current) => ({
                                  ...current,
                                  [String(item.key)]:
                                    current[String(item.key)] === emoji ? '' : emoji,
                                }))
                              }
                              onReply={
                                serverMessages.some((message) => message.id === String(item.key))
                                  ? () => {
                                      if ((contextSelection.references?.length ?? 0) >= 20) {
                                        void message.warning('最多添加 20 个引用。')
                                        return
                                      }
                                      setContextSelection((current) => ({
                                        ...current,
                                        references: [
                                          ...(current.references ?? []),
                                          {
                                            kind: 'session',
                                            sessionId: currentSession.id,
                                            messageIds: [String(item.key)],
                                            name: `回复 · ${messages[index].content.slice(0, 48)}`,
                                          },
                                        ],
                                      }))
                                      senderRef.current?.focus()
                                    }
                                  : undefined
                              }
                              message={messages[index]}
                              hasArtifact={artifactsByMessage.has(String(item.key))}
                              onSource={openSource}
                            />
                            {item.role === 'ai' && artifactsByMessage.has(String(item.key)) ? (
                              <div
                                className="soha-ai-workbench__message-results"
                                data-message-id={item.key}
                              >
                                {(artifactsByMessage.get(String(item.key)) ?? []).map((entry) => (
                                  <div
                                    key={entry.key}
                                    className="soha-ai-workbench__message-result"
                                  >
                                    <Text type="secondary">{artifactTitle(entry)}</Text>
                                    <Space size={[4, 4]} wrap>
                                      <Button
                                        size="small"
                                        onClick={() => {
                                          selectArtifact(entry.key)
                                          setPanel('history')
                                        }}
                                      >
                                        查看{staticArtifactLabel(entry.artifact.kind) || '结果'}
                                      </Button>
                                      {sourceItemsForArtifactEntry(entry).length > 0 ? (
                                        <Button
                                          size="small"
                                          onClick={() => {
                                            selectArtifact(entry.key)
                                            openInspector('evidence')
                                          }}
                                        >
                                          来源
                                        </Button>
                                      ) : null}
                                      {(entry.artifact.hypotheses?.length ?? 0) > 0 ? (
                                        <Button
                                          size="small"
                                          onClick={() => {
                                            selectArtifact(entry.key)
                                            openInspector('hypotheses')
                                          }}
                                        >
                                          假设
                                        </Button>
                                      ) : null}
                                      {(entry.artifact.recommendations?.length ?? 0) > 0 ? (
                                        <Button
                                          size="small"
                                          onClick={() => {
                                            selectArtifact(entry.key)
                                            openInspector('actions')
                                          }}
                                        >
                                          建议
                                        </Button>
                                      ) : null}
                                      {(entry.artifact.toolExecutions?.length ?? 0) > 0 ? (
                                        <Button
                                          size="small"
                                          onClick={() => {
                                            selectArtifact(entry.key)
                                            setPanel('thinking')
                                          }}
                                        >
                                          执行记录 {entry.artifact.toolExecutions?.length}
                                        </Button>
                                      ) : null}
                                    </Space>
                                  </div>
                                ))}
                              </div>
                            ) : null}
                          </>
                        ),
                      }))}
                      role={{
                        ai: {
                          placement: 'start',
                          avatar: (
                            <AssistantAvatar
                              assistant={assistantQuery.data?.data}
                              personal={personal}
                              className="soha-ai-message-avatar"
                            />
                          ),
                          variant: 'borderless',
                        },
                        user: { placement: 'end', variant: 'filled' },
                        system: { placement: 'start', variant: 'outlined' },
                      }}
                      style={{ flex: 1, overflow: 'auto', paddingRight: 8 }}
                    />
                  )}
                </div>

                <WorkbenchComposer
                  key={currentSession.id}
                  heldValue={heldValue}
                  onRestoreDraft={() =>
                    updateDraft({ value: heldValue || '', heldValue: undefined, task: undefined })
                  }
                  assistantTask={assistantTask}
                  images={contextSelection.images}
                  onImagesChange={(images) =>
                    setContextSelection((current) => ({ ...current, images }))
                  }
                  onAssistantTaskChange={(task) =>
                    task
                      ? updateDraft({ task })
                      : updateDraft({
                          task: undefined,
                          value: heldValue ?? senderValue,
                          heldValue: undefined,
                        })
                  }
                  agentProviders={agentProviders}
                  agentProviderId={selectedAgentProviderId}
                  agentMode={activeMode}
                  onAgentChange={(agentProviderId) => {
                    setSelectedAgentProviderId(agentProviderId)
                    patchSessionMutation.mutate(
                      { sessionId: currentSession.id, body: { agentProviderId } },
                      {
                        onError: () => {
                          if (activeSessionRef.current === currentSession.id)
                            setSelectedAgentProviderId(
                              currentSession.metadata?.agentProviderId || 'internal',
                            )
                        },
                      },
                    )
                  }}
                  preferences={modelPreferences}
                  onPreferencesChange={(value) => updateDraft({ model: value })}
                  modelOptions={catalogQuery.data?.data?.modelOptions ?? []}
                  defaultPublicModel={catalogQuery.data?.data?.defaultPublicModel}
                  modelError={
                    catalogQuery.data?.data?.modelOptionsError ||
                    (catalogQuery.isError ? '模型目录加载失败' : undefined)
                  }
                  modelSelectionEnabled={
                    activeMode === 'general' && selectedAgentProviderId === 'internal'
                  }
                  contextSummary={nextContextSummary}
                  usedContextSummary={usedContextSummary}
                  contextUsage={contextUsage}
                  contextSource={
                    currentAlertId ? (
                      <Button
                        type="link"
                        onClick={() =>
                          navigate(
                            `/monitoring-workbench/alerts/${encodeURIComponent(currentAlertId)}`,
                          )
                        }
                      >
                        查看原始告警详情
                      </Button>
                    ) : undefined
                  }
                  contextControls={
                    activeMode === 'general' ? (
                      <>
                        {hasPermission(permissionSnapshotQuery.data?.data, 'ai.memory.view') ? (
                          <Button
                            size="small"
                            type="text"
                            icon={<BookOutlined />}
                            aria-label="个人记忆"
                            disabled={contextParsing}
                            onClick={() => setMemoryDraft({})}
                          >
                            记忆
                            {contextSelection.memoryIds?.length
                              ? ' · ' + contextSelection.memoryIds.length
                              : ''}
                          </Button>
                        ) : null}
                        <ChatContextSelection
                          showSelection={false}
                          selection={contextSelection}
                          onChange={setContextSelection}
                          knowledgeBaseIds={knowledgeBaseIds}
                          onKnowledgeChange={setKnowledgeBaseIds}
                          initialClusterId={currentSession.metadata?.scope?.clusterId}
                          disabled={
                            !canUseChat ||
                            contextParsing ||
                            currentStreamPending ||
                            runningAgentRuns.length > 0
                          }
                          onParsingChange={setContextParsing}
                          onSession={() => {
                            setReferenceSessionId('')
                            setPanel('reference')
                          }}
                        />
                      </>
                    ) : undefined
                  }
                  selectedContext={
                    activeMode === 'general' ? (
                      <>
                        {contextSelection.feedItemId ? (
                          <Tag
                            closable
                            onClose={() =>
                              setContextSelection((current) => ({
                                ...current,
                                feedItemId: undefined,
                              }))
                            }
                          >
                            讨论动态 · {feedReferenceQuery.data?.data?.title || '正在读取内容'}
                          </Tag>
                        ) : null}
                        {contextSelection.memoryIds?.length ? (
                          <Tag>记忆 · {contextSelection.memoryIds.length}</Tag>
                        ) : null}
                        {currentSession.metadata?.pinnedContext?.branchReference ? (
                          <Tag
                            closable
                            onClose={() => {
                              const pinnedContext = { ...currentSession.metadata?.pinnedContext }
                              delete pinnedContext.branchReference
                              patchSessionMutation.mutate({
                                sessionId: currentSession.id,
                                body: { pinnedContext },
                              })
                            }}
                          >
                            分支背景 · 仅引用分支时选定的消息
                          </Tag>
                        ) : null}
                        <ChatContextTags
                          selection={contextSelection}
                          onChange={setContextSelection}
                          knowledgeBaseIds={knowledgeBaseIds}
                          onKnowledgeChange={setKnowledgeBaseIds}
                          disabled={
                            !canUseChat ||
                            contextParsing ||
                            currentStreamPending ||
                            runningAgentRuns.length > 0
                          }
                        />
                      </>
                    ) : undefined
                  }
                  resetVersion={senderResetVersion}
                  senderRef={senderRef}
                  loading={
                    currentStreamPending ||
                    runningAgentRuns.length > 0 ||
                    requestRecovery.pending ||
                    cancelAgentRunsMutation.isPending
                  }
                  readOnly={requestRecovery.pending || Boolean(requestRecovery.error)}
                  disabled={!canUseChat || !currentSession}
                  contextParsing={contextParsing}
                  value={senderValue}
                  onChange={setSenderValue}
                  onCancel={cancelMessageStream}
                  onSubmit={submitMessage}
                  onTools={() => setPanel('toolset')}
                  onSettings={() => setPanel('settings')}
                  onAnalysis={openExplicitAnalysis}
                  onSession={(id = '') => {
                    setReferenceSessionId(id)
                    setPanel('reference')
                  }}
                />
              </div>
            )}
          </div>
        </main>
        <WorkbenchPanel
          title="Soha 助手"
          onAfterClose={() => {
            if (profileFocusPending.current) {
              profileFocusPending.current = false
              requestAnimationFrame(() => senderRef.current?.focus({ cursor: 'end' }))
            }
          }}
          variant="assistant"
          open={personal && panel === 'assistant'}
          onClose={() => setPanel(null)}
          footer={
            <MobileAssistantNav
              value={
                assistantTab === 'activity'
                  ? 'tasks'
                  : assistantTab === 'results'
                    ? 'results'
                    : 'me'
              }
              onChange={(value) => {
                if (value === 'chat' || value === 'feed') {
                  setPanel(null)
                  updateSearchParams({ view: value === 'feed' ? 'feed' : undefined })
                } else
                  setAssistantTab(
                    value === 'tasks' ? 'activity' : value === 'results' ? 'results' : 'identity',
                  )
              }}
            />
          }
        >
          <ReadFeedback query={assistantQuery} label="个人助手" />
          <ReadFeedback query={personalTasksQuery} label="个人任务" />
          <ReadFeedback query={personalEnvironmentQuery} label="个人环境" />
          <ReadFeedback query={environmentTemplatesQuery} label="环境模板" />
          <AssistantPanel
            environmentFeedback={
              <MutationFeedback
                operations={[
                  {
                    mutation: environmentMutation,
                    label: '环境操作',
                    target: assistantQuery.data?.data?.id,
                  },
                  {
                    mutation: environmentDeletion,
                    label: '删除环境数据',
                    target: environmentDeletion.variables?.assistantId,
                  },
                  {
                    mutation: environmentExport,
                    label: '导出环境数据',
                    target: environmentExport.variables?.assistantId,
                    readOnly: true,
                  },
                ]}
              />
            }
            assistant={assistantQuery.data?.data}
            connectionState={
              catalogQuery.isPaused ||
              assistantQuery.isPaused ||
              catalogQuery.isError ||
              assistantQuery.isError
                ? 'unknown'
                : catalogQuery.isPending || assistantQuery.isPending
                  ? 'checking'
                  : assistantQuery.data?.data
                    ? 'connected'
                    : 'unknown'
            }
            environment={personalEnvironmentQuery.data?.data}
            workspaceOwner={owner}
            environmentError={personalEnvironmentQuery.isError}
            environmentLoading={personalEnvironmentQuery.isPending}
            onRetryEnvironment={() => void personalEnvironmentQuery.refetch()}
            environmentTemplates={environmentTemplatesQuery.data?.data ?? []}
            environmentTemplatesError={environmentTemplatesQuery.isError}
            environmentTemplatesLoading={environmentTemplatesQuery.isFetching}
            onRetryEnvironmentTemplates={() => void environmentTemplatesQuery.refetch()}
            environmentChanging={
              environmentMutation.isPending ||
              environmentDeletion.isPending ||
              environmentExport.isPending
            }
            environmentUncertain={
              writeOutcomeUnknown(environmentMutation.error) ||
              writeOutcomeUnknown(environmentDeletion.error)
            }
            environmentExporting={environmentExport.isPending}
            onExportEnvironment={(input) => environmentExport.mutate(input)}
            onCancelEnvironmentExport={() => environmentExportAbort.current?.abort()}
            onChangeEnvironment={(input) => environmentMutation.mutate(input)}
            canRetryDeletion={
              deletionReconciled ===
              `${personalEnvironmentQuery.data?.data?.assistantId}:${personalEnvironmentQuery.data?.data?.version}`
            }
            onDeleteEnvironment={(input) => environmentDeletion.mutateAsync(input)}
            personalRuns={personalTasksQuery.data?.data ?? []}
            personalArtifacts={personalArtifactsQuery.data?.data ?? []}
            artifactsError={personalArtifactsQuery.isError}
            artifactsLoading={personalArtifactsQuery.isFetching}
            onRetryArtifacts={() => void personalArtifactsQuery.refetch()}
            onPersonalArtifact={(entry) => {
              setPersonalArtifactEntry({
                key: entry.id,
                artifact: entry.artifact,
                index: 0,
                message: {
                  id: entry.messageId || entry.id,
                  sessionId: entry.sessionId || '',
                  role: 'assistant',
                  content: '',
                  createdAt: entry.createdAt,
                  metadata: entry.taskId ? { agentRunId: entry.taskId } : undefined,
                },
              })
              setPanel('history')
            }}
            tasksError={
              personalTasksQuery.isError || agentRunsQuery.isError || nativeEventPages.isError
            }
            onRetryTasks={() => {
              void personalTasksQuery.refetch()
              void queryClient.invalidateQueries({ queryKey: workbenchKeys.agentRuns.all() })
            }}
            onSession={(id) => {
              handleSessionChange(id)
              setPanel(null)
            }}
            onProfilePrompt={(kind) => {
              if (feedView) {
                focusAfterFeed.current = true
                updateSearchParams({ view: undefined })
              }
              const prefix = kind === 'name' ? '把你的名称改为 ' : '把虚拟形象改成 '
              updateDraft((draft) => ({
                value: prefix,
                heldValue: draft.heldValue ?? draft.value,
                task: kind === 'avatar' ? { kind: 'avatar' } : undefined,
              }))
              if (window.matchMedia('(max-width: 991px)').matches) {
                profileFocusPending.current = true
                setPanel(null)
              } else if (!feedView)
                requestAnimationFrame(() => senderRef.current?.focus({ cursor: 'end' }))
            }}
            avatarBusy={streamingSessions.length > 0}
            renaming={currentStreamPending}
            tab={assistantTab}
            onTabChange={setAssistantTab}
            provider={activeAgentProvider}
            session={currentSession}
            messages={messages}
            runs={agentRunsQuery.data?.data ?? []}
            artifacts={artifactEntries}
            loading={
              messagesQuery.isLoading ||
              agentRunsQuery.isLoading ||
              personalTasksQuery.isLoading ||
              personalArtifactsQuery.isLoading
            }
            onArtifact={(key) => {
              selectArtifact(key)
              setPanel('history')
            }}
            onSettings={() => setPanel('settings')}
            onTools={() => setPanel('toolset')}
            onSource={openSource}
          />
        </WorkbenchPanel>
        <WorkbenchPanel
          title={selectedSource?.title || '来源'}
          open={panel === 'source'}
          onClose={() => setPanel(null)}
        >
          {selectedSource ? (
            <>
              <Typography.Paragraph style={{ whiteSpace: 'pre-wrap' }}>
                {selectedSource.summary || '此记录仅保留引用标识，未保存摘录。'}
              </Typography.Paragraph>
              <Typography.Text type="secondary">引用 ID：{selectedSource.id}</Typography.Text>
              {safeSourceURL(selectedSource.url) ? (
                <Typography.Paragraph>
                  <a
                    href={safeSourceURL(selectedSource.url)}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    打开原始来源
                  </a>
                </Typography.Paragraph>
              ) : null}
            </>
          ) : null}
        </WorkbenchPanel>
        <WorkbenchPanel
          title="引用其他会话"
          open={panel === 'reference'}
          onClose={() => setPanel(null)}
        >
          {panel === 'reference' && currentSession ? (
            <SessionReferenceReader
              key={currentSession.id}
              currentSessionId={currentSession.id}
              initialId={referenceSessionId}
              onInsert={(reference) => {
                if ((contextSelection.references?.length ?? 0) >= 20) {
                  void message.error('最多添加 20 个引用。请先移除一个条目。')
                  return
                }
                setContextSelection((current) => ({
                  ...current,
                  references: [...(current.references ?? []), reference],
                }))
                setPanel(null)
                senderRef.current?.focus({ cursor: 'end' })
              }}
            />
          ) : null}
        </WorkbenchPanel>
        <WorkbenchPanel title="会话设置" open={panel === 'settings'} onClose={() => setPanel(null)}>
          <Space orientation="vertical" size={16} style={{ width: '100%' }}>
            {currentSession ? (
              <Space orientation="vertical">
                <Typography.Paragraph code>会话 ID：{currentSession.id}</Typography.Paragraph>
                <Button
                  size="small"
                  aria-label="复制会话 ID"
                  icon={<CopyOutlined />}
                  onClick={async () => {
                    try {
                      await navigator.clipboard.writeText(currentSession.id)
                      void message.success('会话 ID 已复制')
                    } catch {
                      void message.warning('无法访问剪贴板，请手动复制上方 ID。')
                    }
                  }}
                >
                  复制编号
                </Button>
              </Space>
            ) : null}
            <Card size="small" title="模型与工具">
              <Paragraph>
                {modelStatusDetail(latestAssistantMessage, currentStreamPending)}
              </Paragraph>
              <Space wrap>
                <Button onClick={() => navigate(getAIModelSettingsPath(location.search))}>
                  模型设置
                </Button>
                <Button onClick={() => setPanel('toolset')}>工具装配</Button>
              </Space>
            </Card>
            {legacyPlatformMessages.length > 0 ? (
              <Alert
                type="warning"
                showIcon
                title="已隐藏旧版平台上下文回复"
                description="这些历史兜底回复不是模型输出。"
              />
            ) : null}
            <Card size="small" title="专业任务">
              <Paragraph type="secondary">
                按本次任务选择分析能力，结果保存在当前对话与成果中。
              </Paragraph>
              <Space wrap>
                <Tooltip title={explicitAnalysisTitle}>
                  <Button
                    onClick={openExplicitAnalysis}
                    disabled={!currentSession || !canRunExplicitAnalysis}
                  >
                    专业分析任务
                  </Button>
                </Tooltip>
                <Button
                  title={createInspectionTitle}
                  onClick={createInspectionFromSession}
                  disabled={
                    !currentSession ||
                    !canCreateInspectionTask ||
                    createInspectionFromSessionMutation.blocked
                  }
                >
                  生成巡检任务
                </Button>
              </Space>
            </Card>
          </Space>
        </WorkbenchPanel>
        <WorkbenchPanel
          title={activeArtifactEntry ? artifactTitle(activeArtifactEntry) : '结果详情'}
          open={panel === 'history'}
          onClose={() => setPanel(null)}
        >
          {!personalArtifactEntry && artifactEntries.length > 0 ? (
            <div className="soha-ai-workbench__artifact-strip">
              <div className="soha-ai-workbench__artifact-strip-head">
                <Text type="secondary">会话结果 · {artifactEntries.length}</Text>
              </div>
              <div className="soha-ai-workbench__artifact-list">
                {artifactEntries.map((entry) => {
                  const selected = entry.key === activeArtifactEntry?.key
                  const contextLinks = artifactContextLinks(entry, currentSession)
                  return (
                    <button
                      key={entry.key}
                      type="button"
                      className={`soha-ai-workbench__artifact-item ${selected ? 'is-active' : ''}`}
                      onClick={() => selectArtifact(entry.key)}
                    >
                      <span className="soha-ai-workbench__artifact-title">
                        {artifactTitle(entry)}
                      </span>
                      <span className="soha-ai-workbench__artifact-meta">
                        {artifactMeta(entry)}
                      </span>
                      <span className="soha-ai-workbench__artifact-counts">
                        {isStaticArtifact(entry.artifact)
                          ? '静态草稿'
                          : `${entry.artifact.evidence?.length ?? 0} 证据 · ${entry.artifact.recommendations?.length ?? 0} 建议`}
                      </span>
                      {contextLinks.length > 0 ? (
                        <span className="soha-ai-workbench__artifact-context">
                          {contextLinks.slice(0, 3).map((link) => (
                            <Tag key={`${entry.key}-${link.key}`} variant="filled">
                              {link.label}
                            </Tag>
                          ))}
                        </span>
                      ) : null}
                    </button>
                  )
                })}
              </div>
            </div>
          ) : (
            <ManagementState
              bordered={false}
              compact
              title="暂无分析工件"
              description="会话产生分析结果后，可在这里查看历史和图谱。"
            />
          )}

          {activeArtifact && isStaticArtifact(activeArtifact) ? (
            <StaticArtifactView
              artifact={activeArtifact}
              sources={metadataSources(activeArtifactEntry?.message.metadata)}
              onSource={openSource}
            />
          ) : null}
          {activeArtifact && !isStaticArtifact(activeArtifact) && !activeGraph?.nodes?.length ? (
            <Paragraph style={{ marginTop: 16, whiteSpace: 'pre-wrap' }}>
              {activeArtifact.summary}
            </Paragraph>
          ) : null}
          {activeGraph?.nodes?.length ? (
            <div className="soha-ai-workbench__graph-panel">
              <div className="soha-ai-workbench__graph-head">
                <div>
                  <Text strong>分析工件图谱</Text>
                  <Paragraph className="soha-ai-workbench__conversation-subtitle">
                    {activeArtifact?.summary ||
                      '把 traces、logs、metrics 与假设收敛成一张会话内动态图。'}
                  </Paragraph>
                </div>
                <Space size={8} wrap>
                  <Tag color="blue">{activeArtifact?.kind || activeMode}</Tag>
                  {activeArtifact?.runId ? <Tag>{activeArtifact.runId}</Tag> : null}
                  <Tag>{activeGraph.nodes?.length || 0} 节点</Tag>
                  <Tag>{activeGraph.edges?.length || 0} 连线</Tag>
                </Space>
              </div>
              {activeArtifactLinks.length > 0 ? (
                <div className="soha-ai-workbench__artifact-linkbar">
                  <Text type="secondary">关联入口</Text>
                  <Space size={[6, 6]} wrap>
                    {activeArtifactLinks.map((link) => (
                      <Button
                        key={link.key}
                        size="small"
                        type="text"
                        icon={<LinkOutlined />}
                        onClick={() => openArtifactLink(link)}
                      >
                        {`${link.label}: ${link.value}`}
                      </Button>
                    ))}
                  </Space>
                </div>
              ) : null}
              {!enabledDataSources.some((item) =>
                ['logs', 'metrics', 'traces'].includes(item.sourceKind),
              ) ? (
                <Alert
                  type="info"
                  showIcon
                  title="当前还没有可用的 logs / metrics / traces 数据源"
                  description="现在展示的是会话范围根节点。配置 Elasticsearch/Loki、Prometheus、Jaeger 之后，根因图会自动扩展成错误链路、日志签名和指标挂件。"
                />
              ) : null}
              <div className="soha-ai-workbench__graph-layout">
                <LazyWorkbenchGraphView
                  selectedNodeId={selectedGraphNodeId}
                  fitKey={graphFitKey}
                  graph={activeGraph}
                  onSelectNode={setSelectedGraphNodeId}
                />
                <div className="soha-workbench-graph-selection">
                  {selectedGraphNode ? (
                    <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                      <div>
                        <Space size={[8, 8]} wrap>
                          <Text strong>{selectedGraphNode.title}</Text>
                          <Tag>{graphNodeLabel(selectedGraphNode.kind)}</Tag>
                          {selectedGraphNode.severity ? (
                            <StatusTag value={selectedGraphNode.severity} />
                          ) : null}
                        </Space>
                        {selectedGraphNode.subtitle ? (
                          <Paragraph type="secondary" style={{ margin: '8px 0 0' }}>
                            {selectedGraphNode.subtitle}
                          </Paragraph>
                        ) : null}
                      </div>
                      {selectedGraphNode.sourceRefs?.length ? (
                        <div className="soha-ai-workbench__tool-chip-list">
                          {selectedGraphNode.sourceRefs.map((item) => (
                            <Tag key={`${selectedGraphNode.id}-${item}`}>{item}</Tag>
                          ))}
                        </div>
                      ) : null}
                      {selectedGraphNode.kind === 'missing_source' ? (
                        <Alert
                          type="info"
                          showIcon
                          title="当前会话缺少这类观测源"
                          description="先到“工具与技能”或“模型设置 / 数据源配置”里补上对应连接，再重新执行显式分析。"
                        />
                      ) : null}
                      {selectedGraphNode.kind === 'recommendation' ? (
                        <Alert
                          type="success"
                          showIcon
                          title="建议的下一步动作"
                          description={selectedGraphNode.subtitle || '优先缩小 scope，再重新分析。'}
                        />
                      ) : null}
                      {selectedGraphNode.evidenceIds?.length ? (
                        <Card size="small" title="关联证据">
                          <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                            {(activeArtifact?.evidence ?? [])
                              .filter((item) => selectedGraphNode.evidenceIds?.includes(item.id))
                              .map((item) => (
                                <div key={item.id}>
                                  <Text strong>{item.title}</Text>
                                  <Paragraph type="secondary" style={{ margin: '4px 0 0' }}>
                                    {item.summary}
                                  </Paragraph>
                                </div>
                              ))}
                          </Space>
                        </Card>
                      ) : null}
                      {selectedGraphNode.attributes ? (
                        <Card size="small" title="节点属性">
                          <pre className="soha-workbench-graph-json">
                            {JSON.stringify(selectedGraphNode.attributes, null, 2)}
                          </pre>
                        </Card>
                      ) : null}
                    </Space>
                  ) : (
                    <ManagementState
                      bordered={false}
                      compact
                      kind="select-scope"
                      title="未选择图谱节点"
                      description="点击图中的节点查看链路明细。"
                    />
                  )}
                </div>
              </div>
            </div>
          ) : null}
        </WorkbenchPanel>
        <WorkbenchPanel title="执行记录" open={panel === 'thinking'} onClose={() => setPanel(null)}>
          {toolCalls.length > 0 ? (
            <Alert
              type={
                toolCalls.some((item) => thoughtChainStatus(item.status) === 'error')
                  ? 'warning'
                  : 'info'
              }
              showIcon
              title={chainToolCallSummary}
              description={
                <Space orientation="vertical" size={4}>
                  {chainThinkingSummary ? <Text>{chainThinkingSummary}</Text> : null}
                  {chainAgentStatus ? (
                    <Text type="secondary">Agent: {agentStatusLabel(chainAgentStatus)}</Text>
                  ) : null}
                </Space>
              }
              style={{ marginBottom: 12 }}
            />
          ) : null}
          <ThoughtChain
            items={
              toolCalls.length === 0
                ? [
                    {
                      key: 'idle',
                      title: '暂无执行记录',
                      description: '这条回复还没有工具调用记录。',
                      status: 'abort' satisfies ThoughtChainStatus,
                    },
                  ]
                : toolCalls.map((item) => ({
                    key: item.id,
                    title: item.toolName,
                    description: item.summary || item.adapterId,
                    content: item.output ? (
                      <pre style={{ whiteSpace: 'pre-wrap', margin: 0 }}>
                        {JSON.stringify(item.output, null, 2)}
                      </pre>
                    ) : undefined,
                    status: thoughtChainStatus(item.status),
                    blink: item.status === 'running',
                  }))
            }
          />
        </WorkbenchPanel>

        <WorkbenchPanel
          title={activeArtifactEntry ? artifactTitle(activeArtifactEntry) : '消息详情'}
          open={panel === 'inspector'}
          onClose={() => setPanel(null)}
          extra={
            <Segmented
              size="small"
              value={inspectorView}
              options={[
                ...(sourceItemsForArtifactEntry(activeArtifactEntry).length
                  ? [{ value: 'evidence', label: '来源' }]
                  : []),
                ...(activeArtifact?.hypotheses?.length
                  ? [{ value: 'hypotheses', label: '假设' }]
                  : []),
                ...(activeArtifact?.recommendations?.length
                  ? [{ value: 'actions', label: '建议' }]
                  : []),
              ]}
              onChange={(value) => setInspectorView(value as InspectorView)}
            />
          }
        >
          {renderInspectorBody()}
        </WorkbenchPanel>

        <WorkbenchPanel
          title="会话级工具集"
          open={panel === 'toolset'}
          onClose={() => {
            if (!patchSessionMutation.blocked) setPanel(null)
          }}
          extra={
            <Tag color={currentSession ? 'blue' : 'default'}>
              {currentSession ? currentSessionTitle : '未选择会话'}
            </Tag>
          }
          footer={
            <Flex justify="space-between" gap={12} wrap="wrap">
              <Space wrap>
                <Button
                  onClick={clearToolset}
                  disabled={!currentSession || patchSessionMutation.blocked}
                >
                  恢复自动选择
                </Button>
                <Button
                  onClick={applyRecommendedToolset}
                  disabled={!currentSession || patchSessionMutation.blocked}
                >
                  应用推荐预设
                </Button>
              </Space>
              <Button
                type="primary"
                loading={patchSessionMutation.isPending}
                onClick={saveToolset}
                disabled={!currentSession || patchSessionMutation.blocked}
              >
                保存会话级装配
              </Button>
            </Flex>
          }
        >
          {patchSessionMutation.feedback}
          {!currentSession ? (
            <ManagementState
              bordered={false}
              compact
              kind="select-scope"
              title="未选择会话"
              description="先选择会话，再配置工具装配。"
            />
          ) : (
            <Space orientation="vertical" size={16} style={{ width: '100%' }}>
              <Card size="small" title="有效执行策略">
                <div className="soha-ai-workbench__tool-stack">
                  {toolsetPolicySummary.map((item) => (
                    <div key={item.label} className="soha-ai-workbench__tool-row">
                      <span>
                        <Text strong>{item.label}</Text>
                        <Text type="secondary">{item.detail}</Text>
                      </span>
                      <Tag>{item.value}</Tag>
                    </div>
                  ))}
                </div>
                {unavailableSelectedAdapterIds.length > 0 ? (
                  <Alert
                    style={{ marginTop: 12 }}
                    type="warning"
                    showIcon
                    title="部分已选 adapter 当前没有启用数据源"
                    description={`${unavailableSelectedAdapterIds.join(', ')} 会保留在会话策略中，但相关工具运行时会因为没有可用数据源而跳过或失败。`}
                  />
                ) : null}
              </Card>

              <Card size="small" title="Agent Provider">
                <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                  <Select
                    disabled={patchSessionMutation.blocked}
                    value={selectedAgentProviderId || defaultAgentProviderId}
                    options={providerOptions}
                    onChange={(value: string) => setSelectedAgentProviderId(value)}
                    placeholder="选择本会话默认执行器"
                  />
                  {activeAgentProvider ? (
                    <Alert
                      type={activeAgentProvider.supportsAsync ? 'info' : 'success'}
                      showIcon
                      title={`${activeAgentProvider.name} · ${activeAgentProvider.supportsAsync ? '异步 runner' : '内置同步分析'}`}
                      description={activeAgentProvider.description}
                    />
                  ) : null}
                  {agentCapabilities.length > 0 ? (
                    <div className="soha-ai-workbench__tool-chip-list">
                      {agentCapabilities.slice(0, 8).map((item) => (
                        <Tag key={item.id}>{item.name}</Tag>
                      ))}
                    </div>
                  ) : null}
                </Space>
              </Card>

              <Card size="small" title="Adapters 与工具">
                <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                  <Select
                    disabled={patchSessionMutation.blocked}
                    mode="multiple"
                    allowClear
                    maxTagCount="responsive"
                    showSearch={{ optionFilterProp: 'label' }}
                    placeholder="留空表示自动允许所有已注册 adapter"
                    value={selectedAdapterIds}
                    onChange={(value: string[]) => setSelectedAdapterIds(value)}
                    options={adapters.map((item) => ({
                      value: item.id,
                      label: `${item.name} (${item.sourceKind})`,
                    }))}
                  />
                  <Select
                    disabled={patchSessionMutation.blocked}
                    mode="multiple"
                    allowClear
                    maxTagCount="responsive"
                    showSearch={{ optionFilterProp: 'label' }}
                    placeholder="选择要屏蔽的工具，保存为 adapter.tool"
                    value={disabledToolNames}
                    onChange={(value: string[]) =>
                      setDisabledToolNames(canonicalDisabledToolNames(value, adapters))
                    }
                    options={disabledToolOptions}
                  />
                  <div className="soha-ai-workbench__tool-stack">
                    {dataSources.length === 0 ? (
                      <ManagementState
                        bordered={false}
                        compact
                        title="暂无全局数据源"
                        description="全局数据源配置完成后会在这里展示。"
                      />
                    ) : (
                      dataSources.map((item) => (
                        <div key={item.id} className="soha-ai-workbench__tool-row">
                          <span>
                            <Text strong>{item.name}</Text>
                            <Text type="secondary">
                              {item.sourceKind} / {item.backendType} / {item.mcpAdapter}
                            </Text>
                          </span>
                          <StatusTag
                            value={item.validationStatus || (item.enabled ? 'enabled' : 'disabled')}
                          />
                        </div>
                      ))
                    )}
                  </div>
                </Space>
              </Card>

              <Card size="small" title="Skills">
                <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                  <Paragraph type="secondary" style={{ marginBottom: 0 }}>
                    这里选择本会话允许暴露给 AI coding 客户端的企业 skills；不影响全局 registry
                    的启用状态。
                  </Paragraph>
                  <Space wrap>
                    {globalSkills.length === 0 ? (
                      <ManagementState
                        bordered={false}
                        compact
                        title="暂无全局 Skills 配置"
                        description="全局 registry 尚未启用可分配的 skills。"
                      />
                    ) : (
                      globalSkills.map((item) => (
                        <Tag.CheckableTag
                          key={item.id}
                          checked={selectedSkillIds.includes(item.id)}
                          aria-disabled={patchSessionMutation.blocked}
                          onChange={(checked) => {
                            if (patchSessionMutation.blocked) return
                            setSelectedSkillIds((current) =>
                              checked
                                ? [...new Set([...current, item.id])]
                                : current.filter((id) => id !== item.id),
                            )
                          }}
                        >
                          {item.name}
                        </Tag.CheckableTag>
                      ))
                    )}
                  </Space>
                </Space>
              </Card>

              <Card size="small" title="Budget Overrides">
                <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                  {TOOLSET_BUDGET_FIELDS.map((field) => (
                    <Flex key={field.key} justify="space-between" align="center" gap={12}>
                      <span>
                        <Text strong>{field.label}</Text>
                        <Text type="secondary" style={{ display: 'block' }}>
                          {field.description}
                        </Text>
                      </span>
                      <InputNumber
                        disabled={patchSessionMutation.blocked}
                        min={0}
                        suffix={field.suffix}
                        value={budgetOverrides[field.key]}
                        onChange={(value) => setBudgetOverrideValue(field.key, value)}
                      />
                    </Flex>
                  ))}
                </Space>
              </Card>

              <Card size="small" title="Scope Overrides">
                <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                  <Alert
                    type="info"
                    showIcon
                    title="Scope override 会叠加到当前会话范围"
                    description={`当前会话范围：${buildScopeSummary(currentSession.metadata?.scope)}`}
                  />
                  <Input
                    disabled={patchSessionMutation.blocked}
                    placeholder="Override cluster"
                    value={scopeOverrides.clusterId || ''}
                    onChange={(event) => setScopeOverrideValue('clusterId', event.target.value)}
                  />
                  <Input
                    disabled={patchSessionMutation.blocked}
                    placeholder="Override namespace"
                    value={scopeOverrides.namespace || ''}
                    onChange={(event) => setScopeOverrideValue('namespace', event.target.value)}
                  />
                  <Input
                    disabled={patchSessionMutation.blocked}
                    placeholder="Override workload"
                    value={scopeOverrides.workload || ''}
                    onChange={(event) => setScopeOverrideValue('workload', event.target.value)}
                  />
                  <Input
                    disabled={patchSessionMutation.blocked}
                    placeholder="Override service"
                    value={scopeOverrides.service || ''}
                    onChange={(event) => setScopeOverrideValue('service', event.target.value)}
                  />
                  <Input
                    disabled={patchSessionMutation.blocked}
                    placeholder="Override alert ID"
                    value={scopeOverrides.alertId || ''}
                    onChange={(event) => setScopeOverrideValue('alertId', event.target.value)}
                  />
                  <InputNumber
                    disabled={patchSessionMutation.blocked}
                    min={0}
                    suffix="minutes"
                    placeholder="Override time range"
                    value={scopeOverrides.timeRangeMinutes}
                    onChange={(value) => setScopeOverrideNumberValue('timeRangeMinutes', value)}
                  />
                </Space>
              </Card>
            </Space>
          )}
        </WorkbenchPanel>
      </WorkbenchShell>

      {searchOpen ? (
        <MessageSearch
          owner={owner}
          personal={personal}
          sessions={visibleSessions}
          onClose={() => setSearchOpen(false)}
          onSelect={(sessionId, messageId, query) => {
            setSearchOpen(false)
            updateSearchParams({
              session: sessionId,
              message: messageId,
              q: query,
              view: undefined,
              mode: undefined,
            })
          }}
        />
      ) : null}
      <Modal
        title="专业分析任务"
        centered
        styles={{ body: { maxHeight: 'calc(100dvh - 220px)', overflowY: 'auto' } }}
        open={analysisOpen}
        onCancel={() => {
          if (currentStreamPending) {
            cancelMessageStream()
          }
          setAnalysisOpen(false)
        }}
        onOk={submitExplicitAnalysis}
        okText="开始分析"
        cancelText="取消"
        confirmLoading={currentStreamPending}
        okButtonProps={{
          disabled: !currentSession || !canSubmitExplicitAnalysis || currentStreamPending,
        }}
        width={680}
      >
        {currentSession ? (
          <Space orientation="vertical" size={14} style={{ width: '100%' }}>
            <Alert
              type="info"
              showIcon
              title="选择本次任务需要的能力"
              description="结果会保存在当前对话与成果中，主要聊天和旁聊继续保持自然对话。"
            />
            <Space orientation="vertical" size={8} style={{ width: '100%' }}>
              <Text strong>任务能力</Text>
              <Select<TaskCapability[]>
                aria-label="任务能力"
                mode="multiple"
                style={{ width: '100%' }}
                value={analysisCapabilities}
                options={RUNNABLE_ANALYSIS_MODE_OPTIONS.map((item) => ({
                  value: item.value,
                  label: item.label,
                  disabled: !analysisCapabilityAllowed(item.value),
                }))}
                onChange={(value) => {
                  const next = value
                  const currentDefault = defaultAnalysisQuestion(analysisMode)
                  setAnalysisCapabilities(next)
                  setAnalysisIntent((current) => {
                    const intent = { ...current }
                    if (!next.includes('root_cause')) delete intent.rootCauseProfileId
                    if (!next.includes('performance')) {
                      delete intent.performanceDataSourceId
                      delete intent.metricKey
                    }
                    if (!next.includes('trace')) {
                      delete intent.traceDataSourceId
                      delete intent.traceId
                      delete intent.minDurationMs
                    }
                    if (!next.includes('inspection_review')) delete intent.inspectionRunIds
                    return intent
                  })
                  if (!analysisQuestion.trim() || analysisQuestion === currentDefault) {
                    setAnalysisQuestion(defaultAnalysisQuestion(next[0] ?? 'general'))
                  }
                }}
              />
              <Text type="secondary">{analysisCapabilities.map(modeDescription).join(' ')}</Text>
            </Space>
            <Space orientation="vertical" size={8} style={{ width: '100%' }}>
              <Text strong>执行助手</Text>
              <Select
                aria-label="任务执行助手"
                value={analysisAgentProviderId}
                options={enabledAgentProviders.map((item) => {
                  const reason = taskAgentUnavailableReason(
                    item,
                    analysisCapabilities,
                    hasAnalysisParameters,
                  )
                  return {
                    value: item.id,
                    label: `${item.name}${reason ? ` · ${reason}` : ''}`,
                    disabled: !!reason,
                  }
                })}
                onChange={(value: string) => setAnalysisAgentProviderId(value)}
              />
              <Text type="secondary">
                {agentProviders.find((item) => item.id === analysisAgentProviderId)?.description ||
                  '选择本次分析使用内置分析还是外部 agent runner。'}
              </Text>
              {!nativeAnalysis ? (
                <Text type="secondary">
                  此助手支持单项专业任务；需要组合能力或指定查询参数时请选择内置助手。
                </Text>
              ) : null}
            </Space>
            {nativeAnalysis && analysisCapabilities.includes('root_cause') ? (
              <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                <Text strong>根因分析模板</Text>
                <Select
                  aria-label="根因分析模板"
                  allowClear
                  showSearch={{ optionFilterProp: 'label' }}
                  value={analysisIntent.rootCauseProfileId || undefined}
                  placeholder="使用已启用的默认根因模板"
                  options={analysisProfileOptions}
                  onChange={(value?: string) =>
                    setAnalysisIntent((current) => ({ ...current, rootCauseProfileId: value }))
                  }
                />
                <Text type="secondary">
                  {selectedAnalysisProfile
                    ? `${selectedAnalysisProfile.name} 会约束根因分析的数据源和调查策略。`
                    : '未选择时由后端使用会话和 provider 默认策略。'}
                </Text>
              </Space>
            ) : null}
            {nativeAnalysis && analysisCapabilities.includes('performance') ? (
              <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                <Text strong>性能分析参数</Text>
                <Select
                  aria-label="指标数据源"
                  placeholder="选择指标数据源"
                  style={{ width: '100%' }}
                  showSearch={{ optionFilterProp: 'label' }}
                  value={analysisIntent.performanceDataSourceId}
                  options={performanceSources.map((source) => ({
                    value: source.id,
                    label: source.name,
                  }))}
                  onChange={(value: string) =>
                    setAnalysisIntent((current) => ({ ...current, performanceDataSourceId: value }))
                  }
                />
                <Select
                  aria-label="性能指标"
                  placeholder="选择实际查询的指标"
                  style={{ width: '100%' }}
                  showSearch={{ optionFilterProp: 'label' }}
                  loading={metricCatalogQuery.isFetching}
                  value={analysisIntent.metricKey}
                  options={(metricCatalogQuery.data ?? []).map((metric) => ({
                    value: metric.key,
                    label: metric.label,
                    disabled: !metric.available,
                  }))}
                  onChange={(value: string) =>
                    setAnalysisIntent((current) => ({ ...current, metricKey: value }))
                  }
                />
                {metricCatalogQuery.isError ? (
                  <Alert
                    type="error"
                    showIcon
                    title="指标目录加载失败"
                    description={metricCatalogQuery.error.message}
                  />
                ) : null}
              </Space>
            ) : null}
            {nativeAnalysis && analysisCapabilities.includes('trace') ? (
              <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                <Text strong>链路分析参数</Text>
                <Select
                  aria-label="链路数据源"
                  placeholder="选择链路数据源"
                  style={{ width: '100%' }}
                  showSearch={{ optionFilterProp: 'label' }}
                  value={analysisIntent.traceDataSourceId}
                  options={traceSources.map((source) => ({ value: source.id, label: source.name }))}
                  onChange={(value: string) =>
                    setAnalysisIntent((current) => ({ ...current, traceDataSourceId: value }))
                  }
                />
                <Input
                  aria-label="Trace ID"
                  placeholder="Trace ID（可选，留空查询当前范围）"
                  maxLength={256}
                  value={analysisIntent.traceId ?? ''}
                  onChange={(event) =>
                    setAnalysisIntent((current) => ({
                      ...current,
                      traceId: event.target.value.trim() || undefined,
                    }))
                  }
                />
                <Text>最短耗时（毫秒，0 表示不按耗时过滤）</Text>
                <InputNumber
                  aria-label="链路最短耗时"
                  min={0}
                  max={86400000}
                  precision={0}
                  value={analysisIntent.minDurationMs ?? 0}
                  onChange={(value: number | null) =>
                    setAnalysisIntent((current) => ({ ...current, minDurationMs: value ?? 0 }))
                  }
                />
              </Space>
            ) : null}
            {nativeAnalysis && analysisCapabilities.includes('inspection_review') ? (
              <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                <Text strong>巡检执行记录</Text>
                <Select<string[]>
                  aria-label="巡检执行记录"
                  mode="multiple"
                  maxCount={20}
                  style={{ width: '100%' }}
                  showSearch={{ optionFilterProp: 'label' }}
                  placeholder="选择已完成的真实巡检记录"
                  loading={inspectionRunsQuery.isFetching}
                  value={analysisIntent.inspectionRunIds ?? []}
                  options={completedInspectionRuns.map((run) => ({
                    value: run.id,
                    label: `${run.summary || run.id} · ${formatSessionTimestamp(run.completedAt)}`,
                  }))}
                  onChange={(value: string[]) =>
                    setAnalysisIntent((current) => ({
                      ...current,
                      inspectionRunIds: value.length ? value : undefined,
                    }))
                  }
                />
                <Text type="secondary">
                  仅显示最近的已完成记录，执行前会重新核对当前权限和会话范围。
                </Text>
                {inspectionRunsQuery.isError ? (
                  <Alert
                    type="error"
                    showIcon
                    title="巡检记录加载失败"
                    description={inspectionRunsQuery.error.message}
                  />
                ) : null}
              </Space>
            ) : null}
            <Space orientation="vertical" size={8} style={{ width: '100%' }}>
              <Text strong>分析目标</Text>
              <Input.TextArea
                value={analysisQuestion}
                onChange={(event) => setAnalysisQuestion(event.target.value)}
                autoSize={{ minRows: 4, maxRows: 8 }}
                maxLength={600}
                showCount
                placeholder="描述这轮分析要回答的问题"
              />
            </Space>
            <Card size="small" title="执行上下文">
              <Space size={[8, 8]} wrap>
                {analysisCapabilities.map((kind) => (
                  <Tag color="blue" key={kind}>
                    {modeLabel(kind)}
                  </Tag>
                ))}
                <Tag
                  color={
                    agentProviders.find((item) => item.id === analysisAgentProviderId)
                      ?.supportsAsync
                      ? 'purple'
                      : 'default'
                  }
                >
                  {agentProviders.find((item) => item.id === analysisAgentProviderId)?.name ||
                    analysisAgentProviderId ||
                    '内置分析'}
                </Tag>
                {selectedAnalysisProfile ? <Tag>{selectedAnalysisProfile.name}</Tag> : null}
                <Tag>{buildScopeSummary(currentSession.metadata?.scope)}</Tag>
                <Tag>{currentSession.metadata?.toolset ? '会话级工具集' : '全局工具集'}</Tag>
              </Space>
            </Card>
            {analysisCapabilities.some((kind) => !analysisCapabilityAllowed(kind)) ? (
              <Alert
                type="warning"
                showIcon
                title="缺少所选专业能力或工具执行权限，无法开始分析。"
              />
            ) : null}
            {analysisUnavailableReason ? (
              <Alert type="warning" showIcon title={analysisUnavailableReason} />
            ) : null}
            {analysisInputReason ? (
              <Alert type="info" showIcon title={analysisInputReason} />
            ) : null}
          </Space>
        ) : (
          <ManagementState
            bordered={false}
            compact
            kind="select-scope"
            title="未选择会话"
            description="先打开一个对话，再启动专业分析任务。"
          />
        )}
      </Modal>

      {memoryDraft ? (
        <ChatMemory
          initialFact={memoryDraft.fact}
          sourceRefs={memoryDraft.sourceRefs}
          selected={contextSelection.memoryIds ?? []}
          onSelect={(memoryIds) => setContextSelection((current) => ({ ...current, memoryIds }))}
          onClose={() => setMemoryDraft(undefined)}
        />
      ) : null}
      <Modal
        title="开启旁聊"
        open={sideOpen}
        onCancel={() => {
          if (!createSessionMutation.blocked) setSideOpen(false)
        }}
        closable={!createSessionMutation.blocked}
        keyboard={!createSessionMutation.blocked}
        mask={{ closable: !createSessionMutation.blocked }}
        cancelButtonProps={{ disabled: createSessionMutation.blocked }}
        okText="开启旁聊"
        cancelText="取消"
        confirmLoading={createSessionMutation.isPending}
        okButtonProps={{
          disabled:
            createSessionMutation.blocked ||
            !sideTitle.trim() ||
            !canUseChat ||
            catalogQuery.isPending,
        }}
        onOk={() => createSession({ title: sideTitle, scope: {} })}
      >
        {createSessionMutation.feedback}
        <Paragraph type="secondary">
          给这个独立话题取个名字。你可以随时在主要聊天与旁聊之间切换。
        </Paragraph>
        <Input
          aria-label="旁聊名称"
          disabled={createSessionMutation.blocked}
          placeholder="例如：发布方案、一个新想法"
          maxLength={120}
          value={sideTitle}
          onChange={(event) => setSideTitle(event.target.value)}
          onPressEnter={() => {
            if (sideTitle.trim() && !createSessionMutation.blocked)
              createSession({ title: sideTitle, scope: {} })
          }}
        />
      </Modal>
      <Modal
        title="重命名会话"
        open={renameOpen}
        onCancel={() => {
          if (!patchSessionMutation.blocked) setRenameOpen(false)
        }}
        closable={!patchSessionMutation.blocked}
        keyboard={!patchSessionMutation.blocked}
        mask={{ closable: !patchSessionMutation.blocked }}
        confirmLoading={patchSessionMutation.isPending}
        okButtonProps={{ disabled: patchSessionMutation.blocked }}
        cancelButtonProps={{ disabled: patchSessionMutation.blocked }}
        onOk={() => {
          if (!renameTargetId || patchSessionMutation.blocked) return
          patchSessionMutation.mutate(
            {
              sessionId: renameTargetId,
              body: {
                title: renameValue,
                tags: sessionTagsWithPin(
                  visibleSessions.find((item) => item.id === renameTargetId)?.metadata?.tags,
                  renamePinned,
                ),
              },
            },
            { onSuccess: () => setRenameOpen(false) },
          )
        }}
      >
        {patchSessionMutation.feedback}
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Input
            aria-label="会话名称"
            disabled={patchSessionMutation.blocked}
            value={renameValue}
            onChange={(event) => setRenameValue(event.target.value)}
          />
          <Space>
            <Switch
              disabled={patchSessionMutation.blocked}
              aria-label="置顶会话"
              checked={renamePinned}
              onChange={setRenamePinned}
            />
            置顶会话
          </Space>
        </Space>
      </Modal>
    </>
  )
}
