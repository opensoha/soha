import { useMemo, useState } from 'react'
import { AlertOutlined, HeartFilled, EyeOutlined } from '@ant-design/icons'
import { App, Button, Card, Form, Input, InputNumber, Modal, Select, Space, Switch, Tabs, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { PageHeader } from '@/components/page-header'
import { StatGrid } from '@/components/stat-grid'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse } from '@/types'
import { useNavigate } from 'react-router-dom'
import { AlertEventDetailDrawer } from '@/features/observability/alert-event-detail'

const { Title, Text, Paragraph } = Typography

/* ─── Monitoring ─── */

interface MonitoringSummary {
  totalCount: number
  firingCount: number
  resolvedCount: number
  criticalCount: number
  warningCount: number
  infoCount: number
  channelCount: number
  lastReceivedAt?: string
}

export function MonitoringPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['monitoring-summary'],
    queryFn: () => api.get<ApiResponse<MonitoringSummary>>('/monitoring/summary'),
  })

  const summary = data?.data

  const stats = summary
    ? [
        { label: '总告警数', value: summary.totalCount, icon: <HeartFilled style={{ fontSize: 28 }} /> },
        { label: '活跃告警', value: summary.firingCount, icon: <AlertOutlined style={{ fontSize: 28 }} /> },
        { label: '已恢复', value: summary.resolvedCount, icon: <HeartFilled style={{ fontSize: 28 }} /> },
        { label: '严重告警', value: summary.criticalCount, icon: <AlertOutlined style={{ fontSize: 28 }} /> },
        { label: '通知渠道', value: summary.channelCount, icon: <HeartFilled style={{ fontSize: 28 }} /> },
      ]
    : []

  return (
    <div className="kc-page">
      <PageHeader title="监控工作台概览" description="查看告警压力、通知覆盖度与整体运行态势。" />

      {isLoading ? (
        <div className="grid grid-cols-1 sm:grid-cols-3 lg:grid-cols-5 gap-4">
          {[...Array(5)].map((_, i) => (
            <Card key={i} bodyStyle={{ padding: 20 }} loading />
          ))}
        </div>
      ) : (
        <StatGrid items={stats} />
      )}

      <Card title="告警分布">
        <div className="flex items-center justify-center h-64 text-gray-400">
          <div className="text-center">
            <HeartFilled style={{ fontSize: 28 }} />
            <Paragraph type="secondary" className="mt-2">
              Critical {summary?.criticalCount ?? 0} / Warning {summary?.warningCount ?? 0} / Info {summary?.infoCount ?? 0}
            </Paragraph>
            <Paragraph type="secondary">
              最近接收时间: {formatDateTime(summary?.lastReceivedAt)}
            </Paragraph>
          </div>
        </div>
      </Card>
    </div>
  )
}

/* ─── Alerts ─── */

interface Alert {
  id: string
  title: string
  summary: string
  severity: string
  status: string
  currentState?: string
  sourceType?: string
  sourceSystem?: string
  clusterId?: string
  namespace?: string
  startsAt?: string
  lastSeenAt?: string
}

