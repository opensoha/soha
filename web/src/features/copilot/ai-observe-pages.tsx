import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { AppstoreOutlined, DeleteOutlined, EditOutlined, PlayCircleOutlined, PlusOutlined, RadarChartOutlined, RobotOutlined, ToolOutlined } from '@ant-design/icons'
import { Alert, App, Button, Card, Col, Flex, Form, Input, InputNumber, List, Modal, Popconfirm, Row, Segmented, Select, Space, Statistic, Switch, Tag, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementDetailHeader,
  ManagementIconButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { api } from '@/services/api-client'
import { tableColumnPresets } from '@/utils/table-columns'
import { AISettingsPage } from '@/features/settings/settings-pages'
import type { ApiResponse } from '@/types'
import { AIWorkbenchPage } from './workbench-page'
import {
  getAIModelSettingsPath,
  getAIOperationsPath,
  getAIToolsPath,
  getAIWorkbenchPathForMode,
  getAIWorkbenchPathForSession,
} from './workbench-navigation'
import { displayWorkbenchSessionTitle } from './workbench-model'
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
import type { WorkbenchCatalog, WorkbenchSession, WorkbenchSessionScope } from './workbench-types'
import './copilot-pages.css'

const { Paragraph, Text } = Typography

interface Insight {
  title: string
  description: string
  severity: string
  actions?: string[]
}

interface RootCauseRun {
  id: string
  kind?: string
  title: string
  status: string
  severity: string
  summary: string
}

interface InspectionTask {
  id: string
  title: string
  scopeType: string
  clusterId?: string
  namespace?: string
  checks?: string[]
  enabled: boolean
  intervalMinutes: number
  lastRunAt?: string
  metadata?: Record<string, unknown>
}

interface InspectionTaskFormValues {
  title?: string
  scopeType?: string
  clusterId?: string
  namespace?: string
  checks?: string[]
  enabled?: boolean
  intervalMinutes?: number
  analysisProfileId?: string
}

interface AnalysisProfile {
  id: string
  name: string
  mode: string
  enabled: boolean
  remediationPolicy?: string
}

interface AutomationPolicy {
  id: string
  name: string
  enabled: boolean
  triggerType: string
  analysisKinds?: string[]
  agentProviderId?: string
  triggerConditions?: Record<string, unknown>
  dedupWindowSeconds?: number
  analysisProfileId: string
  remediationPolicy: string
  approvalPolicy?: Record<string, unknown>
  cooldownSeconds?: number
}

interface InspectionRun {
  id: string
  taskId: string
  status: string
  severity: string
  summary: string
  findings?: Array<{ id: string; title: string; severity: string }>
  startedAt: string
  completedAt?: string
}

interface AutomationPolicyFormValues {
  name?: string
  triggerType?: string
  analysisKinds?: string[]
  agentProviderId?: string
  analysisProfileId?: string
  remediationPolicy?: string
  dedupWindowSeconds?: number
  cooldownSeconds?: number
  enabled?: boolean
  triggerSeverity?: string[]
  triggerStatus?: string[]
  triggerMinDurationSeconds?: number
  triggerLabelKey?: string
  triggerLabelValue?: string
  triggerTimeRangeMinutes?: number
  approvalRequired?: boolean
  approvalRoles?: string[]
}

const INSPECTION_CHECK_OPTIONS = [
  { value: 'cluster_health', label: 'Cluster Health' },
  { value: 'alert_pressure', label: 'Alert Pressure' },
  { value: 'audit_denials', label: 'Audit Denials' },
  { value: 'resource_pressure', label: 'Resource Pressure' },
  { value: 'delivery_risk', label: 'Delivery Risk' },
]

const AUTOMATION_ANALYSIS_KIND_OPTIONS = [
  { value: 'root_cause', label: 'Root Cause' },
  { value: 'performance', label: 'Performance' },
  { value: 'trace', label: 'Trace' },
  { value: 'inspection_review', label: 'Inspection Review' },
]
const SUPPORTED_AUTOMATION_ANALYSIS_KINDS = new Set(AUTOMATION_ANALYSIS_KIND_OPTIONS.map((item) => item.value))

const AUTOMATION_TRIGGER_TYPE_OPTIONS = [
  { value: 'alert_webhook', label: 'Alert Webhook' },
]

const AUTOMATION_REMEDIATION_POLICY_OPTIONS = [
  { value: 'suggest_only', label: 'Suggest Only' },
  { value: 'require_approval', label: 'Require Approval' },
  { value: 'disabled', label: 'Disabled' },
]

const AUTOMATION_SEVERITY_OPTIONS = [
  { value: 'critical', label: 'critical' },
  { value: 'warning', label: 'warning' },
  { value: 'info', label: 'info' },
]

const AUTOMATION_STATUS_OPTIONS = [
  { value: 'firing', label: 'firing' },
  { value: 'resolved', label: 'resolved' },
]

const AI_OVERVIEW_ACTIONS = [
  { key: 'root_cause', label: '根因调查', detail: '告警、事件、异常波动', href: getAIWorkbenchPathForMode('root_cause'), icon: <RobotOutlined /> },
  { key: 'performance', label: '性能分析', detail: '容量、时延、吞吐', href: getAIWorkbenchPathForMode('performance'), icon: <RadarChartOutlined /> },
  { key: 'trace', label: '链路分析', detail: '跨服务路径与热点', href: getAIWorkbenchPathForMode('trace'), icon: <AppstoreOutlined /> },
  { key: 'operations', label: '巡检与自动化', detail: '任务、运行、策略', href: getAIOperationsPath(), icon: <PlayCircleOutlined /> },
  { key: 'tools', label: '工具与技能', detail: 'MCP、数据源、Skills', href: getAIToolsPath(), icon: <ToolOutlined /> },
] as const

function defaultInspectionTaskValues(): InspectionTaskFormValues {
  return {
    title: '',
    scopeType: 'platform',
    clusterId: '',
    namespace: '',
    checks: ['cluster_health', 'alert_pressure', 'audit_denials'],
    enabled: true,
    intervalMinutes: 30,
    analysisProfileId: '',
  }
}

export function inspectionTaskPayload(values: InspectionTaskFormValues) {
  const analysisProfileId = String(values.analysisProfileId ?? '').trim()
  return {
    title: String(values.title ?? '').trim(),
    scopeType: String(values.scopeType || 'platform'),
    clusterId: String(values.clusterId ?? '').trim(),
    namespace: String(values.namespace ?? '').trim(),
    checks: Array.isArray(values.checks) ? values.checks : [],
    enabled: Boolean(values.enabled),
    intervalMinutes: Math.max(Number(values.intervalMinutes || 30), 5),
    metadata: analysisProfileId ? { analysisProfileId } : {},
  }
}

function defaultAutomationPolicyValues(): AutomationPolicyFormValues {
  return {
    name: '',
    triggerType: 'alert_webhook',
    analysisKinds: ['root_cause'],
    agentProviderId: 'internal',
    analysisProfileId: 'default',
    remediationPolicy: 'suggest_only',
    dedupWindowSeconds: 900,
    cooldownSeconds: 900,
    enabled: true,
    triggerSeverity: [],
    triggerStatus: ['firing'],
    triggerMinDurationSeconds: 120,
    triggerLabelKey: '',
    triggerLabelValue: '',
    triggerTimeRangeMinutes: 60,
    approvalRequired: false,
    approvalRoles: [],
  }
}

export function automationPolicyPayload(values: AutomationPolicyFormValues) {
  const secondsOrDefault = (value: number | undefined, fallback: number) => {
    const numberValue = Number(value ?? fallback)
    return Math.max(Number.isFinite(numberValue) ? numberValue : fallback, 60)
  }
  const analysisKinds = Array.isArray(values.analysisKinds)
    ? values.analysisKinds.map((item) => String(item).trim()).filter((item) => SUPPORTED_AUTOMATION_ANALYSIS_KINDS.has(item))
    : []
  const triggerLabelKey = String(values.triggerLabelKey ?? '').trim()
  const triggerLabelValue = String(values.triggerLabelValue ?? '').trim()
  const triggerConditions = {
    severity: Array.isArray(values.triggerSeverity) ? values.triggerSeverity.map((item) => String(item).trim()).filter(Boolean) : [],
    status: Array.isArray(values.triggerStatus) ? values.triggerStatus.map((item) => String(item).trim()).filter(Boolean) : [],
    min_duration_seconds: Number(values.triggerMinDurationSeconds ?? 0),
    time_range_minutes: Number(values.triggerTimeRangeMinutes ?? 0),
    labels: triggerLabelKey && triggerLabelValue ? { [triggerLabelKey]: triggerLabelValue } : {},
  }
  return {
    name: String(values.name ?? '').trim(),
    triggerType: 'alert_webhook',
    analysisKinds: analysisKinds.length > 0 ? analysisKinds : ['root_cause'],
    agentProviderId: String(values.agentProviderId ?? '').trim() || 'internal',
    triggerConditions,
    dedupWindowSeconds: secondsOrDefault(values.dedupWindowSeconds, 900),
    analysisProfileId: String(values.analysisProfileId ?? '').trim() || 'default',
    remediationPolicy: String(values.remediationPolicy ?? '').trim() || 'suggest_only',
    approvalPolicy: {
      required: Boolean(values.approvalRequired),
      approverRoles: Array.isArray(values.approvalRoles) ? values.approvalRoles.map((item) => String(item).trim()).filter(Boolean) : [],
    },
    cooldownSeconds: secondsOrDefault(values.cooldownSeconds, 900),
    enabled: Boolean(values.enabled),
  }
}

export function policyFormValuesFromRecord(policy: AutomationPolicy): AutomationPolicyFormValues {
  const conditions = policy.triggerConditions ?? {}
  const labels = (conditions.labels as Record<string, unknown> | undefined) ?? {}
  const labelKey = Object.keys(labels)[0] ?? ''
  const approval = policy.approvalPolicy ?? {}
  const analysisKinds = policy.analysisKinds
    ?.map((item) => String(item).trim())
    .filter((item) => SUPPORTED_AUTOMATION_ANALYSIS_KINDS.has(item))
  return {
    name: policy.name,
    triggerType: policy.triggerType === 'alert_webhook' ? policy.triggerType : 'alert_webhook',
    analysisKinds: analysisKinds?.length ? analysisKinds : ['root_cause'],
    agentProviderId: policy.agentProviderId || 'internal',
    analysisProfileId: policy.analysisProfileId || 'default',
    remediationPolicy: policy.remediationPolicy || 'suggest_only',
    dedupWindowSeconds: policy.dedupWindowSeconds || 900,
    cooldownSeconds: policy.cooldownSeconds || 900,
    enabled: policy.enabled,
    triggerSeverity: Array.isArray(conditions.severity) ? conditions.severity.map((item) => String(item)) : [],
    triggerStatus: Array.isArray(conditions.status) ? conditions.status.map((item) => String(item)) : [],
    triggerMinDurationSeconds: Number(conditions.min_duration_seconds ?? 120),
    triggerLabelKey: labelKey,
    triggerLabelValue: String(labels[labelKey] ?? ''),
    triggerTimeRangeMinutes: Number(conditions.time_range_minutes ?? 60),
    approvalRequired: Boolean(approval.required),
    approvalRoles: Array.isArray(approval.approverRoles) ? approval.approverRoles.map((item) => String(item)) : [],
  }
}

function buildScopeSummary(scope?: WorkbenchSessionScope) {
  if (!scope) return '未固定上下文'
  return [scope.clusterId, scope.namespace, scope.workload || scope.service, scope.alertId].filter(Boolean).join(' / ') || '未固定上下文'
}

export function AIObserveOverviewPage() {
  const navigate = useNavigate()
  const sessionsQuery = useQuery({
    queryKey: ['ai-observe-overview-sessions'],
    queryFn: () => api.get<ApiResponse<WorkbenchSession[]>>('/copilot/sessions'),
  })
  const insightsQuery = useQuery({
    queryKey: ['ai-observe-overview-insights'],
    queryFn: () => api.get<ApiResponse<Insight[]>>('/copilot/insights'),
  })
  const runsQuery = useQuery({
    queryKey: ['ai-observe-overview-runs'],
    queryFn: () => api.get<ApiResponse<RootCauseRun[]>>('/copilot/analysis/runs'),
  })
  const inspectionRunsQuery = useQuery({
    queryKey: ['ai-observe-overview-inspection-runs'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; status: string; severity: string; summary: string }>>>('/copilot/inspection-runs'),
  })

  const sessions = sessionsQuery.data?.data ?? []
  const insights = insightsQuery.data?.data ?? []
  const runs = runsQuery.data?.data ?? []
  const inspectionRuns = inspectionRunsQuery.data?.data ?? []

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title="AI工作台"
        description="面向全员的 AI 会话入口，统一承接通用问答、巡检复盘、根因、性能与工具链能力。"
        actions={
          <ManagementTableToolbar>
            <Button icon={<ToolOutlined />} onClick={() => navigate(getAIToolsPath())}>工具与技能</Button>
            <Button type="primary" icon={<RobotOutlined />} onClick={() => navigate(getAIWorkbenchPathForMode('general'))}>进入通用聊天</Button>
          </ManagementTableToolbar>
        }
      />

      <Card size="small" variant="outlined" className="soha-management-panel-card soha-ai-overview-console">
        <div className="soha-ai-overview-actions">
          {AI_OVERVIEW_ACTIONS.map((item) => (
            <button key={item.key} className="soha-ai-overview-action" type="button" onClick={() => navigate(item.href)}>
              <span className="soha-ai-overview-action-icon">{item.icon}</span>
              <span className="soha-ai-overview-action-main">
                <span className="soha-ai-overview-action-label">{item.label}</span>
                <span className="soha-ai-overview-action-detail">{item.detail}</span>
              </span>
            </button>
          ))}
        </div>
        <div className="soha-ai-overview-metrics">
          <Statistic title="AI 会话" value={sessions.length} prefix={<RobotOutlined />} />
          <Statistic title="根因运行" value={runs.length} prefix={<RadarChartOutlined />} />
          <Statistic title="巡检运行" value={inspectionRuns.length} prefix={<AppstoreOutlined />} />
          <Statistic title="AI 洞察" value={insights.length} prefix={<ToolOutlined />} />
        </div>
      </Card>

      <Row gutter={[12, 12]}>
        <Col xs={24} xl={8}>
          <Card size="small" variant="outlined" className="soha-compact-note-card" title="最近会话">
            {sessions.length === 0 ? (
              <ManagementState bordered={false} compact title="暂无会话" description="创建或进入 AI 工作台后，这里会展示最近会话。" />
            ) : (
              <List
                dataSource={sessions.slice(0, 5)}
                renderItem={(item) => (
                  <List.Item actions={[<Link key="open" to={getAIWorkbenchPathForSession(item)}>打开</Link>]}>
                    <List.Item.Meta title={displayWorkbenchSessionTitle(item.title)} description={item.metadata?.summary || item.updatedAt} />
                    {item.metadata?.mode ? <Tag>{item.metadata.mode}</Tag> : null}
                  </List.Item>
                )}
              />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={8}>
          <Card size="small" variant="outlined" className="soha-compact-note-card" title="最近分析">
            {runs.length === 0 ? (
              <ManagementState bordered={false} compact title="暂无根因运行" description="运行根因分析后，这里会展示最近结果。" />
            ) : (
              <List
                dataSource={runs.slice(0, 5)}
                renderItem={(item) => (
                  <List.Item>
                    <List.Item.Meta title={item.title} description={item.summary} />
                    <Space orientation="vertical" size={4}>
                      <StatusTag value={item.status} />
                      <StatusTag value={item.severity} />
                    </Space>
                  </List.Item>
                )}
              />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={8}>
          <Card size="small" variant="outlined" className="soha-compact-note-card" title="风险雷达">
            {insights.length === 0 ? (
              <ManagementState bordered={false} compact title="暂无风险信号" description="AI 洞察产生后，这里会展示需要关注的信号。" />
            ) : (
              <List
                dataSource={insights.slice(0, 5)}
                renderItem={(item) => (
                  <List.Item>
                    <List.Item.Meta title={item.title} description={item.description} />
                    <StatusTag value={item.severity} />
                  </List.Item>
                )}
              />
            )}
          </Card>
        </Col>
      </Row>
    </div>
  )
}

export function AIOperationsPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [taskForm] = Form.useForm<InspectionTaskFormValues>()
  const [policyForm] = Form.useForm<AutomationPolicyFormValues>()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canViewAI = hasPermission(permissionSnapshotQuery.data?.data, 'observe.ai.view')
  const canUseChat = hasPermission(permissionSnapshotQuery.data?.data, 'observe.ai.chat')
  const canRunInspection = hasPermission(permissionSnapshotQuery.data?.data, 'observe.ai.inspection.run')
  const canManageInspection = hasPermission(permissionSnapshotQuery.data?.data, 'observe.ai.inspection.manage')
  const canCreateSessionFromRun = canViewAI && canUseChat
  const canManageAISettings = hasPermission(permissionSnapshotQuery.data?.data, 'settings.ai.manage')
  const requestedView = searchParams.get('view')
  const requestedInspectionRunId = searchParams.get('inspectionRunId')?.trim() ?? ''
  const [activeView, setActiveView] = useState<'tasks' | 'runs' | 'policies'>(requestedView === 'runs' ? 'runs' : requestedView === 'policies' ? 'policies' : 'tasks')
  const [taskModalOpen, setTaskModalOpen] = useState(false)
  const [editingTask, setEditingTask] = useState<InspectionTask | null>(null)
  const [policyModalOpen, setPolicyModalOpen] = useState(false)
  const [editingPolicy, setEditingPolicy] = useState<AutomationPolicy | null>(null)
  const tasksQuery = useQuery({
    queryKey: ['ai-operations-tasks'],
    queryFn: () => api.get<ApiResponse<InspectionTask[]>>('/copilot/inspection-tasks'),
  })
  const runsQuery = useQuery({
    queryKey: ['ai-operations-runs'],
    queryFn: () => api.get<ApiResponse<InspectionRun[]>>('/copilot/inspection-runs'),
  })
  const policiesQuery = useQuery({
    queryKey: ['ai-operations-policies'],
    queryFn: () => api.get<ApiResponse<AutomationPolicy[]>>('/copilot/automation-policies'),
    enabled: canManageAISettings,
  })
  const catalogQuery = useQuery({
    queryKey: ['ai-operations-workbench-catalog'],
    queryFn: () => api.get<ApiResponse<WorkbenchCatalog>>('/copilot/workbench/catalog'),
  })
  const createSessionMutation = useMutation({
    mutationFn: (runId: string) => api.post<ApiResponse<WorkbenchSession>>(`/copilot/inspection-runs/${runId}/session`),
    onSuccess: (response) => {
      void message.success('已从巡检运行创建 AI 会话')
      void queryClient.invalidateQueries({ queryKey: ['ai-observe-overview-sessions'] })
      navigate(getAIWorkbenchPathForSession(response.data))
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const createTaskMutation = useMutation({
    mutationFn: (values: InspectionTaskFormValues) => api.post<ApiResponse<InspectionTask>>('/copilot/inspection-tasks', inspectionTaskPayload(values)),
    onSuccess: async () => {
      void message.success('巡检任务已创建')
      await queryClient.invalidateQueries({ queryKey: ['ai-operations-tasks'] })
      setTaskModalOpen(false)
      setEditingTask(null)
      taskForm.resetFields()
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const updateTaskMutation = useMutation({
    mutationFn: (payload: { taskId: string; values: InspectionTaskFormValues }) =>
      api.put<ApiResponse<InspectionTask>>(`/copilot/inspection-tasks/${payload.taskId}`, inspectionTaskPayload(payload.values)),
    onSuccess: async () => {
      void message.success('巡检任务已更新')
      await queryClient.invalidateQueries({ queryKey: ['ai-operations-tasks'] })
      setTaskModalOpen(false)
      setEditingTask(null)
      taskForm.resetFields()
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const deleteTaskMutation = useMutation({
    mutationFn: (taskId: string) => api.delete(`/copilot/inspection-tasks/${taskId}`),
    onSuccess: async () => {
      void message.success('巡检任务已删除')
      await queryClient.invalidateQueries({ queryKey: ['ai-operations-tasks'] })
      await queryClient.invalidateQueries({ queryKey: ['ai-operations-runs'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const createPolicyMutation = useMutation({
    mutationFn: (values: AutomationPolicyFormValues) => api.post<ApiResponse<AutomationPolicy>>('/copilot/automation-policies', automationPolicyPayload(values)),
    onSuccess: async () => {
      void message.success('自动化策略已创建')
      await queryClient.invalidateQueries({ queryKey: ['ai-operations-policies'] })
      setPolicyModalOpen(false)
      setEditingPolicy(null)
      policyForm.resetFields()
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const updatePolicyMutation = useMutation({
    mutationFn: (payload: { policyId: string; values: AutomationPolicyFormValues }) =>
      api.put<ApiResponse<AutomationPolicy>>(`/copilot/automation-policies/${payload.policyId}`, automationPolicyPayload(payload.values)),
    onSuccess: async () => {
      void message.success('自动化策略已更新')
      await queryClient.invalidateQueries({ queryKey: ['ai-operations-policies'] })
      setPolicyModalOpen(false)
      setEditingPolicy(null)
      policyForm.resetFields()
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const deletePolicyMutation = useMutation({
    mutationFn: (policyId: string) => api.delete(`/copilot/automation-policies/${policyId}`),
    onSuccess: async () => {
      void message.success('自动化策略已删除')
      await queryClient.invalidateQueries({ queryKey: ['ai-operations-policies'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const executeMutation = useMutation({
    mutationFn: (taskId: string) => api.post(`/copilot/inspection-tasks/${taskId}/execute`),
    onSuccess: () => {
      void message.success('巡检已执行')
      void queryClient.invalidateQueries({ queryKey: ['ai-operations-runs'] })
      void queryClient.invalidateQueries({ queryKey: ['ai-operations-tasks'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const tasks = tasksQuery.data?.data ?? []
  const runs = runsQuery.data?.data ?? []
  const policies = policiesQuery.data?.data ?? []
  const profiles: AnalysisProfile[] = catalogQuery.data?.data?.analysisProfiles ?? []
  const agentProviders = catalogQuery.data?.data?.agentProviders ?? []
  const profileOptions = profiles
    .filter((item) => item.enabled)
    .map((item) => ({ value: item.id, label: `${item.name} (${item.mode})` }))
  const agentProviderOptions = agentProviders
    .filter((item) => item.enabled)
    .map((item) => ({ value: item.id, label: `${item.name}${item.supportsAsync ? ' / async' : ' / inline'}` }))
  const watchedScopeType = Form.useWatch('scopeType', taskForm)
  const taskSaving = createTaskMutation.isPending || updateTaskMutation.isPending
  const policySaving = createPolicyMutation.isPending || updatePolicyMutation.isPending

  useEffect(() => {
    if (requestedView === 'runs' || requestedInspectionRunId) {
      setActiveView('runs')
      return
    }
    if (requestedView === 'policies') {
      setActiveView('policies')
    }
  }, [requestedInspectionRunId, requestedView])

  const openCreateTask = () => {
    setEditingTask(null)
    taskForm.setFieldsValue(defaultInspectionTaskValues())
    setTaskModalOpen(true)
  }
  const openEditTask = (task: InspectionTask) => {
    setEditingTask(task)
    taskForm.setFieldsValue({
      title: task.title,
      scopeType: task.scopeType || 'platform',
      clusterId: task.clusterId || '',
      namespace: task.namespace || '',
      checks: task.checks ?? [],
      enabled: task.enabled,
      intervalMinutes: task.intervalMinutes || 30,
      analysisProfileId: String(task.metadata?.analysisProfileId ?? ''),
    })
    setTaskModalOpen(true)
  }
  const closeTaskModal = () => {
    setTaskModalOpen(false)
    setEditingTask(null)
    taskForm.resetFields()
  }
  const submitTaskForm = async () => {
    const values = await taskForm.validateFields()
    if (editingTask) {
      updateTaskMutation.mutate({ taskId: editingTask.id, values })
    } else {
      createTaskMutation.mutate(values)
    }
  }
  const openCreatePolicy = () => {
    if (!canManageAISettings) return
    setEditingPolicy(null)
    policyForm.setFieldsValue(defaultAutomationPolicyValues())
    setPolicyModalOpen(true)
  }
  const openEditPolicy = (policy: AutomationPolicy) => {
    if (!canManageAISettings) return
    setEditingPolicy(policy)
    policyForm.setFieldsValue(policyFormValuesFromRecord(policy))
    setPolicyModalOpen(true)
  }
  const closePolicyModal = () => {
    setPolicyModalOpen(false)
    setEditingPolicy(null)
    policyForm.resetFields()
  }
  const submitPolicyForm = async () => {
    const values = await policyForm.validateFields()
    if (editingPolicy) {
      updatePolicyMutation.mutate({ policyId: editingPolicy.id, values })
    } else {
      createPolicyMutation.mutate(values)
    }
  }

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title="巡检与自动化"
        description="统一查看巡检任务、巡检运行、自动化策略，并把发现结果送入 AI 会话。"
        actions={
          <ManagementTableToolbar>
            <Button onClick={() => navigate(getAIWorkbenchPathForMode('inspection_review'))} disabled={!canUseChat}>进入巡检复盘工作台</Button>
            <Button icon={<PlusOutlined />} onClick={openCreateTask} disabled={!canManageInspection} title={canManageInspection ? undefined : '缺少 observe.ai.inspection.manage 权限'}>
              新建巡检任务
            </Button>
            <Button type="primary" onClick={() => navigate(getAIWorkbenchPathForMode('general'))} disabled={!canUseChat}>新建会话</Button>
          </ManagementTableToolbar>
        }
      />
      <Card styles={{ body: { paddingBottom: 8 } }}>
        <Segmented
          value={activeView}
          onChange={(value) => setActiveView(value as typeof activeView)}
          options={[
            { value: 'tasks', label: '巡检任务' },
            { value: 'runs', label: '巡检运行' },
            { value: 'policies', label: '自动化策略' },
          ]}
        />
        <Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0 }}>
          把巡检任务、巡检运行与自动化策略放在同一工作区，避免在调查和自动化之间来回跳转。
        </Paragraph>
      </Card>

      {activeView === 'tasks' ? (
        <Card
          size="small"
          variant="outlined"
          className="soha-management-panel-card"
          title="巡检任务"
          extra={(
            <Button size="small" icon={<PlusOutlined />} onClick={openCreateTask} disabled={!canManageInspection}>
              新建任务
            </Button>
          )}
        >
          <AdminTable
            shellClassName="soha-management-table-shell"
            columnSettingIconOnly
            columnSettingPlacement="header"
            rowKey="id"
            dataSource={tasks}
            loading={tasksQuery.isLoading}
            tableSize="small"
            pageSize={10}
            columns={[
              { title: '任务名称', dataIndex: 'title' },
              { title: '范围', dataIndex: 'scopeType', render: (_value: string, record: InspectionTask) => [record.scopeType, record.clusterId, record.namespace].filter(Boolean).join(' / ') },
              { title: '检查项', dataIndex: 'checks', render: (value: string[]) => <Space wrap>{(value ?? []).map((item) => <Tag key={item}>{item}</Tag>)}</Space> },
              { title: '间隔', dataIndex: 'intervalMinutes', render: (value: number) => `${value} min` },
              { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <StatusTag value={value ? 'enabled' : 'disabled'} /> },
              {
                ...tableColumnPresets.action,
                title: '操作',
                dataIndex: 'id',
                render: (_value: string, record: InspectionTask) => (
                  <Space className="soha-row-action-icons">
                    <ManagementIconButton
                      aria-label="编辑巡检任务"
                      size="small"
                      tooltip="编辑"
                      icon={<EditOutlined />}
                      onClick={() => openEditTask(record)}
                      disabled={!canManageInspection}
                      title={canManageInspection ? undefined : '缺少 observe.ai.inspection.manage 权限'}
                    />
                    <ManagementIconButton
                      aria-label="立即执行巡检"
                      size="small"
                      tooltip="立即执行"
                      icon={<PlayCircleOutlined />}
                      loading={executeMutation.isPending}
                      onClick={() => executeMutation.mutate(record.id)}
                      disabled={!canRunInspection}
                      title={canRunInspection ? undefined : '缺少 observe.ai.inspection.run 权限'}
                    />
                    <Popconfirm
                      title="确认删除巡检任务？"
                      description="关联巡检运行记录会一并删除。"
                      onConfirm={() => deleteTaskMutation.mutate(record.id)}
                      okButtonProps={{ danger: true, loading: deleteTaskMutation.isPending }}
                    >
                      <ManagementIconButton
                        aria-label="删除巡检任务"
                        size="small"
                        tooltip="删除"
                        icon={<DeleteOutlined />}
                        danger
                        disabled={!canManageInspection}
                        title={canManageInspection ? undefined : '缺少 observe.ai.inspection.manage 权限'}
                      />
                    </Popconfirm>
                  </Space>
                ),
              },
            ]}
          />
        </Card>
      ) : null}

      <Modal
        title={editingTask ? '编辑巡检任务' : '新建巡检任务'}
        open={taskModalOpen}
        onCancel={closeTaskModal}
        onOk={submitTaskForm}
        okText={editingTask ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={taskSaving}
        okButtonProps={{ disabled: !canManageInspection }}
        width={640}
      >
        <Form form={taskForm} layout="vertical" preserve={false}>
          <Form.Item
            name="title"
            label="任务名称"
            rules={[{ required: true, message: '请输入任务名称' }]}
          >
            <Input placeholder="例如：支付命名空间巡检" />
          </Form.Item>
          <Form.Item name="scopeType" label="巡检范围" rules={[{ required: true, message: '请选择巡检范围' }]}>
            <Select
              options={[
                { value: 'platform', label: '平台级' },
                { value: 'cluster', label: '集群级' },
                { value: 'namespace', label: '命名空间级' },
              ]}
            />
          </Form.Item>
          {watchedScopeType === 'cluster' || watchedScopeType === 'namespace' ? (
            <Form.Item name="clusterId" label="集群 ID" rules={[{ required: true, message: '请输入集群 ID' }]}>
              <Input placeholder="local-k3s" />
            </Form.Item>
          ) : null}
          {watchedScopeType === 'namespace' ? (
            <Form.Item name="namespace" label="命名空间" rules={[{ required: true, message: '请输入命名空间' }]}>
              <Input placeholder="default" />
            </Form.Item>
          ) : null}
          <Form.Item name="checks" label="检查项" rules={[{ required: true, message: '请选择检查项' }]}>
            <Select mode="multiple" options={INSPECTION_CHECK_OPTIONS} />
          </Form.Item>
          <Form.Item name="analysisProfileId" label="巡检模板">
            <Select
              showSearch
              allowClear
              optionFilterProp="label"
              loading={catalogQuery.isLoading}
              placeholder="可选：按分析模板覆盖巡检 playbooks"
              options={profiles.filter((item) => item.mode === 'inspection' && item.enabled).map((item) => ({ value: item.id, label: `${item.name} (${item.id})` }))}
            />
          </Form.Item>
          <Form.Item name="intervalMinutes" label="执行间隔(分钟)" rules={[{ required: true, message: '请输入执行间隔' }]}>
            <InputNumber min={5} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      {activeView === 'runs' ? (
        <Card size="small" variant="outlined" className="soha-management-panel-card" title="巡检运行记录">
          {requestedInspectionRunId ? (
            <Alert
              type={runs.some((item) => item.id === requestedInspectionRunId) ? 'info' : 'warning'}
              showIcon
              title={runs.some((item) => item.id === requestedInspectionRunId) ? `已定位巡检运行 ${requestedInspectionRunId}` : '未找到关联巡检运行'}
              description={runs.some((item) => item.id === requestedInspectionRunId) ? '该运行来自分析工件关联入口。' : `inspectionRunId=${requestedInspectionRunId}`}
              style={{ marginBottom: 12 }}
            />
          ) : null}
          <AdminTable
            shellClassName="soha-management-table-shell"
            columnSettingIconOnly
            columnSettingPlacement="header"
            rowKey="id"
            dataSource={runs}
            loading={runsQuery.isLoading}
            tableSize="small"
            pageSize={10}
            columns={[
              { title: '运行 ID', dataIndex: 'id', render: (value: string) => <Space size={6} wrap><Text>{value}</Text>{value === requestedInspectionRunId ? <Tag color="blue">已定位</Tag> : null}</Space> },
              { title: '任务', dataIndex: 'taskId' },
              { title: '状态', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
              { title: '严重度', dataIndex: 'severity', render: (value: string) => <StatusTag value={value} /> },
              { title: '发现项', dataIndex: 'findings', render: (value: Array<{ id: string }>) => value?.length ?? 0 },
              { title: '摘要', dataIndex: 'summary' },
              {
                ...tableColumnPresets.action,
                title: '联动',
                dataIndex: 'id',
                render: (value: string) => (
                  <ManagementIconButton
                    aria-label="创建 AI 会话"
                    size="small"
                    tooltip="创建 AI 会话"
                    icon={<RobotOutlined />}
                    onClick={() => createSessionMutation.mutate(value)}
                    disabled={!canCreateSessionFromRun}
                    title={canCreateSessionFromRun ? undefined : !canUseChat ? '缺少 observe.ai.chat 权限' : '缺少 observe.ai.view 权限'}
                  />
                ),
              },
            ]}
          />
        </Card>
      ) : null}

      {activeView === 'policies' ? (
        <Card
          size="small"
          variant="outlined"
          className="soha-management-panel-card"
          title="自动化策略"
          extra={(
            <Button size="small" icon={<PlusOutlined />} onClick={openCreatePolicy} disabled={!canManageAISettings} title={canManageAISettings ? undefined : '缺少 settings.ai.manage 权限'}>
              新建策略
            </Button>
          )}
        >
          <Paragraph type="secondary">
            自动化策略只负责触发和分析范围，不应隐式替代会话级 toolset 选择。需要深入分析时，优先把结果送回 AI 工作台。
          </Paragraph>
          {!canManageAISettings ? (
            <Alert
              type="warning"
              showIcon
              title="缺少 settings.ai.manage 权限"
              description="自动化策略包含全局 AI 执行配置，当前账号不能查看或编辑。巡检任务和运行记录仍可继续使用。"
              style={{ marginBottom: 16 }}
            />
          ) : null}
          <AdminTable
            shellClassName="soha-management-table-shell"
            columnSettingIconOnly
            columnSettingPlacement="header"
            rowKey="id"
            dataSource={policies}
            loading={policiesQuery.isLoading}
            tableSize="small"
            pageSize={10}
            columns={[
              { title: '名称', dataIndex: 'name' },
              { title: '触发类型', dataIndex: 'triggerType' },
              { title: '分析类型', dataIndex: 'analysisKinds', render: (value: string[]) => <Space wrap>{(value ?? []).map((item) => <Tag key={item}>{item}</Tag>)}</Space> },
              {
                title: 'Agent',
                dataIndex: 'agentProviderId',
                render: (value: string) => agentProviders.find((item) => item.id === (value || 'internal'))?.name || value || 'internal',
              },
              {
                title: '分析模板',
                dataIndex: 'analysisProfileId',
                render: (value: string) => profiles.find((item) => item.id === value)?.name || value,
              },
              { title: '修复策略', dataIndex: 'remediationPolicy' },
              { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <StatusTag value={value ? 'enabled' : 'disabled'} /> },
              {
                ...tableColumnPresets.action,
                title: '操作',
                dataIndex: 'id',
                render: (_value: string, record: AutomationPolicy) => (
                  <Space className="soha-row-action-icons">
                    <ManagementIconButton
                      aria-label="编辑自动化策略"
                      size="small"
                      tooltip="编辑"
                      icon={<EditOutlined />}
                      onClick={() => openEditPolicy(record)}
                      disabled={!canManageAISettings}
                      title={canManageAISettings ? undefined : '缺少 settings.ai.manage 权限'}
                    />
                    <Popconfirm
                      title="确认删除自动化策略？"
                      description="删除后不会再由该策略触发新的 AI 分析。"
                      onConfirm={() => deletePolicyMutation.mutate(record.id)}
                      okButtonProps={{ danger: true, loading: deletePolicyMutation.isPending }}
                    >
                      <ManagementIconButton
                        aria-label="删除自动化策略"
                        size="small"
                        tooltip="删除"
                        icon={<DeleteOutlined />}
                        danger
                        disabled={!canManageAISettings}
                        title={canManageAISettings ? undefined : '缺少 settings.ai.manage 权限'}
                      />
                    </Popconfirm>
                  </Space>
                ),
              },
            ]}
          />
        </Card>
      ) : null}

      <Modal
        title={editingPolicy ? '编辑自动化策略' : '新建自动化策略'}
        open={policyModalOpen}
        onCancel={closePolicyModal}
        onOk={submitPolicyForm}
        okText={editingPolicy ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={policySaving}
        okButtonProps={{ disabled: !canManageAISettings }}
        width={680}
      >
        <Form form={policyForm} layout="vertical" preserve={false}>
          <Form.Item name="name" label="策略名称" rules={[{ required: true, message: '请输入策略名称' }]}>
            <Input placeholder="例如：P1 告警根因分析" />
          </Form.Item>
          <Form.Item name="triggerType" label="触发类型" rules={[{ required: true, message: '请选择触发类型' }]}>
            <Select options={AUTOMATION_TRIGGER_TYPE_OPTIONS} />
          </Form.Item>
          <Form.Item name="analysisKinds" label="分析类型" rules={[{ required: true, message: '请选择分析类型' }]}>
            <Select mode="multiple" options={AUTOMATION_ANALYSIS_KIND_OPTIONS} />
          </Form.Item>
          <Form.Item name="agentProviderId" label="Agent Provider" rules={[{ required: true, message: '请选择 Agent Provider' }]}>
            <Select
              showSearch
              optionFilterProp="label"
              loading={catalogQuery.isLoading}
              options={agentProviderOptions.length > 0 ? agentProviderOptions : [{ value: 'internal', label: 'soha 内置分析 / inline' }]}
            />
          </Form.Item>
          <Form.Item name="analysisProfileId" label="分析模板" rules={[{ required: true, message: '请选择分析模板' }]}>
            <Select
              showSearch
              allowClear
              optionFilterProp="label"
              loading={catalogQuery.isLoading}
              placeholder="选择后端分析模板"
              options={profileOptions}
            />
          </Form.Item>
          <Form.Item name="remediationPolicy" label="修复策略">
            <Select options={AUTOMATION_REMEDIATION_POLICY_OPTIONS} />
          </Form.Item>
          <Flex gap={12}>
            <Form.Item name="dedupWindowSeconds" label="去重窗口(秒)" style={{ flex: 1 }}>
              <InputNumber min={60} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="cooldownSeconds" label="冷却时间(秒)" style={{ flex: 1 }}>
              <InputNumber min={60} style={{ width: '100%' }} />
            </Form.Item>
          </Flex>
          <Form.Item name="triggerSeverity" label="告警级别">
            <Select mode="multiple" allowClear options={AUTOMATION_SEVERITY_OPTIONS} />
          </Form.Item>
          <Form.Item name="triggerStatus" label="告警状态">
            <Select mode="multiple" allowClear options={AUTOMATION_STATUS_OPTIONS} />
          </Form.Item>
          <Flex gap={12}>
            <Form.Item name="triggerMinDurationSeconds" label="最小持续(秒)" style={{ flex: 1 }}>
              <InputNumber min={0} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="triggerTimeRangeMinutes" label="分析时间范围(分钟)" style={{ flex: 1 }}>
              <InputNumber min={5} style={{ width: '100%' }} />
            </Form.Item>
          </Flex>
          <Flex gap={12}>
            <Form.Item name="triggerLabelKey" label="标签 Key" style={{ flex: 1 }}>
              <Input placeholder="service" />
            </Form.Item>
            <Form.Item name="triggerLabelValue" label="标签 Value" style={{ flex: 1 }}>
              <Input placeholder="payment-api" />
            </Form.Item>
          </Flex>
          <Form.Item name="approvalRequired" label="需要审批" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="approvalRoles" label="审批角色">
            <Select mode="tags" tokenSeparators={[',']} placeholder="ops / sre / owner" />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}

export function AIToolsPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const requestedSessionId = searchParams.get('session') || undefined
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [selectedAdapterIds, setSelectedAdapterIds] = useState<string[]>([])
  const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([])
  const [disabledToolNames, setDisabledToolNames] = useState<string[]>([])
  const [budgetOverrides, setBudgetOverrides] = useState<Record<string, number>>({})
  const [scopeOverrides, setScopeOverrides] = useState<Partial<WorkbenchSessionScope>>({})
  const catalogQuery = useQuery({
    queryKey: ['ai-tools-catalog'],
    queryFn: () => api.get<ApiResponse<WorkbenchCatalog>>('/copilot/workbench/catalog'),
  })
  const sessionDetailQuery = useQuery({
    queryKey: ['copilot-workbench-session-detail', requestedSessionId],
    queryFn: () => api.get<ApiResponse<WorkbenchSession>>(`/copilot/sessions/${requestedSessionId}`),
    enabled: Boolean(requestedSessionId),
  })

  const adapters = useMemo(() => catalogQuery.data?.data?.adapters ?? [], [catalogQuery.data?.data?.adapters])
  const dataSources = useMemo(() => catalogQuery.data?.data?.dataSources ?? [], [catalogQuery.data?.data?.dataSources])
  const skills = useMemo(() => catalogQuery.data?.data?.skillsRegistry ?? [], [catalogQuery.data?.data?.skillsRegistry])
  const currentSession = sessionDetailQuery.data?.data
  const disabledToolOptions = useMemo(() => buildDisabledToolOptions(adapters), [adapters])
  const cleanedBudgetOverrides = useMemo(() => numberRecord(budgetOverrides), [budgetOverrides])
  const cleanedScopeOverrides = useMemo(() => scopeOverrideState(scopeOverrides), [scopeOverrides])
  const activeDataSourceAdapters = [...new Set(dataSources.filter((item) => item.enabled).map((item) => item.mcpAdapter).filter(Boolean))]
  const unavailableSelectedAdapterIds = selectedAdapterIds.filter((adapterId) => (
    adapterId !== 'platform-native.v1' && !activeDataSourceAdapters.includes(adapterId)
  ))

  useEffect(() => {
    setSelectedAdapterIds(currentSession?.metadata?.toolset?.enabledAdapterIds ?? [])
    setSelectedSkillIds(currentSession?.metadata?.toolset?.enabledSkillIds ?? [])
    setDisabledToolNames(canonicalDisabledToolNames(currentSession?.metadata?.toolset?.disabledToolNames ?? [], adapters))
    setBudgetOverrides(numberRecord(currentSession?.metadata?.toolset?.budgetOverrides))
    setScopeOverrides(scopeOverrideState(currentSession?.metadata?.toolset?.scopeOverrides))
  }, [
    adapters,
    currentSession?.id,
    currentSession?.metadata?.toolset?.budgetOverrides,
    currentSession?.metadata?.toolset?.disabledToolNames,
    currentSession?.metadata?.toolset?.enabledAdapterIds,
    currentSession?.metadata?.toolset?.enabledSkillIds,
    currentSession?.metadata?.toolset?.scopeOverrides,
  ])

  const patchSessionMutation = useMutation({
    mutationFn: (payload: { sessionId: string; body: Record<string, unknown> }) =>
      api.patch<ApiResponse<WorkbenchSession>>(`/copilot/sessions/${payload.sessionId}`, payload.body),
    onSuccess: async (_response, payload) => {
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-sessions'] })
      await queryClient.invalidateQueries({ queryKey: ['copilot-workbench-session-detail', payload.sessionId] })
      void message.success('会话级工具装配已更新')
    },
    onError: (err: Error) => void message.error(err.message),
  })

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

  const applyRecommendedToolset = () => {
    setSelectedAdapterIds(recommendedAdapterIds(adapters, dataSources))
    setSelectedSkillIds(skills.filter((item) => item.enabled).map((item) => item.id))
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

  const saveToolset = () => {
    if (!requestedSessionId || !currentSession) return
    patchSessionMutation.mutate({
      sessionId: requestedSessionId,
      body: {
        toolset: cleanToolsetPayload({
          enabledAdapterIds: selectedAdapterIds,
          enabledSkillIds: selectedSkillIds,
          disabledToolNames: canonicalDisabledToolNames(disabledToolNames, adapters),
          budgetOverrides: cleanedBudgetOverrides,
          scopeOverrides: cleanedScopeOverrides,
        }),
      },
    })
  }

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title="工具与技能"
        description="全局配置镜像与会话级装配入口，统一查看 MCP adapters、数据源和技能能力。"
      />
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={12}>
          <Card title="MCP Adapters">
            <List
              dataSource={adapters}
              renderItem={(item) => (
                <List.Item>
                  <List.Item.Meta
                    title={<Space><Text strong>{item.name}</Text><Tag>{item.sourceKind}</Tag></Space>}
                    description={item.description}
                  />
                </List.Item>
              )}
            />
          </Card>
        </Col>
        <Col xs={24} xl={12}>
          <Card title="Data Sources">
            <List
              dataSource={dataSources}
              renderItem={(item) => (
                <List.Item>
                  <List.Item.Meta
                    title={<Space><Text strong>{item.name}</Text><Tag>{item.backendType}</Tag></Space>}
                    description={`${item.sourceKind} / ${item.mcpAdapter}`}
                  />
                  <StatusTag value={item.validationStatus || (item.enabled ? 'enabled' : 'disabled')} />
                </List.Item>
              )}
            />
          </Card>
        </Col>
        <Col xs={24}>
          <Card title="会话级装配">
            {!requestedSessionId || !currentSession ? (
              <ManagementState bordered={false} compact kind="select-scope" title="未选择会话" description="先从左侧菜单进入一个会话，再配置工具装配。" />
            ) : (
              <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                <Flex justify="space-between" align="start" gap={12} wrap="wrap">
                  <Space size={[8, 8]} wrap>
                    <Tag color="blue">{displayWorkbenchSessionTitle(currentSession.title)}</Tag>
                    <Tag>{currentSession.metadata?.mode || 'general'}</Tag>
                    <Tag>{buildScopeSummary(currentSession.metadata?.scope)}</Tag>
                    <Tag>{selectedAdapterIds.length > 0 ? `${selectedAdapterIds.length} adapters` : 'auto adapters'}</Tag>
                    <Tag>{disabledToolNames.length} disabled tools</Tag>
                    <Tag>{countObjectKeys(cleanedBudgetOverrides)} budgets</Tag>
                  </Space>
                  <Space wrap>
                    <Button onClick={clearToolset}>恢复自动选择</Button>
                    <Button onClick={applyRecommendedToolset}>应用推荐预设</Button>
                    <Button type="primary" loading={patchSessionMutation.isPending} onClick={saveToolset}>
                      保存会话级装配
                    </Button>
                  </Space>
                </Flex>

                {unavailableSelectedAdapterIds.length > 0 ? (
                  <Alert
                    type="warning"
                    showIcon
                    title="部分已选 adapter 当前没有启用数据源"
                    description={`${unavailableSelectedAdapterIds.join(', ')} 会保留在会话策略中，但运行时相关工具可能被跳过。`}
                  />
                ) : null}

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
                    <Paragraph type="secondary" style={{ marginBottom: 0 }}>
                      禁用工具会以 `adapter.tool` 形式保存，避免同名工具跨 adapter 被误屏蔽。
                    </Paragraph>
                  </Space>
                </Card>

                <Card size="small" title="Skills">
                  <Select
                    mode="multiple"
                    allowClear
                    maxTagCount="responsive"
                    optionFilterProp="label"
                    placeholder="选择会话级技能；留空表示沿用全局启用项"
                    value={selectedSkillIds}
                    onChange={(value: string[]) => setSelectedSkillIds(value)}
                    options={skills.filter((item) => item.enabled).map((item) => ({ value: item.id, label: item.name }))}
                  />
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
          </Card>
        </Col>
        <Col xs={24}>
          <Card title="Skills Registry">
            <List
              dataSource={skills}
              locale={{ emptyText: '暂无全局 skills 配置' }}
              renderItem={(item) => (
                <List.Item>
                  <List.Item.Meta
                    title={<Space><Text strong>{item.name}</Text><Tag>{item.id}</Tag></Space>}
                    description={item.description || (item.scopes ?? []).join(', ')}
                  />
                  <StatusTag value={item.enabled ? 'enabled' : 'disabled'} />
                </List.Item>
              )}
            />
            <Space style={{ marginTop: 16 }}>
              <Button onClick={() => navigate(getAIModelSettingsPath(searchParams))}>前往 AI 设置</Button>
              <Button type="primary" onClick={() => navigate(getAIWorkbenchPathForMode(currentSession?.metadata?.mode, searchParams))}>回到 AI 工作台</Button>
            </Space>
          </Card>
        </Col>
      </Row>
    </div>
  )
}

export function AIModelSettingsPage() {
  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title="AI 设置"
        description="在 AI 工作台内查看和调整 Provider、数据源、技能与自动化策略。"
      />
      <AISettingsPage embedded />
    </div>
  )
}

export { AIWorkbenchPage }
