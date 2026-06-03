import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ApiOutlined,
  BranchesOutlined,
  DeleteOutlined,
  EditOutlined,
  ExperimentOutlined,
  EyeOutlined,
  LinkOutlined,
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
import './copilot-pages.css'
import dagre from 'dagre'
import {
  Alert,
  App,
  Button,
  Card,
  Drawer,
  Flex,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Tag,
  Typography,
} from 'antd'
import '@xyflow/react/dist/style.css'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import {
  getAIModelSettingsPath,
  getAIOperationsPath,
  getAIToolsPath,
  getAIWorkbenchPathForMode,
  getAIWorkbenchPathForSession,
  normalizeAIWorkbenchMode,
} from './workbench-navigation'
import {
  TOOLSET_BUDGET_FIELDS,
  buildDisabledToolOptions,
  canonicalDisabledToolNames,
  cleanToolsetPayload,
  countObjectKeys,
  numberRecord,
  recommendedAdapterIds,
  scopeOverrideState,
} from './workbench-toolset'
import type {
  WorkbenchArtifact,
  WorkbenchCatalog,
  WorkbenchGraph,
  WorkbenchGraphNode,
  WorkbenchMessage,
  WorkbenchMessageEnvelope,
  WorkbenchSession,
  WorkbenchSessionScope,
} from './workbench-types'

const { Paragraph, Text, Title } = Typography

type InspectorView = 'context' | 'evidence' | 'hypotheses' | 'actions'
type WorkbenchMode = NonNullable<NonNullable<WorkbenchSession['metadata']>['mode']>
type WorkbenchFlowNode = Node<WorkbenchGraphNode & Record<string, unknown>, 'workbenchGraphNode'>
type WorkbenchFlowEdge = Edge<{ relation: string; severity?: string }, 'smoothstep'>
type ThoughtChainStatus = 'loading' | 'success' | 'error' | 'abort'
type WorkbenchArtifactEntry = {
  key: string
  artifact: WorkbenchArtifact
  message: WorkbenchMessage
  index: number
}
type ArtifactLinkKind = 'session' | 'root_cause' | 'inspection' | 'agent'
type ArtifactContextLink = {
  key: string
  kind: ArtifactLinkKind
  label: string
  value: string
  path: string
}

const GRAPH_NODE_WIDTH = 248
const GRAPH_NODE_HEIGHT = 104

const WORKBENCH_MODE_OPTIONS = [
  { value: 'general', label: '通用聊天' },
  { value: 'root_cause', label: '根因分析' },
  { value: 'performance', label: '性能分析' },
  { value: 'trace', label: '链路分析' },
  { value: 'inspection_review', label: '巡检复盘' },
] as const

export const RUNNABLE_ANALYSIS_MODE_OPTIONS = WORKBENCH_MODE_OPTIONS.filter((item) => (
  item.value === 'root_cause' || item.value === 'performance' || item.value === 'trace' || item.value === 'inspection_review'
))

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

function defaultAnalysisQuestion(mode: WorkbenchMode, session?: WorkbenchSession) {
  const summary = session?.metadata?.summary?.trim()
  if (summary) return summary
  switch (mode) {
    case 'root_cause':
      return '请基于当前会话范围执行一次根因分析，输出证据、假设、影响面和下一步动作。'
    case 'performance':
      return '请分析当前会话范围内的性能波动、容量风险和优化建议。'
    case 'trace':
      return '请围绕当前会话范围定位关键链路、热点 span 和可能的下游阻塞点。'
    case 'inspection_review':
      return '请复盘当前巡检发现，整理风险、证据和后续自动化动作。'
    default:
      return '请把当前会话上下文转成结构化分析，输出证据、结论和下一步建议。'
  }
}

