import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ApiOutlined,
  BranchesOutlined,
  DeleteOutlined,
  EditOutlined,
  ExperimentOutlined,
  EyeOutlined,
  PlusOutlined,
  PlayCircleOutlined,
  RadarChartOutlined,
  RobotOutlined,
  ThunderboltOutlined,
  ToolOutlined,
} from '@ant-design/icons'
import {
  Bubble,
  Prompts,
  Sender,
  ThoughtChain,
  Welcome,
} from '@ant-design/x'
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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  WorkbenchAdapter,
  WorkbenchArtifact,
  WorkbenchMessage,
  WorkbenchMessageEnvelope,
  WorkbenchSession,
  WorkbenchSessionScope,
} from './workbench-types'

const { Paragraph, Text, Title } = Typography

const WORKBENCH_MODE_OPTIONS = [
  { value: 'general', label: '通用调查' },
  { value: 'root_cause', label: '根因分析' },
  { value: 'performance', label: '性能分析' },
  { value: 'trace', label: '链路分析' },
  { value: 'inspection_review', label: '巡检复盘' },
] as const

type InspectorView = 'context' | 'evidence' | 'hypotheses' | 'actions'

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
      return '通用调查'
  }
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

export function AIWorkbenchPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canUseChat = hasPermission(permissionSnapshotQuery.data?.data, 'observe.ai.chat')
  const autoSessionScopeKeyRef = useRef<string>('')

  const requestedSessionId = searchParams.get('session') || undefined
  const initialMode = (searchParams.get('mode') as NonNullable<WorkbenchSession['metadata']>['mode']) || 'general'
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
    queryFn: () => api.get<ApiResponse<{ skillsRegistry?: Array<{ id: string; name: string; description?: string; enabled: boolean; scopes?: string[] }> }>>('/settings/ai'),
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

  const visibleSessions = (sessionsQuery.data?.data ?? []).filter((item) => !isSyntheticSession(item))
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
  const queryError = sessionsQuery.error || sessionDetailQuery.error || messagesQuery.error || adaptersQuery.error || dataSourcesQuery.error || settingsQuery.error
  const promptItems = buildPromptItems(currentSession?.metadata?.mode || draftMode)
  const functionSwitches = [
    { key: 'chat', label: '会话调查', detail: '当前主工作区', active: true, action: () => undefined, icon: <RobotOutlined /> },
    { key: 'automation', label: '巡检与自动化', detail: '任务、运行与策略', active: false, action: () => navigate('/ai-workbench/automation'), icon: <PlayCircleOutlined /> },
    { key: 'tools', label: '工具与技能', detail: '数据源与技能装配', active: false, action: () => navigate('/ai-workbench/tools'), icon: <ToolOutlined /> },
  ] as const
  const artifactSummary = [
    {
      key: 'context',
      label: '上下文',
      value: currentSession?.metadata?.analysisRunRefs?.length ?? 0,
      description: buildScopeSummary(currentSession?.metadata?.scope),
      icon: <EyeOutlined />,
    },
    {
      key: 'evidence',
      label: '证据',
      value: activeArtifact?.evidence?.length ?? 0,
      description: activeArtifact?.summary || '还没有提取证据摘要',
      icon: <RadarChartOutlined />,
    },
    {
      key: 'hypotheses',
      label: '假设',
      value: activeArtifact?.hypotheses?.length ?? 0,
      description: activeArtifact?.hypotheses?.[0]?.summary || '还没有形成假设',
      icon: <RobotOutlined />,
    },
    {
      key: 'actions',
      label: '建议',
      value: activeArtifact?.recommendations?.length ?? 0,
      description: activeArtifact?.recommendations?.[0] || '还没有建议动作',
      icon: <ToolOutlined />,
    },
  ] as Array<{
    key: InspectorView
    label: string
    value: number
    description: string
    icon: React.ReactNode
  }>

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
        <section className="kc-ai-workbench__hero">
          <div className="kc-ai-workbench__hero-main">
            <div className="kc-ai-workbench__eyebrow">AI Workbench</div>
            <Title level={3} className="kc-ai-workbench__title">AI工作台</Title>
            <Paragraph className="kc-ai-workbench__description">
              顶栏与工作台切换保持不动，会话记录和功能切换收敛到 AI 左栏；当前主画布聚焦对话流程、分析步骤和调查沉淀。
            </Paragraph>
            <div className="kc-ai-workbench__mode-bar">
              <Segmented
                value={draftMode}
                options={WORKBENCH_MODE_OPTIONS.map((item) => ({ value: item.value, label: item.label }))}
                onChange={(value) => {
                  const next = value as NonNullable<WorkbenchSession['metadata']>['mode']
                  setDraftMode(next)
                  updateSearchParams({ mode: next === 'general' ? undefined : next })
                  if (currentSession && currentSession.metadata?.mode !== next) {
                    patchSessionMutation.mutate({ sessionId: currentSession.id, body: { mode: next } })
                  }
                }}
              />
            </div>
          </div>
          <div className="kc-ai-workbench__hero-actions">
            <Button icon={<ToolOutlined />} onClick={() => setToolsetOpen(true)}>
              会话工具装配
            </Button>
            <Button icon={<PlayCircleOutlined />} onClick={() => navigate('/ai-workbench/automation')}>
              巡检与自动化
            </Button>
            <Button type="primary" icon={<PlusOutlined />} loading={createSessionMutation.isPending} onClick={() => createSessionMutation.mutate({ scope: draftScope })} disabled={!canUseChat}>
              新建会话
            </Button>
          </div>
        </section>

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

        <div className="kc-ai-workbench__summary-grid">
          {artifactSummary.map((item) => (
            <button
              key={item.key}
              className="kc-ai-workbench__summary-card"
              type="button"
              onClick={() => openInspector(item.key)}
            >
              <span className="kc-ai-workbench__summary-icon">{item.icon}</span>
              <span className="kc-ai-workbench__summary-copy">
                <span className="kc-ai-workbench__summary-label">{item.label}</span>
                <span className="kc-ai-workbench__summary-value">{item.value}</span>
                <span className="kc-ai-workbench__summary-detail">{item.description}</span>
              </span>
            </button>
          ))}
        </div>

        <section className="kc-ai-workbench__body">
          <aside className="kc-ai-workbench__rail">
            <Card className="kc-ai-workbench__rail-card" size="small">
              <div className="kc-ai-workbench__rail-heading">
                <Text strong>功能切换</Text>
                <Text type="secondary">AI 内部导航</Text>
              </div>
              <div className="kc-ai-workbench__switch-list">
                {functionSwitches.map((item) => (
                  <button
                    key={item.key}
                    className={['kc-ai-workbench__switch-item', item.active ? 'is-active' : ''].filter(Boolean).join(' ')}
                    type="button"
                    onClick={item.action}
                  >
                    <span className="kc-ai-workbench__switch-icon">{item.icon}</span>
                    <span className="kc-ai-workbench__switch-copy">
                      <span className="kc-ai-workbench__switch-label">{item.label}</span>
                      <span className="kc-ai-workbench__switch-detail">{item.detail}</span>
                    </span>
                  </button>
                ))}
              </div>
            </Card>

            <Card className="kc-ai-workbench__rail-card kc-ai-workbench__session-list-card" size="small">
              <div className="kc-ai-workbench__rail-heading">
                <Text strong>会话记录</Text>
                <Text type="secondary">{visibleSessions.length}</Text>
              </div>
              <div className="kc-ai-workbench__session-list">
                {visibleSessions.length === 0 ? (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无会话" />
                ) : (
                  visibleSessions.map((item) => {
                    const active = item.id === currentSession?.id
                    return (
                      <button
                        key={item.id}
                        className={['kc-ai-workbench__session-item', active ? 'is-active' : ''].filter(Boolean).join(' ')}
                        type="button"
                        onClick={() => updateSearchParams({ session: item.id })}
                      >
                        <span className="kc-ai-workbench__session-item-main">
                          <span className="kc-ai-workbench__session-item-title">{item.title}</span>
                          <span className="kc-ai-workbench__session-item-meta">{modeLabel(item.metadata?.mode)} · {item.updatedAt}</span>
                        </span>
                        <span className="kc-ai-workbench__session-item-tags">
                          {item.metadata?.scope?.clusterId ? <Tag>{item.metadata.scope.clusterId}</Tag> : null}
                        </span>
                      </button>
                    )
                  })
                )}
              </div>
            </Card>

            <Card className="kc-ai-workbench__rail-card" size="small">
              <div className="kc-ai-workbench__rail-heading">
                <Text strong>快捷入口</Text>
                <Text type="secondary">工具与证据</Text>
              </div>
              <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                <Button block onClick={() => openInspector('context')}>查看上下文</Button>
                <Button block onClick={() => openInspector('evidence')}>查看证据与结论</Button>
                <Button block onClick={() => setToolsetOpen(true)}>会话工具装配</Button>
              </Space>
            </Card>
          </aside>

          <div className="kc-ai-workbench__dialog-shell">
            {!currentSession ? (
              <Card className="kc-ai-workbench__empty-card">
                <Welcome
                  icon={<ExperimentOutlined />}
                  title={visibleSessions.length > 0 ? '正在准备会话' : '开始一轮调查'}
                  description={visibleSessions.length > 0 ? '正在同步当前会话，请稍候。' : '从左侧会话记录选择既有调查，或直接新建一个会话开始排障。'}
                  extra={
                    <Space wrap>
                      <Button type="primary" icon={<PlusOutlined />} loading={createSessionMutation.isPending} onClick={() => createSessionMutation.mutate({ scope: draftScope })} disabled={!canUseChat}>
                        新建会话
                      </Button>
                      <Button onClick={() => navigate('/ai-workbench/automation')}>查看巡检与自动化</Button>
                      <Button onClick={() => navigate('/ai-workbench/tools')}>查看工具与技能</Button>
                    </Space>
                  }
                />
              </Card>
            ) : (
              <>
                <Card className="kc-ai-workbench__session-card" size="small">
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
                        {currentSession.metadata?.summary || '围绕当前范围继续追问、分析和沉淀调查结论。'}
                      </Paragraph>
                      <Space size={[8, 8]} wrap>
                        <Tag color="blue">{modeLabel(currentSession.metadata?.mode)}</Tag>
                        <Tag>{buildScopeSummary(currentSession.metadata?.scope)}</Tag>
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
                </Card>

                <div className="kc-ai-workbench__conversation-card">
                  <div className="kc-ai-workbench__conversation-topbar">
                    <div>
                      <Text strong>对话流程</Text>
                      <Paragraph className="kc-ai-workbench__conversation-subtitle">
                        右侧主区域以问答、工具调用和分析沉淀为主，证据与建议通过侧抽屉查看。
                      </Paragraph>
                    </div>
                    <Space wrap>
                      <Button size="small" onClick={() => openInspector('evidence')}>证据</Button>
                      <Button size="small" onClick={() => openInspector('hypotheses')}>假设</Button>
                      <Button size="small" onClick={() => openInspector('actions')}>建议</Button>
                    </Space>
                  </div>

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
