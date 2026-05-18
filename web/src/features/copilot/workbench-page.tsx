import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ApiOutlined,
  BranchesOutlined,
  DeleteOutlined,
  EditOutlined,
  ExperimentOutlined,
  EyeOutlined,
  PlayCircleOutlined,
  RadarChartOutlined,
  RobotOutlined,
  ThunderboltOutlined,
  ToolOutlined,
} from '@ant-design/icons'
import {
  Bubble,
  Conversations,
  Prompts,
  Sender,
  ThoughtChain,
  Welcome,
} from '@ant-design/x'
import {
  Background,
  Controls,
  MarkerType,
  Position,
  ReactFlow,
  ReactFlowProvider,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react'
import dagre from 'dagre'
import {
  Alert,
  App,
  Button,
  Card,
  Drawer,
  Empty,
  Flex,
  Input,
  InputNumber,
  Modal,
  Segmented,
  Select,
  Space,
  Tag,
  Typography,
} from 'antd'
import '@xyflow/react/dist/style.css'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  WorkbenchAdapter,
  WorkbenchArtifact,
  WorkbenchGraph,
  WorkbenchGraphNode,
  WorkbenchMessage,
  WorkbenchMessageEnvelope,
  WorkbenchSession,
  WorkbenchSessionScope,
} from './workbench-types'

const { Paragraph, Text, Title } = Typography

type InspectorView = 'context' | 'evidence' | 'hypotheses' | 'actions'
type WorkbenchFlowNode = Node<WorkbenchGraphNode & Record<string, unknown>, 'workbenchGraphNode'>
type WorkbenchFlowEdge = Edge<{ relation: string; severity?: string }, 'smoothstep'>

const GRAPH_NODE_WIDTH = 248
const GRAPH_NODE_HEIGHT = 104

const WORKBENCH_MODE_OPTIONS = [
  { value: 'general', label: '通用聊天' },
  { value: 'root_cause', label: '根因分析' },
  { value: 'performance', label: '性能分析' },
  { value: 'trace', label: '链路分析' },
  { value: 'inspection_review', label: '巡检复盘' },
] as const

function modeLabel(mode?: string) {
  switch (mode) {
    case 'root_cause':
      return '根因分析'
    case 'performance':
      return '性能分析'
    case 'trace':
      return '链路分析'
    case 'inspection_review':
      return '巡检复盘'
    default:
      return '通用聊天'
  }
}

function modeDescription(mode?: string) {
  switch (mode) {
    case 'root_cause':
      return '围绕告警、变更和异常证据收敛根因。'
    case 'performance':
      return '聚焦延迟、容量和抖动问题，沉淀优化建议。'
    case 'trace':
      return '从入口请求向下游链路展开，定位热点 span。'
    case 'inspection_review':
      return '把巡检发现整理成后续动作和交接结论。'
    default:
      return '沉淀一轮完整问答、证据和下一步排障动作。'
  }
}