function defaultAnalysisProfileIdForMode(
  mode: WorkbenchMode,
  profiles: Array<{ id: string; mode: string; enabled: boolean }>,
) {
  const expectedMode = mode === 'inspection_review' ? 'inspection' : mode
  return profiles.find((item) => item.enabled && item.mode === expectedMode)?.id
    ?? profiles.find((item) => item.enabled)?.id
    ?? ''
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

function artifactTitle(entry: WorkbenchArtifactEntry) {
  return entry.artifact.title || modeLabel(entry.artifact.kind) || entry.artifact.kind
}

function artifactMeta(entry: WorkbenchArtifactEntry) {
  return `${entry.artifact.kind} · ${formatSessionTimestamp(entry.message.createdAt)}`
}

function artifactSnapshotText(snapshot: Record<string, unknown> | undefined, ...keys: string[]) {
  if (!snapshot) return ''
  for (const key of keys) {
    const value = snapshot[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
    if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  }
  return ''
}

function artifactContextLinks(entry: WorkbenchArtifactEntry, session?: WorkbenchSession): ArtifactContextLink[] {
  const snapshot = entry.artifact.dataSourceSnapshot
  const sessionId = artifactSnapshotText(snapshot, 'sessionId') || entry.message.sessionId || session?.id || ''
  const rootCauseRunId = artifactSnapshotText(snapshot, 'rootCauseRunId')
    || session?.metadata?.analysisRunRefs?.find((item) => item.id === entry.artifact.runId && item.kind === 'root_cause')?.id
    || (entry.artifact.kind === 'root_cause' ? entry.artifact.runId : '')
  const inspectionRunId = artifactSnapshotText(snapshot, 'inspectionRunId')
    || session?.metadata?.analysisRunRefs?.find((item) => item.id === entry.artifact.runId && item.kind === 'inspection_review')?.id
    || (entry.artifact.kind === 'inspection_review' && entry.artifact.runId.startsWith('inspection-') ? entry.artifact.runId : '')
  const agentRunId = artifactSnapshotText(snapshot, 'agentRunId', 'agentRuntimeId')
    || artifactSnapshotText(entry.message.metadata, 'agentRunId')
    || (entry.artifact.runId.startsWith('agent:') ? entry.artifact.runId : '')
  const links: ArtifactContextLink[] = []
  if (sessionId) {
    links.push({
      key: 'session',
      kind: 'session',
      label: '会话',
      value: sessionId,
      path: getAIWorkbenchPathForSession({ id: sessionId, metadata: { mode: entry.artifact.kind as WorkbenchMode } }),
    })
  }
  if (rootCauseRunId) {
    links.push({
      key: 'root-cause',
      kind: 'root_cause',
      label: '根因运行',
      value: rootCauseRunId,
      path: getAIWorkbenchPathForMode('root_cause', new URLSearchParams({ session: sessionId || '', rootCauseRunId })),
    })
  }
  if (inspectionRunId) {
    links.push({
      key: 'inspection',
      kind: 'inspection',
      label: '巡检运行',
      value: inspectionRunId,
      path: `/ai-workbench/inspection?view=runs&inspectionRunId=${encodeURIComponent(inspectionRunId)}${sessionId ? `&session=${encodeURIComponent(sessionId)}` : ''}`,
    })
  }
  if (agentRunId) {
    links.push({
      key: 'agent',
      kind: 'agent',
      label: 'Agent Run',
      value: agentRunId,
      path: getAIWorkbenchPathForMode(entry.artifact.kind, new URLSearchParams({ session: sessionId || '', agentRunId })),
    })
  }
  return links
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
    <div className={`soha-workbench-graph-node ${selected ? 'is-selected' : ''}`}>
      <div
        className="soha-workbench-graph-node__card"
        style={{
          borderColor: selected ? accent : `${accent}44`,
          boxShadow: selected ? `0 0 0 2px ${accent}22` : undefined,
        }}
      >
        <div className="soha-workbench-graph-node__head">
          <span className="soha-workbench-graph-node__kind" style={{ color: accent, background: `${accent}1a` }}>
            {graphNodeLabel(data.kind)}
          </span>
          {data.severity ? <StatusTag value={data.severity} /> : null}
        </div>
        <div className="soha-workbench-graph-node__title">{data.title}</div>
        {data.subtitle ? <div className="soha-workbench-graph-node__subtitle">{data.subtitle}</div> : null}
        {data.sourceRefs?.length ? (
          <div className="soha-workbench-graph-node__refs">
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
    <div className="soha-workbench-graph-canvas">
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
  const canManageInspection = hasPermission(permissionSnapshotQuery.data?.data, 'observe.ai.inspection.manage')
  const canCreateInspectionTask = canUseChat && canManageInspection
  const canRunRootCause = hasPermission(permissionSnapshotQuery.data?.data, 'observe.ai.root-cause.run')
  const autoSessionScopeKeyRef = useRef<string>('')
  const routeModePatchKeyRef = useRef<string>('')

  const requestedSessionId = searchParams.get('session') || undefined
  const searchMode = normalizeAIWorkbenchMode(searchParams.get('mode')) || 'general'
  const pathMode = useMemo<WorkbenchMode>(() => {
    if (location.pathname === '/ai-workbench/root-cause') return 'root_cause'
    if (location.pathname === '/ai-workbench/performance') return 'performance'
    if (location.pathname === '/ai-workbench/chat') {
      return searchMode
    }
    return 'general'
  }, [location.pathname, searchMode])
  const isExplicitRouteMode = location.pathname === '/ai-workbench/root-cause'
    || location.pathname === '/ai-workbench/performance'
    || (location.pathname === '/ai-workbench/chat' && searchParams.has('mode'))
  const initialMode = pathMode
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
  const [draftMode, setDraftMode] = useState<WorkbenchMode>(initialMode)
  const [analysisOpen, setAnalysisOpen] = useState(false)
  const [analysisMode, setAnalysisMode] = useState<WorkbenchMode>(initialMode)
  const [analysisQuestion, setAnalysisQuestion] = useState('')
  const [selectedAnalysisProfileId, setSelectedAnalysisProfileId] = useState('')
  const [selectedAgentProviderId, setSelectedAgentProviderId] = useState('internal')
  const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([])
  const [selectedAdapterIds, setSelectedAdapterIds] = useState<string[]>([])
  const [disabledToolNames, setDisabledToolNames] = useState<string[]>([])
  const [budgetOverrides, setBudgetOverrides] = useState<Record<string, number>>({})
  const [scopeOverrides, setScopeOverrides] = useState<Partial<WorkbenchSessionScope>>({})
  const [skillsDisclosureExpanded, setSkillsDisclosureExpanded] = useState<Record<string, boolean>>({})
  const [showAllSkills, setShowAllSkills] = useState(false)
  const [selectedArtifactKey, setSelectedArtifactKey] = useState<string>()
  const [selectedGraphNodeId, setSelectedGraphNodeId] = useState<string | null>(null)

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
  const catalogQuery = useQuery({
    queryKey: ['copilot-workbench-catalog'],
    queryFn: () => api.get<ApiResponse<WorkbenchCatalog>>('/copilot/workbench/catalog'),
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
  const adapters = useMemo(() => catalogQuery.data?.data?.adapters ?? [], [catalogQuery.data?.data?.adapters])
  const dataSources = useMemo(() => catalogQuery.data?.data?.dataSources ?? [], [catalogQuery.data?.data?.dataSources])
  const globalSkills = useMemo(() => catalogQuery.data?.data?.skillsRegistry ?? [], [catalogQuery.data?.data?.skillsRegistry])
  const analysisProfiles = useMemo(() => catalogQuery.data?.data?.analysisProfiles ?? [], [catalogQuery.data?.data?.analysisProfiles])
  const agentProviders = useMemo(() => catalogQuery.data?.data?.agentProviders ?? [], [catalogQuery.data?.data?.agentProviders])
  const agentCapabilities = useMemo(() => catalogQuery.data?.data?.capabilities ?? [], [catalogQuery.data?.data?.capabilities])
  const defaultAgentProviderId = useMemo(() => {
    return agentProviders.find((item) => item.default && item.enabled)?.id
      ?? agentProviders.find((item) => item.enabled)?.id
      ?? 'internal'
  }, [agentProviders])
  const defaultInspectionProfileId = useMemo(() => {
    return analysisProfiles.find((item) => item.enabled && item.mode === 'inspection')?.id
  }, [analysisProfiles])

  useEffect(() => {
    if (!requestedSessionId && visibleSessions[0]?.id) {
      updateSearchParams({ session: visibleSessions[0].id })
    }
  }, [requestedSessionId, searchParams, setSearchParams, visibleSessions])

  useEffect(() => {
    setDraftMode(isExplicitRouteMode ? pathMode : currentSession?.metadata?.mode || pathMode)
  }, [currentSession?.id, currentSession?.metadata?.mode, isExplicitRouteMode, pathMode])

  const patchSessionMutation = useMutation({
    mutationFn: (payload: { sessionId: string; body: Record<string, unknown> }) =>
      api.patch<ApiResponse<WorkbenchSession>>(`/copilot/sessions/${payload.sessionId}`, payload.body),
    onSuccess: async (_response, payload) => {
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-session-detail', payload.sessionId] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  useEffect(() => {
    if (!currentSession || !isExplicitRouteMode || currentSession.metadata?.mode === pathMode) {
      routeModePatchKeyRef.current = ''
      return
    }
    const patchKey = `${currentSession.id}:${pathMode}`
    if (patchSessionMutation.isPending || routeModePatchKeyRef.current === patchKey) {
      return
    }
    routeModePatchKeyRef.current = patchKey
    patchSessionMutation.mutate({ sessionId: currentSession.id, body: { mode: pathMode } })
  }, [
    currentSession?.id,
    currentSession?.metadata?.mode,
    isExplicitRouteMode,
    pathMode,
    patchSessionMutation.isPending,
    patchSessionMutation.mutate,
  ])

  const createSessionMutation = useMutation({
    mutationFn: (payload?: { title?: string; scope?: WorkbenchSessionScope }) => api.post<ApiResponse<WorkbenchSession>>('/copilot/sessions', {
      title: payload?.title || '',
      mode: draftMode,
      agentProviderId: selectedAgentProviderId || defaultAgentProviderId,
      scope: payload?.scope || draftScope,
      tags: [],
    }),
    onSuccess: async (response) => {
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      navigate(getAIWorkbenchPathForMode(draftMode, new URLSearchParams({ session: response.data.id })))
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
    mutationFn: (payload: { sessionId: string; mode: WorkbenchMode; question: string; scope: WorkbenchSessionScope; agentProviderId: string; analysisProfileId?: string }) =>
      api.post<ApiResponse<WorkbenchMessageEnvelope>>(`/copilot/sessions/${payload.sessionId}/analyze`, {
        mode: payload.mode,
        question: payload.question,
        agentProviderId: payload.agentProviderId,
        analysisProfileId: payload.analysisProfileId,
        scope: payload.scope,
      }),
    onSuccess: async (response, payload) => {
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-messages', payload.sessionId] })
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-session-detail', payload.sessionId] })
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      const nextMode = typeof response.data.sessionPatch?.mode === 'string'
        ? response.data.sessionPatch.mode
        : payload.mode
      navigate(getAIWorkbenchPathForMode(nextMode, new URLSearchParams({ session: payload.sessionId })))
      setAnalysisOpen(false)
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
      metadata: {
        ...(defaultInspectionProfileId ? { analysisProfileId: defaultInspectionProfileId } : {}),
      },
    }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['ai-operations-tasks'] })
      void message.success('已从当前会话生成巡检任务')
      navigate(getAIOperationsPath(location.search))
    },
    onError: (err: Error) => void message.error(err.message),
  })

  useEffect(() => {
    setSelectedSkillIds(currentSession?.metadata?.toolset?.enabledSkillIds ?? [])
    setSelectedAdapterIds(currentSession?.metadata?.toolset?.enabledAdapterIds ?? [])
    setDisabledToolNames(canonicalDisabledToolNames(currentSession?.metadata?.toolset?.disabledToolNames ?? [], adapters))
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
    setSelectedAgentProviderId(currentSession?.metadata?.agentProviderId || defaultAgentProviderId)
  }, [currentSession?.id, currentSession?.metadata?.agentProviderId, defaultAgentProviderId])

  const artifactEntries = useMemo<WorkbenchArtifactEntry[]>(() => {
    const entries: WorkbenchArtifactEntry[] = []
    for (const item of [...messages].reverse()) {
      if (item.role !== 'assistant') continue
      const raw = item.metadata?.analysisArtifacts
      if (!Array.isArray(raw)) continue
      ;(raw as WorkbenchArtifact[]).forEach((artifact, index) => {
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

  useEffect(() => {
    if (artifactEntries.length === 0) {
      if (selectedArtifactKey) setSelectedArtifactKey(undefined)
      return
    }
    if (!selectedArtifactKey || !artifactEntries.some((item) => item.key === selectedArtifactKey)) {
      setSelectedArtifactKey(artifactEntries[0].key)
    }
  }, [artifactEntries, selectedArtifactKey])

  const activeArtifactEntry = artifactEntries.find((item) => item.key === selectedArtifactKey) ?? artifactEntries[0]
  const activeArtifact = activeArtifactEntry?.artifact
  const activeArtifactLinks = activeArtifactEntry ? artifactContextLinks(activeArtifactEntry, currentSession) : []
  const toolCalls = activeArtifact?.toolExecutions ?? []
  const activeGraph = activeArtifact?.graph
  const queryError = sessionsQuery.error || sessionDetailQuery.error || messagesQuery.error || catalogQuery.error
  const activeMode = isExplicitRouteMode ? pathMode : currentSession?.metadata?.mode || draftMode
  const promptItems = buildPromptItems(activeMode)
  const conversationItems = useMemo(() => visibleSessions.map((item) => ({
    key: item.id,
    icon: modeIcon(item.metadata?.mode),
    label: (
      <div className="soha-ai-workbench__conversation-label">
        <span className="soha-ai-workbench__conversation-label-title">{item.title}</span>
        <span className="soha-ai-workbench__conversation-label-meta">
          {modeLabel(item.metadata?.mode)} · {formatSessionTimestamp(item.updatedAt)}
        </span>
        <span className="soha-ai-workbench__conversation-label-scope">{buildScopeSummary(item.metadata?.scope)}</span>
      </div>
    ),
  })), [visibleSessions])
  const artifactSummary = [
    {
      key: 'context' as const,
      label: '上下文',
      value: artifactEntries.length,
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
  const enabledDataSources = dataSources.filter((item) => item.enabled)
  const enabledAgentProviders = agentProviders.filter((item) => item.enabled)
  const providerOptions = enabledAgentProviders.map((item) => ({
    value: item.id,
    label: `${item.name}${item.supportsAsync ? ' / async' : ' / inline'}`,
  }))
  const analysisProfileOptions = analysisProfiles
    .filter((item) => item.enabled)
    .map((item) => ({ value: item.id, label: `${item.name} / ${item.mode}` }))
  const selectedAnalysisProfile = analysisProfiles.find((item) => item.id === selectedAnalysisProfileId)
  const activeAgentProvider = agentProviders.find((item) => item.id === selectedAgentProviderId)
    ?? agentProviders.find((item) => item.id === currentSession?.metadata?.agentProviderId)
    ?? agentProviders.find((item) => item.id === defaultAgentProviderId)
  const activeCapability = agentCapabilities.find((item) => (item.analysisKinds ?? []).includes(activeMode) || item.id === activeMode)
  const analysisCapability = agentCapabilities.find((item) => (item.analysisKinds ?? []).includes(analysisMode) || item.id === analysisMode)
  const disabledToolOptions = useMemo(() => buildDisabledToolOptions(adapters), [adapters])
  const cleanedBudgetOverrides = useMemo(() => numberRecord(budgetOverrides), [budgetOverrides])
  const cleanedScopeOverrides = useMemo(() => scopeOverrideState(scopeOverrides), [scopeOverrides])
  const effectiveAdapterIds = selectedAdapterIds.length > 0 ? selectedAdapterIds : adapters.map((item) => item.id)
  const activeDataSourceAdapters = [...new Set(enabledDataSources.map((item) => item.mcpAdapter).filter(Boolean))]
  const unavailableSelectedAdapterIds = selectedAdapterIds.filter((adapterId) => (
    adapterId !== 'platform-native.v1' && !activeDataSourceAdapters.includes(adapterId)
  ))
  const toolsetPolicySummary = [
    {
      label: 'Agent Provider',
      value: activeAgentProvider?.name || selectedAgentProviderId || 'Auto',
      detail: activeAgentProvider?.description || '会话级 provider 决定显式分析由内置分析还是外部 agent runner 执行。',
    },
    {
      label: 'Capability',
      value: activeCapability?.name || activeMode,
      detail: activeCapability?.toolRefs?.length ? activeCapability.toolRefs.join(', ') : '按当前分析模式自动选择 capability 与工具绑定。',
    },
    {
      label: 'Adapter',
      value: selectedAdapterIds.length > 0 ? `${selectedAdapterIds.length} selected` : 'Auto',
      detail: selectedAdapterIds.length > 0 ? selectedAdapterIds.join(', ') : '默认允许已注册 adapter，运行时按数据源可用性选择。',
    },
    {
      label: 'Disabled Tools',
      value: disabledToolNames.length,
      detail: disabledToolNames.length > 0 ? disabledToolNames.join(', ') : '没有屏蔽具体工具。',
    },
    {
      label: 'Budget',
      value: countObjectKeys(cleanedBudgetOverrides),
      detail: countObjectKeys(cleanedBudgetOverrides) > 0 ? Object.entries(cleanedBudgetOverrides).map(([key, value]) => `${key}=${value}`).join(', ') : '沿用 profile 和数据源默认预算。',
    },
    {
      label: 'Scope Override',
      value: countObjectKeys(cleanedScopeOverrides as Record<string, unknown>),
      detail: countObjectKeys(cleanedScopeOverrides as Record<string, unknown>) > 0 ? buildScopeSummary(cleanedScopeOverrides) : '沿用会话固定范围。',
    },
  ]
  const selectedSkillNames = globalSkills.filter((item) => selectedSkillIds.includes(item.id)).map((item) => item.name)
  const canRunExplicitAnalysis = canUseChat && (activeMode !== 'root_cause' || canRunRootCause)
  const explicitAnalysisTitle = canRunExplicitAnalysis
    ? undefined
    : activeMode === 'root_cause'
      ? '缺少 observe.ai.root-cause.run 权限'
      : '缺少 observe.ai.chat 权限'
  const createInspectionTitle = canCreateInspectionTask
    ? undefined
    : !canUseChat
      ? '缺少 observe.ai.chat 权限'
      : '缺少 observe.ai.inspection.manage 权限'
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

  const canSubmitExplicitAnalysis = canUseChat && (analysisMode !== 'root_cause' || canRunRootCause)
  const handleModeChange = (value: string | number) => {
    const next = value as WorkbenchMode
    setDraftMode(next)
    navigate(getAIWorkbenchPathForMode(next, searchParams))
    if (currentSession && currentSession.metadata?.mode !== next) {
      patchSessionMutation.mutate({ sessionId: currentSession.id, body: { mode: next } })
    }
  }
  const openArtifactLink = (link: ArtifactContextLink) => {
    navigate(link.path)
    if (link.kind === 'inspection') {
      return
    }
    if (link.kind === 'root_cause' || link.kind === 'agent') {
      setThinkingOpen(true)
    }
  }
  const openExplicitAnalysis = () => {
    if (!currentSession || !canRunExplicitAnalysis) return
    const nextMode = activeMode
    const runnableMode = RUNNABLE_ANALYSIS_MODE_OPTIONS.some((item) => item.value === nextMode) ? nextMode : 'root_cause'
    setAnalysisMode(runnableMode)
    setSelectedAgentProviderId(currentSession.metadata?.agentProviderId || defaultAgentProviderId)
    setSelectedAnalysisProfileId(defaultAnalysisProfileIdForMode(runnableMode, analysisProfiles))
    setAnalysisQuestion(defaultAnalysisQuestion(runnableMode, currentSession))
    setAnalysisOpen(true)
  }
  const submitExplicitAnalysis = () => {
    if (!currentSession || !canSubmitExplicitAnalysis) return
    const question = analysisQuestion.trim() || defaultAnalysisQuestion(analysisMode, currentSession)
    analyzeSessionMutation.mutate({
      sessionId: currentSession.id,
      mode: analysisMode,
      question,
      scope: currentSession.metadata?.scope || {},
      agentProviderId: selectedAgentProviderId || defaultAgentProviderId,
      analysisProfileId: selectedAnalysisProfileId || undefined,
    })
  }
  const openInspector = (view: InspectorView) => {
    setInspectorView(view)
    setInspectorOpen(true)
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
  const setScopeOverrideNumberValue = (key: keyof WorkbenchSessionScope, value: number | string | null) => {
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
        body: { toolset: payload, agentProviderId: selectedAgentProviderId || defaultAgentProviderId },
      },
      {
        onSuccess: () => void message.success('已更新会话级工具装配'),
      },
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

  const renderInspectorBody = () => {
    if (!currentSession && inspectorView === 'context') {
      return <ManagementState bordered={false} compact kind="select-scope" title="未选择会话" description="选择一个 AI 会话后查看调查范围、证据和建议。" />
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
            {(currentSession.metadata?.analysisRunRefs ?? []).length === 0 ? (
              <ManagementState bordered={false} compact title="暂无运行记录" description="当前会话还没有关联的分析运行。" />
            ) : (
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
      ) : <ManagementState bordered={false} compact kind="select-scope" title="未选择会话" description="选择一个 AI 会话后查看上下文。" />
    }

    if (inspectorView === 'evidence') {
      return activeArtifact ? (
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          {(activeArtifact.evidence ?? []).length === 0 ? (
            <ManagementState bordered={false} compact title="暂无证据" description="当前分析工件还没有结构化证据。" />
          ) : (
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
      ) : <ManagementState bordered={false} compact title="暂无分析工件" description="会话产生分析结果后这里会展示证据。" />
    }

    if (inspectorView === 'hypotheses') {
      return activeArtifact ? (
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          {(activeArtifact.hypotheses ?? []).length === 0 ? (
            <ManagementState bordered={false} compact title="暂无假设" description="当前分析工件还没有形成候选根因。" />
          ) : (
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
      ) : <ManagementState bordered={false} compact title="暂无假设" description="选择一个分析工件后查看候选根因。" />
    }

    return activeArtifact ? (
      <Space orientation="vertical" size={8} style={{ width: '100%' }}>
        {(activeArtifact.recommendations ?? []).length === 0 ? (
          <ManagementState bordered={false} compact title="暂无建议动作" description="当前分析工件还没有生成下一步操作。" />
        ) : (
          (activeArtifact.recommendations ?? []).map((item) => (
            <Card key={item} size="small">
              <Paragraph style={{ marginBottom: 0 }}>{item}</Paragraph>
            </Card>
          ))
        )}
      </Space>
    ) : <ManagementState bordered={false} compact title="暂无建议" description="选择一个分析工件后查看建议动作。" />
  }

  return (
    <div className="soha-page soha-ai-workbench-page">
      <div className="soha-ai-workbench">
        {!canUseChat ? (
          <Alert
            type="warning"
            showIcon
            title="当前账号缺少 observe.ai.chat 权限，无法发送消息或创建会话。"
          />
        ) : null}

        {queryError ? (
          <Alert
            type="error"
            showIcon
            title="工作台数据加载失败"
            description={queryError instanceof Error ? queryError.message : '请检查当前 API 服务和权限快照。'}
          />
        ) : null}

        <section className="soha-ai-workbench__workspace">
          <aside className="soha-ai-workbench-sidebar">
            <div className="soha-ai-workbench__tools-header">
              <div className="soha-ai-workbench__tools-title">
                <span className="soha-ai-workbench__tools-icon">{modeIcon(activeMode)}</span>
                <span>
                  <Text strong>会话记录</Text>
                  <Text type="secondary">{visibleSessions.length > 0 ? `${visibleSessions.length} 个调查会话` : '从这里切换当前调查'}</Text>
                </span>
              </div>
              <Button size="small" type="text" onClick={() => navigate(getAIModelSettingsPath(location.search))}>
                模型设置
              </Button>
            </div>

            <Conversations
              items={conversationItems}
              activeKey={currentSession?.id}
              onActiveChange={(value) => updateSearchParams({ session: String(value) })}
              className="soha-ai-workbench__conversations"
              creation={{
                icon: <EditOutlined />,
                label: '新建会话',
                onClick: () => createSessionMutation.mutate({ scope: draftScope }),
                disabled: !canUseChat || createSessionMutation.isPending,
              }}
            />

            <div className="soha-ai-workbench-sidebar__footer">
              <Button block onClick={() => navigate(getAIOperationsPath(location.search))}>
                巡检与自动化
              </Button>
              <Button block onClick={() => setToolsetOpen(true)}>
                工具装配
              </Button>
            </div>
          </aside>

          <main className="soha-ai-workbench__canvas">
            <div className="soha-ai-workbench__function-bar">
              <div className="soha-ai-workbench__function-main">
                <div className="soha-ai-workbench__function-copy">
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
              <Space wrap className="soha-ai-workbench__function-tabs">
                <Button icon={<ToolOutlined />} onClick={() => setToolsetOpen(true)}>
                  工具装配
                </Button>
                <Button icon={<PlayCircleOutlined />} onClick={() => navigate(getAIModelSettingsPath(location.search))}>
                  模型设置
                </Button>
                <Button type="primary" icon={<EditOutlined />} loading={createSessionMutation.isPending} onClick={() => createSessionMutation.mutate({ scope: draftScope })} disabled={!canUseChat}>
                  新建会话
                </Button>
              </Space>
            </div>

            <div className="soha-ai-workbench__dialog-shell">
              {!currentSession ? (
                <div className="soha-ai-workbench__empty-state">
                  <Welcome
                    icon={<ExperimentOutlined />}
                    title={visibleSessions.length > 0 ? '正在准备会话' : '开始一轮调查'}
                    description={visibleSessions.length > 0 ? '正在同步当前会话，请稍候。' : '从左侧菜单的会话记录选择既有调查，或直接新建一个会话开始排障。'}
                    extra={
                      <Space wrap>
                        <Button type="primary" loading={createSessionMutation.isPending} onClick={() => createSessionMutation.mutate({ scope: draftScope })} disabled={!canUseChat}>
                          新建会话
                        </Button>
                        <Button onClick={() => navigate(getAIOperationsPath(location.search))}>查看巡检与自动化</Button>
                        <Button onClick={() => navigate(getAIToolsPath(location.search))}>查看工具与技能</Button>
                      </Space>
                    }
                  />
                </div>
              ) : (
                <>
                  <div className="soha-ai-workbench__session-card">
                    <Flex justify="space-between" align="start" gap={16} wrap="wrap">
                      <div className="soha-ai-workbench__session-copy">
                        <div className="soha-ai-workbench__session-title-row">
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
                        <Paragraph className="soha-ai-workbench__session-description">
                          {currentSession.metadata?.summary || modeDescription(currentSession.metadata?.mode)}
                        </Paragraph>
                        <Space size={[8, 8]} wrap>
                          <Tag color="blue">{modeLabel(currentSession.metadata?.mode)}</Tag>
                          <Tag color={activeAgentProvider?.supportsAsync ? 'purple' : 'default'}>{activeAgentProvider?.name || currentSession.metadata?.agentProviderId || '内置分析'}</Tag>
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
                        <Button
                          loading={analyzeSessionMutation.isPending}
                          onClick={openExplicitAnalysis}
                          disabled={!canRunExplicitAnalysis}
                          title={explicitAnalysisTitle}
                        >
                          显式分析
                        </Button>
                        <Button
                          loading={createInspectionFromSessionMutation.isPending}
                          onClick={() => createInspectionFromSessionMutation.mutate()}
                          disabled={!canCreateInspectionTask}
                          title={createInspectionTitle}
                        >
                          生成巡检任务
                        </Button>
                        <Button icon={<BranchesOutlined />} onClick={() => setThinkingOpen(true)}>
                          分析链路
                        </Button>
                        <Popconfirm
                          title="确认归档当前会话？"
                          description="归档后会话将从当前工作台列表移除。"
                          okButtonProps={{ danger: true, loading: deleteSessionMutation.isPending }}
                          onConfirm={() => deleteSessionMutation.mutate(currentSession.id)}
                        >
                          <Button danger icon={<DeleteOutlined />} loading={deleteSessionMutation.isPending}>
                            归档
                          </Button>
                        </Popconfirm>
                      </Space>
                    </Flex>
                  </div>

                  <div className="soha-ai-workbench__conversation-card">
                    <div className="soha-ai-workbench__conversation-topbar">
                      <div>
                        <Text strong>对话流程</Text>
                        <Paragraph className="soha-ai-workbench__conversation-subtitle">
                          {buildScopeSummary(currentSession.metadata?.scope)} · {messages.length} 条消息
                        </Paragraph>
                      </div>
                      <Space wrap>
                        <Button size="small" onClick={() => openInspector('evidence')}>证据</Button>
                        <Button size="small" onClick={() => openInspector('hypotheses')}>假设</Button>
                        <Button size="small" onClick={() => openInspector('actions')}>建议</Button>
                      </Space>
                    </div>

                    {artifactEntries.length > 0 ? (
                      <div className="soha-ai-workbench__artifact-strip">
                        <div className="soha-ai-workbench__artifact-strip-head">
                          <Text strong>分析工件历史</Text>
                          <Tag>{artifactEntries.length}</Tag>
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
                                onClick={() => setSelectedArtifactKey(entry.key)}
                              >
                                <span className="soha-ai-workbench__artifact-title">{artifactTitle(entry)}</span>
                                <span className="soha-ai-workbench__artifact-meta">{artifactMeta(entry)}</span>
                                <span className="soha-ai-workbench__artifact-counts">
                                  {(entry.artifact.evidence?.length ?? 0)} 证据 · {(entry.artifact.recommendations?.length ?? 0)} 建议
                                </span>
                                {contextLinks.length > 0 ? (
                                  <span className="soha-ai-workbench__artifact-context">
                                    {contextLinks.slice(0, 3).map((link) => (
                                      <Tag key={`${entry.key}-${link.key}`} variant="filled">{link.label}</Tag>
                                    ))}
                                  </span>
                                ) : null}
                              </button>
                            )
                          })}
                        </div>
                      </div>
                    ) : null}

                    {activeGraph?.nodes?.length ? (
                      <div className="soha-ai-workbench__graph-panel">
                        <div className="soha-ai-workbench__graph-head">
                          <div>
                            <Text strong>分析工件图谱</Text>
                            <Paragraph className="soha-ai-workbench__conversation-subtitle">
                              {activeArtifact?.summary || '把 traces、logs、metrics 与假设收敛成一张会话内动态图。'}
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
                        {!enabledDataSources.some((item) => ['logs', 'metrics', 'traces'].includes(item.sourceKind)) ? (
                          <Alert
                            type="info"
                            showIcon
                            title="当前还没有可用的 logs / metrics / traces 数据源"
                            description="现在展示的是会话范围根节点。配置 Elasticsearch/Loki、Prometheus、Jaeger 之后，根因图会自动扩展成错误链路、日志签名和指标挂件。"
                          />
                        ) : null}
                        <div className="soha-ai-workbench__graph-layout">
                          <WorkbenchGraphCanvas
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
                                    {selectedGraphNode.severity ? <StatusTag value={selectedGraphNode.severity} /> : null}
                                  </Space>
                                  {selectedGraphNode.subtitle ? (
                                    <Paragraph type="secondary" style={{ margin: '8px 0 0' }}>
                                      {selectedGraphNode.subtitle}
                                    </Paragraph>
                                  ) : null}
                                </div>
                                {selectedGraphNode.sourceRefs?.length ? (
                                  <div className="soha-ai-workbench__tool-chip-list">
                                    {selectedGraphNode.sourceRefs.map((item) => <Tag key={`${selectedGraphNode.id}-${item}`}>{item}</Tag>)}
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
                                    <pre className="soha-workbench-graph-json">{JSON.stringify(selectedGraphNode.attributes, null, 2)}</pre>
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

                    <div className="soha-ai-workbench__conversation-scroll">
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
                              onItemClick={({ data }) => {
                                if (!canUseChat || !currentSession) return
                                sendMessageMutation.mutate(String(data.label))
                              }}
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
                          onItemClick={({ data }) => {
                            if (!canUseChat || !currentSession) return
                            sendMessageMutation.mutate(String(data.label))
                          }}
                        />
                      }
                    />
                  </div>
                </>
              )}
            </div>
          </main>
          <aside className="soha-ai-workbench__tools-pane">
            <div className="soha-ai-workbench__tools-header">
              <div className="soha-ai-workbench__tools-title">
                <span className="soha-ai-workbench__tools-icon"><BranchesOutlined /></span>
                <span>
                  <Text strong>调查焦点</Text>
                  <Text type="secondary">把上下文、证据和下一步动作收在右侧。</Text>
                </span>
              </div>
              <Button size="small" type="text" onClick={() => setThinkingOpen(true)}>
                分析链路
              </Button>
            </div>

            <div className="soha-ai-workbench__insight-list">
              {artifactSummary.map((item) => (
                <button key={item.key} className="soha-ai-workbench__insight-item" type="button" onClick={() => openInspector(item.key)}>
                  <span className="soha-ai-workbench__insight-icon">{item.icon}</span>
                  <span className="soha-ai-workbench__insight-copy">
                    <span className="soha-ai-workbench__insight-title">{item.label} · {item.value}</span>
                    <span className="soha-ai-workbench__insight-detail">{item.description}</span>
                  </span>
                </button>
              ))}
            </div>

                    <div className="soha-ai-workbench__tool-section">
                      <div className="soha-ai-workbench__tool-section-title">
                        <Text strong>会话装配</Text>
                        <Button size="small" type="text" onClick={() => setToolsetOpen(true)}>
                          调整
                </Button>
              </div>
              <div className="soha-ai-workbench__tool-stack">
                <div className="soha-ai-workbench__tool-row">
                  <span>
                    <Text strong>有效适配器</Text>
                    <Text type="secondary">{selectedAdapterIds.length > 0 ? selectedAdapterIds.join(', ') : '自动允许已注册 adapter'}</Text>
                  </span>
                  <Tag>{selectedAdapterIds.length || effectiveAdapterIds.length || 'Auto'}</Tag>
                </div>
                <div className="soha-ai-workbench__tool-row">
                  <span>
                    <Text strong>会话技能</Text>
                    <Text type="secondary">{selectedSkillNames.length > 0 ? selectedSkillNames.join(', ') : '沿用全局技能'}</Text>
                  </span>
                  <Tag>{selectedSkillNames.length || globalSkills.filter((item) => item.enabled).length}</Tag>
                </div>
                <div className="soha-ai-workbench__tool-row">
                  <span>
                    <Text strong>活跃数据源</Text>
                    <Text type="secondary">{enabledDataSources.length > 0 ? enabledDataSources.map((item) => item.name).join(', ') : '暂无可用数据源'}</Text>
                  </span>
                  <Tag>{enabledDataSources.length}</Tag>
                </div>
                <div className="soha-ai-workbench__tool-row">
                  <span>
                    <Text strong>预算 / 屏蔽</Text>
                    <Text type="secondary">{disabledToolNames.length} 个工具屏蔽，{countObjectKeys(cleanedBudgetOverrides)} 项预算覆盖</Text>
                  </span>
                  <Button size="small" onClick={() => setToolsetOpen(true)}>调整</Button>
                </div>
                        </div>
                      </div>

                      <div className="soha-ai-workbench__tool-section">
                        <div className="soha-ai-workbench__tool-section-title">
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
                        <div className="soha-ai-workbench__tool-stack">
                          {primarySkills.length === 0 ? (
                            <ManagementState bordered={false} compact title="暂无启用 Skills" description="当前会话和全局配置没有可用 skills。" />
                          ) : primarySkills.map((skill) => {
                            const expanded = Boolean(skillsDisclosureExpanded[skill.id])
                            const selected = selectedSkillIds.includes(skill.id)
                            return (
                              <div key={skill.id} className="soha-ai-workbench__tool-row is-skill">
                                <span>
                                  <Space size={[6, 6]} wrap>
                                    <Text strong>{skill.name}</Text>
                                    {selected ? <StatusTag value="enabled" /> : null}
                                    {skill.category ? <Tag>{skill.category}</Tag> : null}
                                  </Space>
                                  <Text type="secondary">{skill.description || (skill.scopes ?? []).join(', ') || '未填写说明'}</Text>
                                  {expanded ? (
                                    <div className="soha-ai-workbench__tool-chip-list" style={{ marginTop: 8 }}>
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

                      <div className="soha-ai-workbench__tool-section">
                        <div className="soha-ai-workbench__tool-section-title">
                          <Text strong>快捷动作</Text>
                <Button size="small" type="text" onClick={() => navigate(getAIToolsPath(location.search))}>
                  工具与技能
                </Button>
              </div>
              <div className="soha-ai-workbench__tool-stack">
                <div className="soha-ai-workbench__tool-row">
                  <span>
                    <Text strong>当前范围</Text>
                    <Text type="secondary">{buildScopeSummary(currentSession?.metadata?.scope)}</Text>
                  </span>
                  <Button size="small" onClick={() => openInspector('context')}>查看</Button>
                </div>
                <div className="soha-ai-workbench__tool-row">
                  <span>
                    <Text strong>显式分析</Text>
                    <Text type="secondary">把当前会话转成一轮结构化分析输出。</Text>
                  </span>
                  <Button
                    size="small"
                    loading={analyzeSessionMutation.isPending}
                    onClick={openExplicitAnalysis}
                    disabled={!currentSession || !canRunExplicitAnalysis}
                    title={explicitAnalysisTitle}
                  >
                    运行
                  </Button>
                </div>
                <div className="soha-ai-workbench__tool-row">
                  <span>
                    <Text strong>生成巡检任务</Text>
                    <Text type="secondary">把会话结论转成后续巡检与自动化入口。</Text>
                  </span>
                  <Button
                    size="small"
                    loading={createInspectionFromSessionMutation.isPending}
                    onClick={() => createInspectionFromSessionMutation.mutate()}
                    disabled={!currentSession || !canCreateInspectionTask}
                    title={createInspectionTitle}
                  >
                    生成
                  </Button>
                </div>
              </div>
            </div>
          </aside>
        </section>
      </div>

      <Drawer title="分析链路" placement="right" open={thinkingOpen} onClose={() => setThinkingOpen(false)} size="large">
        <ThoughtChain
          items={toolCalls.length === 0 ? [
            { key: 'idle', title: '尚未执行工具', description: '发送消息后，这里会展示工具调用与分析步骤。', status: 'loading' satisfies ThoughtChainStatus },
          ] : toolCalls.map((item) => ({
            key: item.id,
            title: item.toolName,
            description: item.summary || item.adapterId,
            content: item.output ? <pre style={{ whiteSpace: 'pre-wrap', margin: 0 }}>{JSON.stringify(item.output, null, 2)}</pre> : undefined,
            status: (item.status === 'success' ? 'success' : 'error') satisfies ThoughtChainStatus,
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

      <Drawer
        title="会话级工具集"
        placement="right"
        open={toolsetOpen}
        onClose={() => setToolsetOpen(false)}
        size="large"
        extra={<Tag color={currentSession ? 'blue' : 'default'}>{currentSession ? currentSession.title : '未选择会话'}</Tag>}
        footer={(
          <Flex justify="space-between" gap={12} wrap="wrap">
            <Space wrap>
              <Button onClick={clearToolset} disabled={!currentSession}>恢复自动选择</Button>
              <Button onClick={applyRecommendedToolset} disabled={!currentSession}>应用推荐预设</Button>
            </Space>
            <Button type="primary" loading={patchSessionMutation.isPending} onClick={saveToolset} disabled={!currentSession}>
              保存会话级装配
            </Button>
          </Flex>
        )}
      >
        {!currentSession ? (
          <ManagementState bordered={false} compact kind="select-scope" title="未选择会话" description="先选择会话，再配置工具装配。" />
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
                    {agentCapabilities.slice(0, 8).map((item) => <Tag key={item.id}>{item.name}</Tag>)}
                  </div>
                ) : null}
              </Space>
            </Card>

            <Card size="small" title="Adapters 与工具">
              <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                <Select
                  mode="multiple"
                  allowClear
                  maxTagCount="responsive"
                  optionFilterProp="label"
                  placeholder="留空表示自动允许所有已注册 adapter"
                  value={selectedAdapterIds}
                  onChange={(value: string[]) => setSelectedAdapterIds(value)}
                  options={adapters.map((item) => ({ value: item.id, label: `${item.name} (${item.sourceKind})` }))}
                />
                <Select
                  mode="multiple"
                  allowClear
                  maxTagCount="responsive"
                  optionFilterProp="label"
                  placeholder="选择要屏蔽的工具，保存为 adapter.tool"
                  value={disabledToolNames}
                  onChange={(value: string[]) => setDisabledToolNames(canonicalDisabledToolNames(value, adapters))}
                  options={disabledToolOptions}
                />
                <div className="soha-ai-workbench__tool-stack">
                  {dataSources.length === 0 ? (
                    <ManagementState bordered={false} compact title="暂无全局数据源" description="全局数据源配置完成后会在这里展示。" />
                  ) : dataSources.map((item) => (
                    <div key={item.id} className="soha-ai-workbench__tool-row">
                      <span>
                        <Text strong>{item.name}</Text>
                        <Text type="secondary">{item.sourceKind} / {item.backendType} / {item.mcpAdapter}</Text>
                      </span>
                      <StatusTag value={item.validationStatus || (item.enabled ? 'enabled' : 'disabled')} />
                    </div>
                  ))}
                </div>
              </Space>
            </Card>

            <Card size="small" title="Skills">
              <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                <Paragraph type="secondary" style={{ marginBottom: 0 }}>
                  这里选择本会话允许暴露给 AI coding 客户端的企业 skills；不影响全局 registry 的启用状态。
                </Paragraph>
                <Space wrap>
                  {globalSkills.length === 0 ? (
                    <ManagementState bordered={false} compact title="暂无全局 Skills 配置" description="全局 registry 尚未启用可分配的 skills。" />
                  ) : globalSkills.map((item) => (
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
              </Space>
            </Card>

            <Card size="small" title="Budget Overrides">
              <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                {TOOLSET_BUDGET_FIELDS.map((field) => (
                  <Flex key={field.key} justify="space-between" align="center" gap={12}>
                    <span>
                      <Text strong>{field.label}</Text>
                      <Text type="secondary" style={{ display: 'block' }}>{field.description}</Text>
                    </span>
                    <InputNumber
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
                <Input placeholder="Override cluster" value={scopeOverrides.clusterId || ''} onChange={(event) => setScopeOverrideValue('clusterId', event.target.value)} />
                <Input placeholder="Override namespace" value={scopeOverrides.namespace || ''} onChange={(event) => setScopeOverrideValue('namespace', event.target.value)} />
                <Input placeholder="Override workload" value={scopeOverrides.workload || ''} onChange={(event) => setScopeOverrideValue('workload', event.target.value)} />
                <Input placeholder="Override service" value={scopeOverrides.service || ''} onChange={(event) => setScopeOverrideValue('service', event.target.value)} />
                <Input placeholder="Override alert ID" value={scopeOverrides.alertId || ''} onChange={(event) => setScopeOverrideValue('alertId', event.target.value)} />
                <InputNumber
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
      </Drawer>

      <Modal
        title="显式分析设置"
        open={analysisOpen}
        onCancel={() => setAnalysisOpen(false)}
        onOk={submitExplicitAnalysis}
        okText="开始分析"
        cancelText="取消"
        confirmLoading={analyzeSessionMutation.isPending}
        okButtonProps={{ disabled: !currentSession || !canSubmitExplicitAnalysis }}
        width={680}
      >
        {currentSession ? (
          <Space orientation="vertical" size={14} style={{ width: '100%' }}>
            <Alert
              type="info"
              showIcon
              title="本次分析会写回当前会话"
              description="分析结果会追加为 assistant 消息，并进入分析工件历史、图谱、证据和建议面板。"
            />
            <Space orientation="vertical" size={8} style={{ width: '100%' }}>
              <Text strong>分析模式</Text>
              <Segmented
                block
                value={analysisMode}
                options={RUNNABLE_ANALYSIS_MODE_OPTIONS.map((item) => ({ value: item.value, label: item.label }))}
                onChange={(value) => {
                  const next = value as WorkbenchMode
                  const currentDefault = defaultAnalysisQuestion(analysisMode, currentSession)
                  setAnalysisMode(next)
                  setSelectedAnalysisProfileId(defaultAnalysisProfileIdForMode(next, analysisProfiles))
                  if (!analysisQuestion.trim() || analysisQuestion === currentDefault) {
                    setAnalysisQuestion(defaultAnalysisQuestion(next, currentSession))
                  }
                }}
              />
            </Space>
            <Space orientation="vertical" size={8} style={{ width: '100%' }}>
              <Text strong>Agent Provider</Text>
              <Select
                value={selectedAgentProviderId || defaultAgentProviderId}
                options={providerOptions}
                onChange={(value: string) => setSelectedAgentProviderId(value)}
              />
              <Text type="secondary">
                {agentProviders.find((item) => item.id === selectedAgentProviderId)?.description || '选择本次分析使用内置分析还是外部 agent runner。'}
              </Text>
            </Space>
            <Space orientation="vertical" size={8} style={{ width: '100%' }}>
              <Text strong>分析模板</Text>
              <Select
                allowClear
                showSearch
                optionFilterProp="label"
                value={selectedAnalysisProfileId || undefined}
                placeholder="使用当前模式的默认 profile"
                options={analysisProfileOptions}
                onChange={(value?: string) => setSelectedAnalysisProfileId(value || '')}
              />
              <Text type="secondary">
                {selectedAnalysisProfile ? `${selectedAnalysisProfile.name} 会约束数据源、playbook、预算和输出风格。` : '未选择时由后端使用会话和 provider 默认策略。'}
              </Text>
            </Space>
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
                <Tag color="blue">{modeLabel(analysisMode)}</Tag>
                <Tag color={agentProviders.find((item) => item.id === selectedAgentProviderId)?.supportsAsync ? 'purple' : 'default'}>
                  {agentProviders.find((item) => item.id === selectedAgentProviderId)?.name || selectedAgentProviderId || '内置分析'}
                </Tag>
                {analysisCapability ? <Tag>{analysisCapability.name}</Tag> : null}
                {selectedAnalysisProfile ? <Tag>{selectedAnalysisProfile.name}</Tag> : null}
                <Tag>{buildScopeSummary(currentSession.metadata?.scope)}</Tag>
                <Tag>{currentSession.metadata?.toolset ? '会话级工具集' : '全局工具集'}</Tag>
              </Space>
            </Card>
            {analysisMode === 'root_cause' && !canRunRootCause ? (
              <Alert
                type="warning"
                showIcon
                title="缺少 observe.ai.root-cause.run 权限，无法运行根因分析。"
              />
            ) : null}
          </Space>
        ) : (
          <ManagementState bordered={false} compact kind="select-scope" title="未选择会话" description="先选择会话，再运行显式分析。" />
        )}
      </Modal>

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