export function AlertsPage() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { message } = App.useApp()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canAcknowledge = hasPermission(permissionSnapshotQuery.data?.data, 'observe.alerts.ack')
  const canResolve = hasPermission(permissionSnapshotQuery.data?.data, 'observe.alerts.manage')
  const canHeal = hasPermission(permissionSnapshotQuery.data?.data, 'observe.healing.manage')
  const [healOpen, setHealOpen] = useState(false)
  const [healingPolicyId, setHealingPolicyId] = useState<string>('')
  const [selectedAlertId, setSelectedAlertId] = useState<string>('')
  const [detailOpen, setDetailOpen] = useState(false)
  const [detailEventId, setDetailEventId] = useState<string>('')

  const { data, isLoading } = useQuery({
    queryKey: ['alert-events'],
    queryFn: () => api.get<ApiResponse<Alert[]>>('/alert-events'),
  })
  const healingPoliciesQuery = useQuery({
    queryKey: ['healing-policies'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string; enabled: boolean }>>>('/healing-policies'),
  })

  const ackMutation = useMutation({
    mutationFn: (id: string) => api.post(`/alert-events/${id}/acknowledge`),
    onSuccess: () => {
      message.success('告警已确认')
      queryClient.invalidateQueries({ queryKey: ['alert-events'] })
    },
    onError: (err: Error) => message.error(err.message),
  })
  const resolveMutation = useMutation({
    mutationFn: (id: string) => api.post(`/alert-events/${id}/resolve`),
    onSuccess: () => {
      message.success('告警已恢复')
      queryClient.invalidateQueries({ queryKey: ['alert-events'] })
    },
    onError: (err: Error) => message.error(err.message),
  })
  const healMutation = useMutation({
    mutationFn: ({ id, policyId }: { id: string; policyId: string }) => api.post(`/alert-events/${id}/heal?policyId=${encodeURIComponent(policyId)}`),
    onSuccess: () => {
      message.success('自愈运行已创建')
      queryClient.invalidateQueries({ queryKey: ['healing-runs'] })
      setHealOpen(false)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const columns: ColumnsType<Alert> = [
    { title: '名称', dataIndex: 'title' },
    {
      ...tableColumnPresets.status,
      title: '严重程度',
      dataIndex: 'severity',
      render: (s: string) => <StatusTag value={s} />,
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    { title: '来源', dataIndex: 'sourceSystem', render: (value: string, record: Alert) => value || record.sourceType || '-' },
    { title: '范围', dataIndex: 'namespace', render: (value: string, record: Alert) => [record.clusterId, value].filter(Boolean).join(' / ') || '-' },
    { title: '消息', dataIndex: 'summary', ellipsis: true },
    { ...tableColumnPresets.datetime, title: '触发时间', dataIndex: 'startsAt', render: (value: string) => formatDateTime(value) },
    { ...tableColumnPresets.datetime, title: '最近命中', dataIndex: 'lastSeenAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: Alert) =>
        <Space>
          <Button size="small" type="primary" onClick={() => navigate(`/ai-workbench/investigation?mode=root_cause&alertId=${encodeURIComponent(record.id)}&clusterId=${encodeURIComponent(record.clusterId || '')}&namespace=${encodeURIComponent(record.namespace || '')}&timeRangeMinutes=60`)}>AI调查</Button>
          <Button size="small" icon={<EyeOutlined />} onClick={() => { setDetailEventId(record.id); setDetailOpen(true) }}>详情</Button>
          {canHeal ? <Button size="small" onClick={() => { setSelectedAlertId(record.id); setHealingPolicyId(''); setHealOpen(true) }}>自愈</Button> : null}
          {canAcknowledge && record.status !== 'acknowledged' ? <Button size="small" type="link" onClick={() => ackMutation.mutate(record.id)}>确认</Button> : null}
          {canResolve && record.status !== 'resolved' ? <Button size="small" type="link" onClick={() => resolveMutation.mutate(record.id)}>恢复</Button> : null}
        </Space>,
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader title="活跃告警" description="查看当前告警事件、来源、状态以及确认/恢复/自愈操作。" />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} pageSize={20} />
      <Modal title="发起自愈" open={healOpen} onCancel={() => setHealOpen(false)} onOk={() => healMutation.mutate({ id: selectedAlertId, policyId: healingPolicyId })} okButtonProps={{ disabled: !healingPolicyId }} destroyOnHidden>
        <Select
          style={{ width: '100%' }}
          placeholder="选择自愈策略"
          value={healingPolicyId}
          onChange={(value) => setHealingPolicyId(String(value))}
          options={(healingPoliciesQuery.data?.data ?? []).filter((item) => item.enabled).map((item) => ({ value: item.id, label: item.name }))}
        />
      </Modal>
      <AlertEventDetailDrawer
        eventId={detailEventId}
        open={detailOpen}
        onClose={() => setDetailOpen(false)}
        onOpenStandalone={(eventId) => {
          setDetailOpen(false)
          navigate(`/monitoring-workbench/alerts/${eventId}`)
        }}
      />
    </div>
  )
}

/* ─── Notifications ─── */

interface NotificationChannel {
  id: string
  name: string
  channelType: string
  config?: Record<string, unknown>
  enabled: boolean
}

interface NotificationRoute {
  id: string
  name: string
  matchers?: Record<string, unknown>
  channelIds?: string[]
  enabled: boolean
}

interface Silence {
  id: string
  matchers: string
  creator: string
  startsAt: string
  endsAt: string
  status: string
}

interface NotificationPolicy {
  id: string
  name: string
  matchers?: Record<string, unknown>
  processorChain?: string[]
  channelRefs?: string[]
  oncallRef?: string
  sendResolved: boolean
  cooldownSeconds: number
  enabled: boolean
}

interface NotificationTemplate {
  id: string
  name: string
  templateType: string
  contentType: string
  bodyTemplate?: string
  headers?: Record<string, unknown>
  queryParams?: Record<string, unknown>
  samplePayload?: Record<string, unknown>
  enabled: boolean
}

function resolveChannelEndpoint(config?: Record<string, unknown>) {
  const keys = ['url', 'webhookUrl', 'webhook_url', 'endpoint']
  for (const key of keys) {
    const value = config?.[key]
    if (typeof value === 'string' && value.trim()) {
      return value.trim()
    }
  }
  return '-'
}

function stringifyRouteMatchers(matchers?: Record<string, unknown>) {
  if (!matchers || Object.keys(matchers).length === 0) {
    return '{}'
  }
  return JSON.stringify(matchers)
}

export function NotificationsPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManageNotifications = hasPermission(permissionSnapshotQuery.data?.data, 'observe.notifications.manage')
  const [policyForm] = Form.useForm<Record<string, unknown>>()
  const [templateForm] = Form.useForm<Record<string, unknown>>()
  const [policyOpen, setPolicyOpen] = useState(false)
  const [templateOpen, setTemplateOpen] = useState(false)
  const [editingPolicy, setEditingPolicy] = useState<NotificationPolicy | null>(null)
  const [editingTemplate, setEditingTemplate] = useState<NotificationTemplate | null>(null)
  const [previewOpen, setPreviewOpen] = useState(false)
  const [previewPolicy, setPreviewPolicy] = useState<NotificationPolicy | null>(null)
  const [previewEventId, setPreviewEventId] = useState<string>('')
  const [previewItems, setPreviewItems] = useState<Array<Record<string, unknown>>>([])

  const channelsQuery = useQuery({
    queryKey: ['notification-channels'],
    queryFn: () => api.get<ApiResponse<NotificationChannel[]>>('/notification-channels'),
  })
  const alertEventsQuery = useQuery({
    queryKey: ['notification-preview-events'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; title: string; status: string }>>>('/alert-events?limit=20'),
  })
  const policiesQuery = useQuery({
    queryKey: ['notification-policies'],
    queryFn: () => api.get<ApiResponse<NotificationPolicy[]>>('/notification-policies'),
  })
  const templatesQuery = useQuery({
    queryKey: ['notification-templates'],
    queryFn: () => api.get<ApiResponse<NotificationTemplate[]>>('/notification-templates'),
  })
  const routesQuery = useQuery({
    queryKey: ['notification-routes'],
    queryFn: () => api.get<ApiResponse<NotificationRoute[]>>('/alert-routes'),
  })
  const silencesQuery = useQuery({
    queryKey: ['notification-silences'],
    queryFn: () => api.get<ApiResponse<Silence[]>>('/alert-silences'),
  })

  const createPolicy = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post('/notification-policies', payload),
    onSuccess: () => {
      message.success('通知策略已保存')
      queryClient.invalidateQueries({ queryKey: ['notification-policies'] })
      setPolicyOpen(false)
      setEditingPolicy(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updatePolicy = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/notification-policies/${id}`, payload),
    onSuccess: () => {
      message.success('通知策略已更新')
      queryClient.invalidateQueries({ queryKey: ['notification-policies'] })
      setPolicyOpen(false)
      setEditingPolicy(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const createTemplate = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post('/notification-templates', payload),
    onSuccess: () => {
      message.success('通知模板已保存')
      queryClient.invalidateQueries({ queryKey: ['notification-templates'] })
      setTemplateOpen(false)
      setEditingTemplate(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateTemplate = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/notification-templates/${id}`, payload),
    onSuccess: () => {
      message.success('通知模板已更新')
      queryClient.invalidateQueries({ queryKey: ['notification-templates'] })
      setTemplateOpen(false)
      setEditingTemplate(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const previewMutation = useMutation({
    mutationFn: ({ policyId, eventId }: { policyId: string; eventId: string }) => api.get<ApiResponse<Array<Record<string, unknown>>>>(`/notification-policies/${policyId}/preview?eventId=${encodeURIComponent(eventId)}`),
    onSuccess: (payload) => {
      setPreviewItems(payload.data ?? [])
      setPreviewOpen(true)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const channelNamesById = useMemo(() => {
    return Object.fromEntries((channelsQuery.data?.data ?? []).map((item) => [item.id, item.name]))
  }, [channelsQuery.data?.data])

  const channelColumns: ColumnsType<NotificationChannel> = [
    { title: '名称', dataIndex: 'name' },
    { title: '类型', dataIndex: 'channelType', render: (value: string) => <Tag>{value}</Tag> },
    { title: 'Endpoint', dataIndex: 'config', ellipsis: true, render: (value: Record<string, unknown>) => resolveChannelEndpoint(value) },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'enabled',
      render: (v: boolean) => <BooleanTag value={v} trueLabel="启用" falseLabel="禁用" />,
    },
  ]

  const routeColumns: ColumnsType<NotificationRoute> = [
    { title: '名称', dataIndex: 'name' },
    { title: '匹配规则', dataIndex: 'matchers', render: (value: Record<string, unknown>) => <Text code>{stringifyRouteMatchers(value)}</Text> },
    {
      title: '接收器',
      dataIndex: 'channelIds',
      render: (value: string[]) => {
        const items = (value ?? []).map((item) => channelNamesById[item] || item)
        return items.length > 0 ? <Space wrap>{items.map((item) => <Tag key={item}>{item}</Tag>)}</Space> : '-'
      },
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'enabled',
      render: (v: boolean) => <BooleanTag value={v} trueLabel="启用" falseLabel="禁用" />,
    },
  ]

  const silenceColumns: ColumnsType<Silence> = [
    { title: '匹配器', dataIndex: 'matchers' },
    { title: '创建者', dataIndex: 'creator' },
    { ...tableColumnPresets.datetime, title: '开始时间', dataIndex: 'startsAt', render: (value: string) => formatDateTime(value) },
    { ...tableColumnPresets.datetime, title: '结束时间', dataIndex: 'endsAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
  ]

  const policyColumns: ColumnsType<NotificationPolicy> = [
    { title: '名称', dataIndex: 'name' },
    { title: '处理链', dataIndex: 'processorChain', render: (value: string[]) => <Space wrap>{(value ?? []).map((item) => <Tag key={item}>{item}</Tag>)}</Space> },
    { title: '渠道', dataIndex: 'channelRefs', render: (value: string[]) => <Space wrap>{(value ?? []).map((item) => <Tag key={item}>{item}</Tag>)}</Space> },
    { title: 'OnCall', dataIndex: 'oncallRef', render: (value: string) => value || '-' },
    { title: '恢复通知', dataIndex: 'sendResolved', render: (value: boolean) => <BooleanTag value={value} trueLabel="发送" falseLabel="不发送" /> },
    { title: '冷却(s)', dataIndex: 'cooldownSeconds' },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" /> },
    {
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record: NotificationPolicy) => (
        <Space>
          {canManageNotifications ? <Button size="small" onClick={() => openPolicyEditor(record)}>编辑</Button> : null}
          <Button
            size="small"
            onClick={() => {
              const firstEvent = alertEventsQuery.data?.data?.[0]?.id || ''
              setPreviewPolicy(record)
              setPreviewEventId(firstEvent)
              if (firstEvent) {
                previewMutation.mutate({ policyId: record.id, eventId: firstEvent })
              } else {
                setPreviewItems([])
                setPreviewOpen(true)
              }
            }}
          >
            预览
          </Button>
        </Space>
      ),
    },
  ]

  const templateColumns: ColumnsType<NotificationTemplate> = [
    { title: '名称', dataIndex: 'name' },
    { title: '模板类型', dataIndex: 'templateType', render: (value: string) => <Tag>{value}</Tag> },
    { title: '内容类型', dataIndex: 'contentType' },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" /> },
    { title: '操作', dataIndex: 'id', render: (_: string, record: NotificationTemplate) => canManageNotifications ? <Button size="small" onClick={() => openTemplateEditor(record)}>编辑</Button> : null },
  ]

  function openPolicyEditor(record: NotificationPolicy | null) {
    setEditingPolicy(record)
    setPolicyOpen(true)
    policyForm.setFieldsValue(record ? {
      ...record,
      matchers: JSON.stringify(record.matchers ?? {}, null, 2),
      processorChain: (record.processorChain ?? []).join(', '),
      channelRefs: (record.channelRefs ?? []).join(', '),
    } : {
      name: '',
      matchers: '{}',
      processorChain: 'template_render, webhook_update',
      channelRefs: '',
      oncallRef: '',
      sendResolved: false,
      cooldownSeconds: 0,
      enabled: true,
    })
  }

  function openTemplateEditor(record: NotificationTemplate | null) {
    setEditingTemplate(record)
    setTemplateOpen(true)
    templateForm.setFieldsValue(record ? {
      ...record,
      headers: JSON.stringify(record.headers ?? {}, null, 2),
      queryParams: JSON.stringify(record.queryParams ?? {}, null, 2),
      samplePayload: JSON.stringify(record.samplePayload ?? {}, null, 2),
    } : {
      name: '',
      templateType: 'generic_json',
      contentType: 'application/json',
      bodyTemplate: '{"alert":"{{ .alert.title }}"}',
      headers: '{}',
      queryParams: '{}',
      samplePayload: '{}',
      enabled: true,
    })
  }

  function submitPolicy(values: Record<string, unknown>) {
    try {
      const payload = {
        name: values.name,
        matchers: JSON.parse(String(values.matchers || '{}')),
        processorChain: String(values.processorChain || '').split(',').map((item) => item.trim()).filter(Boolean),
        channelRefs: String(values.channelRefs || '').split(',').map((item) => item.trim()).filter(Boolean),
        oncallRef: String(values.oncallRef || ''),
        sendResolved: Boolean(values.sendResolved),
        cooldownSeconds: Number(values.cooldownSeconds || 0),
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

  function submitTemplate(values: Record<string, unknown>) {
    try {
      const payload = {
        name: values.name,
        templateType: values.templateType,
        contentType: values.contentType,
        bodyTemplate: values.bodyTemplate,
        headers: JSON.parse(String(values.headers || '{}')),
        queryParams: JSON.parse(String(values.queryParams || '{}')),
        samplePayload: JSON.parse(String(values.samplePayload || '{}')),
        enabled: Boolean(values.enabled),
      }
      if (editingTemplate?.id) {
        updateTemplate.mutate({ id: editingTemplate.id, payload })
        return
      }
      createTemplate.mutate(payload)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  return (
    <div className="kc-page">
      <PageHeader
        title="通知策略"
        description="维护通知策略、模板、渠道、路由规则与静默策略。"
        actions={canManageNotifications ? <Space><Button onClick={() => openTemplateEditor(null)}>新建模板</Button><Button type="primary" onClick={() => openPolicyEditor(null)}>新建策略</Button></Space> : null}
      />
      <Tabs
        items={[
          {
            key: 'policies',
            label: '通知策略',
            children: <AdminTable columns={policyColumns} dataSource={policiesQuery.data?.data ?? []} rowKey="id" loading={policiesQuery.isLoading} />,
          },
          {
            key: 'templates',
            label: '通知模板',
            children: <AdminTable columns={templateColumns} dataSource={templatesQuery.data?.data ?? []} rowKey="id" loading={templatesQuery.isLoading} />,
          },
          {
            key: 'channels',
            label: '通知渠道',
            children: <AdminTable columns={channelColumns} dataSource={channelsQuery.data?.data ?? []} rowKey="id" loading={channelsQuery.isLoading} />,
          },
          {
            key: 'routes',
            label: '路由规则（兼容）',
            children: (
              <AdminTable
                columns={routeColumns}
                dataSource={routesQuery.data?.data ?? []}
                rowKey="id"
                loading={routesQuery.isLoading}
                headerExtra={<Text data-testid="notification-route-compat-note" type="secondary">只读兼容视图。旧 `/alert-routes` 已投影到 canonical `notification_policies`。</Text>}
              />
            ),
          },
          {
            key: 'silences',
            label: '静默规则',
            children: <AdminTable columns={silenceColumns} dataSource={silencesQuery.data?.data ?? []} rowKey="id" loading={silencesQuery.isLoading} />,
          },
        ]}
      />
      <Modal title={editingPolicy ? '编辑通知策略' : '新建通知策略'} open={policyOpen} onCancel={() => setPolicyOpen(false)} footer={null} destroyOnHidden width={760}>
        <Form layout="vertical" form={policyForm} onFinish={submitPolicy} initialValues={{ sendResolved: false, cooldownSeconds: 0, enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="matchers" label="匹配器(JSON)"><Input.TextArea rows={4} /></Form.Item>
          <Form.Item name="processorChain" label="处理链(逗号分隔)"><Input /></Form.Item>
          <Form.Item name="channelRefs" label="渠道引用(逗号分隔)"><Input /></Form.Item>
          <Form.Item name="oncallRef" label="OnCall 引用"><Input /></Form.Item>
          <Form.Item name="cooldownSeconds" label="冷却(s)"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="sendResolved" label="恢复通知" valuePropName="checked"><Switch /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Space><Button type="primary" htmlType="submit">保存</Button><Button onClick={() => setPolicyOpen(false)}>取消</Button></Space>
        </Form>
      </Modal>
      <Modal title={editingTemplate ? '编辑通知模板' : '新建通知模板'} open={templateOpen} onCancel={() => setTemplateOpen(false)} footer={null} destroyOnHidden width={860}>
        <Form layout="vertical" form={templateForm} onFinish={submitTemplate} initialValues={{ templateType: 'generic_json', contentType: 'application/json', enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Space size={16} style={{ width: '100%' }}>
            <Form.Item name="templateType" label="模板类型" style={{ flex: 1 }}><Select options={[{ value: 'generic_json', label: 'generic_json' }, { value: 'alertmanager_v1', label: 'alertmanager_v1' }, { value: 'grafana_v1', label: 'grafana_v1' }]} /></Form.Item>
            <Form.Item name="contentType" label="Content-Type" style={{ flex: 1 }}><Input /></Form.Item>
          </Space>
          <Form.Item name="bodyTemplate" label="Body 模板"><Input.TextArea rows={6} /></Form.Item>
          <Form.Item name="headers" label="Headers(JSON)"><Input.TextArea rows={3} /></Form.Item>
          <Form.Item name="queryParams" label="QueryParams(JSON)"><Input.TextArea rows={3} /></Form.Item>
          <Form.Item name="samplePayload" label="样例 Payload(JSON)"><Input.TextArea rows={4} /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Space><Button type="primary" htmlType="submit">保存</Button><Button onClick={() => setTemplateOpen(false)}>取消</Button></Space>
        </Form>
      </Modal>
      <Modal
        title={previewPolicy ? `通知预览 · ${previewPolicy.name}` : '通知预览'}
        open={previewOpen}
        onCancel={() => setPreviewOpen(false)}
        footer={null}
        width={960}
        destroyOnHidden
      >
        <Space direction="vertical" style={{ width: '100%' }} size={16}>
          <Select
            value={previewEventId}
            onChange={(value) => {
              const next = String(value)
              setPreviewEventId(next)
              if (previewPolicy && next) {
                previewMutation.mutate({ policyId: previewPolicy.id, eventId: next })
              }
            }}
            style={{ width: '100%' }}
            placeholder="选择告警事件"
            options={(alertEventsQuery.data?.data ?? []).map((item) => ({ value: item.id, label: `${item.title} (${item.status})` }))}
          />
          <AdminTable
            columns={[
              { title: '渠道', dataIndex: 'channelId' },
              { title: '模板', dataIndex: 'templateId', render: (value: string) => value || '-' },
              { title: 'URL', dataIndex: 'url', ellipsis: true },
              { title: 'Method', dataIndex: 'method' },
              { title: 'Content-Type', dataIndex: 'contentType' },
              { title: 'Body', dataIndex: 'body', render: (value: string) => <Text code>{String(value || '')}</Text> },
            ]}
            dataSource={previewItems}
            rowKey={(record) => `${record.channelId || 'channel'}:${record.templateId || 'template'}:${record.url || 'url'}`}
            pagination={false}
          />
        </Space>
      </Modal>
    </div>
  )
}

/* ─── Events ─── */

interface EventStreamEntry {
  id: string
  source: string
  category: string
  severity?: string
  clusterId?: string
  namespace?: string
  summary: string
  payload?: Record<string, unknown>
}

export function EventsPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['events'],
    queryFn: () => api.get<ApiResponse<EventStreamEntry[]>>('/events'),
  })

  const columns: ColumnsType<EventStreamEntry> = [
    { title: '来源', dataIndex: 'source', width: 180, render: (value: string) => value || '-' },
    { title: '类别', dataIndex: 'category', width: 160, render: (value: string) => value || '-' },
    { ...tableColumnPresets.status, title: '严重度', dataIndex: 'severity', render: (value?: string) => <StatusTag value={value} /> },
    { title: '范围', dataIndex: 'namespace', width: 220, render: (value: string, record: EventStreamEntry) => [record.clusterId, value].filter(Boolean).join(' / ') || '-' },
    { title: '摘要', dataIndex: 'summary', ellipsis: true, render: (value: string) => value || '-' },
    {
      title: 'Payload',
      dataIndex: 'payload',
      ellipsis: true,
      render: (value: Record<string, unknown>) => {
        if (!value || Object.keys(value).length === 0) {
          return '-'
        }
        return <Text code>{JSON.stringify(value)}</Text>
      },
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader title="事件流" description="查看时间线上的平台事件、来源对象和详细消息。" />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} pageSize={50} />
    </div>
  )
}

/* ─── OnCall ─── */

export function OnCallPage() {
  return (
    <div className="kc-page">
      <PageHeader title="值班协同" description="值班排班、升级策略与通知联动能力的预留入口。" />
      <Card>
        <div className="flex flex-col items-center justify-center h-64 text-gray-400">
          <AlertOutlined style={{ fontSize: 28 }} />
          <Title level={5} style={{ marginTop: 16 }}>值班协同</Title>
          <Paragraph type="secondary">
            配置值班轮换计划、升级策略和通知规则。该功能即将上线。
          </Paragraph>
        </div>
      </Card>
    </div>
  )
}
