import { useMemo, useState } from 'react'
import { App, Avatar, Badge, Button, Calendar, Card, Col, DatePicker, Descriptions, Drawer, Form, Input, InputNumber, Modal, Radio, Row, Segmented, Select, Space, Statistic, Switch, Tag, Tabs, Timeline, Tooltip, Typography } from 'antd'
import type { CalendarProps } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlusOutlined, PlayCircleOutlined, EditOutlined, CheckOutlined, CloseOutlined, ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import dayjs from 'dayjs'
import type { Dayjs } from 'dayjs'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementDetailHeader,
  ManagementIconButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { StatusTag, BooleanTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import type { ApiResponse } from '@/types'
import { ReleaseFlowDagEditor } from '@/components/release-flow-dag-editor'
import { createDefaultReleaseDagDefinition, normalizeReleaseDagDefinition } from '@/components/release-flow-dag-definition'
import type { ReleaseDagDefinition } from '@/components/release-flow-dag-definition'
import './observability-pages.css'
import { useNavigate, useParams } from 'react-router-dom'
import { AlertEventDetailPageContent } from '@/features/observability/alert-event-detail'

const { Text, Paragraph } = Typography

interface AlertRule {
  id: string
  name: string
  ruleType: string
  datasourceSelector?: Record<string, unknown>
  querySpec?: Record<string, unknown>
  thresholdSpec?: Record<string, unknown>
  forSeconds: number
  groupBy?: string[]
  labels?: Record<string, string>
  annotations?: Record<string, string>
  notificationPolicyId?: string
  healingPolicyIds?: string[]
  enabled: boolean
  createdAt: string
  updatedAt: string
}

interface NotificationPolicy {
  id: string
  name: string
  enabled: boolean
}

interface HealingPolicy {
  id: string
  name: string
  triggerMode: string
  workflowTemplateId: string
  approvalPolicyRef?: string
  cooldownSeconds: number
  concurrencyKey?: string
  safetyWindowSeconds: number
  definition?: ReleaseDagDefinition
  enabled: boolean
}

interface HealingRun {
  id: string
  policyId: string
  eventId?: string
  status: string
  approvalStatus?: string
  approvalComment?: string
  requestedBy?: string
  approvedBy?: string
  workflowRunId?: string
  workflowStatus?: string
  workflowSummary?: string
  result?: Record<string, unknown>
  startedAt?: string
  completedAt?: string
  createdAt: string
  updatedAt: string
}

interface OnCallSchedule {
  id: string
  name: string
  timeZone?: string
  description?: string
  enabled: boolean
  createdAt: string
  updatedAt: string
}

interface OnCallRotation {
  id: string
  scheduleId: string
  name: string
  participants?: string[]
  rotationConfig?: Record<string, unknown>
  enabled: boolean
  createdAt: string
  updatedAt: string
}

interface OnCallEscalationPolicy {
  id: string
  name: string
  steps?: Array<Record<string, unknown>>
  enabled: boolean
  createdAt: string
  updatedAt: string
}

interface OnCallAssignmentRule {
  id: string
  name: string
  integrationId?: string
  integrationType?: string
  businessLineId?: string
  alertCategory?: string
  alertName?: string
  severity?: string
  service?: string
  role?: string
  matchers?: Record<string, unknown>
  targetType: 'schedule' | 'escalation'
  targetRef: string
  routeOrder: number
  groupBy?: string[]
  priority: number
  enabled: boolean
  createdAt: string
  updatedAt: string
}

interface OnCallTask {
  id: string
  eventId: string
  title: string
  summary?: string
  severity: string
  status: string
  integrationId?: string
  integrationType?: string
  clusterId?: string
  namespace?: string
  service?: string
  businessLineId?: string
  routeId?: string
  routeName?: string
  groupKey?: string
  groupBy?: string[]
  targetType?: 'schedule' | 'escalation'
  targetRef?: string
  currentParticipant?: string
  participants?: string[]
  resolutionStatus: string
  labels?: Record<string, string>
  lastSeenAt?: string
  createdAt: string
  updatedAt: string
}

interface AlertRuleFormValues {
  id?: string
  name: string
  ruleType: string
  datasourceSelector: string
  querySpec: string
  thresholdSpec: string
  forSeconds: number
  groupBy: string
  labels: string
  annotations: string
  notificationPolicyId?: string
  healingPolicyIds: string[]
  enabled: boolean
}

interface HealingPolicyFormValues {
  id?: string
  name: string
  triggerMode: string
  workflowTemplateId: string
  approvalPolicyRef?: string
  cooldownSeconds: number
  concurrencyKey?: string
  safetyWindowSeconds: number
  enabled: boolean
}

function safeParseJson(raw: string, fallback: any) {
  const text = raw.trim()
  if (!text) return fallback
  const parsed = JSON.parse(text)
  if (Array.isArray(fallback) && Array.isArray(parsed)) return parsed
  if (!Array.isArray(fallback) && parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return parsed
  throw new Error('需要合法 JSON 对象')
}

function safeParseStringArray(raw: string) {
  return raw.split(',').map((item) => item.trim()).filter(Boolean)
}

function prettyJson(value: unknown) {
  if (value == null) return ''
  return JSON.stringify(value, null, 2)
}

const ONCALL_DATE_FORMAT = 'YYYY-MM-DD'

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function normalizeParticipantList(value: unknown) {
  if (Array.isArray(value)) {
    return value.map((item) => String(item).trim()).filter(Boolean)
  }
  if (typeof value === 'string') {
    const text = value.trim()
    if (!text) return []
    if (text.startsWith('[')) {
      try {
        const parsed = JSON.parse(text)
        if (Array.isArray(parsed)) {
          return normalizeParticipantList(parsed)
        }
      } catch {
        // Fall back to comma splitting below.
      }
    }
    return text.split(',').map((item) => item.trim()).filter(Boolean)
  }
  return []
}

function readRotationOverrides(rotationConfig?: Record<string, unknown>) {
  const rawOverrides = rotationConfig?.overrides
  if (!isPlainRecord(rawOverrides)) return {}
  return Object.entries(rawOverrides).reduce<Record<string, string[]>>((acc, [dateKey, value]) => {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(dateKey)) return acc
    const participants = isPlainRecord(value)
      ? normalizeParticipantList(value.participants ?? value.currentParticipants ?? value.currentParticipant)
      : normalizeParticipantList(value)
    if (participants.length > 0) {
      acc[dateKey] = participants
    }
    return acc
  }, {})
}

function buildRotationConfigWithOverride(rotationConfig: Record<string, unknown> | undefined, dateKey: string, participants: string[]) {
  const nextConfig = { ...(rotationConfig ?? {}) }
  const overrides = { ...readRotationOverrides(rotationConfig) }
  if (participants.length > 0) {
    overrides[dateKey] = participants
  } else {
    delete overrides[dateKey]
  }
  nextConfig.overrides = overrides
  return nextConfig
}

function readPositiveNumber(value: unknown, fallback: number) {
  const numeric = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(numeric) && numeric > 0 ? numeric : fallback
}

function parseDayjs(value: unknown) {
  if (typeof value !== 'string' && typeof value !== 'number' && !(value instanceof Date)) return null
  const parsed = dayjs(value)
  return parsed.isValid() ? parsed : null
}

function rotationShiftMinutes(rotation: OnCallRotation) {
  const config = rotation.rotationConfig ?? {}
  const rotationMinutes = readPositiveNumber(config.rotationMinutes, 0)
  if (rotationMinutes > 0) return rotationMinutes
  return readPositiveNumber(config.shiftHours, 24) * 60
}

function rotationStartAt(rotation: OnCallRotation, date: Dayjs) {
  return parseDayjs(rotation.rotationConfig?.startAt) ?? parseDayjs(rotation.createdAt) ?? date.startOf('day')
}

function baseParticipantsForDate(rotation: OnCallRotation, date: Dayjs) {
  const participants = normalizeParticipantList(rotation.participants)
  if (participants.length === 0) return []

  const shiftMinutes = rotationShiftMinutes(rotation)
  const startAt = rotationStartAt(rotation, date)
  const dayStart = date.startOf('day')
  const dayEnd = dayStart.add(1, 'day')
  const elapsedAtDayStart = dayStart.diff(startAt, 'minute')
  let slot = elapsedAtDayStart < 0 ? 0 : Math.floor(elapsedAtDayStart / shiftMinutes)
  let slotStart = startAt.add(slot * shiftMinutes, 'minute')

  let rewindGuard = 0
  while (slotStart.isAfter(dayStart) && slot > 0 && rewindGuard < 10) {
    slot -= 1
    slotStart = startAt.add(slot * shiftMinutes, 'minute')
    rewindGuard += 1
  }

  const result: string[] = []
  const seen = new Set<string>()
  let guard = 0
  while (slotStart.isBefore(dayEnd) && guard < 200) {
    const slotEnd = slotStart.add(shiftMinutes, 'minute')
    if (slotEnd.isAfter(dayStart) || slotEnd.isSame(dayStart)) {
      const participantIndex = slot < 0 ? 0 : slot % participants.length
      const participant = participants[participantIndex]
      if (participant && !seen.has(participant)) {
        seen.add(participant)
        result.push(participant)
      }
    }
    slot += 1
    slotStart = slotStart.add(shiftMinutes, 'minute')
    guard += 1
  }

  return result.length > 0 ? result : [participants[0]]
}

type OnCallBoardView = 'calendar' | 'timeline' | 'list'
type OnCallRotationMode = 'daily' | 'weekly' | 'custom'

interface OnCallRotationFormValues {
  name: string
  scheduleId: string
  participants: string[]
  rotationMode: OnCallRotationMode
  shiftHours?: number
  startAt?: Dayjs | null
  enabled: boolean
}

interface OnCallEscalationStepFormValues {
  scheduleId?: string
  delayMinutes?: number
  role?: string
  description?: string
}

interface OnCallEscalationPolicyFormValues {
  name: string
  steps: OnCallEscalationStepFormValues[]
  enabled: boolean
}

interface OnCallUser {
  id: string
  username: string
  displayName?: string
  email?: string
  status?: string
}

function onCallUserOptions(users: OnCallUser[]) {
  return users.map((user) => {
    const username = user.username || user.email || user.id
    const display = user.displayName || username
    const label = display && display !== username ? `${display} (${username})` : username
    return { value: username, label }
  })
}

const ONCALL_INTEGRATION_TYPE_OPTIONS = [
  { value: 'prometheus', label: 'Prometheus' },
  { value: 'grafana_alerting', label: 'Grafana Alerting' },
  { value: 'alertmanager', label: 'Alertmanager' },
  { value: 'webhook', label: 'Webhook' },
  { value: 'logs', label: 'Logs' },
  { value: 'traces', label: 'Traces' },
]

const ONCALL_GROUP_BY_OPTIONS = ['alertName', 'clusterId', 'namespace', 'service', 'severity', 'businessLineId', 'integrationId'].map((value) => ({ value, label: value }))

const ONCALL_ROLE_OPTIONS = [
  { value: 'dev', label: '开发 Dev' },
  { value: 'qa', label: '测试 QA' },
  { value: 'ops', label: '运维 Ops' },
  { value: 'sre', label: 'SRE' },
  { value: 'security', label: '安全 Security' },
  { value: 'owner', label: '业务负责人 Owner' },
]

const ONCALL_SEVERITY_OPTIONS = [
  { value: 'critical', label: 'Critical' },
  { value: 'warning', label: 'Warning' },
  { value: 'info', label: 'Info' },
]

function participantAvatarText(value: string) {
  const parts = value.split(/[\s._-]+/).map((item) => item.trim()).filter(Boolean)
  if (parts.length >= 2) return `${parts[0][0] || ''}${parts[1][0] || ''}`.toUpperCase()
  return value.slice(0, 2).toUpperCase()
}

function formatParticipantSummary(participants: string[]) {
  return participants.length > 0 ? participants.join('、') : '未排班'
}

function rotationConfigShiftHours(rotationConfig?: Record<string, unknown>) {
  const rotationMinutes = readPositiveNumber(rotationConfig?.rotationMinutes, 0)
  if (rotationMinutes > 0) return Number((rotationMinutes / 60).toFixed(2))
  return readPositiveNumber(rotationConfig?.shiftHours, 24)
}

function rotationModeFromConfig(rotationConfig?: Record<string, unknown>): OnCallRotationMode {
  const shiftHours = rotationConfigShiftHours(rotationConfig)
  if (Math.abs(shiftHours - 24) < 0.001) return 'daily'
  if (Math.abs(shiftHours - 168) < 0.001) return 'weekly'
  return 'custom'
}

function rotationModeLabel(rotationConfig?: Record<string, unknown>) {
  const mode = rotationModeFromConfig(rotationConfig)
  if (mode === 'daily') return '每日轮换'
  if (mode === 'weekly') return '每周轮换'
  const shiftHours = rotationConfigShiftHours(rotationConfig)
  return `自定义 · ${shiftHours} 小时`
}

function defaultOnCallRotationFormValues(): OnCallRotationFormValues {
  return {
    name: '',
    scheduleId: '',
    participants: [],
    rotationMode: 'daily',
    shiftHours: 24,
    startAt: dayjs(),
    enabled: true,
  }
}

function toOnCallRotationFormValues(record: OnCallRotation | null): OnCallRotationFormValues {
  if (!record) return defaultOnCallRotationFormValues()
  return {
    name: record.name,
    scheduleId: record.scheduleId,
    participants: normalizeParticipantList(record.participants),
    rotationMode: rotationModeFromConfig(record.rotationConfig),
    shiftHours: rotationConfigShiftHours(record.rotationConfig),
    startAt: parseDayjs(record.rotationConfig?.startAt) ?? parseDayjs(record.createdAt),
    enabled: record.enabled,
  }
}

function buildRotationConfigFromForm(values: OnCallRotationFormValues, currentConfig?: Record<string, unknown>) {
  const nextConfig = { ...(currentConfig ?? {}) }
  delete nextConfig.shiftHours
  delete nextConfig.rotationMinutes
  delete nextConfig.startAt

  const shiftHours = values.rotationMode === 'daily'
    ? 24
    : values.rotationMode === 'weekly'
      ? 168
      : readPositiveNumber(values.shiftHours, 24)

  nextConfig.shiftHours = shiftHours
  if (values.rotationMode === 'custom') {
    nextConfig.rotationMinutes = Math.max(1, Math.round(shiftHours * 60))
  }
  if (values.startAt?.isValid()) {
    nextConfig.startAt = values.startAt.toISOString()
  }
  return nextConfig
}

function defaultEscalationStep(): OnCallEscalationStepFormValues {
  return { scheduleId: '', delayMinutes: 0, role: '', description: '' }
}

function toOnCallEscalationStepFormValues(steps?: Array<Record<string, unknown>>) {
  if (!steps?.length) return [defaultEscalationStep()]
  return steps.map((step) => ({
    scheduleId: typeof step.scheduleId === 'string' ? step.scheduleId : '',
    delayMinutes: Number(step.delayMinutes ?? 0),
    role: typeof step.role === 'string' ? step.role : '',
    description: typeof step.description === 'string' ? step.description : '',
  }))
}

function buildEscalationStepsFromForm(values: OnCallEscalationStepFormValues[], currentSteps?: Array<Record<string, unknown>>) {
  return values.map((step, index) => {
    const current = isPlainRecord(currentSteps?.[index]) ? { ...currentSteps?.[index] } : {}
    return {
      ...current,
      scheduleId: step.scheduleId || '',
      delayMinutes: Number(step.delayMinutes ?? 0),
      role: step.role || '',
      description: step.description || '',
    }
  })
}

function serializeRulePayload(values: Record<string, unknown> | Partial<AlertRuleFormValues> | Partial<AlertRule>) {
  return {
    id: values.id,
    name: values.name || '',
    ruleType: values.ruleType || 'metrics',
    datasourceSelector: typeof values.datasourceSelector === 'string' ? safeParseJson(values.datasourceSelector, {}) : values.datasourceSelector ?? {},
    querySpec: typeof values.querySpec === 'string' ? safeParseJson(values.querySpec, {}) : values.querySpec ?? {},
    thresholdSpec: typeof values.thresholdSpec === 'string' ? safeParseJson(values.thresholdSpec, {}) : values.thresholdSpec ?? {},
    forSeconds: Number(values.forSeconds ?? 0),
    groupBy: Array.isArray(values.groupBy)
      ? values.groupBy
      : typeof values.groupBy === 'string'
        ? safeParseStringArray(values.groupBy)
        : [],
    labels: typeof values.labels === 'string' ? safeParseJson(values.labels, {}) : values.labels ?? {},
    annotations: typeof values.annotations === 'string' ? safeParseJson(values.annotations, {}) : values.annotations ?? {},
    notificationPolicyId: values.notificationPolicyId || '',
    healingPolicyIds: values.healingPolicyIds ?? [],
    enabled: Boolean(values.enabled),
  }
}

export function AlertRulesPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManageRules = hasPermission(permissionSnapshotQuery.data?.data, 'observe.alert-rules.manage')
  const [form] = Form.useForm<AlertRuleFormValues>()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<AlertRule | null>(null)
  const [testOpen, setTestOpen] = useState(false)
  const [testResult, setTestResult] = useState<Record<string, unknown> | null>(null)
  const [runsOpen, setRunsOpen] = useState(false)
  const [selectedRuleId, setSelectedRuleId] = useState<string>('')

  const rulesQuery = useQuery({
    queryKey: ['alert-rules'],
    queryFn: () => api.get<ApiResponse<AlertRule[]>>('/alert-rules'),
  })
  const notificationPoliciesQuery = useQuery({
    queryKey: ['notification-policies'],
    queryFn: () => api.get<ApiResponse<NotificationPolicy[]>>('/notification-policies'),
  })
  const healingPoliciesQuery = useQuery({
    queryKey: ['healing-policies'],
    queryFn: () => api.get<ApiResponse<HealingPolicy[]>>('/healing-policies'),
  })
  const ruleRunsQuery = useQuery({
    queryKey: ['alert-rule-runs', selectedRuleId],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; status: string; matched: boolean; summary?: string; durationMs: number; error?: string; createdAt: string }>>>(`/alert-rule-runs?ruleId=${encodeURIComponent(selectedRuleId)}`),
    enabled: runsOpen && selectedRuleId !== '',
  })

  const createMutation = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post('/alert-rules', payload),
    onSuccess: () => {
      void message.success('告警规则已保存')
      void queryClient.invalidateQueries({ queryKey: ['alert-rules'] })
      setOpen(false)
      setEditing(null)
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/alert-rules/${id}`, payload),
    onSuccess: () => {
      void message.success('告警规则已更新')
      void queryClient.invalidateQueries({ queryKey: ['alert-rules'] })
      setOpen(false)
      setEditing(null)
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const testMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.post<ApiResponse<Record<string, unknown>>>(`/alert-rules/${id}/test`, payload),
    onSuccess: (payload: ApiResponse<Record<string, unknown>>) => {
      setTestResult(payload.data as Record<string, unknown>)
      setTestOpen(true)
      void message.success('规则测试已执行')
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const ruleColumns: ColumnsType<AlertRule> = [
    { title: '名称', dataIndex: 'name' },
    { title: '类型', dataIndex: 'ruleType', render: (value: string) => <Tag>{value}</Tag> },
    { title: '数据源', dataIndex: 'datasourceSelector', render: (value: Record<string, unknown>) => <Text code>{prettyJson(value)}</Text> },
    { title: '通知策略', dataIndex: 'notificationPolicyId', render: (value: string) => value || '-' },
    { title: '自愈策略', dataIndex: 'healingPolicyIds', render: (value: string[]) => <Space wrap>{(value ?? []).map((item) => <Tag key={item}>{item}</Tag>)}</Space> },
    { title: '持续(s)', dataIndex: 'forSeconds' },
    {
      ...{ title: '启用', dataIndex: 'enabled' },
      render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" />,
    },
    { title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record: AlertRule) => (
        <Space className="soha-row-action-icons" size={2}>
          <ManagementIconButton
            aria-label="测试告警规则"
            size="small"
            tooltip="测试"
            icon={<PlayCircleOutlined />}
            onClick={() => {
              try {
                testMutation.mutate({ id: record.id, payload: serializeRulePayload(record) })
              } catch (error) {
                message.error(error instanceof Error ? error.message : '规则测试失败')
              }
            }}
          />
          <ManagementIconButton
            aria-label="查看运行记录"
            size="small"
            tooltip="运行记录"
            icon={<ReloadOutlined />}
            onClick={() => { setSelectedRuleId(record.id); setRunsOpen(true) }}
          />
          {canManageRules ? (
            <ManagementIconButton
              aria-label="编辑告警规则"
              size="small"
              tooltip="编辑"
              icon={<EditOutlined />}
              onClick={() => openEditor(record)}
            />
          ) : null}
        </Space>
      ),
    },
  ]

  function openEditor(record: AlertRule | null) {
    setEditing(record)
    setOpen(true)
    const defaults = record ?? {
      name: '',
      ruleType: 'metrics',
      datasourceSelector: {},
      querySpec: { metricKey: 'cpu_usage', windowMinutes: 60, stepSeconds: 60 },
      thresholdSpec: { sampleLimit: 20 },
      forSeconds: 60,
      groupBy: [],
      labels: {},
      annotations: {},
      notificationPolicyId: '',
      healingPolicyIds: [],
      enabled: true,
    }
    form.setFieldsValue({
      name: defaults.name,
      ruleType: defaults.ruleType,
      datasourceSelector: prettyJson(defaults.datasourceSelector),
      querySpec: prettyJson(defaults.querySpec),
      thresholdSpec: prettyJson(defaults.thresholdSpec),
      forSeconds: defaults.forSeconds,
      groupBy: (defaults.groupBy ?? []).join(', '),
      labels: prettyJson(defaults.labels),
      annotations: prettyJson(defaults.annotations),
      notificationPolicyId: defaults.notificationPolicyId,
      healingPolicyIds: defaults.healingPolicyIds ?? [],
      enabled: defaults.enabled,
    })
  }

  function submit(values: AlertRuleFormValues) {
    try {
      const payload = {
        id: values.id,
        name: values.name,
        ruleType: values.ruleType,
        datasourceSelector: safeParseJson(values.datasourceSelector, {}),
        querySpec: safeParseJson(values.querySpec, {}),
        thresholdSpec: safeParseJson(values.thresholdSpec, {}),
        forSeconds: values.forSeconds,
        groupBy: safeParseStringArray(values.groupBy),
        labels: safeParseJson(values.labels, {}),
        annotations: safeParseJson(values.annotations, {}),
        notificationPolicyId: values.notificationPolicyId || '',
        healingPolicyIds: values.healingPolicyIds ?? [],
        enabled: values.enabled,
      }
      if (editing?.id) {
        updateMutation.mutate({ id: editing.id, payload })
      } else {
        createMutation.mutate(payload)
      }
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title="告警规则"
        description="按数据源、查询和阈值创建规则，并绑定通知策略与自愈策略。"
        actions={canManageRules ? <Button icon={<PlusOutlined />} type="primary" onClick={() => openEditor(null)}>新建规则</Button> : null}
      />
      <Card>
        <Paragraph type="secondary" className="mb-0">
          规则支持 `metrics` / `logs` / `traces` / `external_passthrough`。测试会按选择的数据源执行一次预览查询。
        </Paragraph>
      </Card>
      <AdminTable shellClassName="soha-management-table-shell" columns={ruleColumns} dataSource={rulesQuery.data?.data ?? []} rowKey="id" loading={rulesQuery.isLoading} />

      <Modal
        title={editing ? '编辑告警规则' : '新建告警规则'}
        open={open}
        onCancel={() => { setOpen(false); setEditing(null) }}
        footer={null}
        width={920}
        destroyOnHidden
      >
        <Form layout="vertical" form={form} onFinish={submit} initialValues={{ ruleType: 'metrics', forSeconds: 60, groupBy: '', enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入规则名称' }]}><Input /></Form.Item>
          <Form.Item name="ruleType" label="规则类型" rules={[{ required: true }]}>
            <Select options={[
              { value: 'metrics', label: 'Metrics' },
              { value: 'logs', label: 'Logs' },
              { value: 'traces', label: 'Traces' },
              { value: 'external_passthrough', label: 'External passthrough' },
            ]} />
          </Form.Item>
          <Form.Item name="datasourceSelector" label="数据源选择器(JSON)" rules={[{ required: true }]}><Input.TextArea rows={3} /></Form.Item>
          <Form.Item name="querySpec" label="查询定义(JSON)" rules={[{ required: true }]}><Input.TextArea rows={4} /></Form.Item>
          <Form.Item name="thresholdSpec" label="阈值定义(JSON)" rules={[{ required: true }]}><Input.TextArea rows={3} /></Form.Item>
          <Form.Item name="groupBy" label="分组标签(逗号分隔)"><Input /></Form.Item>
          <Form.Item name="labels" label="事件标签(JSON)" rules={[{ required: true }]}><Input.TextArea rows={3} /></Form.Item>
          <Form.Item name="annotations" label="事件注释(JSON)" rules={[{ required: true }]}><Input.TextArea rows={3} /></Form.Item>
          <Space size={16} style={{ width: '100%' }}>
            <Form.Item name="forSeconds" label="持续时间(s)" style={{ flex: 1 }}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
            <Form.Item name="notificationPolicyId" label="通知策略" style={{ flex: 1 }}>
              <Select allowClear options={(notificationPoliciesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} />
            </Form.Item>
          </Space>
          <Form.Item name="healingPolicyIds" label="自愈策略">
            <Select mode="multiple" allowClear options={(healingPoliciesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={createMutation.isPending || updateMutation.isPending}>保存</Button>
            <Button onClick={() => setOpen(false)}>取消</Button>
            {editing?.id ? (
              <Button
                icon={<PlayCircleOutlined />}
                onClick={() => {
                  try {
                    testMutation.mutate({ id: editing.id, payload: serializeRulePayload(form.getFieldsValue() as AlertRuleFormValues) })
                  } catch (error) {
                    message.error(error instanceof Error ? error.message : '规则测试失败')
                  }
                }}
              >
                测试
              </Button>
            ) : null}
          </Space>
        </Form>
      </Modal>
      <Modal title="规则测试结果" open={testOpen} onCancel={() => setTestOpen(false)} footer={null} width={920} destroyOnHidden>
        <Space orientation="vertical" style={{ width: '100%' }} size={16}>
          <Card size="small" title="摘要">
            <Paragraph className="mb-0">{String(testResult?.summary || '-')}</Paragraph>
          </Card>
          <Card size="small" title="命中结果">
            <Paragraph className="mb-0">Matched: {String(testResult?.matched ?? false)}</Paragraph>
            <Paragraph className="mb-0">DataSources: {JSON.stringify(testResult?.dataSources ?? [])}</Paragraph>
          </Card>
          <Card size="small" title="样本">
            <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{JSON.stringify(testResult?.samples ?? [], null, 2)}</pre>
          </Card>
          <Card size="small" title="通知预览">
            <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{JSON.stringify(testResult?.notificationPreview ?? [], null, 2)}</pre>
          </Card>
        </Space>
      </Modal>
      <Modal title="最近运行记录" open={runsOpen} onCancel={() => setRunsOpen(false)} footer={null} width={920} destroyOnHidden>
        <AdminTable
          columns={[
            { title: '运行ID', dataIndex: 'id' },
            { title: '状态', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
            { title: '命中', dataIndex: 'matched', render: (value: boolean) => <BooleanTag value={value} trueLabel="命中" falseLabel="未命中" /> },
            { title: '耗时(ms)', dataIndex: 'durationMs' },
            { title: '摘要', dataIndex: 'summary' },
            { title: '错误', dataIndex: 'error', render: (value: string) => value || '-' },
            { title: '时间', dataIndex: 'createdAt', render: (value: string) => formatDateTime(value) },
          ]}
          dataSource={ruleRunsQuery.data?.data ?? []}
          rowKey="id"
          loading={ruleRunsQuery.isLoading}
          pagination={{ pageSize: 10 }}
        />
      </Modal>
    </div>
  )
}

export function AlertEventDetailPage() {
  const { eventId = '' } = useParams()
  const navigate = useNavigate()
  return <AlertEventDetailPageContent eventId={eventId} onBack={() => navigate('/monitoring-workbench/alerts')} />
}

export function HealingPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManageHealing = hasPermission(permissionSnapshotQuery.data?.data, 'observe.healing.manage')
  const [form] = Form.useForm<HealingPolicyFormValues>()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<HealingPolicy | null>(null)
  const [definition, setDefinition] = useState<ReleaseDagDefinition>(createDefaultReleaseDagDefinition())

  const policiesQuery = useQuery({
    queryKey: ['healing-policies'],
    queryFn: () => api.get<ApiResponse<HealingPolicy[]>>('/healing-policies'),
  })
  const runsQuery = useQuery({
    queryKey: ['healing-runs'],
    queryFn: () => api.get<ApiResponse<HealingRun[]>>('/healing-runs'),
  })

  const createMutation = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post('/healing-policies', payload),
    onSuccess: () => {
      void message.success('自愈策略已保存')
      void queryClient.invalidateQueries({ queryKey: ['healing-policies'] })
      setOpen(false)
      setEditing(null)
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/healing-policies/${id}`, payload),
    onSuccess: () => {
      void message.success('自愈策略已更新')
      void queryClient.invalidateQueries({ queryKey: ['healing-policies'] })
      setOpen(false)
      setEditing(null)
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const approveMutation = useMutation({
    mutationFn: ({ id, comment }: { id: string; comment: string }) => api.post(`/healing-runs/${id}/approve`, { comment }),
    onSuccess: () => {
      void message.success('已审批通过')
      void queryClient.invalidateQueries({ queryKey: ['healing-runs'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const rejectMutation = useMutation({
    mutationFn: ({ id, comment }: { id: string; comment: string }) => api.post(`/healing-runs/${id}/reject`, { comment }),
    onSuccess: () => {
      void message.success('已拒绝')
      void queryClient.invalidateQueries({ queryKey: ['healing-runs'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const retryMutation = useMutation({
    mutationFn: (id: string) => api.post(`/healing-runs/${id}/retry`),
    onSuccess: () => {
      void message.success('已重试')
      void queryClient.invalidateQueries({ queryKey: ['healing-runs'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const runColumns: ColumnsType<HealingRun> = useMemo(() => [
    { title: '运行ID', dataIndex: 'id' },
    { title: '策略', dataIndex: 'policyId' },
    { title: '事件', dataIndex: 'eventId' },
    { title: '状态', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
    { title: '审批', dataIndex: 'approvalStatus', render: (value: string) => value ? <StatusTag value={value} /> : '-' },
    { title: 'Workflow', dataIndex: 'workflowStatus', render: (value: string, record: HealingRun) => value ? <StatusTag value={value} /> : record.workflowRunId || '-' },
    { title: '审批人', dataIndex: 'approvedBy', render: (value: string) => value || '-' },
    { title: '执行摘要', dataIndex: 'workflowSummary', render: (value: string) => value || '-' },
    { title: '创建时间', dataIndex: 'createdAt', render: (value: string) => formatDateTime(value) },
    {
      title: '操作',
      dataIndex: 'id',
      render: (value: string, record: HealingRun) => (
        <Space>
          {canManageHealing ? <Button size="small" icon={<CheckOutlined />} onClick={() => approveMutation.mutate({ id: value, comment: 'approved from console' })} disabled={['completed', 'rejected'].includes(record.status)}>通过</Button> : null}
          {canManageHealing ? <Button size="small" icon={<CloseOutlined />} onClick={() => rejectMutation.mutate({ id: value, comment: 'rejected from console' })} disabled={['completed', 'rejected'].includes(record.status)}>拒绝</Button> : null}
          {canManageHealing ? <Button size="small" icon={<ReloadOutlined />} onClick={() => retryMutation.mutate(value)}>重试</Button> : null}
        </Space>
      ),
    },
  ], [approveMutation, rejectMutation, retryMutation])

  function openEditor(record: HealingPolicy | null) {
    setEditing(record)
    setOpen(true)
    const defaults = record ?? {
      name: '',
      triggerMode: 'approval_then_auto',
      workflowTemplateId: '',
      approvalPolicyRef: '',
      cooldownSeconds: 300,
      concurrencyKey: '',
      safetyWindowSeconds: 600,
      enabled: true,
      definition: createDefaultReleaseDagDefinition(),
    }
    setDefinition(normalizeReleaseDagDefinition(defaults.definition ?? createDefaultReleaseDagDefinition()))
    form.setFieldsValue({
      name: defaults.name,
      triggerMode: defaults.triggerMode,
      workflowTemplateId: defaults.workflowTemplateId,
      approvalPolicyRef: defaults.approvalPolicyRef,
      cooldownSeconds: defaults.cooldownSeconds,
      concurrencyKey: defaults.concurrencyKey,
      safetyWindowSeconds: defaults.safetyWindowSeconds,
      enabled: defaults.enabled,
    })
  }

  function submit(values: HealingPolicyFormValues) {
    const payload = {
      id: values.id,
      name: values.name,
      triggerMode: values.triggerMode,
      workflowTemplateId: values.workflowTemplateId,
      approvalPolicyRef: values.approvalPolicyRef,
      cooldownSeconds: values.cooldownSeconds,
      concurrencyKey: values.concurrencyKey,
      safetyWindowSeconds: values.safetyWindowSeconds,
      definition,
      enabled: values.enabled,
    }
    if (editing?.id) {
      updateMutation.mutate({ id: editing.id, payload })
    } else {
      createMutation.mutate(payload)
    }
  }

  const policyColumns: ColumnsType<HealingPolicy> = [
    { title: '名称', dataIndex: 'name' },
    { title: '触发模式', dataIndex: 'triggerMode', render: (value: string) => <Tag>{value}</Tag> },
    { title: '工作流模板', dataIndex: 'workflowTemplateId' },
    { title: '审批策略', dataIndex: 'approvalPolicyRef', render: (value: string) => value || '-' },
    { title: '冷却(s)', dataIndex: 'cooldownSeconds' },
    { title: '安全窗(s)', dataIndex: 'safetyWindowSeconds' },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" /> },
    {
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record: HealingPolicy) => canManageHealing ? (
        <ManagementIconButton
          aria-label="编辑自愈策略"
          size="small"
          tooltip="编辑"
          icon={<EditOutlined />}
          onClick={() => openEditor(record)}
        />
      ) : null,
    },
  ]

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title="自愈中心"
        description="维护自愈策略和审批运行记录，策略定义复用 DAG 编辑器。"
        actions={canManageHealing ? <Button icon={<PlusOutlined />} type="primary" onClick={() => openEditor(null)}>新建自愈策略</Button> : null}
      />
      <Card>
        <Paragraph type="secondary" className="mb-0">
          自愈策略以 `approval_then_auto` 为默认触发模式，审批通过后由运行记录推进。当前版本先做策略和审批台，执行可在后续接入工作流执行器。
        </Paragraph>
      </Card>
      <AdminTable shellClassName="soha-management-table-shell" columns={policyColumns} dataSource={policiesQuery.data?.data ?? []} rowKey="id" loading={policiesQuery.isLoading} />
      <Card className="soha-overview-panel-card" title="自愈运行">
        <AdminTable shellClassName="soha-management-table-shell" columns={runColumns} dataSource={runsQuery.data?.data ?? []} rowKey="id" loading={runsQuery.isLoading} pagination={{ pageSize: 10 }} />
      </Card>

      <Modal
        title={editing ? '编辑自愈策略' : '新建自愈策略'}
        open={open}
        onCancel={() => { setOpen(false); setEditing(null) }}
        footer={null}
        width={1180}
        destroyOnHidden
      >
        <Form layout="vertical" form={form} onFinish={submit} initialValues={{ triggerMode: 'approval_then_auto', cooldownSeconds: 300, safetyWindowSeconds: 600, enabled: true }}>
          <Space size={16} style={{ width: '100%' }}>
            <Form.Item name="name" label="名称" rules={[{ required: true }]} style={{ flex: 1 }}><Input /></Form.Item>
            <Form.Item name="triggerMode" label="触发模式" style={{ width: 240 }}>
              <Select options={[
                { value: 'approval_then_auto', label: '审批后自动' },
                { value: 'manual', label: '仅手动' },
              ]} />
            </Form.Item>
            <Form.Item name="workflowTemplateId" label="工作流模板 ID" rules={[{ required: true }]} style={{ flex: 1 }}><Input /></Form.Item>
          </Space>
          <Space size={16} style={{ width: '100%' }}>
            <Form.Item name="approvalPolicyRef" label="审批策略引用" style={{ flex: 1 }}><Input /></Form.Item>
            <Form.Item name="concurrencyKey" label="并发键" style={{ flex: 1 }}><Input /></Form.Item>
            <Form.Item name="cooldownSeconds" label="冷却(s)" style={{ width: 180 }}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
            <Form.Item name="safetyWindowSeconds" label="安全窗(s)" style={{ width: 180 }}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          </Space>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Card title="自愈 DAG" size="small">
            <ReleaseFlowDagEditor
              initialDefinition={definition}
              onChange={(next) => setDefinition(next)}
            />
          </Card>
          <Space style={{ marginTop: 16 }}>
            <Button type="primary" htmlType="submit" loading={createMutation.isPending || updateMutation.isPending}>保存</Button>
            <Button onClick={() => setOpen(false)}>取消</Button>
          </Space>
        </Form>
      </Modal>
    </div>
  )
}

export function OnCallSettingsPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManageOnCall = hasPermission(permissionSnapshotQuery.data?.data, 'observe.oncall.manage')
  const [scheduleForm] = Form.useForm<Record<string, unknown>>()
  const [rotationForm] = Form.useForm<OnCallRotationFormValues>()
  const [policyForm] = Form.useForm<OnCallEscalationPolicyFormValues>()
  const [assignmentForm] = Form.useForm<Record<string, unknown>>()
  const [scheduleOpen, setScheduleOpen] = useState(false)
  const [rotationOpen, setRotationOpen] = useState(false)
  const [policyOpen, setPolicyOpen] = useState(false)
  const [assignmentOpen, setAssignmentOpen] = useState(false)
  const [editingSchedule, setEditingSchedule] = useState<OnCallSchedule | null>(null)
  const [editingRotation, setEditingRotation] = useState<OnCallRotation | null>(null)
  const [editingPolicy, setEditingPolicy] = useState<OnCallEscalationPolicy | null>(null)
  const [editingAssignment, setEditingAssignment] = useState<OnCallAssignmentRule | null>(null)

  const usersQuery = useQuery({
    queryKey: ['access-users'],
    queryFn: () => api.get<ApiResponse<OnCallUser[]>>('/access/users'),
  })
  const schedulesQuery = useQuery({
    queryKey: ['oncall-schedules'],
    queryFn: () => api.get<ApiResponse<OnCallSchedule[]>>('/oncall/schedules'),
  })
  const rotationsQuery = useQuery({
    queryKey: ['oncall-rotations'],
    queryFn: () => api.get<ApiResponse<OnCallRotation[]>>('/oncall/rotations'),
  })
  const policiesQuery = useQuery({
    queryKey: ['oncall-escalation-policies'],
    queryFn: () => api.get<ApiResponse<OnCallEscalationPolicy[]>>('/oncall/escalation-policies'),
  })
  const assignmentsQuery = useQuery({
    queryKey: ['oncall-routes'],
    queryFn: () => api.get<ApiResponse<OnCallAssignmentRule[]>>('/oncall/routes'),
  })

  const createSchedule = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post('/oncall/schedules', payload),
    onSuccess: () => { void message.success('排班已保存'); void queryClient.invalidateQueries({ queryKey: ['oncall-schedules'] }); setScheduleOpen(false); setEditingSchedule(null) },
    onError: (err: Error) => void message.error(err.message),
  })
  const updateSchedule = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/oncall/schedules/${id}`, payload),
    onSuccess: () => { void message.success('排班已更新'); void queryClient.invalidateQueries({ queryKey: ['oncall-schedules'] }); setScheduleOpen(false); setEditingSchedule(null) },
    onError: (err: Error) => void message.error(err.message),
  })
  const createRotation = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post('/oncall/rotations', payload),
    onSuccess: () => { void message.success('轮值已保存'); void queryClient.invalidateQueries({ queryKey: ['oncall-rotations'] }); setRotationOpen(false); setEditingRotation(null) },
    onError: (err: Error) => void message.error(err.message),
  })
  const updateRotation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/oncall/rotations/${id}`, payload),
    onSuccess: () => { void message.success('轮值已更新'); void queryClient.invalidateQueries({ queryKey: ['oncall-rotations'] }); setRotationOpen(false); setEditingRotation(null) },
    onError: (err: Error) => void message.error(err.message),
  })
  const createPolicy = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post('/oncall/escalation-policies', payload),
    onSuccess: () => { void message.success('升级链已保存'); void queryClient.invalidateQueries({ queryKey: ['oncall-escalation-policies'] }); setPolicyOpen(false); setEditingPolicy(null) },
    onError: (err: Error) => void message.error(err.message),
  })
  const updatePolicy = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/oncall/escalation-policies/${id}`, payload),
    onSuccess: () => { void message.success('升级链已更新'); void queryClient.invalidateQueries({ queryKey: ['oncall-escalation-policies'] }); setPolicyOpen(false); setEditingPolicy(null) },
    onError: (err: Error) => void message.error(err.message),
  })
  const createAssignment = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post('/oncall/routes', payload),
    onSuccess: () => { void message.success('分派规则已保存'); void queryClient.invalidateQueries({ queryKey: ['oncall-routes'] }); setAssignmentOpen(false); setEditingAssignment(null) },
    onError: (err: Error) => void message.error(err.message),
  })
  const updateAssignment = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/oncall/routes/${id}`, payload),
    onSuccess: () => { void message.success('分派规则已更新'); void queryClient.invalidateQueries({ queryKey: ['oncall-routes'] }); setAssignmentOpen(false); setEditingAssignment(null) },
    onError: (err: Error) => void message.error(err.message),
  })

  const scheduleMap = useMemo(
    () => Object.fromEntries((schedulesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [schedulesQuery.data?.data],
  )
  const escalationMap = useMemo(
    () => Object.fromEntries((policiesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [policiesQuery.data?.data],
  )
  const schedules = schedulesQuery.data?.data ?? []
  const targetOptions = useMemo(() => [
    ...(policiesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: `升级链 · ${item.name}` })),
    ...(schedulesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: `排班 · ${item.name}` })),
  ], [policiesQuery.data?.data, schedulesQuery.data?.data])
  const integrationTypeOptions = ONCALL_INTEGRATION_TYPE_OPTIONS
  const groupByOptions = ONCALL_GROUP_BY_OPTIONS
  const roleOptions = ONCALL_ROLE_OPTIONS
  const severityOptions = ONCALL_SEVERITY_OPTIONS
  const userOptions = useMemo(() => onCallUserOptions(usersQuery.data?.data ?? []), [usersQuery.data?.data])

  function targetLabel(type?: string, ref?: string) {
    if (!ref) return '-'
    if (type === 'escalation') return escalationMap[ref] ? `升级链 · ${escalationMap[ref]}` : ref
    return scheduleMap[ref] ? `排班 · ${scheduleMap[ref]}` : ref
  }

  const scheduleColumns: ColumnsType<OnCallSchedule> = [
    { title: '名称', dataIndex: 'name' },
    { title: '时区', dataIndex: 'timeZone', render: (value: string) => value || '-' },
    { title: '描述', dataIndex: 'description', render: (value: string) => value || '-' },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" /> },
    { title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    { title: '操作', dataIndex: 'id', render: (_: string, record: OnCallSchedule) => canManageOnCall ? <ManagementIconButton aria-label="编辑排班" size="small" tooltip="编辑" icon={<EditOutlined />} onClick={() => { setEditingSchedule(record); scheduleForm.setFieldsValue({ ...record }); setScheduleOpen(true) }} /> : null },
  ]

  const rotationColumns: ColumnsType<OnCallRotation> = [
    { title: '名称', dataIndex: 'name' },
    { title: '排班', dataIndex: 'scheduleId' },
    { title: '参与人', dataIndex: 'participants', render: (value: string[]) => <Space wrap>{(value ?? []).map((item) => <Tag key={item}>{item}</Tag>)}</Space> },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" /> },
    { title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    { title: '操作', dataIndex: 'id', render: (_: string, record: OnCallRotation) => canManageOnCall ? <ManagementIconButton aria-label="编辑轮值" size="small" tooltip="编辑" icon={<EditOutlined />} onClick={() => { setEditingRotation(record); rotationForm.setFieldsValue(toOnCallRotationFormValues(record)); setRotationOpen(true) }} /> : null },
  ]

  const escalationColumns: ColumnsType<OnCallEscalationPolicy> = [
    { title: '名称', dataIndex: 'name' },
    { title: '步骤数', dataIndex: 'steps', render: (value: Array<Record<string, unknown>>) => value?.length ?? 0 },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" /> },
    { title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    { title: '操作', dataIndex: 'id', render: (_: string, record: OnCallEscalationPolicy) => canManageOnCall ? <ManagementIconButton aria-label="编辑升级策略" size="small" tooltip="编辑" icon={<EditOutlined />} onClick={() => { setEditingPolicy(record); policyForm.setFieldsValue({ name: record.name, steps: toOnCallEscalationStepFormValues(record.steps), enabled: record.enabled }); setPolicyOpen(true) }} /> : null },
  ]

  const assignmentColumns: ColumnsType<OnCallAssignmentRule> = [
    { title: '顺序', dataIndex: 'routeOrder', width: 78, render: (value: number, record: OnCallAssignmentRule) => value || record.priority || '-' },
    { title: '规则名称', dataIndex: 'name', width: 220 },
    {
      title: '集成源',
      dataIndex: 'integrationType',
      render: (value: string, record: OnCallAssignmentRule) => (
        <Space wrap>
          {value ? <Tag>{integrationTypeOptions.find((item) => item.value === value)?.label || value}</Tag> : <Tag>全部入口</Tag>}
          {record.integrationId ? <Tag>{record.integrationId}</Tag> : null}
        </Space>
      ),
    },
    {
      title: '匹配器',
      dataIndex: 'matchers',
      render: (_: Record<string, unknown>, record: OnCallAssignmentRule) => (
        <Space wrap>
          {record.businessLineId ? <Tag>范围:{record.businessLineId}</Tag> : null}
          {record.service ? <Tag>服务:{record.service}</Tag> : null}
          {record.severity ? <StatusTag value={record.severity} /> : null}
          {record.role ? <Tag>角色:{roleOptions.find((item) => item.value === record.role)?.label || record.role}</Tag> : null}
          {record.alertCategory ? <Tag>类型:{record.alertCategory}</Tag> : null}
          {record.matchers && Object.keys(record.matchers).length ? <Tag>扩展 {Object.keys(record.matchers).length}</Tag> : null}
          {!record.businessLineId && !record.service && !record.severity && !record.role && !record.alertCategory && (!record.matchers || Object.keys(record.matchers).length === 0) ? '全部告警' : null}
        </Space>
      ),
    },
    { title: '分组', dataIndex: 'groupBy', render: (value: string[]) => value?.length ? <Space wrap>{value.map((item) => <Tag key={item}>{item}</Tag>)}</Space> : '默认分组' },
    { title: '升级目标', dataIndex: 'targetRef', render: (value: string, record: OnCallAssignmentRule) => targetLabel(record.targetType, value) },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" /> },
    { title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record: OnCallAssignmentRule) => canManageOnCall ? (
        <ManagementIconButton
          aria-label="编辑分派规则"
          size="small"
          tooltip="编辑"
          icon={<EditOutlined />}
          onClick={() => openAssignmentEditor(record)}
        />
      ) : null,
    },
  ]

  function submitSchedule(values: Record<string, unknown>) {
    const payload = { ...values, enabled: Boolean(values.enabled) }
    if (editingSchedule?.id) {
      updateSchedule.mutate({ id: editingSchedule.id, payload })
      return
    }
    createSchedule.mutate(payload)
  }

  function submitRotation(values: OnCallRotationFormValues) {
    const payload = {
      name: values.name,
      scheduleId: values.scheduleId,
      participants: values.participants ?? [],
      rotationConfig: buildRotationConfigFromForm(values, editingRotation?.rotationConfig),
      enabled: Boolean(values.enabled),
    }
    if (editingRotation?.id) {
      updateRotation.mutate({ id: editingRotation.id, payload })
      return
    }
    createRotation.mutate(payload)
  }

  function submitPolicy(values: OnCallEscalationPolicyFormValues) {
    const payload = {
      name: values.name,
      steps: buildEscalationStepsFromForm(values.steps ?? [], editingPolicy?.steps),
      enabled: Boolean(values.enabled),
    }
    if (editingPolicy?.id) {
      updatePolicy.mutate({ id: editingPolicy.id, payload })
      return
    }
    createPolicy.mutate(payload)
  }

  function openAssignmentEditor(record: OnCallAssignmentRule | null) {
    setEditingAssignment(record)
    setAssignmentOpen(true)
    assignmentForm.setFieldsValue(record ? {
      ...record,
      matchers: prettyJson(record.matchers ?? {}),
      groupBy: record.groupBy ?? [],
    } : {
      name: '',
      integrationId: '',
      integrationType: 'prometheus',
      businessLineId: '',
      alertCategory: '',
      alertName: '',
      severity: '',
      service: '',
      role: '',
      matchers: '{}',
      targetType: 'escalation',
      targetRef: '',
      routeOrder: 100,
      groupBy: ['alertName', 'clusterId', 'namespace', 'service'],
      priority: 100,
      enabled: true,
    })
  }

  function submitAssignment(values: Record<string, unknown>) {
    try {
      const payload = {
        name: values.name,
        integrationId: values.integrationId || '',
        integrationType: values.integrationType || '',
        businessLineId: values.businessLineId || '',
        alertCategory: values.alertCategory || '',
        alertName: values.alertName || '',
        severity: values.severity || '',
        service: values.service || '',
        role: values.role || '',
        matchers: safeParseJson(String(values.matchers || '{}'), {}),
        targetType: values.targetType || 'escalation',
        targetRef: values.targetRef,
        routeOrder: Number(values.routeOrder ?? 100),
        groupBy: Array.isArray(values.groupBy) ? values.groupBy : [],
        priority: Number(values.priority ?? 100),
        enabled: Boolean(values.enabled),
      }
      if (editingAssignment?.id) {
        updateAssignment.mutate({ id: editingAssignment.id, payload })
        return
      }
      createAssignment.mutate(payload)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title="值班设置"
        description="集中维护值班排班、轮值策略、升级链与告警分派规则。"
        actions={(
          <ManagementTableToolbar>
            {canManageOnCall ? <Button icon={<PlusOutlined />} onClick={() => { setEditingAssignment(null); assignmentForm.resetFields(); setAssignmentOpen(true) }}>新增分派规则</Button> : null}
            {canManageOnCall ? <Button icon={<PlusOutlined />} onClick={() => { setEditingPolicy(null); policyForm.setFieldsValue({ name: '', steps: [defaultEscalationStep()], enabled: true }); setPolicyOpen(true) }}>新增升级链</Button> : null}
            {canManageOnCall ? <Button icon={<PlusOutlined />} onClick={() => { setEditingRotation(null); rotationForm.setFieldsValue(defaultOnCallRotationFormValues()); setRotationOpen(true) }}>新增轮值</Button> : null}
            {canManageOnCall ? <Button icon={<PlusOutlined />} onClick={() => { setEditingSchedule(null); scheduleForm.resetFields(); setScheduleOpen(true) }}>新增排班</Button> : null}
          </ManagementTableToolbar>
        )}
      />
      <Tabs
        items={[
          {
            key: 'assignments',
            label: '告警分派',
            children: <AdminTable shellClassName="soha-management-table-shell" columns={assignmentColumns} dataSource={assignmentsQuery.data?.data ?? []} rowKey="id" loading={assignmentsQuery.isLoading} />,
          },
          {
            key: 'schedules',
            label: '排班',
            children: <AdminTable shellClassName="soha-management-table-shell" columns={scheduleColumns} dataSource={schedulesQuery.data?.data ?? []} rowKey="id" loading={schedulesQuery.isLoading} />,
          },
          {
            key: 'rotations',
            label: '轮值',
            children: <AdminTable shellClassName="soha-management-table-shell" columns={rotationColumns} dataSource={rotationsQuery.data?.data ?? []} rowKey="id" loading={rotationsQuery.isLoading} />,
          },
          {
            key: 'policies',
            label: '升级链',
            children: <AdminTable shellClassName="soha-management-table-shell" columns={escalationColumns} dataSource={policiesQuery.data?.data ?? []} rowKey="id" loading={policiesQuery.isLoading} />,
          },
        ]}
      />

      <Modal title={editingSchedule ? '编辑排班' : '新建排班'} open={scheduleOpen} onCancel={() => setScheduleOpen(false)} footer={null} destroyOnHidden>
        <Form layout="vertical" form={scheduleForm} onFinish={submitSchedule} initialValues={{ enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="timeZone" label="时区"><Input /></Form.Item>
          <Form.Item name="description" label="描述"><Input.TextArea rows={3} /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Space>
            <Button type="primary" htmlType="submit">保存</Button>
            <Button onClick={() => setScheduleOpen(false)}>取消</Button>
          </Space>
        </Form>
      </Modal>

      <Modal title={editingAssignment ? '编辑告警分派规则' : '新建告警分派规则'} open={assignmentOpen} onCancel={() => setAssignmentOpen(false)} footer={null} destroyOnHidden width={960}>
        <Form layout="vertical" form={assignmentForm} onFinish={submitAssignment} initialValues={{ targetType: 'escalation', integrationType: 'prometheus', routeOrder: 100, groupBy: ['alertName', 'clusterId', 'namespace', 'service'], enabled: true }}>
          <Space size={16} style={{ width: '100%' }}>
            <Form.Item name="name" label="规则名称" rules={[{ required: true }]} style={{ flex: 1 }}><Input /></Form.Item>
            <Form.Item name="routeOrder" label="匹配顺序" style={{ width: 160 }}><InputNumber min={1} style={{ width: '100%' }} /></Form.Item>
          </Space>
          <Space size={16} style={{ width: '100%' }}>
            <Form.Item name="integrationType" label="集成类型" style={{ flex: 1 }}><Select allowClear options={integrationTypeOptions} /></Form.Item>
            <Form.Item name="integrationId" label="集成ID" style={{ flex: 1 }}><Input placeholder="grafana-prod / am-main" /></Form.Item>
            <Form.Item name="severity" label="严重度" style={{ flex: 1 }}><Select allowClear options={severityOptions} /></Form.Item>
          </Space>
          <Space size={16} style={{ width: '100%' }}>
            <Form.Item name="service" label="服务/应用" style={{ flex: 1 }}><Input placeholder="checkout / api" /></Form.Item>
            <Form.Item name="alertName" label="告警名称包含" style={{ flex: 1 }}><Input /></Form.Item>
            <Form.Item name="alertCategory" label="告警类型标签" style={{ flex: 1 }}><Input placeholder="business / platform / security" /></Form.Item>
          </Space>
          <Space size={16} style={{ width: '100%' }}>
            <Form.Item name="businessLineId" label="范围标签" style={{ flex: 1 }}>
              <Input allowClear placeholder="businessLineId / app group" />
            </Form.Item>
            <Form.Item name="role" label="响应角色标签" style={{ flex: 1 }}><Select allowClear options={roleOptions} /></Form.Item>
            <Form.Item name="groupBy" label="分组键" style={{ flex: 1 }}>
              <Select mode="tags" options={groupByOptions} placeholder="alertName / clusterId / namespace / service" />
            </Form.Item>
          </Space>
          <Form.Item name="matchers" label="扩展匹配器(JSON)">
            <Input.TextArea rows={4} placeholder='例如 {"clusterId":"prod-a","label:team":"payment"}' />
          </Form.Item>
          <Space size={16} style={{ width: '100%' }}>
            <Form.Item name="targetType" label="目标类型" rules={[{ required: true }]} style={{ width: 180 }}>
              <Select options={[{ value: 'escalation', label: '升级链' }, { value: 'schedule', label: '排班' }]} />
            </Form.Item>
            <Form.Item name="targetRef" label="升级目标" rules={[{ required: true }]} style={{ flex: 1 }}>
              <Select showSearch options={targetOptions} />
            </Form.Item>
            <Form.Item name="priority" label="兼容优先级" style={{ width: 160 }}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          </Space>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={createAssignment.isPending || updateAssignment.isPending}>保存</Button>
            <Button onClick={() => setAssignmentOpen(false)}>取消</Button>
          </Space>
        </Form>
      </Modal>

      <Modal title={editingRotation ? '编辑轮值' : '新建轮值'} open={rotationOpen} onCancel={() => setRotationOpen(false)} footer={null} destroyOnHidden width={720}>
        <Form layout="vertical" form={rotationForm} onFinish={submitRotation} initialValues={defaultOnCallRotationFormValues()}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="scheduleId" label="排班" rules={[{ required: true }]}>
            <Select showSearch options={(schedulesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} />
          </Form.Item>
          <Form.Item name="participants" label="参与人" rules={[{ required: true, message: '至少选择一个参与人' }]}>
            <Select mode="multiple" showSearch optionFilterProp="label" options={userOptions} placeholder="选择参与人" />
          </Form.Item>
          <Form.Item name="rotationMode" label="轮换节奏" rules={[{ required: true }]}>
            <Radio.Group
              options={[
                { value: 'daily', label: '每日轮换' },
                { value: 'weekly', label: '每周轮换' },
                { value: 'custom', label: '自定义' },
              ]}
              optionType="button"
              buttonStyle="solid"
            />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(prev, next) => prev.rotationMode !== next.rotationMode}>
            {({ getFieldValue }) => getFieldValue('rotationMode') === 'custom' ? (
              <Form.Item name="shiftHours" label="单班时长(小时)" rules={[{ required: true, message: '请填写时长' }]}>
                <InputNumber min={1} max={168 * 4} step={1} style={{ width: '100%' }} />
              </Form.Item>
            ) : null}
          </Form.Item>
          <Form.Item name="startAt" label="轮值起始时间">
            <DatePicker showTime style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={createRotation.isPending || updateRotation.isPending}>保存</Button>
            <Button onClick={() => setRotationOpen(false)}>取消</Button>
          </Space>
        </Form>
      </Modal>

      <Modal title={editingPolicy ? '编辑升级链' : '新建升级链'} open={policyOpen} onCancel={() => setPolicyOpen(false)} footer={null} destroyOnHidden width={840}>
        <Form layout="vertical" form={policyForm} onFinish={submitPolicy} initialValues={{ enabled: true, steps: [defaultEscalationStep()] }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.List name="steps">
            {(fields, { add, remove }) => (
              <>
                {fields.map(({ key, name, ...rest }, index) => (
                  <Card key={key} size="small" className="soha-oncall-step-card" title={`步骤 ${index + 1}`} extra={fields.length > 1 ? <Button size="small" danger onClick={() => remove(name)}>删除</Button> : null}>
                    <Row gutter={12}>
                      <Col span={12}>
                        <Form.Item {...rest} name={[name, 'scheduleId']} label="排班对象" rules={[{ required: true, message: '请选择排班' }]}>
                          <Select
                            showSearch
                            allowClear
                            options={schedules.map((item) => ({ value: item.id, label: item.name }))}
                            placeholder="选择排班"
                          />
                        </Form.Item>
                      </Col>
                      <Col span={6}>
                        <Form.Item {...rest} name={[name, 'delayMinutes']} label="延迟(分钟)">
                          <InputNumber min={0} style={{ width: '100%' }} />
                        </Form.Item>
                      </Col>
                      <Col span={6}>
                        <Form.Item {...rest} name={[name, 'role']} label="响应角色">
                          <Select allowClear options={ONCALL_ROLE_OPTIONS} />
                        </Form.Item>
                      </Col>
                      <Col span={24}>
                        <Form.Item {...rest} name={[name, 'description']} label="说明">
                          <Input placeholder="例如：5 分钟内未确认则升级" />
                        </Form.Item>
                      </Col>
                    </Row>
                  </Card>
                ))}
                <Button type="dashed" onClick={() => add(defaultEscalationStep())} icon={<PlusOutlined />} block>
                  新增步骤
                </Button>
              </>
            )}
          </Form.List>
          <Form.Item name="enabled" label="启用" valuePropName="checked" style={{ marginTop: 16 }}><Switch /></Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={createPolicy.isPending || updatePolicy.isPending}>保存</Button>
            <Button onClick={() => setPolicyOpen(false)}>取消</Button>
          </Space>
        </Form>
      </Modal>
    </div>
  )
}

interface OnCallDayAssignment {
  participants: string[]
  override: boolean
}

function assignmentForDate(rotation: OnCallRotation | null, date: Dayjs): OnCallDayAssignment {
  if (!rotation) return { participants: [], override: false }
  const overrides = readRotationOverrides(rotation.rotationConfig)
  const dateKey = date.format(ONCALL_DATE_FORMAT)
  if (overrides[dateKey]) return { participants: overrides[dateKey], override: true }
  return { participants: baseParticipantsForDate(rotation, date), override: false }
}

interface ShiftSlot {
  participant: string
  start: string
  end: string
}

function shiftsForDate(rotation: OnCallRotation | null, date: Dayjs): ShiftSlot[] {
  if (!rotation) return []
  const participants = normalizeParticipantList(rotation.participants)
  if (participants.length === 0) return []
  const shiftMinutes = rotationShiftMinutes(rotation)
  const startAt = rotationStartAt(rotation, date)
  const dayStart = date.startOf('day')
  const dayEnd = dayStart.add(1, 'day')
  const elapsedAtDayStart = dayStart.diff(startAt, 'minute')
  let slot = elapsedAtDayStart < 0 ? 0 : Math.floor(elapsedAtDayStart / shiftMinutes)
  let slotStart = startAt.add(slot * shiftMinutes, 'minute')
  while (slotStart.isAfter(dayStart) && slot > 0) {
    slot -= 1
    slotStart = startAt.add(slot * shiftMinutes, 'minute')
  }
  const result: ShiftSlot[] = []
  let guard = 0
  while (slotStart.isBefore(dayEnd) && guard < 200) {
    const slotEnd = slotStart.add(shiftMinutes, 'minute')
    if (slotEnd.isAfter(dayStart)) {
      const participantIndex = slot < 0 ? 0 : slot % participants.length
      const displayStart = slotStart.isBefore(dayStart) ? dayStart : slotStart
      const displayEnd = slotEnd.isAfter(dayEnd) ? dayEnd : slotEnd
      result.push({
        participant: participants[participantIndex],
        start: displayStart.format('HH:mm'),
        end: displayEnd.format('HH:mm'),
      })
    }
    slot += 1
    slotStart = slotEnd
    guard += 1
  }
  return result
}

export function OnCallBoardPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManageOnCall = hasPermission(permissionSnapshotQuery.data?.data, 'observe.oncall.manage')
  const [overrideForm] = Form.useForm<{ participants: string[] }>()
  const [view, setView] = useState<OnCallBoardView>('calendar')
  const [calendarValue, setCalendarValue] = useState<Dayjs>(dayjs())
  const [selectedScheduleId, setSelectedScheduleId] = useState<string>('')
  const [drawerDate, setDrawerDate] = useState<Dayjs | null>(null)
  const [overrideOpen, setOverrideOpen] = useState(false)
  const [overrideDate, setOverrideDate] = useState<Dayjs | null>(null)

  const schedulesQuery = useQuery({
    queryKey: ['oncall-schedules'],
    queryFn: () => api.get<ApiResponse<OnCallSchedule[]>>('/oncall/schedules'),
  })
  const rotationsQuery = useQuery({
    queryKey: ['oncall-rotations'],
    queryFn: () => api.get<ApiResponse<OnCallRotation[]>>('/oncall/rotations'),
  })
  const tasksQuery = useQuery({
    queryKey: ['oncall-tasks'],
    queryFn: () => api.get<ApiResponse<OnCallTask[]>>('/oncall/tasks?status=pending&status=acknowledged'),
    refetchInterval: 30_000,
  })
  const usersQuery = useQuery({
    queryKey: ['access-users'],
    queryFn: () => api.get<ApiResponse<OnCallUser[]>>('/access/users'),
    enabled: canManageOnCall,
  })

  const updateRotationOverride = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/oncall/rotations/${id}`, payload),
    onSuccess: () => {
      void message.success('值班覆盖已保存')
      void queryClient.invalidateQueries({ queryKey: ['oncall-rotations'] })
      setOverrideOpen(false)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const schedules = schedulesQuery.data?.data ?? []
  const rotations = rotationsQuery.data?.data ?? []
  const tasks = tasksQuery.data?.data ?? []

  const scheduleOptions = useMemo(() => schedules.map((item) => ({ value: item.id, label: item.name })), [schedules])

  const effectiveScheduleId = selectedScheduleId || schedules[0]?.id || ''
  const selectedSchedule = useMemo(
    () => schedules.find((item) => item.id === effectiveScheduleId) ?? null,
    [schedules, effectiveScheduleId],
  )
  const selectedRotation = useMemo(
    () => rotations.find((item) => item.scheduleId === effectiveScheduleId && item.enabled) ?? rotations.find((item) => item.scheduleId === effectiveScheduleId) ?? null,
    [rotations, effectiveScheduleId],
  )
  const overrides = useMemo(() => readRotationOverrides(selectedRotation?.rotationConfig), [selectedRotation])
  const overrideCount = Object.keys(overrides).length

  const today = dayjs()
  const todayAssignment = assignmentForDate(selectedRotation, today)
  const tomorrowAssignment = assignmentForDate(selectedRotation, today.add(1, 'day'))

  const overrideEntries = useMemo(
    () => Object.entries(overrides).sort(([a], [b]) => (a < b ? 1 : a > b ? -1 : 0)),
    [overrides],
  )

  function openOverride(date: Dayjs) {
    if (!canManageOnCall) return
    if (!selectedRotation) {
      void message.warning('当前排班尚未配置轮值，请先到值班设置中创建')
      return
    }
    setOverrideDate(date)
    overrideForm.setFieldsValue({ participants: assignmentForDate(selectedRotation, date).participants })
    setOverrideOpen(true)
  }

  function submitOverride(values: { participants: string[] }) {
    if (!selectedRotation || !overrideDate) return
    const next = normalizeParticipantList(values.participants)
    const dateKey = overrideDate.format(ONCALL_DATE_FORMAT)
    updateRotationOverride.mutate({
      id: selectedRotation.id,
      payload: {
        scheduleId: selectedRotation.scheduleId,
        name: selectedRotation.name,
        participants: normalizeParticipantList(selectedRotation.participants),
        rotationConfig: buildRotationConfigWithOverride(selectedRotation.rotationConfig, dateKey, next),
        enabled: selectedRotation.enabled,
      },
    })
  }

  function clearOverride() {
    if (!selectedRotation || !overrideDate) return
    const dateKey = overrideDate.format(ONCALL_DATE_FORMAT)
    updateRotationOverride.mutate({
      id: selectedRotation.id,
      payload: {
        scheduleId: selectedRotation.scheduleId,
        name: selectedRotation.name,
        participants: normalizeParticipantList(selectedRotation.participants),
        rotationConfig: buildRotationConfigWithOverride(selectedRotation.rotationConfig, dateKey, []),
        enabled: selectedRotation.enabled,
      },
    })
  }

  const cellRender: CalendarProps<Dayjs>['cellRender'] = (current, info) => {
    if (info.type !== 'date') return info.originNode
    const assignment = assignmentForDate(selectedRotation, current)
    const list = assignment.participants
    if (list.length === 0) {
      return <div className="soha-oncall-cell soha-oncall-cell-empty"><Text type="secondary">未排班</Text></div>
    }
    const isToday = current.isSame(today, 'day')
    return (
      <Tooltip title={list.join('、')}>
        <div className={`soha-oncall-cell${assignment.override ? ' soha-oncall-cell-override' : ''}${isToday ? ' soha-oncall-cell-today' : ''}`}>
          <Avatar.Group size="small" max={{ count: 3 }}>
            {list.map((name) => (
              <Avatar key={name} style={{ backgroundColor: assignment.override ? '#fa8c16' : '#1677ff' }}>{participantAvatarText(name)}</Avatar>
            ))}
          </Avatar.Group>
          {assignment.override ? <Badge status="warning" text="覆盖" /> : null}
        </div>
      </Tooltip>
    )
  }

  const taskColumns: ColumnsType<OnCallTask> = [
    { title: '标题', dataIndex: 'title' },
    { title: '严重度', dataIndex: 'severity', render: (value: string) => <StatusTag value={value} /> },
    { title: '状态', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
    { title: '当前响应人', dataIndex: 'currentParticipant', render: (value: string) => value || '-' },
    { title: '分派规则', dataIndex: 'routeName', render: (value: string) => value || '-' },
    { title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
  ]

  const drawerAssignment = drawerDate ? assignmentForDate(selectedRotation, drawerDate) : null

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title="值班协同"
        description="跟踪当前排班、轮值与待响应任务，必要时可临时覆盖某天的值班人。"
        actions={(
          <ManagementTableToolbar>
            <Select
              style={{ minWidth: 220 }}
              placeholder="选择排班"
              value={effectiveScheduleId || undefined}
              options={scheduleOptions}
              onChange={(value) => setSelectedScheduleId(value)}
            />
            <Button onClick={() => navigate('/monitoring-workbench/oncall/settings')}>值班设置</Button>
          </ManagementTableToolbar>
        )}
      />
      {schedules.length === 0 ? (
        <Card>
          <ManagementState
            bordered={false}
            compact
            description="尚未创建排班，请前往值班设置新增。"
            kind="not-configured"
            actions={canManageOnCall ? <Button type="primary" onClick={() => navigate('/monitoring-workbench/oncall/settings')}>前往设置</Button> : null}
          />
        </Card>
      ) : (
        <>
          <Row gutter={16} className="soha-oncall-stats-row">
            <Col xs={24} sm={12} md={6}>
              <Card><Statistic title="今日值班" value={formatParticipantSummary(todayAssignment.participants)} valueStyle={{ fontSize: 18 }} /></Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card><Statistic title="明日值班" value={formatParticipantSummary(tomorrowAssignment.participants)} valueStyle={{ fontSize: 18 }} /></Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card><Statistic title="轮换节奏" value={selectedRotation ? rotationModeLabel(selectedRotation.rotationConfig) : '未配置'} valueStyle={{ fontSize: 18 }} /></Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Statistic title="待响应任务" value={tasks.length} suffix={overrideCount > 0 ? <Tag color="orange">覆盖 {overrideCount}</Tag> : null} />
              </Card>
            </Col>
          </Row>
          <Card
            className="soha-oncall-board-card"
            title={(
              <Space>
                <Text strong>{selectedSchedule?.name || '未选择排班'}</Text>
                {selectedRotation ? <Tag color="blue">{selectedRotation.name}</Tag> : <Tag>暂无轮值</Tag>}
                {todayAssignment.override ? <Tag color="orange">今日已覆盖</Tag> : null}
              </Space>
            )}
            extra={(
              <Segmented
                value={view}
                onChange={(value) => setView(value as OnCallBoardView)}
                options={[
                  { value: 'calendar', label: '月历' },
                  { value: 'timeline', label: '时间轴' },
                  { value: 'list', label: '覆盖列表' },
                ]}
              />
            )}
          >
            {view === 'calendar' ? (
              <Calendar
                value={calendarValue}
                onPanelChange={(value) => setCalendarValue(value)}
                onSelect={(value, selectInfo) => {
                  setCalendarValue(value)
                  if (selectInfo?.source === 'date') setDrawerDate(value)
                }}
                cellRender={cellRender}
              />
            ) : null}
            {view === 'timeline' ? (
              <Timeline
                className="soha-oncall-timeline"
                items={Array.from({ length: 14 }).map((_, idx) => {
                  const date = today.add(idx, 'day')
                  const a = assignmentForDate(selectedRotation, date)
                  return {
                    color: a.override ? 'orange' : idx === 0 ? 'blue' : 'gray',
                    children: (
                      <Space orientation="vertical" size={2}>
                        <Text strong>{date.format('MM-DD ddd')}{idx === 0 ? ' · 今日' : ''}</Text>
                        <Text>{formatParticipantSummary(a.participants)}</Text>
                        {a.override ? <Tag color="orange">手动覆盖</Tag> : null}
                      </Space>
                    ),
                  }
                })}
              />
            ) : null}
            {view === 'list' ? (
              overrideEntries.length === 0 ? (
                <ManagementState bordered={false} compact description="暂无覆盖记录" />
              ) : (
                <AdminTable
                  shellClassName="soha-management-table-shell"
                  columns={[
                    { title: '日期', dataIndex: 'date', render: (value: string) => value },
                    { title: '值班人', dataIndex: 'participants', render: (value: string[]) => formatParticipantSummary(value) },
                    {
                      title: '操作',
                      dataIndex: 'date',
                      render: (value: string) => canManageOnCall ? (
                        <ManagementIconButton
                          aria-label="编辑覆盖记录"
                          size="small"
                          tooltip="编辑"
                          icon={<EditOutlined />}
                          onClick={() => openOverride(dayjs(value))}
                        />
                      ) : null,
                    },
                  ]}
                  dataSource={overrideEntries.map(([date, list]) => ({ date, participants: list }))}
                  rowKey="date"
                  pagination={false}
                />
              )
            ) : null}
          </Card>
          <Card title="待响应任务" className="soha-oncall-tasks-card">
            <AdminTable shellClassName="soha-management-table-shell" columns={taskColumns} dataSource={tasks} rowKey="id" loading={tasksQuery.isLoading} pagination={{ pageSize: 10 }} />
          </Card>
        </>
      )}
      <Drawer
        title={drawerDate ? `${drawerDate.format('YYYY-MM-DD ddd')} 值班详情` : '值班详情'}
        open={Boolean(drawerDate)}
        onClose={() => setDrawerDate(null)}
        width={420}
      >
        {drawerDate && drawerAssignment ? (
          <Space orientation="vertical" size={16} style={{ width: '100%' }}>
            <Descriptions column={1} size="small" bordered>
              <Descriptions.Item label="排班">{selectedSchedule?.name || '-'}</Descriptions.Item>
              <Descriptions.Item label="轮值">{selectedRotation?.name || '未配置'}</Descriptions.Item>
              <Descriptions.Item label="值班人">{formatParticipantSummary(drawerAssignment.participants)}</Descriptions.Item>
              <Descriptions.Item label="是否覆盖">{drawerAssignment.override ? <Tag color="orange">手动覆盖</Tag> : <Tag>遵循轮换</Tag>}</Descriptions.Item>
            </Descriptions>
            {!drawerAssignment.override && selectedRotation ? (
              <Card size="small" title="当日班次">
                <Timeline
                  items={shiftsForDate(selectedRotation, drawerDate).map((slot) => ({
                    children: <Text>{slot.start} – {slot.end}　{slot.participant}</Text>,
                  }))}
                />
              </Card>
            ) : null}
            {canManageOnCall ? (
              <Space>
                <Button type="primary" icon={<EditOutlined />} onClick={() => openOverride(drawerDate)}>覆盖当日</Button>
                <Button onClick={() => navigate('/monitoring-workbench/oncall/settings')}>调整轮值</Button>
              </Space>
            ) : null}
          </Space>
        ) : null}
      </Drawer>
      <Modal
        title={overrideDate ? `${overrideDate.format(ONCALL_DATE_FORMAT)} 值班覆盖` : '值班覆盖'}
        open={overrideOpen}
        onCancel={() => setOverrideOpen(false)}
        footer={null}
        destroyOnHidden
      >
        <Form layout="vertical" form={overrideForm} onFinish={submitOverride}>
          <Form.Item name="participants" label="当日值班人员">
            <Select
              mode="multiple"
              allowClear
              showSearch
              optionFilterProp="label"
              placeholder="选择当日值班人员"
              options={onCallUserOptions(usersQuery.data?.data ?? [])}
            />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={updateRotationOverride.isPending}>保存覆盖</Button>
            <Button onClick={() => setOverrideOpen(false)}>取消</Button>
            <Button
              danger
              disabled={!overrideDate || !overrides[overrideDate.format(ONCALL_DATE_FORMAT)]}
              loading={updateRotationOverride.isPending}
              onClick={clearOverride}
            >
              清除覆盖
            </Button>
          </Space>
        </Form>
      </Modal>
    </div>
  )
}