function modeIcon(mode?: string) {
  switch (mode) {
    case 'root_cause':
      return <ThunderboltOutlined />
    case 'performance':
      return <RadarChartOutlined />
    case 'trace':
      return <BranchesOutlined />
    case 'inspection_review':
      return <PlayCircleOutlined />
    default:
      return <RobotOutlined />
  }
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

function bubbleItems(messages: WorkbenchMessage[]) {
  return messages.map((item) => ({
    key: item.id,
    role: item.role === 'assistant' ? 'ai' : item.role === 'system' ? 'system' : 'user',
    content: item.content,
    status: 'success' as const,
    extraInfo: { createdAt: item.createdAt },
  }))
}

function graphAccent(kind: string) {
  switch (kind) {
    case 'scope':
      return '#1d4ed8'
    case 'service':
      return '#0f766e'
    case 'span':
      return '#7c3aed'
    case 'log_signature':
      return '#b45309'
    case 'metric_signal':
      return '#2563eb'
    case 'hypothesis':
      return '#dc2626'
    case 'missing_source':
      return '#64748b'
    case 'recommendation':
      return '#0f766e'
    default:
      return '#475569'
  }
}

function graphNodeLabel(kind: string) {
  switch (kind) {
    case 'scope':
      return '范围'
    case 'service':
      return '服务'
    case 'span':
      return 'Span'
    case 'log_signature':
      return '日志'
    case 'metric_signal':
      return '指标'
    case 'hypothesis':
      return '假设'
    case 'missing_source':
      return '缺失源'
    case 'recommendation':
      return '建议'
    default:
      return kind
  }
}

function buildScopeSummary(scope?: WorkbenchSessionScope) {
  if (!scope) return '未固定上下文'
  return [scope.clusterId, scope.namespace, scope.workload || scope.service, scope.alertId].filter(Boolean).join(' / ') || '未固定上下文'
}

function isSyntheticSession(item: WorkbenchSession) {
  const title = String(item.title || '').trim().toLowerCase()
  return title === 'new chat' || title === '新对话' || title.startsWith('e2e-')
}

function buildPromptItems(mode: NonNullable<WorkbenchSession['metadata']>['mode']) {
  if (mode === 'root_cause') {
    return [
      { key: 'alert', icon: <ThunderboltOutlined />, label: '分析当前告警根因' },
      { key: 'blast-radius', icon: <RobotOutlined />, label: '给出影响面和最可能触发链路' },
      { key: 'evidence', icon: <EyeOutlined />, label: '整理异常证据并输出结论' },
    ]
  }
  if (mode === 'performance') {
    return [
      { key: 'latency', icon: <ApiOutlined />, label: '分析服务延迟热点' },
      { key: 'capacity', icon: <RadarChartOutlined />, label: '判断容量瓶颈与资源抖动' },
      { key: 'compare', icon: <EyeOutlined />, label: '对比近期波动与基线差异' },
    ]
  }
  if (mode === 'trace') {
    return [
      { key: 'trace-hotspot', icon: <BranchesOutlined />, label: '定位最慢调用链与热点 span' },
      { key: 'upstream', icon: <RobotOutlined />, label: '总结跨服务链路中的关键阻塞点' },
      { key: 'entry', icon: <EyeOutlined />, label: '从入口请求开始追踪异常路径' },
    ]
  }
  if (mode === 'inspection_review') {
    return [
      { key: 'review', icon: <PlayCircleOutlined />, label: '复盘最近一次巡检异常' },
      { key: 'policy', icon: <ToolOutlined />, label: '根据巡检结果生成自动化建议' },
      { key: 'handoff', icon: <RobotOutlined />, label: '把巡检发现转成调查任务' },
    ]
  }
  return [
    { key: 'incident', icon: <ThunderboltOutlined />, label: '汇总当前异常的调查重点' },
    { key: 'logs', icon: <ToolOutlined />, label: '汇总错误签名与日志上下文' },
    { key: 'next', icon: <RobotOutlined />, label: '给出下一轮排查建议' },
  ]
}

function layoutWorkbenchGraph(nodes: WorkbenchFlowNode[], edges: WorkbenchFlowEdge[]) {
  const graph = new dagre.graphlib.Graph()
  graph.setDefaultEdgeLabel(() => ({}))
  graph.setGraph({ rankdir: 'LR', ranksep: 88, nodesep: 28 })

  nodes.forEach((node) => {
    graph.setNode(node.id, { width: GRAPH_NODE_WIDTH, height: GRAPH_NODE_HEIGHT })
  })

  edges.forEach((edge) => {
    graph.setEdge(edge.source, edge.target)
  })

  dagre.layout(graph)

  return nodes.map((node) => {
    const position = graph.node(node.id) ?? { x: GRAPH_NODE_WIDTH / 2, y: GRAPH_NODE_HEIGHT / 2 }
    return {
      ...node,
      position: {
        x: position.x - GRAPH_NODE_WIDTH / 2,
        y: position.y - GRAPH_NODE_HEIGHT / 2,
      },
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    }
  })
}

function WorkbenchGraphNodeCard({ data, selected }: NodeProps<WorkbenchFlowNode>) {
  const accent = graphAccent(data.kind)
  return (
    <div className={`kc-workbench-graph-node ${selected ? 'is-selected' : ''}`}>
      <div
        className="kc-workbench-graph-node__card"
        style={{
          borderColor: selected ? accent : `${accent}44`,
          boxShadow: selected ? `0 0 0 2px ${accent}22` : undefined,
        }}
      >
        <div className="kc-workbench-graph-node__head">
          <span className="kc-workbench-graph-node__kind" style={{ color: accent, background: `${accent}1a` }}>
            {graphNodeLabel(data.kind)}
          </span>
          {data.severity ? <StatusTag value={data.severity} /> : null}
        </div>
        <div className="kc-workbench-graph-node__title">{data.title}</div>
        {data.subtitle ? <div className="kc-workbench-graph-node__subtitle">{data.subtitle}</div> : null}
        {data.sourceRefs?.length ? (
          <div className="kc-workbench-graph-node__refs">
            {data.sourceRefs.slice(0, 2).join(' · ')}
          </div>
        ) : null}
      </div>
    </div>
  )
}

const WORKBENCH_GRAPH_NODE_TYPES = {
  workbenchGraphNode: WorkbenchGraphNodeCard,
} as const

function WorkbenchGraphCanvasInner({
  graph,
  onSelectNode,
}: {
  graph: WorkbenchGraph
  onSelectNode: (nodeId: string | null) => void
}) {
  const nodes = useMemo(() => {
    const rawNodes = (graph.nodes ?? []).map((item) => ({
      id: item.id,
      type: 'workbenchGraphNode' as const,
      position: { x: 0, y: 0 },
      data: {
        ...item,
      } as WorkbenchGraphNode & Record<string, unknown>,
    }))
    const rawEdges = (graph.edges ?? []).map((item) => ({
      id: item.id,
      source: item.source,
      target: item.target,
      type: 'smoothstep' as const,
      data: { relation: item.relation, severity: item.severity },
      markerEnd: {
        type: MarkerType.ArrowClosed,
        color: graphAccent(item.target),
      },
      label: item.relation,
      style: {
        stroke: item.severity === 'critical' ? '#dc2626' : item.severity === 'warning' ? '#d97706' : '#94a3b8',
        strokeWidth: item.relation === 'supports' ? 1.4 : 1.8,
        strokeDasharray: item.relation === 'supports' ? '8 4' : undefined,
      },
      labelStyle: { fontSize: 11, fill: '#475569' },
    }))
    return layoutWorkbenchGraph(rawNodes, rawEdges)
  }, [graph])
  const edges = useMemo(() => (graph.edges ?? []).map((item) => ({
    id: item.id,
    source: item.source,
    target: item.target,
    type: 'smoothstep' as const,
    data: { relation: item.relation, severity: item.severity },
    markerEnd: {
      type: MarkerType.ArrowClosed,
      color: graphAccent(item.target),
    },
    label: item.relation,
    style: {
      stroke: item.severity === 'critical' ? '#dc2626' : item.severity === 'warning' ? '#d97706' : '#94a3b8',
      strokeWidth: item.relation === 'supports' ? 1.4 : 1.8,
      strokeDasharray: item.relation === 'supports' ? '8 4' : undefined,
    },
    labelStyle: { fontSize: 11, fill: '#475569' },
  })), [graph.edges])

  return (
    <div className="kc-workbench-graph-canvas">
      <ReactFlow<WorkbenchFlowNode, WorkbenchFlowEdge>
        nodes={nodes}
        edges={edges}
        nodeTypes={WORKBENCH_GRAPH_NODE_TYPES}
        fitView
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable
        edgesFocusable={false}
        proOptions={{ hideAttribution: true }}
        onPaneClick={() => onSelectNode(null)}
        onNodeClick={(_, node) => onSelectNode(node.id)}
      >
        <Background gap={18} size={1} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  )
}

function WorkbenchGraphCanvas({
  fitKey,
  graph,
  onSelectNode,
}: {
  fitKey: string
  graph: WorkbenchGraph
  onSelectNode: (nodeId: string | null) => void
}) {
  return (
    <ReactFlowProvider>
      <WorkbenchGraphCanvasInner key={fitKey} graph={graph} onSelectNode={onSelectNode} />
    </ReactFlowProvider>
  )
}

export function AIWorkbenchPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canUseChat = hasPermission(permissionSnapshotQuery.data?.data, 'observe.ai.chat')
  const autoSessionScopeKeyRef = useRef<string>('')

  const requestedSessionId = searchParams.get('session') || undefined
  const pathMode = useMemo<NonNullable<WorkbenchSession['metadata']>['mode']>(() => {
    if (location.pathname === '/ai-workbench/root-cause') return 'root_cause'
    if (location.pathname === '/ai-workbench/performance') return 'performance'
    return 'general'
  }, [location.pathname])
  const initialMode = (searchParams.get('mode') as NonNullable<WorkbenchSession['metadata']>['mode']) || pathMode
  const draftScope = useMemo<WorkbenchSessionScope>(() => ({
    clusterId: searchParams.get('clusterId') || undefined,
    namespace: searchParams.get('namespace') || undefined,
    workload: searchParams.get('workload') || undefined,
    alertId: searchParams.get('alertId') || undefined,
    timeRangeMinutes: Number(searchParams.get('timeRangeMinutes') || 60) || 60,
  }), [searchParams])

  const [renameOpen, setRenameOpen] = useState(false)
  const [renameValue, setRenameValue] = useState('')
  const [renameTargetId, setRenameTargetId] = useState<string>()
  const [thinkingOpen, setThinkingOpen] = useState(false)
  const [toolsetOpen, setToolsetOpen] = useState(false)
  const [inspectorOpen, setInspectorOpen] = useState(false)
  const [inspectorView, setInspectorView] = useState<InspectorView>('context')
  const [draftMode, setDraftMode] = useState<NonNullable<WorkbenchSession['metadata']>['mode']>(initialMode)
  const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([])
  const [selectedAdapterIds, setSelectedAdapterIds] = useState<string[]>([])
  const [disabledToolNames, setDisabledToolNames] = useState<string[]>([])
  const [budgetOverrides, setBudgetOverrides] = useState<Record<string, number>>({})
  const [scopeOverrides, setScopeOverrides] = useState<Record<string, string>>({})
  const [skillsDisclosureExpanded, setSkillsDisclosureExpanded] = useState<Record<string, boolean>>({})
  const [showAllSkills, setShowAllSkills] = useState(false)

  const updateSearchParams = (patch: Record<string, string | undefined>) => {
    const next = new URLSearchParams(searchParams)
    for (const [key, value] of Object.entries(patch)) {
      if (!value) {
        next.delete(key)
      } else {
        next.set(key, value)
      }
    }
    setSearchParams(next)
  }

  const sessionsQuery = useQuery({
    queryKey: ['copilot-workbench-sessions'],
    queryFn: () => api.get<ApiResponse<WorkbenchSession[]>>('/copilot/sessions'),
  })
  const adaptersQuery = useQuery({
    queryKey: ['copilot-workbench-adapters'],
    queryFn: () => api.get<ApiResponse<WorkbenchAdapter[]>>('/copilot/data-source-capabilities'),
  })
  const settingsQuery = useQuery({
    queryKey: ['copilot-workbench-settings-ai'],
    queryFn: () => api.get<ApiResponse<{ skillsRegistry?: Array<{ id: string; name: string; description?: string; enabled: boolean; scopes?: string[]; capabilityRefs?: string[]; scopeRules?: string[]; category?: string }> }>>('/settings/ai'),
  })
  const dataSourcesQuery = useQuery({
    queryKey: ['copilot-workbench-datasources'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string; sourceKind: string; backendType: string; enabled: boolean; mcpAdapter: string; validationStatus?: string; validationMessage?: string }>>>('/copilot/data-sources'),
  })
  const sessionDetailQuery = useQuery({
    queryKey: ['copilot-workbench-session-detail', requestedSessionId],
    queryFn: () => api.get<ApiResponse<WorkbenchSession>>(`/copilot/sessions/${requestedSessionId}`),
    enabled: Boolean(requestedSessionId),
  })
  const messagesQuery = useQuery({
    queryKey: ['copilot-workbench-messages', requestedSessionId],
    queryFn: () => api.get<ApiResponse<WorkbenchMessage[]>>(`/copilot/sessions/${requestedSessionId}/messages`),
    enabled: Boolean(requestedSessionId),
  })

  const visibleSessions = useMemo(() => (sessionsQuery.data?.data ?? []).filter((item) => !isSyntheticSession(item)), [sessionsQuery.data?.data])
  const currentSession = (sessionDetailQuery.data?.data && !isSyntheticSession(sessionDetailQuery.data.data) ? sessionDetailQuery.data.data : undefined)
    ?? visibleSessions.find((item) => item.id === requestedSessionId)
  const messages = messagesQuery.data?.data ?? []
  const globalSkills = settingsQuery.data?.data?.skillsRegistry ?? []

  useEffect(() => {
    if (!requestedSessionId && visibleSessions[0]?.id) {
      updateSearchParams({ session: visibleSessions[0].id })
    }
  }, [requestedSessionId, searchParams, setSearchParams, visibleSessions])

  useEffect(() => {
    if (currentSession?.metadata?.mode) {
      setDraftMode(currentSession.metadata.mode)
    }
  }, [currentSession?.id, currentSession?.metadata?.mode])

  const patchSessionMutation = useMutation({
    mutationFn: (payload: { sessionId: string; body: Record<string, unknown> }) =>
      api.patch<ApiResponse<WorkbenchSession>>(`/copilot/sessions/${payload.sessionId}`, payload.body),
    onSuccess: async (_response, payload) => {
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-session-detail', payload.sessionId] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const createSessionMutation = useMutation({
    mutationFn: (payload?: { title?: string; scope?: WorkbenchSessionScope }) => api.post<ApiResponse<WorkbenchSession>>('/copilot/sessions', {
      title: payload?.title || '',
      mode: draftMode,
      scope: payload?.scope || draftScope,
      tags: [],
    }),
    onSuccess: async (response) => {
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      updateSearchParams({ session: response.data.id, mode: draftMode === 'general' ? undefined : draftMode })
      void message.success('已创建调查会话')
    },
    onError: (err: Error) => void message.error(err.message),
  })

  useEffect(() => {
    const scopeKey = JSON.stringify(draftScope)
    const hasScopedEntry = Boolean(draftScope.alertId || draftScope.clusterId || draftScope.namespace || draftScope.workload)
    if (!hasScopedEntry || requestedSessionId || !canUseChat || createSessionMutation.isPending || autoSessionScopeKeyRef.current === scopeKey) {
      return
    }
    autoSessionScopeKeyRef.current = scopeKey
    createSessionMutation.mutate({
      title: draftScope.alertId ? `Alert ${draftScope.alertId}` : draftScope.workload ? `${draftScope.workload} 调查` : '新的调查会话',
      scope: draftScope,
    })
  }, [canUseChat, createSessionMutation, draftScope, requestedSessionId])

  const deleteSessionMutation = useMutation({
    mutationFn: (sessionId: string) => api.delete(`/copilot/sessions/${sessionId}`),
    onSuccess: async (_response, sessionId) => {
      if (requestedSessionId === sessionId) {
        updateSearchParams({ session: undefined })
      }
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      void message.success('会话已归档')
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const sendMessageMutation = useMutation({
    mutationFn: (content: string) =>
      api.post<ApiResponse<WorkbenchMessageEnvelope>>(`/copilot/sessions/${requestedSessionId}/messages`, { content }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-messages', requestedSessionId] })
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-session-detail', requestedSessionId] })
      setThinkingOpen(true)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const analyzeSessionMutation = useMutation({
    mutationFn: () => api.post<ApiResponse<WorkbenchMessageEnvelope>>(`/copilot/sessions/${requestedSessionId}/analyze`, {
      mode: currentSession?.metadata?.mode || draftMode,
      question: currentSession?.metadata?.summary || 'Run analysis for current session scope',
      scope: currentSession?.metadata?.scope || {},
    }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-messages', requestedSessionId] })
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-session-detail', requestedSessionId] })
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      setThinkingOpen(true)
      void message.success('已触发显式分析')
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const createInspectionFromSessionMutation = useMutation({
    mutationFn: () => api.post(`/copilot/sessions/${requestedSessionId}/inspection-task`, {
      title: `${currentSession?.title || '调查'} 巡检模板`,
      scopeType: currentSession?.metadata?.scope?.namespace ? 'namespace' : currentSession?.metadata?.scope?.clusterId ? 'cluster' : 'platform',
      clusterId: currentSession?.metadata?.scope?.clusterId,
      namespace: currentSession?.metadata?.scope?.namespace,
      checks: ['cluster_health', 'alert_pressure', 'audit_denials'],
      enabled: true,
      intervalMinutes: 30,
      metadata: {},
    }),
    onSuccess: () => void message.success('已从当前会话生成巡检任务'),
    onError: (err: Error) => void message.error(err.message),
  })

  useEffect(() => {
    setSelectedSkillIds(currentSession?.metadata?.toolset?.enabledSkillIds ?? [])
    setSelectedAdapterIds(currentSession?.metadata?.toolset?.enabledAdapterIds ?? [])
    setDisabledToolNames(currentSession?.metadata?.toolset?.disabledToolNames ?? [])
    setBudgetOverrides((currentSession?.metadata?.toolset?.budgetOverrides as Record<string, number> | undefined) ?? {})
    setScopeOverrides((currentSession?.metadata?.toolset?.scopeOverrides as Record<string, string> | undefined) ?? {})
  }, [
    currentSession?.id,
    currentSession?.metadata?.toolset?.enabledSkillIds,
    currentSession?.metadata?.toolset?.enabledAdapterIds,
    currentSession?.metadata?.toolset?.disabledToolNames,
    currentSession?.metadata?.toolset?.budgetOverrides,
    currentSession?.metadata?.toolset?.scopeOverrides,
  ])

  const artifacts = useMemo(() => {
    const lastAssistant = [...messages].reverse().find((item) => item.role === 'assistant')
    const raw = lastAssistant?.metadata?.analysisArtifacts
    return Array.isArray(raw) ? raw as WorkbenchArtifact[] : []
  }, [messages])

  const toolCalls = useMemo(() => {
    const lastAssistant = [...messages].reverse().find((item) => item.role === 'assistant')
    const raw = lastAssistant?.metadata?.analysisArtifacts
    if (!Array.isArray(raw)) return []
    return (raw as WorkbenchArtifact[]).flatMap((item) => item.toolExecutions ?? [])
  }, [messages])

  const activeArtifact = artifacts[0]
  const activeGraph = activeArtifact?.graph
  const [selectedGraphNodeId, setSelectedGraphNodeId] = useState<string | null>(null)
  const queryError = sessionsQuery.error || sessionDetailQuery.error || messagesQuery.error || adaptersQuery.error || dataSourcesQuery.error || settingsQuery.error
  const promptItems = buildPromptItems(currentSession?.metadata?.mode || draftMode)
  const conversationItems = useMemo(() => visibleSessions.map((item) => ({
    key: item.id,
    icon: modeIcon(item.metadata?.mode),
    label: (
      <div className="kc-ai-workbench__conversation-label">
        <span className="kc-ai-workbench__conversation-label-title">{item.title}</span>
        <span className="kc-ai-workbench__conversation-label-meta">
          {modeLabel(item.metadata?.mode)} · {formatSessionTimestamp(item.updatedAt)}
        </span>
        <span className="kc-ai-workbench__conversation-label-scope">{buildScopeSummary(item.metadata?.scope)}</span>
      </div>
    ),
  })), [visibleSessions])
  const artifactSummary = [
    {
      key: 'context' as const,
      label: '上下文',
      value: currentSession?.metadata?.analysisRunRefs?.length ?? 0,
      description: buildScopeSummary(currentSession?.metadata?.scope),
      icon: <EyeOutlined />,
    },
    {
      key: 'evidence' as const,
      label: '证据',
      value: activeArtifact?.evidence?.length ?? 0,
      description: activeArtifact?.summary || '还没有提取证据摘要',
      icon: <RadarChartOutlined />,
    },
    {
      key: 'hypotheses' as const,
      label: '假设',
      value: activeArtifact?.hypotheses?.length ?? 0,
      description: activeArtifact?.hypotheses?.[0]?.summary || '还没有形成假设',
      icon: <RobotOutlined />,
    },
    {
      key: 'actions' as const,
      label: '建议',
      value: activeArtifact?.recommendations?.length ?? 0,
      description: activeArtifact?.recommendations?.[0] || '还没有建议动作',
      icon: <ToolOutlined />,
    },
  ]
  const enabledDataSources = (dataSourcesQuery.data?.data ?? []).filter((item) => item.enabled)
  const selectedSkillNames = globalSkills.filter((item) => selectedSkillIds.includes(item.id)).map((item) => item.name)
  const activeMode = currentSession?.metadata?.mode || draftMode
  const enabledSkills = globalSkills.filter((item) => item.enabled)
  const skillRelevanceTokens = useMemo(() => {
    if (activeMode === 'root_cause') return ['logs', 'metrics', 'traces', 'events', 'alerts']
    if (activeMode === 'performance') return ['metrics', 'traces', 'capacity', 'latency']
    if (activeMode === 'trace') return ['traces', 'logs', 'spans', 'service']
    if (activeMode === 'inspection_review') return ['inspection', 'automation', 'policy', 'events']
    return ['logs', 'metrics', 'traces', 'events', 'platform']
  }, [activeMode])
  const rankedSkills = useMemo(() => {
    const scoreSkill = (skill: typeof enabledSkills[number]) => {
      const haystack = [
        skill.name,
        skill.description,
        ...(skill.scopes ?? []),
        ...(skill.capabilityRefs ?? []),
        ...(skill.scopeRules ?? []),
        skill.category,
      ].join(' ').toLowerCase()
      const relevance = skillRelevanceTokens.reduce((score, token) => score + (haystack.includes(token) ? 2 : 0), 0)
      const selected = selectedSkillIds.includes(skill.id) ? 6 : 0
      return relevance + selected
    }
    return [...enabledSkills].sort((left, right) => scoreSkill(right) - scoreSkill(left) || left.name.localeCompare(right.name))
  }, [enabledSkills, selectedSkillIds, skillRelevanceTokens])
  const primarySkills = rankedSkills.slice(0, showAllSkills ? rankedSkills.length : 3)
  const hiddenSkillCount = Math.max(rankedSkills.length - 3, 0)
  const selectedGraphNode = useMemo(
    () => activeGraph?.nodes?.find((item) => item.id === selectedGraphNodeId) ?? null,
    [activeGraph?.nodes, selectedGraphNodeId],
  )
  const graphFitKey = useMemo(
    () => `${activeGraph?.nodes?.map((item) => item.id).join(',') || ''}::${activeGraph?.edges?.map((item) => item.id).join(',') || ''}`,
    [activeGraph?.edges, activeGraph?.nodes],
  )

  useEffect(() => {
    setSelectedGraphNodeId(activeGraph?.focusNodeId ?? activeGraph?.nodes?.[0]?.id ?? null)
  }, [activeGraph?.focusNodeId, activeGraph?.nodes])

  const handleModeChange = (value: string | number) => {
    const next = value as NonNullable<WorkbenchSession['metadata']>['mode']
    setDraftMode(next)
    const nextSearch = new URLSearchParams(searchParams)
    if (next === 'general') {
      nextSearch.delete('mode')
    } else {
      nextSearch.set('mode', String(next))
    }
    const nextPath = next === 'root_cause'
      ? '/ai-workbench/root-cause'
      : next === 'performance'
        ? '/ai-workbench/performance'
        : '/ai-workbench/chat'
    navigate({
      pathname: nextPath,
      search: nextSearch.toString() ? `?${nextSearch.toString()}` : '',
    })
    if (currentSession && currentSession.metadata?.mode !== next) {
      patchSessionMutation.mutate({ sessionId: currentSession.id, body: { mode: next } })
    }
  }
  const openInspector = (view: InspectorView) => {
    setInspectorView(view)
    setInspectorOpen(true)
  }

  const renderInspectorBody = () => {
    if (!currentSession && inspectorView === 'context') {
      return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无会话" />
    }

    if (inspectorView === 'context') {
      return currentSession ? (
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Card size="small" title="调查范围">
            <Paragraph style={{ marginBottom: 0 }}>{buildScopeSummary(currentSession.metadata?.scope)}</Paragraph>
            {currentSession.metadata?.scope?.alertId ? (
              <Button style={{ marginTop: 12 }} size="small" onClick={() => navigate(`/monitoring-workbench/alerts/${currentSession.metadata?.scope?.alertId}`)}>
                查看原始告警详情
              </Button>
            ) : null}
          </Card>
          <Card size="small" title="分析运行">
            {(currentSession.metadata?.analysisRunRefs ?? []).length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有运行记录" /> : (
              <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                {(currentSession.metadata?.analysisRunRefs ?? []).map((item) => (
                  <Flex key={item.id} justify="space-between">
                    <Text>{item.kind}</Text>
                    <StatusTag value={item.status || 'completed'} />
                  </Flex>
                ))}
              </Space>
            )}
          </Card>
        </Space>
      ) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无会话" />
    }

    if (inspectorView === 'evidence') {
      return activeArtifact ? (
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          {(activeArtifact.evidence ?? []).length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无证据" /> : (
            (activeArtifact.evidence ?? []).map((item) => (
              <Card key={item.id} size="small">
                <Flex justify="space-between">
                  <Text strong>{item.title}</Text>
                  {item.severity ? <StatusTag value={item.severity} /> : null}
                </Flex>
                <Paragraph type="secondary" style={{ margin: '8px 0 0' }}>{item.summary}</Paragraph>
              </Card>
            ))
          )}
        </Space>
      ) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无分析工件" />
    }

    if (inspectorView === 'hypotheses') {
      return activeArtifact ? (
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          {(activeArtifact.hypotheses ?? []).length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无假设" /> : (
            (activeArtifact.hypotheses ?? []).map((item) => (
              <Card key={item.id} size="small">
                <Flex justify="space-between">
                  <Text strong>{item.title}</Text>
                  <Tag color="gold">{item.confidence}%</Tag>
                </Flex>
                <Paragraph type="secondary" style={{ margin: '8px 0 0' }}>{item.summary}</Paragraph>
              </Card>
            ))
          )}
        </Space>
      ) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无假设" />
    }

    return activeArtifact ? (
      <Space orientation="vertical" size={8} style={{ width: '100%' }}>
        {(activeArtifact.recommendations ?? []).length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无建议动作" /> : (
          (activeArtifact.recommendations ?? []).map((item) => (
            <Card key={item} size="small">
              <Paragraph style={{ marginBottom: 0 }}>{item}</Paragraph>
            </Card>
          ))
        )}
      </Space>
    ) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无建议" />
  }

  return (
    <div className="kc-page kc-ai-workbench-page">
      <div className="kc-ai-workbench">
        {!canUseChat ? (
          <Alert
            type="warning"
            showIcon
            message="当前账号缺少 observe.ai.chat 权限，无法发送消息或创建会话。"
          />
        ) : null}

        {queryError ? (
          <Alert
            type="error"
            showIcon
            message="工作台数据加载失败"
            description={queryError instanceof Error ? queryError.message : '请检查当前 API 服务和权限快照。'}
          />
        ) : null}

        <section className="kc-ai-workbench__workspace">
          <aside className="kc-ai-workbench-sidebar">
            <div className="kc-ai-workbench__tools-header">
              <div className="kc-ai-workbench__tools-title">
                <span className="kc-ai-workbench__tools-icon">{modeIcon(activeMode)}</span>
                <span>
                  <Text strong>会话记录</Text>
                  <Text type="secondary">{visibleSessions.length > 0 ? `${visibleSessions.length} 个调查会话` : '从这里切换当前调查'}</Text>
                </span>
              </div>
              <Button size="small" type="text" onClick={() => navigate('/ai-workbench/model-settings')}>
                模型设置
              </Button>
            </div>

            <Conversations
              items={conversationItems}
              activeKey={currentSession?.id}
              onActiveChange={(value) => updateSearchParams({ session: String(value) })}
              className="kc-ai-workbench__conversations"
              creation={{
                icon: <EditOutlined />,
                label: '新建会话',
                onClick: () => createSessionMutation.mutate({ scope: draftScope }),
                disabled: !canUseChat || createSessionMutation.isPending,
              }}
            />

            <div className="kc-ai-workbench-sidebar__footer">
              <Button block onClick={() => navigate('/ai-workbench/automation')}>
                巡检与自动化
              </Button>
              <Button block onClick={() => setToolsetOpen(true)}>
                工具装配
              </Button>
            </div>
          </aside>

          <main className="kc-ai-workbench__canvas">
            <div className="kc-ai-workbench__function-bar">
              <div className="kc-ai-workbench__function-main">
                <div className="kc-ai-workbench__function-copy">
                  <Text type="secondary">调查模式</Text>
                  <Title level={5} style={{ margin: 0 }}>{modeLabel(activeMode)}</Title>
                  <Paragraph style={{ marginBottom: 0 }} type="secondary">
                    {modeDescription(activeMode)}
                  </Paragraph>
                </div>
                <Segmented
                  value={activeMode}
                  options={WORKBENCH_MODE_OPTIONS.map((item) => ({ value: item.value, label: item.label }))}
                  onChange={handleModeChange}
                />
              </div>
              <Space wrap className="kc-ai-workbench__function-tabs">
                <Button icon={<ToolOutlined />} onClick={() => setToolsetOpen(true)}>
                  工具装配
                </Button>
                <Button icon={<PlayCircleOutlined />} onClick={() => navigate('/ai-workbench/model-settings')}>
                  模型设置
                </Button>
                <Button type="primary" icon={<EditOutlined />} loading={createSessionMutation.isPending} onClick={() => createSessionMutation.mutate({ scope: draftScope })} disabled={!canUseChat}>
                  新建会话
                </Button>
              </Space>
            </div>

            <div className="kc-ai-workbench__dialog-shell">
              {!currentSession ? (
                <div className="kc-ai-workbench__empty-state">
                  <Welcome
                    icon={<ExperimentOutlined />}
                    title={visibleSessions.length > 0 ? '正在准备会话' : '开始一轮调查'}
                    description={visibleSessions.length > 0 ? '正在同步当前会话，请稍候。' : '从左侧菜单的会话记录选择既有调查，或直接新建一个会话开始排障。'}
                    extra={
                      <Space wrap>
                        <Button type="primary" loading={createSessionMutation.isPending} onClick={() => createSessionMutation.mutate({ scope: draftScope })} disabled={!canUseChat}>
                          新建会话
                        </Button>
                        <Button onClick={() => navigate('/ai-workbench/automation')}>查看巡检与自动化</Button>
                        <Button onClick={() => navigate('/ai-workbench/tools')}>查看工具与技能</Button>
                      </Space>
                    }
                  />
                </div>
              ) : (
                <>
                  <div className="kc-ai-workbench__session-card">
                    <Flex justify="space-between" align="start" gap={16} wrap="wrap">
                      <div className="kc-ai-workbench__session-copy">
                        <div className="kc-ai-workbench__session-title-row">
                          <Title level={4} style={{ margin: 0 }}>{currentSession.title}</Title>
                          <Button
                            type="text"
                            size="small"
                            icon={<EditOutlined />}
                            onClick={() => {
                              setRenameTargetId(currentSession.id)
                              setRenameValue(currentSession.title)
                              setRenameOpen(true)
                            }}
                          />
                        </div>
                        <Paragraph className="kc-ai-workbench__session-description">
                          {currentSession.metadata?.summary || modeDescription(currentSession.metadata?.mode)}
                        </Paragraph>
                        <Space size={[8, 8]} wrap>
                          <Tag color="blue">{modeLabel(currentSession.metadata?.mode)}</Tag>
                          <Tag>{buildScopeSummary(currentSession.metadata?.scope)}</Tag>
                          {currentSession.metadata?.analysisRunRefs?.[0]?.status ? <StatusTag value={currentSession.metadata.analysisRunRefs[0].status} /> : null}
                          {(currentSession.metadata?.tags ?? []).map((item) => <Tag key={item}>{item}</Tag>)}
                        </Space>
                      </div>
                      <Space wrap>
                        {currentSession.metadata?.scope?.alertId ? (
                          <Button onClick={() => navigate(`/monitoring-workbench/alerts/${currentSession.metadata?.scope?.alertId}`)}>
                            回到原告警
                          </Button>
                        ) : null}
                        <Button onClick={() => openInspector('context')}>查看上下文</Button>
                        <Button loading={analyzeSessionMutation.isPending} onClick={() => analyzeSessionMutation.mutate()}>
                          显式分析
                        </Button>
                        <Button loading={createInspectionFromSessionMutation.isPending} onClick={() => createInspectionFromSessionMutation.mutate()}>
                          生成巡检任务
                        </Button>
                        <Button icon={<BranchesOutlined />} onClick={() => setThinkingOpen(true)}>
                          分析链路
                        </Button>
                        <Button danger icon={<DeleteOutlined />} onClick={() => deleteSessionMutation.mutate(currentSession.id)}>
                          归档
                        </Button>
                      </Space>
                    </Flex>
                  </div>

                  <div className="kc-ai-workbench__conversation-card">
                    <div className="kc-ai-workbench__conversation-topbar">
                      <div>
                        <Text strong>对话流程</Text>
                        <Paragraph className="kc-ai-workbench__conversation-subtitle">
                          {buildScopeSummary(currentSession.metadata?.scope)} · {messages.length} 条消息
                        </Paragraph>
                      </div>
                      <Space wrap>
                        <Button size="small" onClick={() => openInspector('evidence')}>证据</Button>
                        <Button size="small" onClick={() => openInspector('hypotheses')}>假设</Button>
                        <Button size="small" onClick={() => openInspector('actions')}>建议</Button>
                      </Space>
                    </div>

                    {activeGraph?.nodes?.length ? (
                      <div className="kc-ai-workbench__graph-panel">
                        <div className="kc-ai-workbench__graph-head">
                          <div>
                            <Text strong>根因链路图</Text>
                            <Paragraph className="kc-ai-workbench__conversation-subtitle">
                              把 traces、logs、metrics 与假设收敛成一张会话内动态图。
                            </Paragraph>
                          </div>
                          <Space size={8} wrap>
                            <Tag color="blue">{activeArtifact?.kind || activeMode}</Tag>
                            <Tag>{activeGraph.nodes?.length || 0} 节点</Tag>
                            <Tag>{activeGraph.edges?.length || 0} 连线</Tag>
                          </Space>
                        </div>
                        {!enabledDataSources.some((item) => ['logs', 'metrics', 'traces'].includes(item.sourceKind)) ? (
                          <Alert
                            type="info"
                            showIcon
                            message="当前还没有可用的 logs / metrics / traces 数据源"
                            description="现在展示的是会话范围根节点。配置 Elasticsearch/Loki、Prometheus、Jaeger 之后，根因图会自动扩展成错误链路、日志签名和指标挂件。"
                          />
                        ) : null}
                        <div className="kc-ai-workbench__graph-layout">
                          <WorkbenchGraphCanvas
                            fitKey={graphFitKey}
                            graph={activeGraph}
                            onSelectNode={setSelectedGraphNodeId}
                          />
                          <div className="kc-workbench-graph-selection">
                            {selectedGraphNode ? (
                              <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                                <div>
                                  <Space size={[8, 8]} wrap>
                                    <Text strong>{selectedGraphNode.title}</Text>
                                    <Tag>{graphNodeLabel(selectedGraphNode.kind)}</Tag>
                                    {selectedGraphNode.severity ? <StatusTag value={selectedGraphNode.severity} /> : null}
                                  </Space>
                                  {selectedGraphNode.subtitle ? (
                                    <Paragraph type="secondary" style={{ margin: '8px 0 0' }}>
                                      {selectedGraphNode.subtitle}
                                    </Paragraph>
                                  ) : null}
                                </div>
                                {selectedGraphNode.sourceRefs?.length ? (
                                  <div className="kc-ai-workbench__tool-chip-list">
                                    {selectedGraphNode.sourceRefs.map((item) => <Tag key={`${selectedGraphNode.id}-${item}`}>{item}</Tag>)}
                                  </div>
                                ) : null}
                                {selectedGraphNode.kind === 'missing_source' ? (
                                  <Alert
                                    type="info"
                                    showIcon
                                    message="当前会话缺少这类观测源"
                                    description="先到“工具与技能”或“模型设置 / 数据源配置”里补上对应连接，再重新执行显式分析。"
                                  />
                                ) : null}
                                {selectedGraphNode.kind === 'recommendation' ? (
                                  <Alert
                                    type="success"
                                    showIcon
                                    message="建议的下一步动作"
                                    description={selectedGraphNode.subtitle || '优先缩小 scope，再重新分析。'}
                                  />
                                ) : null}
                                {selectedGraphNode.evidenceIds?.length ? (
                                  <Card size="small" title="关联证据">
                                    <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                                      {(activeArtifact?.evidence ?? []).filter((item) => selectedGraphNode.evidenceIds?.includes(item.id)).map((item) => (
                                        <div key={item.id}>
                                          <Text strong>{item.title}</Text>
                                          <Paragraph type="secondary" style={{ margin: '4px 0 0' }}>{item.summary}</Paragraph>
                                        </div>
                                      ))}
                                    </Space>
                                  </Card>
                                ) : null}
                                {selectedGraphNode.attributes ? (
                                  <Card size="small" title="节点属性">
                                    <pre className="kc-workbench-graph-json">{JSON.stringify(selectedGraphNode.attributes, null, 2)}</pre>
                                  </Card>
                                ) : null}
                              </Space>
                            ) : (
                              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="点击图中的节点，查看链路明细" />
                            )}
                          </div>
                        </div>
                      </div>
                    ) : null}

                    <div className="kc-ai-workbench__conversation-scroll">
                      {messages.length === 0 ? (
                        <Welcome
                          icon={<ExperimentOutlined />}
                          title="开始一轮调查"
                          description="围绕当前模式发起提问，AI 会把工具调用、证据和建议回流到当前会话。"
                          extra={
                            <Prompts
                              title="建议起手问题"
                              wrap
                              items={promptItems.map((item) => ({
                                key: item.key,
                                label: item.label,
                                description: item.label,
                              }))}
                              onItemClick={({ data }) => sendMessageMutation.mutate(String(data.label))}
                            />
                          }
                        />
                      ) : (
                        <Bubble.List
                          autoScroll
                          items={bubbleItems(messages)}
                          role={{
                            ai: { placement: 'start', avatar: <RobotOutlined />, variant: 'borderless' },
                            user: { placement: 'end', variant: 'filled' },
                            system: { placement: 'start', variant: 'outlined' },
                          }}
                          style={{ flex: 1, overflow: 'auto', paddingRight: 8 }}
                        />
                      )}
                    </div>

                    <Sender
                      placeholder="输入排障问题、分析目标或进一步追问"
                      loading={sendMessageMutation.isPending}
                      disabled={!canUseChat || !currentSession}
                      onSubmit={(value) => {
                        if (!value?.trim()) return
                        sendMessageMutation.mutate(value)
                      }}
                      header={
                        <Prompts
                          wrap
                          items={promptItems}
                          onItemClick={({ data }) => sendMessageMutation.mutate(String(data.label))}
                        />
                      }
                    />
                  </div>
                </>
              )}
            </div>
          </main>
          <aside className="kc-ai-workbench__tools-pane">
            <div className="kc-ai-workbench__tools-header">
              <div className="kc-ai-workbench__tools-title">
                <span className="kc-ai-workbench__tools-icon"><BranchesOutlined /></span>
                <span>
                  <Text strong>调查焦点</Text>
                  <Text type="secondary">把上下文、证据和下一步动作收在右侧。</Text>
                </span>
              </div>
              <Button size="small" type="text" onClick={() => setThinkingOpen(true)}>
                分析链路
              </Button>
            </div>

            <div className="kc-ai-workbench__insight-list">
              {artifactSummary.map((item) => (
                <button key={item.key} className="kc-ai-workbench__insight-item" type="button" onClick={() => openInspector(item.key)}>
                  <span className="kc-ai-workbench__insight-icon">{item.icon}</span>
                  <span className="kc-ai-workbench__insight-copy">
                    <span className="kc-ai-workbench__insight-title">{item.label} · {item.value}</span>
                    <span className="kc-ai-workbench__insight-detail">{item.description}</span>
                  </span>
                </button>
              ))}
            </div>

                    <div className="kc-ai-workbench__tool-section">
                      <div className="kc-ai-workbench__tool-section-title">
                        <Text strong>会话装配</Text>
                        <Button size="small" type="text" onClick={() => setToolsetOpen(true)}>
                          调整
                </Button>
              </div>
              <div className="kc-ai-workbench__tool-stack">
                <div className="kc-ai-workbench__tool-row">
                  <span>
                    <Text strong>已选适配器</Text>
                    <Text type="secondary">{selectedAdapterIds.length > 0 ? selectedAdapterIds.join(', ') : '默认自动选择'}</Text>
                  </span>
                  <Tag>{selectedAdapterIds.length || 'Auto'}</Tag>
                </div>
                <div className="kc-ai-workbench__tool-row">
                  <span>
                    <Text strong>会话技能</Text>
                    <Text type="secondary">{selectedSkillNames.length > 0 ? selectedSkillNames.join(', ') : '沿用全局技能'}</Text>
                  </span>
                  <Tag>{selectedSkillNames.length || globalSkills.filter((item) => item.enabled).length}</Tag>
                </div>
                <div className="kc-ai-workbench__tool-row">
                  <span>
                    <Text strong>活跃数据源</Text>
                    <Text type="secondary">{enabledDataSources.length > 0 ? enabledDataSources.map((item) => item.name).join(', ') : '暂无可用数据源'}</Text>
                  </span>
                  <Tag>{enabledDataSources.length}</Tag>
                </div>
                        </div>
                      </div>

                      <div className="kc-ai-workbench__tool-section">
                        <div className="kc-ai-workbench__tool-section-title">
                          <Text strong>Skills 渐进式披露</Text>
                          {hiddenSkillCount > 0 ? (
                            <Button size="small" type="text" onClick={() => setShowAllSkills((current) => !current)}>
                              {showAllSkills ? '收起扩展' : `展开更多 (${hiddenSkillCount})`}
                            </Button>
                          ) : null}
                        </div>
                        <Paragraph type="secondary" style={{ marginBottom: 0 }}>
                          先展示当前模式最相关、或本会话已经启用的 skills；只有继续展开时才披露能力引用、范围规则和附加技能。
                        </Paragraph>
                        <div className="kc-ai-workbench__tool-stack">
                          {primarySkills.length === 0 ? (
                            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前没有启用的 skills" />
                          ) : primarySkills.map((skill) => {
                            const expanded = Boolean(skillsDisclosureExpanded[skill.id])
                            const selected = selectedSkillIds.includes(skill.id)
                            return (
                              <div key={skill.id} className="kc-ai-workbench__tool-row is-skill">
                                <span>
                                  <Space size={[6, 6]} wrap>
                                    <Text strong>{skill.name}</Text>
                                    {selected ? <StatusTag value="enabled" /> : null}
                                    {skill.category ? <Tag>{skill.category}</Tag> : null}
                                  </Space>
                                  <Text type="secondary">{skill.description || (skill.scopes ?? []).join(', ') || '未填写说明'}</Text>
                                  {expanded ? (
                                    <div className="kc-ai-workbench__tool-chip-list" style={{ marginTop: 8 }}>
                                      {(skill.capabilityRefs ?? []).map((item) => <Tag key={`${skill.id}-cap-${item}`}>{item}</Tag>)}
                                      {(skill.scopeRules ?? []).map((item) => <Tag key={`${skill.id}-scope-${item}`}>{item}</Tag>)}
                                      {(skill.scopes ?? []).map((item) => <Tag key={`${skill.id}-grant-${item}`}>{item}</Tag>)}
                                    </div>
                                  ) : null}
                                </span>
                                <Button
                                  size="small"
                                  onClick={() => setSkillsDisclosureExpanded((current) => ({ ...current, [skill.id]: !expanded }))}
                                >
                                  {expanded ? '收起能力' : '展开能力'}
                                </Button>
                              </div>
                            )
                          })}
                        </div>
                      </div>

                      <div className="kc-ai-workbench__tool-section">
                        <div className="kc-ai-workbench__tool-section-title">
                          <Text strong>快捷动作</Text>
                <Button size="small" type="text" onClick={() => navigate('/ai-workbench/tools')}>
                  工具与技能
                </Button>
              </div>
              <div className="kc-ai-workbench__tool-stack">
                <div className="kc-ai-workbench__tool-row">
                  <span>
                    <Text strong>当前范围</Text>
                    <Text type="secondary">{buildScopeSummary(currentSession?.metadata?.scope)}</Text>
                  </span>
                  <Button size="small" onClick={() => openInspector('context')}>查看</Button>
                </div>
                <div className="kc-ai-workbench__tool-row">
                  <span>
                    <Text strong>显式分析</Text>
                    <Text type="secondary">把当前会话转成一轮结构化分析输出。</Text>
                  </span>
                  <Button size="small" loading={analyzeSessionMutation.isPending} onClick={() => analyzeSessionMutation.mutate()} disabled={!currentSession}>运行</Button>
                </div>
                <div className="kc-ai-workbench__tool-row">
                  <span>
                    <Text strong>生成巡检任务</Text>
                    <Text type="secondary">把会话结论转成后续巡检与自动化入口。</Text>
                  </span>
                  <Button size="small" loading={createInspectionFromSessionMutation.isPending} onClick={() => createInspectionFromSessionMutation.mutate()} disabled={!currentSession}>生成</Button>
                </div>
              </div>
            </div>
          </aside>
        </section>
      </div>

      <Drawer title="分析链路" placement="right" open={thinkingOpen} onClose={() => setThinkingOpen(false)} size="large">
        <ThoughtChain
          items={toolCalls.length === 0 ? [
            { key: 'idle', title: '尚未执行工具', description: '发送消息后，这里会展示工具调用与分析步骤。', status: 'pending' as any },
          ] : toolCalls.map((item) => ({
            key: item.id,
            title: item.toolName,
            description: item.summary || item.adapterId,
            content: item.output ? <pre style={{ whiteSpace: 'pre-wrap', margin: 0 }}>{JSON.stringify(item.output, null, 2)}</pre> : undefined,
            status: item.status === 'success' ? 'success' as any : 'error' as any,
          }))}
        />
      </Drawer>

      <Drawer
        title="调查上下文"
        placement="right"
        open={inspectorOpen}
        onClose={() => setInspectorOpen(false)}
        size="large"
        extra={(
          <Segmented
            size="small"
            value={inspectorView}
            options={[
              { value: 'context', label: '上下文' },
              { value: 'evidence', label: '证据' },
              { value: 'hypotheses', label: '假设' },
              { value: 'actions', label: '建议' },
            ]}
            onChange={(value) => setInspectorView(value as InspectorView)}
          />
        )}
      >
        {renderInspectorBody()}
      </Drawer>

      <Drawer title="会话级工具集" placement="right" open={toolsetOpen} onClose={() => setToolsetOpen(false)} size="large">
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <Card size="small" title="已配置适配器">
            <Space orientation="vertical" size={8} style={{ width: '100%' }}>
              {(adaptersQuery.data?.data ?? []).map((item) => (
                <Flex key={item.id} justify="space-between" align="start" gap={12}>
                  <div>
                    <Text strong>{item.name}</Text>
                    <Paragraph type="secondary" style={{ margin: '4px 0 0' }}>{item.description}</Paragraph>
                  </div>
                  <Tag color={item.requiresConfig ? 'blue' : 'green'}>{item.sourceKind}</Tag>
                </Flex>
              ))}
            </Space>
          </Card>
          <Card size="small" title="全局数据源镜像">
            <Space orientation="vertical" size={8} style={{ width: '100%' }}>
              {(dataSourcesQuery.data?.data ?? []).map((item) => (
                <Flex key={item.id} justify="space-between" align="start" gap={12}>
                  <div>
                    <Text strong>{item.name}</Text>
                    <Paragraph type="secondary" style={{ margin: '4px 0 0' }}>{item.sourceKind} / {item.backendType}</Paragraph>
                  </div>
                  <StatusTag value={item.validationStatus || (item.enabled ? 'enabled' : 'disabled')} />
                </Flex>
              ))}
            </Space>
          </Card>
          <Card size="small" title="当前会话装配">
            {!currentSession ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="先选择会话" /> : (
              <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                <Paragraph type="secondary" style={{ marginBottom: 0 }}>已启用适配器：{(currentSession.metadata?.toolset?.enabledAdapterIds ?? []).join(', ') || '默认自动选择'}</Paragraph>
                <Paragraph type="secondary" style={{ marginBottom: 0 }}>已启用 skills：{(currentSession.metadata?.toolset?.enabledSkillIds ?? []).join(', ') || '暂无'}</Paragraph>
                <Paragraph type="secondary" style={{ marginBottom: 0 }}>全局可用 skills：{globalSkills.filter((item) => item.enabled).map((item) => item.id).join(', ') || '暂无'}</Paragraph>
                <Paragraph type="secondary" style={{ marginBottom: 0 }}>没有 `observe.ai.chat` 权限时，工作台不会允许发送消息；没有 `observe.ai.view` 时，即使会话存在，也不应该依赖总览和运行视图。</Paragraph>
                <Select
                  mode="multiple"
                  placeholder="选择会话级适配器"
                  value={selectedAdapterIds}
                  onChange={(value) => setSelectedAdapterIds(value)}
                  options={(adaptersQuery.data?.data ?? []).map((item) => ({ value: item.id, label: `${item.name} (${item.sourceKind})` }))}
                />
                <Select
                  mode="multiple"
                  placeholder="禁用当前会话中的具体工具"
                  value={disabledToolNames}
                  onChange={(value) => setDisabledToolNames(value)}
                  options={(adaptersQuery.data?.data ?? []).flatMap((item) => (item.tools ?? []).map((tool) => ({
                    value: tool.name,
                    label: `${item.name} / ${tool.name}`,
                  })))}
                />
                <Space wrap>
                  {globalSkills.map((item) => (
                    <Tag.CheckableTag
                      key={item.id}
                      checked={selectedSkillIds.includes(item.id)}
                      onChange={(checked) => {
                        setSelectedSkillIds((current) => checked ? [...new Set([...current, item.id])] : current.filter((id) => id !== item.id))
                      }}
                    >
                      {item.name}
                    </Tag.CheckableTag>
                  ))}
                </Space>
                <Card size="small" title="Budget Overrides">
                  <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                    <Flex justify="space-between" gap={12}>
                      <Text type="secondary">Max Queries</Text>
                      <InputNumber min={0} value={budgetOverrides.maxQueries} onChange={(value) => setBudgetOverrides((current) => ({ ...current, maxQueries: Number(value || 0) }))} />
                    </Flex>
                    <Flex justify="space-between" gap={12}>
                      <Text type="secondary">Timeout Seconds</Text>
                      <InputNumber min={0} value={budgetOverrides.timeoutSeconds} onChange={(value) => setBudgetOverrides((current) => ({ ...current, timeoutSeconds: Number(value || 0) }))} />
                    </Flex>
                    <Flex justify="space-between" gap={12}>
                      <Text type="secondary">Max Log Bytes</Text>
                      <InputNumber min={0} value={budgetOverrides.maxLogBytes} onChange={(value) => setBudgetOverrides((current) => ({ ...current, maxLogBytes: Number(value || 0) }))} />
                    </Flex>
                    <Flex justify="space-between" gap={12}>
                      <Text type="secondary">Max Evidence Items</Text>
                      <InputNumber min={0} value={budgetOverrides.maxEvidenceItems} onChange={(value) => setBudgetOverrides((current) => ({ ...current, maxEvidenceItems: Number(value || 0) }))} />
                    </Flex>
                  </Space>
                </Card>
                <Card size="small" title="Scope Overrides">
                  <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                    <Input placeholder="Override cluster" value={scopeOverrides.clusterId || ''} onChange={(event) => setScopeOverrides((current) => ({ ...current, clusterId: event.target.value }))} />
                    <Input placeholder="Override namespace" value={scopeOverrides.namespace || ''} onChange={(event) => setScopeOverrides((current) => ({ ...current, namespace: event.target.value }))} />
                    <Input placeholder="Override workload/service" value={scopeOverrides.workload || ''} onChange={(event) => setScopeOverrides((current) => ({ ...current, workload: event.target.value }))} />
                    <Input placeholder="Override alert ID" value={scopeOverrides.alertId || ''} onChange={(event) => setScopeOverrides((current) => ({ ...current, alertId: event.target.value }))} />
                    <InputNumber min={0} placeholder="Override time range (minutes)" value={Number(scopeOverrides.timeRangeMinutes || 0) || undefined} onChange={(value) => setScopeOverrides((current) => ({ ...current, timeRangeMinutes: String(Number(value || 0)) }))} />
                  </Space>
                </Card>
                <Button
                  onClick={() => {
                    if (!currentSession) return
                    patchSessionMutation.mutate({
                      sessionId: currentSession.id,
                      body: {
                        toolset: {
                          enabledAdapterIds: selectedAdapterIds,
                          enabledSkillIds: selectedSkillIds,
                          disabledToolNames,
                          budgetOverrides,
                          scopeOverrides,
                        },
                      },
                    })
                    void message.success('已更新会话级工具装配')
                  }}
                >
                  保存会话级装配
                </Button>
                <Button
                  onClick={() => {
                    setSelectedAdapterIds(['platform-native.v1', 'logs.v1', 'metrics.v1', 'traces.v1'])
                    setSelectedSkillIds(globalSkills.filter((item) => item.enabled).map((item) => item.id))
                    setDisabledToolNames([])
                    setBudgetOverrides({})
                    setScopeOverrides({})
                  }}
                >
                  恢复推荐预设
                </Button>
              </Space>
            )}
          </Card>
        </Space>
      </Drawer>

      <Modal
        title="重命名会话"
        open={renameOpen}
        onCancel={() => setRenameOpen(false)}
        onOk={() => {
          if (!renameTargetId) return
          patchSessionMutation.mutate({ sessionId: renameTargetId, body: { title: renameValue } })
          setRenameOpen(false)
        }}
      >
        <Input value={renameValue} onChange={(event) => setRenameValue(event.target.value)} />
      </Modal>
    </div>
  )
}
