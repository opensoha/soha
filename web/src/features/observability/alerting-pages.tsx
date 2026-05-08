import { useMemo, useState } from 'react'
import { App, Button, Card, Form, Input, InputNumber, Modal, Select, Space, Switch, Tag, Tabs, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlusOutlined, PlayCircleOutlined, EditOutlined, CheckOutlined, CloseOutlined, ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { StatusTag, BooleanTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import type { ApiResponse } from '@/types'
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
      <AdminTable columns={ruleColumns} dataSource={rulesQuery.data?.data ?? []} rowKey="id" loading={rulesQuery.isLoading} />

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
        <Space direction="vertical" style={{ width: '100%' }} size={16}>
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
  return <AlertEventDetailPageContent eventId={eventId} onBack={() => navigate('/observability/alerts')} />
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
      <AdminTable columns={policyColumns} dataSource={policiesQuery.data?.data ?? []} rowKey="id" loading={policiesQuery.isLoading} />
      <Card title="自愈运行">
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
  const [scheduleOpen, setScheduleOpen] = useState(false)
  const [rotationOpen, setRotationOpen] = useState(false)
  const [policyOpen, setPolicyOpen] = useState(false)
  const [editingSchedule, setEditingSchedule] = useState<OnCallSchedule | null>(null)
  const [editingRotation, setEditingRotation] = useState<OnCallRotation | null>(null)
  const [editingPolicy, setEditingPolicy] = useState<OnCallEscalationPolicy | null>(null)
  const [previewRef, setPreviewRef] = useState<string>('')

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
  const currentOnCallQuery = useQuery({
    queryKey: ['oncall-current', previewRef],
    queryFn: () => api.get<ApiResponse<Record<string, unknown>>>(`/oncall/current?ref=${encodeURIComponent(previewRef)}`),
    enabled: previewRef !== '',
  })

  const createSchedule = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post('/oncall/schedules', payload),
    onSuccess: () => { void message.success('值班表已保存'); void queryClient.invalidateQueries({ queryKey: ['oncall-schedules'] }); setScheduleOpen(false); setEditingSchedule(null) },
    onError: (err: Error) => void message.error(err.message),
  })
  const updateSchedule = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/oncall/schedules/${id}`, payload),
    onSuccess: () => { void message.success('值班表已更新'); void queryClient.invalidateQueries({ queryKey: ['oncall-schedules'] }); setScheduleOpen(false); setEditingSchedule(null) },
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
    onSuccess: () => { void message.success('升级策略已保存'); void queryClient.invalidateQueries({ queryKey: ['oncall-escalation-policies'] }); setPolicyOpen(false); setEditingPolicy(null) },
    onError: (err: Error) => void message.error(err.message),
  })
  const updatePolicy = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/oncall/escalation-policies/${id}`, payload),
    onSuccess: () => { void message.success('升级策略已更新'); void queryClient.invalidateQueries({ queryKey: ['oncall-escalation-policies'] }); setPolicyOpen(false); setEditingPolicy(null) },
    onError: (err: Error) => void message.error(err.message),
  })

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
    { title: '值班表', dataIndex: 'scheduleId' },
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

  return (
    <div className="kc-page">
      <PageHeader
        title="值班协同"
        description="维护值班表、轮值和升级策略，为告警通知和审批提供引用。"
        actions={(
          <Space>
            {canManageOnCall ? <Button icon={<PlusOutlined />} onClick={() => { setEditingPolicy(null); policyForm.resetFields(); setPolicyOpen(true) }}>新增升级策略</Button> : null}
            {canManageOnCall ? <Button icon={<PlusOutlined />} onClick={() => { setEditingRotation(null); rotationForm.resetFields(); setRotationOpen(true) }}>新增轮值</Button> : null}
            {canManageOnCall ? <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditingSchedule(null); scheduleForm.resetFields(); setScheduleOpen(true) }}>新增值班表</Button> : null}
          </Space>
        )}
      />
      <Card title="当前值班预览">
        <Space direction="vertical" style={{ width: '100%' }}>
          <Select
            value={previewRef || undefined}
            onChange={(value) => setPreviewRef(String(value))}
            placeholder="选择一个值班表或升级策略"
            options={[
              ...(schedulesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: `Schedule · ${item.name}` })),
              ...(policiesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: `Escalation · ${item.name}` })),
            ]}
          />
          <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{JSON.stringify(currentOnCallQuery.data?.data ?? {}, null, 2)}</pre>
        </Space>
      </Card>
      <Tabs
        items={[
          {
            key: 'schedules',
            label: '值班表',
            children: <AdminTable columns={scheduleColumns} dataSource={schedulesQuery.data?.data ?? []} rowKey="id" loading={schedulesQuery.isLoading} />,
          },
          {
            key: 'rotations',
            label: '轮值',
            children: <Card><AdminTable columns={rotationColumns} dataSource={rotationsQuery.data?.data ?? []} rowKey="id" loading={rotationsQuery.isLoading} /></Card>,
          },
          {
            key: 'policies',
            label: '升级策略',
            children: <AdminTable columns={escalationColumns} dataSource={policiesQuery.data?.data ?? []} rowKey="id" loading={policiesQuery.isLoading} />,
          },
        ]}
      />

      <Modal title={editingSchedule ? '编辑值班表' : '新建值班表'} open={scheduleOpen} onCancel={() => setScheduleOpen(false)} footer={null} destroyOnClose>
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

      <Modal title={editingRotation ? '编辑轮值' : '新建轮值'} open={rotationOpen} onCancel={() => setRotationOpen(false)} footer={null} destroyOnClose width={720}>
        <Form layout="vertical" form={rotationForm} onFinish={submitRotation} initialValues={{ enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="scheduleId" label="值班表ID" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="participants" label="参与人(逗号分隔)"><Input /></Form.Item>
          <Form.Item name="rotationConfig" label="轮值配置(JSON)"><Input.TextArea rows={4} /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Space>
            <Button type="primary" htmlType="submit">保存</Button>
            <Button onClick={() => setRotationOpen(false)}>取消</Button>
          </Space>
        </Form>
      </Modal>

      <Modal title={editingPolicy ? '编辑升级策略' : '新建升级策略'} open={policyOpen} onCancel={() => setPolicyOpen(false)} footer={null} destroyOnClose width={720}>
        <Form layout="vertical" form={policyForm} onFinish={submitPolicy} initialValues={{ enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="steps" label="步骤(JSON数组)" rules={[{ required: true }]}><Input.TextArea rows={8} /></Form.Item>
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
