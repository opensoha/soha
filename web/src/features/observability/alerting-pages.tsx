import { useMemo, useState } from 'react'
import { App, Button, Calendar, Card, Empty, Form, Input, InputNumber, Modal, Select, Space, Switch, Tag, Tabs, Typography } from 'antd'
import type { CalendarProps } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlusOutlined, PlayCircleOutlined, EditOutlined, CheckOutlined, CloseOutlined, ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import dayjs from 'dayjs'
import type { Dayjs } from 'dayjs'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { StatusTag, BooleanTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import type { ApiResponse, BusinessLine } from '@/types'
import { ReleaseFlowDagEditor } from '@/components/release-flow-dag-editor'
import { createDefaultReleaseDagDefinition, normalizeReleaseDagDefinition } from '@/components/release-flow-dag-definition'
import type { ReleaseDagDefinition } from '@/components/release-flow-dag-definition'
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
        <Space>
          <Button
            size="small"
            icon={<PlayCircleOutlined />}
            onClick={() => {
              try {
                testMutation.mutate({ id: record.id, payload: serializeRulePayload(record) })
              } catch (error) {
                message.error(error instanceof Error ? error.message : '规则测试失败')
              }
            }}
          >
            测试
          </Button>
          <Button size="small" onClick={() => { setSelectedRuleId(record.id); setRunsOpen(true) }}>运行记录</Button>
          {canManageRules ? <Button size="small" icon={<EditOutlined />} onClick={() => openEditor(record)}>编辑</Button> : null}
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
    <div className="kc-page">
      <PageHeader
        title="告警规则"
        description="按数据源、查询和阈值创建规则，并绑定通知策略与自愈策略。"
        actions={canManageRules ? <Button icon={<PlusOutlined />} type="primary" onClick={() => openEditor(null)}>新建规则</Button> : null}
      />
      <Card>
        <Paragraph type="secondary" className="mb-0">
          规则支持 `metrics` / `logs` / `traces` / `external_passthrough`。测试会按选择的数据源执行一次预览查询。
        </Paragraph>
      </Card>
      <AdminTable shellClassName="is-panel" columns={ruleColumns} dataSource={rulesQuery.data?.data ?? []} rowKey="id" loading={rulesQuery.isLoading} />

      <Modal
        title={editing ? '编辑告警规则' : '新建告警规则'}
        open={open}
        onCancel={() => { setOpen(false); setEditing(null) }}
        footer={null}
        width={920}
        destroyOnClose
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
      <Modal title="规则测试结果" open={testOpen} onCancel={() => setTestOpen(false)} footer={null} width={920} destroyOnClose>
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
      <Modal title="最近运行记录" open={runsOpen} onCancel={() => setRunsOpen(false)} footer={null} width={920} destroyOnClose>
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
      render: (_: string, record: HealingPolicy) => canManageHealing ? <Button size="small" icon={<EditOutlined />} onClick={() => openEditor(record)}>编辑</Button> : null,
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title="自愈中心"
        description="维护自愈策略和审批运行记录，策略定义复用 DAG 编辑器。"
        actions={canManageHealing ? <Button icon={<PlusOutlined />} type="primary" onClick={() => openEditor(null)}>新建自愈策略</Button> : null}
      />
      <Card>
        <Paragraph type="secondary" className="mb-0">
          自愈策略以 `approval_then_auto` 为默认触发模式，审批通过后由运行记录推进。当前版本先做策略和审批台，执行可在后续接入工作流执行器。
        </Paragraph>
      </Card>
      <AdminTable shellClassName="is-panel" columns={policyColumns} dataSource={policiesQuery.data?.data ?? []} rowKey="id" loading={policiesQuery.isLoading} />
      <Card className="kc-overview-panel-card" title="自愈运行">
        <AdminTable columns={runColumns} dataSource={runsQuery.data?.data ?? []} rowKey="id" loading={runsQuery.isLoading} pagination={{ pageSize: 10 }} />
      </Card>

      <Modal
        title={editing ? '编辑自愈策略' : '新建自愈策略'}
        open={open}
        onCancel={() => { setOpen(false); setEditing(null) }}
        footer={null}
        width={1180}
        destroyOnClose
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

export function OnCallPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManageOnCall = hasPermission(permissionSnapshotQuery.data?.data, 'observe.oncall.manage')
  const [scheduleForm] = Form.useForm<Record<string, unknown>>()
  const [rotationForm] = Form.useForm<Record<string, unknown>>()
  const [policyForm] = Form.useForm<Record<string, unknown>>()
  const [assignmentForm] = Form.useForm<Record<string, unknown>>()
  const [overrideForm] = Form.useForm<Record<string, unknown>>()
  const [scheduleOpen, setScheduleOpen] = useState(false)
  const [rotationOpen, setRotationOpen] = useState(false)
  const [policyOpen, setPolicyOpen] = useState(false)
  const [assignmentOpen, setAssignmentOpen] = useState(false)
  const [overrideOpen, setOverrideOpen] = useState(false)
  const [editingSchedule, setEditingSchedule] = useState<OnCallSchedule | null>(null)
  const [editingRotation, setEditingRotation] = useState<OnCallRotation | null>(null)
  const [editingPolicy, setEditingPolicy] = useState<OnCallEscalationPolicy | null>(null)
  const [editingAssignment, setEditingAssignment] = useState<OnCallAssignmentRule | null>(null)
  const [selectedScheduleId, setSelectedScheduleId] = useState('')
  const [calendarValue, setCalendarValue] = useState<Dayjs>(() => dayjs())
  const [overrideDate, setOverrideDate] = useState<Dayjs | null>(null)

  const businessLinesQuery = useQuery({
    queryKey: ['business-lines'],
    queryFn: () => api.get<ApiResponse<BusinessLine[]>>('/business-lines'),
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
  const tasksQuery = useQuery({
    queryKey: ['oncall-tasks'],
    queryFn: () => api.get<ApiResponse<OnCallTask[]>>('/oncall/tasks?limit=50'),
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
  const updateRotationOverride = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/oncall/rotations/${id}`, payload),
    onSuccess: () => {
      void message.success('值班覆盖已保存')
      void queryClient.invalidateQueries({ queryKey: ['oncall-rotations'] })
      void queryClient.invalidateQueries({ queryKey: ['oncall-tasks'] })
      setOverrideOpen(false)
      setOverrideDate(null)
    },
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
    onSuccess: () => { void message.success('路由规则已保存'); void queryClient.invalidateQueries({ queryKey: ['oncall-routes'] }); setAssignmentOpen(false); setEditingAssignment(null) },
    onError: (err: Error) => void message.error(err.message),
  })
  const updateAssignment = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/oncall/routes/${id}`, payload),
    onSuccess: () => { void message.success('路由规则已更新'); void queryClient.invalidateQueries({ queryKey: ['oncall-routes'] }); setAssignmentOpen(false); setEditingAssignment(null) },
    onError: (err: Error) => void message.error(err.message),
  })

  const businessLineMap = useMemo(
    () => Object.fromEntries((businessLinesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [businessLinesQuery.data?.data],
  )
  const scheduleMap = useMemo(
    () => Object.fromEntries((schedulesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [schedulesQuery.data?.data],
  )
  const escalationMap = useMemo(
    () => Object.fromEntries((policiesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [policiesQuery.data?.data],
  )
  const schedules = schedulesQuery.data?.data ?? []
  const rotations = rotationsQuery.data?.data ?? []
  const selectedSchedule = useMemo(() => {
    const matchedSchedule = schedules.find((item) => item.id === selectedScheduleId)
    if (matchedSchedule) return matchedSchedule
    return schedules.find((item) => item.enabled) ?? schedules[0] ?? null
  }, [schedules, selectedScheduleId])
  const selectedRotation = useMemo(() => {
    if (!selectedSchedule) return null
    const scheduleRotations = rotations.filter((item) => item.scheduleId === selectedSchedule.id)
    return scheduleRotations.find((item) => item.enabled) ?? scheduleRotations[0] ?? null
  }, [rotations, selectedSchedule])
  const selectedOverrides = useMemo(() => readRotationOverrides(selectedRotation?.rotationConfig), [selectedRotation?.rotationConfig])
  const scheduleOptions = useMemo(
    () => schedules.map((item) => ({ value: item.id, label: item.enabled ? item.name : `${item.name} (禁用)` })),
    [schedules],
  )
  const participantOptions = useMemo(() => {
    const names = new Set<string>()
    normalizeParticipantList(selectedRotation?.participants).forEach((item) => names.add(item))
    Object.values(selectedOverrides).flat().forEach((item) => names.add(item))
    return Array.from(names).map((item) => ({ value: item, label: item }))
  }, [selectedOverrides, selectedRotation?.participants])
  const targetOptions = useMemo(() => [
    ...(policiesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: `升级链 · ${item.name}` })),
    ...(schedulesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: `排班 · ${item.name}` })),
  ], [policiesQuery.data?.data, schedulesQuery.data?.data])
  const integrationTypeOptions = [
    { value: 'prometheus', label: 'Prometheus' },
    { value: 'grafana_alerting', label: 'Grafana Alerting' },
    { value: 'alertmanager', label: 'Alertmanager' },
    { value: 'webhook', label: 'Webhook' },
    { value: 'logs', label: 'Logs' },
    { value: 'traces', label: 'Traces' },
  ]
  const groupByOptions = ['alertName', 'clusterId', 'namespace', 'service', 'severity', 'businessLineId', 'integrationId'].map((value) => ({ value, label: value }))
  const roleOptions = [
    { value: 'dev', label: '开发 Dev' },
    { value: 'qa', label: '测试 QA' },
    { value: 'ops', label: '运维 Ops' },
    { value: 'sre', label: 'SRE' },
    { value: 'security', label: '安全 Security' },
    { value: 'owner', label: '业务负责人 Owner' },
  ]
  const severityOptions = [
    { value: 'critical', label: 'Critical' },
    { value: 'warning', label: 'Warning' },
    { value: 'info', label: 'Info' },
  ]

  function targetLabel(type?: string, ref?: string) {
    if (!ref) return '-'
    if (type === 'escalation') return escalationMap[ref] ? `升级链 · ${escalationMap[ref]}` : ref
    return scheduleMap[ref] ? `排班 · ${scheduleMap[ref]}` : ref
  }

  function assignmentForDate(date: Dayjs) {
    if (!selectedRotation) return { participants: [] as string[], override: false }
    const dateKey = date.format(ONCALL_DATE_FORMAT)
    const overrideParticipants = selectedOverrides[dateKey]
    if (overrideParticipants?.length) {
      return { participants: overrideParticipants, override: true }
    }
    return { participants: baseParticipantsForDate(selectedRotation, date), override: false }
  }

  const selectedDateAssignment = assignmentForDate(calendarValue)

  const calendarCellRender: CalendarProps<Dayjs>['cellRender'] = (current, info) => {
    if (info.type !== 'date') return info.originNode
    if (!current.isSame(calendarValue, 'month')) return null
    const assignment = assignmentForDate(current)
    const visibleParticipants = assignment.participants.slice(0, 3)
    return (
      <div className="kc-oncall-calendar-cell">
        {visibleParticipants.length > 0 ? (
          <Space size={[4, 4]} wrap className="kc-oncall-calendar-duty-list">
            {visibleParticipants.map((participant) => (
              <Tag key={participant} color={assignment.override ? 'gold' : undefined} className="kc-oncall-duty-tag">
                {participant}
              </Tag>
            ))}
            {assignment.participants.length > visibleParticipants.length ? (
              <Tag className="kc-oncall-duty-tag">+{assignment.participants.length - visibleParticipants.length}</Tag>
            ) : null}
          </Space>
        ) : (
          <Text type="secondary" className="kc-oncall-calendar-empty">未排班</Text>
        )}
        {assignment.override ? <Tag color="processing" className="kc-oncall-override-tag">覆盖</Tag> : null}
      </div>
    )
  }

  const taskColumns: ColumnsType<OnCallTask> = [
    {
      title: '告警任务',
      dataIndex: 'title',
      render: (value: string, record: OnCallTask) => (
        <Space orientation="vertical" size={2}>
          <Text strong>{value}</Text>
          <Text type="secondary" className="text-xs">{record.eventId}</Text>
        </Space>
      ),
    },
    { title: '严重度', dataIndex: 'severity', width: 100, render: (value: string) => <StatusTag value={value} /> },
    {
      title: '集成源',
      dataIndex: 'integrationType',
      render: (value: string, record: OnCallTask) => (
        <Space wrap>
          {value ? <Tag>{integrationTypeOptions.find((item) => item.value === value)?.label || value}</Tag> : <Tag>unknown</Tag>}
          {record.integrationId ? <Tag>{record.integrationId}</Tag> : null}
        </Space>
      ),
    },
    { title: '路由', dataIndex: 'routeName', render: (value: string, record: OnCallTask) => value || (record.resolutionStatus === 'matched' ? record.routeId : <StatusTag value="no_match" />) },
    { title: '分组键', dataIndex: 'groupKey', render: (value: string) => value || '-' },
    { title: '当前响应人', dataIndex: 'currentParticipant', render: (value: string, record: OnCallTask) => value || (record.participants?.length ? record.participants.join(', ') : '-') },
    { title: '服务', dataIndex: 'service', render: (value: string, record: OnCallTask) => value || record.namespace || '-' },
    { title: '最近出现', dataIndex: 'lastSeenAt', render: (value: string) => formatDateTime(value) },
  ]

  const scheduleColumns: ColumnsType<OnCallSchedule> = [
    { title: '名称', dataIndex: 'name' },
    { title: '时区', dataIndex: 'timeZone', render: (value: string) => value || '-' },
    { title: '描述', dataIndex: 'description', render: (value: string) => value || '-' },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" /> },
    { title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    { title: '操作', dataIndex: 'id', render: (_: string, record: OnCallSchedule) => canManageOnCall ? <Button size="small" icon={<EditOutlined />} onClick={() => { setEditingSchedule(record); scheduleForm.setFieldsValue({ ...record }); setScheduleOpen(true) }}>编辑</Button> : null },
  ]

  const rotationColumns: ColumnsType<OnCallRotation> = [
    { title: '名称', dataIndex: 'name' },
    { title: '排班', dataIndex: 'scheduleId' },
    { title: '参与人', dataIndex: 'participants', render: (value: string[]) => <Space wrap>{(value ?? []).map((item) => <Tag key={item}>{item}</Tag>)}</Space> },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" /> },
    { title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    { title: '操作', dataIndex: 'id', render: (_: string, record: OnCallRotation) => canManageOnCall ? <Button size="small" icon={<EditOutlined />} onClick={() => { setEditingRotation(record); rotationForm.setFieldsValue({ ...record, participants: (record.participants ?? []).join(', '), rotationConfig: prettyJson(record.rotationConfig) }); setRotationOpen(true) }}>编辑</Button> : null },
  ]

  const escalationColumns: ColumnsType<OnCallEscalationPolicy> = [
    { title: '名称', dataIndex: 'name' },
    { title: '步骤数', dataIndex: 'steps', render: (value: Array<Record<string, unknown>>) => value?.length ?? 0 },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" /> },
    { title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    { title: '操作', dataIndex: 'id', render: (_: string, record: OnCallEscalationPolicy) => canManageOnCall ? <Button size="small" icon={<EditOutlined />} onClick={() => { setEditingPolicy(record); policyForm.setFieldsValue({ ...record, steps: prettyJson(record.steps) }); setPolicyOpen(true) }}>编辑</Button> : null },
  ]

  const assignmentColumns: ColumnsType<OnCallAssignmentRule> = [
    { title: '顺序', dataIndex: 'routeOrder', width: 78, render: (value: number, record: OnCallAssignmentRule) => value || record.priority || '-' },
    { title: '路由', dataIndex: 'name', width: 220 },
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
          {record.businessLineId ? <Tag>业务线:{businessLineMap[record.businessLineId] || record.businessLineId}</Tag> : null}
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
        <Button size="small" icon={<EditOutlined />} onClick={() => openAssignmentEditor(record)}>编辑</Button>
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

  function submitRotation(values: Record<string, unknown>) {
    try {
      const payload = {
        ...values,
        participants: String(values.participants || '').split(',').map((item) => item.trim()).filter(Boolean),
        rotationConfig: safeParseJson(String(values.rotationConfig || '{}'), {}),
        enabled: Boolean(values.enabled),
      }
      if (editingRotation?.id) {
        updateRotation.mutate({ id: editingRotation.id, payload })
        return
      }
      createRotation.mutate(payload)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  function openOverrideEditor(date: Dayjs) {
    setCalendarValue(date)
    if (!canManageOnCall) return
    if (!selectedSchedule) {
      void message.warning('请先选择排班表')
      return
    }
    if (!selectedRotation) {
      void message.warning('请先为当前排班新增轮值')
      return
    }
    setOverrideDate(date)
    overrideForm.setFieldsValue({ participants: assignmentForDate(date).participants })
    setOverrideOpen(true)
  }

  function submitOverride(values: Record<string, unknown>) {
    if (!selectedRotation || !overrideDate) return
    const participants = normalizeParticipantList(values.participants)
    const dateKey = overrideDate.format(ONCALL_DATE_FORMAT)
    const payload = {
      scheduleId: selectedRotation.scheduleId,
      name: selectedRotation.name,
      participants: normalizeParticipantList(selectedRotation.participants),
      rotationConfig: buildRotationConfigWithOverride(selectedRotation.rotationConfig, dateKey, participants),
      enabled: selectedRotation.enabled,
    }
    updateRotationOverride.mutate({ id: selectedRotation.id, payload })
  }

  function clearOverride() {
    if (!selectedRotation || !overrideDate) return
    const dateKey = overrideDate.format(ONCALL_DATE_FORMAT)
    const payload = {
      scheduleId: selectedRotation.scheduleId,
      name: selectedRotation.name,
      participants: normalizeParticipantList(selectedRotation.participants),
      rotationConfig: buildRotationConfigWithOverride(selectedRotation.rotationConfig, dateKey, []),
      enabled: selectedRotation.enabled,
    }
    updateRotationOverride.mutate({ id: selectedRotation.id, payload })
  }

  function submitPolicy(values: Record<string, unknown>) {
    try {
      const payload = {
        ...values,
        steps: safeParseJson(String(values.steps || '[]'), []),
        enabled: Boolean(values.enabled),
      }
      if (editingPolicy?.id) {
        updatePolicy.mutate({ id: editingPolicy.id, payload })
        return
      }
      createPolicy.mutate(payload)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
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
    <div className="kc-page">
      <PageHeader
        title="值班协同"
        description="以排班日历承载每日值班安排，IRM 路由、告警任务和升级配置作为后续操作面。"
        showResourceScope={false}
        actions={(
          <Space>
            {canManageOnCall ? <Button icon={<PlusOutlined />} onClick={() => { setEditingRotation(null); rotationForm.resetFields(); setRotationOpen(true) }}>新增轮值</Button> : null}
            {canManageOnCall ? <Button icon={<PlusOutlined />} onClick={() => { setEditingSchedule(null); scheduleForm.resetFields(); setScheduleOpen(true) }}>新增排班</Button> : null}
          </Space>
        )}
      />
      <Card
        className="kc-overview-panel-card kc-oncall-calendar-card"
        title="排班日历"
        extra={(
          <Select
            showSearch
            value={selectedSchedule?.id}
            placeholder="选择排班表"
            options={scheduleOptions}
            loading={schedulesQuery.isLoading}
            style={{ width: 260 }}
            onChange={(value) => setSelectedScheduleId(value)}
          />
        )}
      >
        {selectedSchedule ? (
          <Space orientation="vertical" size={16} style={{ width: '100%' }}>
            <div className="kc-oncall-calendar-toolbar">
              <Space wrap>
                <Text strong>{selectedSchedule.name}</Text>
                <Tag>{selectedSchedule.timeZone || 'Local time'}</Tag>
                {selectedRotation ? <Tag>{selectedRotation.name}</Tag> : <Tag color="orange">缺少轮值</Tag>}
                <Tag>覆盖 {Object.keys(selectedOverrides).length}</Tag>
              </Space>
              <Space wrap>
                <Text type="secondary">{calendarValue.format(ONCALL_DATE_FORMAT)}</Text>
                {selectedDateAssignment.participants.length > 0 ? (
                  selectedDateAssignment.participants.map((participant) => (
                    <Tag key={participant} color={selectedDateAssignment.override ? 'gold' : undefined}>{participant}</Tag>
                  ))
                ) : (
                  <Tag>未排班</Tag>
                )}
              </Space>
            </div>
            <Calendar
              value={calendarValue}
              cellRender={calendarCellRender}
              onPanelChange={(value) => setCalendarValue(value)}
              onSelect={(date, info) => {
                if (info.source === 'date') {
                  openOverrideEditor(date)
                  return
                }
                setCalendarValue(date)
              }}
            />
          </Space>
        ) : (
          <Empty description="暂无排班表" />
        )}
      </Card>
      <Card className="kc-overview-panel-card" title="待响应任务">
        <AdminTable shellClassName="is-panel" columns={taskColumns} dataSource={tasksQuery.data?.data ?? []} rowKey="id" loading={tasksQuery.isLoading} />
      </Card>
      <Tabs
        items={[
          {
            key: 'assignments',
            label: 'IRM 路由',
            children: <AdminTable shellClassName="is-panel" columns={assignmentColumns} dataSource={assignmentsQuery.data?.data ?? []} rowKey="id" loading={assignmentsQuery.isLoading} />,
          },
          {
            key: 'schedules',
            label: '排班',
            children: <AdminTable shellClassName="is-panel" columns={scheduleColumns} dataSource={schedulesQuery.data?.data ?? []} rowKey="id" loading={schedulesQuery.isLoading} />,
          },
          {
            key: 'rotations',
            label: '轮值',
            children: <AdminTable shellClassName="is-panel" columns={rotationColumns} dataSource={rotationsQuery.data?.data ?? []} rowKey="id" loading={rotationsQuery.isLoading} />,
          },
          {
            key: 'policies',
            label: '升级链',
            children: <AdminTable shellClassName="is-panel" columns={escalationColumns} dataSource={policiesQuery.data?.data ?? []} rowKey="id" loading={policiesQuery.isLoading} />,
          },
        ]}
      />

      <Modal
        title={overrideDate ? `${overrideDate.format(ONCALL_DATE_FORMAT)} 值班覆盖` : '值班覆盖'}
        open={overrideOpen}
        onCancel={() => setOverrideOpen(false)}
        footer={null}
        destroyOnHidden
      >
        <Form layout="vertical" form={overrideForm} onFinish={submitOverride} clearOnDestroy>
          <Form.Item name="participants" label="当日值班人员">
            <Select
              mode="tags"
              allowClear
              options={participantOptions}
              tokenSeparators={[',']}
              placeholder="输入人员名称后回车"
            />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={updateRotationOverride.isPending}>保存覆盖</Button>
            <Button onClick={() => setOverrideOpen(false)}>取消</Button>
            <Button danger disabled={!overrideDate || !selectedOverrides[overrideDate.format(ONCALL_DATE_FORMAT)]} loading={updateRotationOverride.isPending} onClick={clearOverride}>清除覆盖</Button>
          </Space>
        </Form>
      </Modal>

      <Modal title={editingSchedule ? '编辑排班' : '新建排班'} open={scheduleOpen} onCancel={() => setScheduleOpen(false)} footer={null} destroyOnClose>
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

      <Modal title={editingAssignment ? '编辑 IRM 路由' : '新建 IRM 路由'} open={assignmentOpen} onCancel={() => setAssignmentOpen(false)} footer={null} destroyOnClose width={960}>
        <Form layout="vertical" form={assignmentForm} onFinish={submitAssignment} initialValues={{ targetType: 'escalation', integrationType: 'prometheus', routeOrder: 100, groupBy: ['alertName', 'clusterId', 'namespace', 'service'], enabled: true }}>
          <Space size={16} style={{ width: '100%' }}>
            <Form.Item name="name" label="路由名称" rules={[{ required: true }]} style={{ flex: 1 }}><Input /></Form.Item>
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
            <Form.Item name="businessLineId" label="业务线标签" style={{ flex: 1 }}>
              <Select allowClear options={(businessLinesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} />
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

      <Modal title={editingRotation ? '编辑轮值' : '新建轮值'} open={rotationOpen} onCancel={() => setRotationOpen(false)} footer={null} destroyOnClose width={720}>
        <Form layout="vertical" form={rotationForm} onFinish={submitRotation} initialValues={{ enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="scheduleId" label="排班" rules={[{ required: true }]}>
            <Select showSearch options={(schedulesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name }))} />
          </Form.Item>
          <Form.Item name="participants" label="参与人(逗号分隔)"><Input /></Form.Item>
          <Form.Item name="rotationConfig" label="轮值配置(JSON)"><Input.TextArea rows={4} /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Space>
            <Button type="primary" htmlType="submit">保存</Button>
            <Button onClick={() => setRotationOpen(false)}>取消</Button>
          </Space>
        </Form>
      </Modal>

      <Modal title={editingPolicy ? '编辑升级链' : '新建升级链'} open={policyOpen} onCancel={() => setPolicyOpen(false)} footer={null} destroyOnClose width={720}>
        <Form layout="vertical" form={policyForm} onFinish={submitPolicy} initialValues={{ enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="steps" label="升级步骤(JSON数组)" rules={[{ required: true }]}><Input.TextArea rows={8} /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Space>
            <Button type="primary" htmlType="submit">保存</Button>
            <Button onClick={() => setPolicyOpen(false)}>取消</Button>
          </Space>
        </Form>
      </Modal>
    </div>
  )
}
