import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ApiOutlined,
  BranchesOutlined,
  DeleteOutlined,
  EditOutlined,
  ExperimentOutlined,
  PlusOutlined,
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
  Alert,
  App,
  Button,
  Card,
  Drawer,
  Empty,
  Flex,
  InputNumber,
  Input,
  Modal,
  Select,
  Space,
  Splitter,
  Tabs,
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

export function AIWorkbenchPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canUseChat = hasPermission(permissionSnapshotQuery.data?.data, 'observe.ai.chat')
  const autoSessionScopeKeyRef = useRef<string>('')

  const initialMode = (searchParams.get('mode') as NonNullable<WorkbenchSession['metadata']>['mode']) || 'general'
  const draftScope = useMemo<WorkbenchSessionScope>(() => ({
    clusterId: searchParams.get('clusterId') || undefined,
    namespace: searchParams.get('namespace') || undefined,
    workload: searchParams.get('workload') || undefined,
    alertId: searchParams.get('alertId') || undefined,
    timeRangeMinutes: Number(searchParams.get('timeRangeMinutes') || 60) || 60,
  }), [searchParams])
  const [activeSessionId, setActiveSessionId] = useState<string>()
  const [renameOpen, setRenameOpen] = useState(false)
  const [renameValue, setRenameValue] = useState('')
  const [thinkingOpen, setThinkingOpen] = useState(false)
  const [toolsetOpen, setToolsetOpen] = useState(false)
  const [draftMode, setDraftMode] = useState<NonNullable<WorkbenchSession['metadata']>['mode']>(initialMode)
  const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([])
  const [selectedAdapterIds, setSelectedAdapterIds] = useState<string[]>([])
  const [disabledToolNames, setDisabledToolNames] = useState<string[]>([])
  const [budgetOverrides, setBudgetOverrides] = useState<Record<string, number>>({})
  const [scopeOverrides, setScopeOverrides] = useState<Record<string, string>>({})

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
    queryKey: ['copilot-workbench-session-detail', activeSessionId],
    queryFn: () => api.get<ApiResponse<WorkbenchSession>>(`/copilot/sessions/${activeSessionId}`),
    enabled: Boolean(activeSessionId),
  })
  const messagesQuery = useQuery({
    queryKey: ['copilot-workbench-messages', activeSessionId],
    queryFn: () => api.get<ApiResponse<WorkbenchMessage[]>>(`/copilot/sessions/${activeSessionId}/messages`),
    enabled: Boolean(activeSessionId),
  })

  useEffect(() => {
    const first = (sessionsQuery.data?.data ?? []).find((item) => !isSyntheticSession(item))
    if (!activeSessionId && first?.id) {
      setActiveSessionId(first.id)
    }
  }, [sessionsQuery.data, activeSessionId])

  const createSessionMutation = useMutation({
    mutationFn: (payload?: { title?: string; scope?: WorkbenchSessionScope }) => api.post<ApiResponse<WorkbenchSession>>('/copilot/sessions', {
      title: payload?.title || '',
      mode: draftMode,
      scope: payload?.scope || draftScope,
      tags: [],
    }),
    onSuccess: (response) => {
      void queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      setActiveSessionId(response.data.id)
      void message.success('已创建调查会话')
    },
    onError: (err: Error) => void message.error(err.message),
  })

  useEffect(() => {
    const scopeKey = JSON.stringify(draftScope)
    const hasScopedEntry = Boolean(draftScope.alertId || draftScope.clusterId || draftScope.namespace || draftScope.workload)
    if (!hasScopedEntry || !canUseChat || createSessionMutation.isPending || autoSessionScopeKeyRef.current === scopeKey) {
      return
    }
    autoSessionScopeKeyRef.current = scopeKey
    setActiveSessionId(undefined)
    createSessionMutation.mutate({
      title: draftScope.alertId ? `Alert ${draftScope.alertId}` : draftScope.workload ? `${draftScope.workload} 调查` : '新的调查会话',
      scope: draftScope,
    })
  }, [canUseChat, createSessionMutation, draftScope])

  const patchSessionMutation = useMutation({
    mutationFn: (payload: { sessionId: string; body: Record<string, unknown> }) =>
      api.patch<ApiResponse<WorkbenchSession>>(`/copilot/sessions/${payload.sessionId}`, payload.body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      void queryClient.invalidateQueries({ queryKey: ['copilot-workbench-session-detail', activeSessionId] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const deleteSessionMutation = useMutation({
    mutationFn: (sessionId: string) => api.delete(`/copilot/sessions/${sessionId}`),
    onSuccess: async (_response, sessionId) => {
      if (activeSessionId === sessionId) {
        setActiveSessionId(undefined)
      }
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      void message.success('会话已归档')
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const sendMessageMutation = useMutation({
    mutationFn: (content: string) =>
      api.post<ApiResponse<WorkbenchMessageEnvelope>>(`/copilot/sessions/${activeSessionId}/messages`, { content }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-messages', activeSessionId] })
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-session-detail', activeSessionId] })
      setThinkingOpen(true)
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const analyzeSessionMutation = useMutation({
    mutationFn: () => api.post<ApiResponse<WorkbenchMessageEnvelope>>(`/copilot/sessions/${activeSessionId}/analyze`, {
      mode: currentSession?.metadata?.mode || draftMode,
      question: currentSession?.metadata?.summary || 'Run analysis for current session scope',
      scope: currentSession?.metadata?.scope || {},
    }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-session-detail', activeSessionId] })
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      setThinkingOpen(true)
      void message.success('已触发显式分析')
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const createInspectionFromSessionMutation = useMutation({
    mutationFn: () => api.post(`/copilot/sessions/${activeSessionId}/inspection-task`, {
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

  const visibleSessions = (sessionsQuery.data?.data ?? []).filter((item) => !isSyntheticSession(item))
  const currentSession = (sessionDetailQuery.data?.data && !isSyntheticSession(sessionDetailQuery.data.data) ? sessionDetailQuery.data.data : undefined) ?? visibleSessions.find((item) => item.id === activeSessionId)
  const messages = messagesQuery.data?.data ?? []
  const globalSkills = settingsQuery.data?.data?.skillsRegistry ?? []
  useEffect(() => {
    setSelectedSkillIds(currentSession?.metadata?.toolset?.enabledSkillIds ?? [])
    setSelectedAdapterIds(currentSession?.metadata?.toolset?.enabledAdapterIds ?? [])
    setDisabledToolNames(currentSession?.metadata?.toolset?.disabledToolNames ?? [])
    setBudgetOverrides((currentSession?.metadata?.toolset?.budgetOverrides as Record<string, number> | undefined) ?? {})
    setScopeOverrides((currentSession?.metadata?.toolset?.scopeOverrides as Record<string, string> | undefined) ?? {})
  }, [currentSession?.id, currentSession?.metadata?.toolset?.enabledSkillIds, currentSession?.metadata?.toolset?.enabledAdapterIds, currentSession?.metadata?.toolset?.disabledToolNames, currentSession?.metadata?.toolset?.budgetOverrides, currentSession?.metadata?.toolset?.scopeOverrides])
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
  const sessionItems = visibleSessions.map((item) => ({
    key: item.id,
    label: item.title,
    group: item.metadata?.mode ? modeLabel(item.metadata.mode) : '会话',
    timestamp: item.updatedAt,
  }))

  return (
    <div className="kc-page">
      <div style={{ border: '1px solid var(--ant-colorBorderSecondary)', borderRadius: 16, overflow: 'hidden', background: 'var(--ant-colorBgContainer)' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 16, padding: '18px 20px 16px', borderBottom: '1px solid var(--ant-colorBorderSecondary)' }}>
          <div style={{ minWidth: 0 }}>
            <Title level={4} style={{ margin: 0 }}>调查工作台</Title>
            <Paragraph type="secondary" style={{ margin: '6px 0 0' }}>
              AI Chat、链路根因、性能分析与巡检复盘统一在同一个会话工作台内，通过模式切换进入。
            </Paragraph>
          </div>
          <Space wrap align="center">
            <Tag color="blue">{modeLabel(draftMode)}</Tag>
          </Space>
        </div>
        {!canUseChat ? (
          <div style={{ padding: '12px 20px 0' }}>
            <Alert
              type="warning"
              showIcon
              message="当前账号缺少 observe.ai.chat 权限，无法发送消息或创建会话。"
            />
          </div>
        ) : null}
        {queryError ? (
          <div style={{ padding: '12px 20px 0' }}>
            <Alert
              type="error"
              showIcon
              message="工作台数据加载失败"
              description={queryError instanceof Error ? queryError.message : '请检查当前 API 服务和权限快照。'}
            />
          </div>
        ) : null}

      <Splitter style={{ minHeight: 'calc(100vh - 250px)', border: 0, borderRadius: 0, overflow: 'hidden', background: 'var(--ant-colorBgContainer)' }}>
        <Splitter.Panel defaultSize="20%" min="16%">
          <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
            <div style={{ padding: '16px 16px 12px', borderBottom: '1px solid var(--ant-colorBorderSecondary)' }}>
              <Space direction="vertical" size={12} style={{ width: '100%' }}>
                <Flex justify="space-between" align="center">
                  <Text strong>会话</Text>
                  <Text type="secondary" style={{ fontSize: 12 }}>{visibleSessions.length}</Text>
                </Flex>
                <Select
                  size="middle"
                  value={draftMode}
                  style={{ width: '100%' }}
                  onChange={(value) => {
                    const next = value as NonNullable<WorkbenchSession['metadata']>['mode']
                    setDraftMode(next)
                    setSearchParams((prev) => {
                      prev.set('mode', next || 'general')
                      return prev
                    })
                  }}
                  options={[
                    { value: 'general', label: '通用调查' },
                    { value: 'root_cause', label: '根因分析' },
                    { value: 'performance', label: '性能分析' },
                    { value: 'trace', label: '链路分析' },
                    { value: 'inspection_review', label: '巡检复盘' },
                  ]}
                />
                <Space>
                  <Button icon={<ToolOutlined />} onClick={() => setToolsetOpen(true)}>
                    工具集
                  </Button>
                  <Button type="primary" icon={<PlusOutlined />} loading={createSessionMutation.isPending} onClick={() => createSessionMutation.mutate({ scope: draftScope })} disabled={!canUseChat}>
                    新建调查
                  </Button>
                </Space>
              </Space>
            </div>
            <div style={{ padding: '12px 16px 16px', flex: 1, minHeight: 0 }}>
              <Conversations
                items={sessionItems}
                activeKey={activeSessionId}
                onActiveChange={(value) => setActiveSessionId(String(value))}
                groupable
                menu={(item) => ({
                  items: [
                    { key: 'rename', label: '重命名', icon: <EditOutlined /> },
                    { key: 'archive', label: '归档', icon: <DeleteOutlined />, danger: true },
                  ],
                  onClick: ({ key }) => {
                    if (key === 'rename') {
                      const target = sessionsQuery.data?.data?.find((entry) => entry.id === item.key)
                      setRenameValue(target?.title ?? '')
                      setActiveSessionId(String(item.key))
                      setRenameOpen(true)
                    }
                    if (key === 'archive') {
                      deleteSessionMutation.mutate(String(item.key))
                    }
                  },
                })}
                styles={{ root: { height: '100%' } }}
              />
            </div>
          </div>
        </Splitter.Panel>
        <Splitter.Panel defaultSize="48%" min="36%">
          <div style={{ height: '100%', display: 'flex', flexDirection: 'column', padding: 20, gap: 16 }}>
            {!currentSession ? (
              <Empty description="先创建或选择一个调查会话" />
            ) : (
              <>
                <Card size="small" styles={{ body: { padding: 16 } }}>
                  <Flex justify="space-between" align="start" gap={16}>
                    <div>
                      <Title level={5} style={{ margin: 0 }}>{currentSession.title}</Title>
                      <Paragraph type="secondary" style={{ margin: '8px 0 0' }}>
                        {currentSession.metadata?.summary || '围绕当前范围继续追问、分析和沉淀调查结论。'}
                      </Paragraph>
                    <Space size={[8, 8]} wrap style={{ marginTop: 12 }}>
                      <Tag color="blue">{modeLabel(currentSession.metadata?.mode)}</Tag>
                      <Tag>{buildScopeSummary(currentSession.metadata?.scope)}</Tag>
                      {(currentSession.metadata?.tags ?? []).map((item) => <Tag key={item}>{item}</Tag>)}
                    </Space>
                    </div>
                    <Space>
                      {currentSession.metadata?.scope?.alertId ? (
                        <Button onClick={() => navigate(`/monitoring-workbench/alerts/${currentSession.metadata?.scope?.alertId}`)}>
                          回到原告警
                        </Button>
                      ) : null}
                      <Button loading={analyzeSessionMutation.isPending} onClick={() => analyzeSessionMutation.mutate()}>
                        显式分析
                      </Button>
                      <Button loading={createInspectionFromSessionMutation.isPending} onClick={() => createInspectionFromSessionMutation.mutate()}>
                        生成巡检任务
                      </Button>
                      <Button icon={<BranchesOutlined />} onClick={() => setThinkingOpen(true)}>
                        分析链路
                      </Button>
                    </Space>
                  </Flex>
                </Card>

                <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', gap: 12 }}>
                  {messages.length === 0 ? (
                    <Welcome
                      icon={<ExperimentOutlined />}
                      title="开始一轮调查"
                      description="用 Ant Design X 组织聊天、工具调用和分析结果。"
                      extra={
                        <Prompts
                          title="建议起手问题"
                          wrap
                          items={[
                            { key: 'root-cause', label: '最近告警为什么爆发？', description: '直接生成根因分析上下文' },
                            { key: 'performance', label: '当前服务延迟为什么抖动？', description: '偏性能与指标视角' },
                            { key: 'trace', label: '链路最慢的 span 在哪里？', description: '偏 tracing 视角' },
                          ]}
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
                      items={[
                        { key: 'incident', icon: <ThunderboltOutlined />, label: '分析当前告警根因' },
                        { key: 'latency', icon: <ApiOutlined />, label: '分析服务延迟热点' },
                        { key: 'logs', icon: <ToolOutlined />, label: '汇总错误签名与日志上下文' },
                      ]}
                      onItemClick={({ data }) => sendMessageMutation.mutate(String(data.label))}
                    />
                  }
                />
              </>
            )}
          </div>
        </Splitter.Panel>
        <Splitter.Panel defaultSize="32%" min="24%">
          <div style={{ height: '100%', padding: 20, overflow: 'auto' }}>
            <Tabs
              defaultActiveKey="context"
              items={[
                {
                  key: 'context',
                  label: '上下文',
                  children: currentSession ? (
                    <Space direction="vertical" size={12} style={{ width: '100%' }}>
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
                          <Space direction="vertical" size={8} style={{ width: '100%' }}>
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
                  ) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无会话" />,
                },
                {
                  key: 'evidence',
                  label: '证据',
                  children: activeArtifact ? (
                    <Space direction="vertical" size={12} style={{ width: '100%' }}>
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
                  ) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无分析工件" />,
                },
                {
                  key: 'hypotheses',
                  label: '假设',
                  children: activeArtifact ? (
                    <Space direction="vertical" size={12} style={{ width: '100%' }}>
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
                  ) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无假设" />,
                },
                {
                  key: 'actions',
                  label: '建议',
                  children: activeArtifact ? (
                    <Space direction="vertical" size={8} style={{ width: '100%' }}>
                      {(activeArtifact.recommendations ?? []).length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无建议动作" /> : (
                        (activeArtifact.recommendations ?? []).map((item) => (
                          <Card key={item} size="small">
                            <Paragraph style={{ marginBottom: 0 }}>{item}</Paragraph>
                          </Card>
                        ))
                      )}
                    </Space>
                  ) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无建议" />,
                },
              ]}
            />
          </div>
        </Splitter.Panel>
      </Splitter>
      </div>

      <Drawer title="分析链路" placement="right" open={thinkingOpen} onClose={() => setThinkingOpen(false)} width={460}>
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

      <Drawer title="会话级工具集" placement="right" open={toolsetOpen} onClose={() => setToolsetOpen(false)} width={520}>
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Card size="small" title="已配置适配器">
            <Space direction="vertical" size={8} style={{ width: '100%' }}>
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
            <Space direction="vertical" size={8} style={{ width: '100%' }}>
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
              <Space direction="vertical" size={8} style={{ width: '100%' }}>
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
                  <Space direction="vertical" size={8} style={{ width: '100%' }}>
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
                  <Space direction="vertical" size={8} style={{ width: '100%' }}>
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
          if (!activeSessionId) return
          patchSessionMutation.mutate({ sessionId: activeSessionId, body: { title: renameValue } })
          setRenameOpen(false)
        }}
      >
        <Input value={renameValue} onChange={(event) => setRenameValue(event.target.value)} />
      </Modal>
    </div>
  )
}
