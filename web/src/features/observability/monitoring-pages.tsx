import { useMemo, useState } from 'react'
import {
  AlertOutlined,
  BellOutlined,
  EyeOutlined,
  FireOutlined,
  NotificationOutlined,
  PlusOutlined,
  ReloadOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import { App, Button, Card, Empty, Form, Input, InputNumber, Modal, Select, Space, Statistic, Switch, Tabs, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { PageHeader } from '@/components/page-header'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { getAIWorkbenchPathForMode } from '@/features/copilot/workbench-navigation'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse } from '@/types'
import { useNavigate } from 'react-router-dom'
import { AlertEventDetailDrawer } from '@/features/observability/alert-event-detail'

const { Text } = Typography

function safeParseJson(raw: string, fallback: any) {
  const text = raw.trim()
  if (!text) return fallback
  const parsed = JSON.parse(text)
  if (Array.isArray(fallback)) {
    if (Array.isArray(parsed)) return parsed
    throw new Error('需要合法 JSON 数组')
  }
  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return parsed
  throw new Error('需要合法 JSON 对象')
}

function prettyJson(value: unknown) {
  if (value == null) return ''
  return JSON.stringify(value, null, 2)
}

function splitList(value: unknown) {
  if (Array.isArray(value)) {
    return value.map((item) => String(item).trim()).filter(Boolean)
  }
  return String(value || '').split(',').map((item) => item.trim()).filter(Boolean)
}

function parseIsoTime(value: unknown, fieldName: string) {
  const text = String(value || '').trim()
  if (!text) {
    throw new Error(`${fieldName}不能为空`)
  }
  const date = new Date(text)
  if (Number.isNaN(date.getTime())) {
    throw new Error(`${fieldName}需要 ISO 时间格式`)
  }
  return date.toISOString()
}

function shortJson(value?: Record<string, unknown>) {
  if (!value || Object.keys(value).length === 0) return '{}'
  return JSON.stringify(value)
}

function isTerminalStatus(status?: string) {
  return ['completed', 'resolved', 'rejected', 'failed', 'canceled', 'cancelled'].includes(String(status || '').toLowerCase())
}

function formatSilenceStatus(item: Silence) {
  if (!item.enabled) return 'disabled'
  const now = Date.now()
  const starts = new Date(item.startsAt).getTime()
  const ends = new Date(item.endsAt).getTime()
  if (Number.isFinite(starts) && now < starts) return 'scheduled'
  if (Number.isFinite(ends) && now > ends) return 'expired'
  return 'active'
}

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
  const navigate = useNavigate()
  const summaryQuery = useQuery({
    queryKey: ['monitoring-summary'],
    queryFn: () => api.get<ApiResponse<MonitoringSummary>>('/monitoring/summary'),
  })
  const alertsQuery = useQuery({
    queryKey: ['monitoring-overview-alerts'],
    queryFn: () => api.get<ApiResponse<Alert[]>>('/alert-events?limit=8'),
  })
  const rulesQuery = useQuery({
    queryKey: ['monitoring-overview-rules'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string; enabled: boolean; updatedAt?: string }>>>('/alert-rules'),
  })
  const policiesQuery = useQuery({
    queryKey: ['monitoring-overview-policies'],
    queryFn: () => api.get<ApiResponse<NotificationPolicy[]>>('/notification-policies'),
  })
  const oncallQuery = useQuery({
    queryKey: ['monitoring-overview-oncall-schedules'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string; enabled: boolean }>>>('/oncall/schedules'),
  })
  const healingRunsQuery = useQuery({
    queryKey: ['monitoring-overview-healing-runs'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; status: string; policyId?: string; eventId?: string; createdAt?: string }>>>('/healing-runs?limit=6'),
  })

  const summary = summaryQuery.data?.data
  const recentAlerts = alertsQuery.data?.data ?? []
  const rules = rulesQuery.data?.data ?? []
  const policies = policiesQuery.data?.data ?? []
  const oncallSchedules = oncallQuery.data?.data ?? []
  const healingRuns = healingRunsQuery.data?.data ?? []
  const enabledRules = rules.filter((item) => item.enabled).length
  const enabledPolicies = policies.filter((item) => item.enabled).length
  const enabledOncall = oncallSchedules.filter((item) => item.enabled).length
  const pendingHealing = healingRuns.filter((item) => !isTerminalStatus(item.status)).length
  const isLoading = summaryQuery.isLoading

  const overviewStats = [
    {
      key: 'firing',
      label: '活跃告警',
      helper: '当前仍需处理的告警事件',
      value: summary?.firingCount ?? 0,
      icon: <AlertOutlined />,
      tone: (summary?.firingCount ?? 0) > 0 ? 'warning' : 'default',
    },
    {
      key: 'critical',
      label: '严重告警',
      helper: 'Critical 优先级信号',
      value: summary?.criticalCount ?? 0,
      icon: <FireOutlined />,
      tone: (summary?.criticalCount ?? 0) > 0 ? 'danger' : 'default',
    },
    {
      key: 'rules',
      label: '启用规则',
      helper: `共 ${rules.length} 条规则`,
      value: enabledRules,
      icon: <BellOutlined />,
      tone: 'default',
    },
    {
      key: 'channels',
      label: '通知渠道',
      helper: '可用于投递的渠道数',
      value: summary?.channelCount ?? 0,
      icon: <NotificationOutlined />,
      tone: 'default',
    },
  ]

  const alertChips = [
    { key: 'total', label: '总数', value: summary?.totalCount ?? 0, tone: 'default' },
    { key: 'firing', label: '活跃', value: summary?.firingCount ?? 0, tone: 'warning' },
    { key: 'resolved', label: '已恢复', value: summary?.resolvedCount ?? 0, tone: 'success' },
    { key: 'critical', label: 'Critical', value: summary?.criticalCount ?? 0, tone: 'danger' },
    { key: 'warning', label: 'Warning', value: summary?.warningCount ?? 0, tone: 'warning' },
    { key: 'info', label: 'Info', value: summary?.infoCount ?? 0, tone: 'default' },
  ]

  const operationStats = [
    { key: 'policies', label: '启用通知策略', value: enabledPolicies, helper: `共 ${policies.length} 条策略`, icon: <NotificationOutlined />, tone: 'default' },
    { key: 'oncall', label: '启用值班表', value: enabledOncall, helper: `共 ${oncallSchedules.length} 张值班表`, icon: <TeamOutlined />, tone: 'default' },
    { key: 'healing', label: '待处理自愈', value: pendingHealing, helper: `最近 ${healingRuns.length} 条运行`, icon: <ReloadOutlined />, tone: pendingHealing > 0 ? 'warning' : 'default' },
  ]

  return (
    <div className="kc-page kc-overview-page kc-monitoring-overview-page">
      <section className="kc-overview-hero">
        <div className="kc-overview-hero-header">
          <h1 className="kc-overview-title">总览</h1>
        </div>

        <div className="kc-overview-metric-grid">
          {overviewStats.map((item) => (
            <Card key={item.key} size="small" variant="outlined" className={`kc-overview-metric-card is-${item.tone}`} loading={isLoading}>
              <div className="kc-overview-metric-card-head">
                <div className="kc-overview-metric-copy">
                  <Text className="kc-overview-metric-label">{item.label}</Text>
                  <Statistic value={item.value} />
                </div>
                <span className="kc-overview-metric-icon">{item.icon}</span>
              </div>
              <Text className="kc-overview-metric-helper">{item.helper}</Text>
            </Card>
          ))}
        </div>
      </section>

      <div className="kc-overview-summary-grid">
        <Card
          className="kc-overview-panel-card"
          title="告警态势"
          extra={<Text type="secondary" className="text-xs">最近接收: {formatDateTime(summary?.lastReceivedAt)}</Text>}
        >
          {summary ? (
            <div className="kc-overview-alert-stack">
              <div className="kc-overview-inline-banner">
                <div>
                  <Text strong>告警分布</Text>
                  <div className="kc-overview-inline-caption">
                    {summary.firingCount > 0 ? '当前仍有活跃告警，优先处置 Critical 和 Warning。' : '当前没有活跃告警，继续关注规则、通知和值班链路。'}
                  </div>
                </div>
                <Button type="text" icon={<EyeOutlined />} onClick={() => navigate('/monitoring-workbench/alerts')}>
                  查看活跃告警
                </Button>
              </div>
              <div className="kc-overview-chip-grid kc-monitoring-chip-grid">
                {alertChips.map((item) => (
                  <div key={item.key} className={`kc-overview-chip is-${item.tone}`}>
                    <span className="kc-overview-chip-label">{item.label}</span>
                    <span className="kc-overview-chip-value">{item.value}</span>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <Empty description="暂无告警摘要" />
          )}
        </Card>

        <Card className="kc-overview-panel-card" title="运行链路">
          <div className="kc-monitoring-operation-grid">
            {operationStats.map((item) => (
              <div key={item.key} className={`kc-overview-chip is-${item.tone}`}>
                <span className="kc-overview-metric-icon">{item.icon}</span>
                <span className="kc-overview-chip-label">{item.label}</span>
                <span className="kc-overview-chip-value">{item.value}</span>
                <span className="kc-overview-metric-helper">{item.helper}</span>
              </div>
            ))}
          </div>
        </Card>
      </div>

      <Card
        className="kc-overview-runtime-card"
        title="最近告警"
        extra={<Button type="text" icon={<EyeOutlined />} onClick={() => navigate('/monitoring-workbench/alerts')}>进入告警处理</Button>}
      >
        {alertsQuery.isLoading ? (
          <div className="kc-monitoring-alert-list">
            {[0, 1, 2].map((item) => <Card key={item} loading size="small" />)}
          </div>
        ) : recentAlerts.length === 0 ? (
          <Empty description="暂无最近告警" />
        ) : (
          <div className="kc-monitoring-alert-list">
            {recentAlerts.map((item) => (
              <div key={item.id} className="kc-overview-attention-row">
                <div className="kc-overview-attention-main">
                  <div className="kc-monitoring-alert-title-row">
                    <Text strong>{item.title || item.id}</Text>
                    <StatusTag value={item.severity} />
                    <StatusTag value={item.status} />
                  </div>
                  <div className="kc-overview-inline-caption">{item.summary || '-'}</div>
                </div>
                <div className="kc-overview-attention-meta">
                  <span>{[item.clusterId, item.namespace].filter(Boolean).join(' / ') || '-'}</span>
                  <span>{formatDateTime(item.lastSeenAt || item.startsAt)}</span>
                  <Button size="small" onClick={() => navigate(`/monitoring-workbench/alerts/${item.id}`)}>详情</Button>
                </div>
              </div>
            ))}
          </div>
        )}
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
          <Button
            size="small"
            type="primary"
            onClick={() => {
              const search = new URLSearchParams()
              search.set('alertId', record.id)
              search.set('timeRangeMinutes', '60')
              if (record.clusterId) search.set('clusterId', record.clusterId)
              if (record.namespace) search.set('namespace', record.namespace)
              navigate(getAIWorkbenchPathForMode('root_cause', search))
            }}
          >
            AI调查
          </Button>
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
      <AdminTable shellClassName="is-panel" columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} pageSize={20} />
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
  createdAt?: string
  updatedAt?: string
}

interface NotificationRoute {
  id: string
  name: string
  matchers?: Record<string, unknown>
  channelIds?: string[]
  enabled: boolean
  createdAt?: string
  updatedAt?: string
}

interface Silence {
  id: string
  name: string
  matchers?: Record<string, unknown>
  reason?: string
  startsAt: string
  endsAt: string
  enabled: boolean
  createdAt?: string
  updatedAt?: string
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
  const [channelForm] = Form.useForm<Record<string, unknown>>()
  const [routeForm] = Form.useForm<Record<string, unknown>>()
  const [silenceForm] = Form.useForm<Record<string, unknown>>()
  const [policyOpen, setPolicyOpen] = useState(false)
  const [templateOpen, setTemplateOpen] = useState(false)
  const [channelOpen, setChannelOpen] = useState(false)
  const [routeOpen, setRouteOpen] = useState(false)
  const [silenceOpen, setSilenceOpen] = useState(false)
  const [editingPolicy, setEditingPolicy] = useState<NotificationPolicy | null>(null)
  const [editingTemplate, setEditingTemplate] = useState<NotificationTemplate | null>(null)
  const [editingChannel, setEditingChannel] = useState<NotificationChannel | null>(null)
  const [editingRoute, setEditingRoute] = useState<NotificationRoute | null>(null)
  const [editingSilence, setEditingSilence] = useState<Silence | null>(null)
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
  const oncallSchedulesQuery = useQuery({
    queryKey: ['notification-oncall-schedules'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string; enabled: boolean }>>>('/oncall/schedules'),
  })
  const oncallPoliciesQuery = useQuery({
    queryKey: ['notification-oncall-policies'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string; enabled: boolean }>>>('/oncall/escalation-policies'),
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
  const createChannel = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post('/notification-channels', payload),
    onSuccess: () => {
      message.success('通知渠道已保存')
      queryClient.invalidateQueries({ queryKey: ['notification-channels'] })
      setChannelOpen(false)
      setEditingChannel(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateChannel = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/notification-channels/${id}`, payload),
    onSuccess: () => {
      message.success('通知渠道已更新')
      queryClient.invalidateQueries({ queryKey: ['notification-channels'] })
      setChannelOpen(false)
      setEditingChannel(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const createRoute = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post('/alert-routes', payload),
    onSuccess: () => {
      message.success('路由规则已保存')
      queryClient.invalidateQueries({ queryKey: ['notification-routes'] })
      queryClient.invalidateQueries({ queryKey: ['notification-policies'] })
      setRouteOpen(false)
      setEditingRoute(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateRoute = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/alert-routes/${id}`, payload),
    onSuccess: () => {
      message.success('路由规则已更新')
      queryClient.invalidateQueries({ queryKey: ['notification-routes'] })
      queryClient.invalidateQueries({ queryKey: ['notification-policies'] })
      setRouteOpen(false)
      setEditingRoute(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const createSilence = useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post('/alert-silences', payload),
    onSuccess: () => {
      message.success('静默规则已保存')
      queryClient.invalidateQueries({ queryKey: ['notification-silences'] })
      setSilenceOpen(false)
      setEditingSilence(null)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateSilence = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: Record<string, unknown> }) => api.put(`/alert-silences/${id}`, payload),
    onSuccess: () => {
      message.success('静默规则已更新')
      queryClient.invalidateQueries({ queryKey: ['notification-silences'] })
      setSilenceOpen(false)
      setEditingSilence(null)
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

  const oncallOptions = useMemo(() => [
    ...(oncallSchedulesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: `值班表 · ${item.name}` })),
    ...(oncallPoliciesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: `升级策略 · ${item.name}` })),
  ], [oncallPoliciesQuery.data?.data, oncallSchedulesQuery.data?.data])

  const channelOptions = useMemo(() => (channelsQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name })), [channelsQuery.data?.data])

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
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record: NotificationChannel) => canManageNotifications ? <Button size="small" onClick={() => openChannelEditor(record)}>编辑</Button> : null,
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
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record: NotificationRoute) => canManageNotifications ? <Button size="small" onClick={() => openRouteEditor(record)}>编辑</Button> : null,
    },
  ]

  const silenceColumns: ColumnsType<Silence> = [
    { title: '名称', dataIndex: 'name' },
    { title: '匹配器', dataIndex: 'matchers', render: (value: Record<string, unknown>) => <Text code>{shortJson(value)}</Text> },
    { title: '原因', dataIndex: 'reason', render: (value: string) => value || '-' },
    { ...tableColumnPresets.datetime, title: '开始时间', dataIndex: 'startsAt', render: (value: string) => formatDateTime(value) },
    { ...tableColumnPresets.datetime, title: '结束时间', dataIndex: 'endsAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'enabled',
      render: (_: boolean, record: Silence) => <StatusTag value={formatSilenceStatus(record)} />,
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record: Silence) => canManageNotifications ? <Button size="small" onClick={() => openSilenceEditor(record)}>编辑</Button> : null,
    },
  ]

  const policyColumns: ColumnsType<NotificationPolicy> = [
    { title: '名称', dataIndex: 'name' },
    { title: '处理链', dataIndex: 'processorChain', render: (value: string[]) => <Space wrap>{(value ?? []).map((item) => <Tag key={item}>{item}</Tag>)}</Space> },
    { title: '渠道', dataIndex: 'channelRefs', render: (value: string[]) => <Space wrap>{(value ?? []).map((item) => <Tag key={item}>{channelNamesById[item] || item}</Tag>)}</Space> },
    { title: 'OnCall', dataIndex: 'oncallRef', render: (value: string) => oncallOptions.find((item) => item.value === value)?.label || value || '-' },
    { title: '恢复通知', dataIndex: 'sendResolved', render: (value: boolean) => <BooleanTag value={value} trueLabel="发送" falseLabel="不发送" /> },
    { title: '冷却(s)', dataIndex: 'cooldownSeconds' },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" /> },
    {
      ...tableColumnPresets.action,
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
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record: NotificationTemplate) => canManageNotifications ? <Button size="small" onClick={() => openTemplateEditor(record)}>编辑</Button> : null,
    },
  ]

  function openPolicyEditor(record: NotificationPolicy | null) {
    setEditingPolicy(record)
    setPolicyOpen(true)
    policyForm.setFieldsValue(record ? {
      ...record,
      matchers: JSON.stringify(record.matchers ?? {}, null, 2),
      processorChain: record.processorChain ?? [],
      channelRefs: record.channelRefs ?? [],
    } : {
      name: '',
      matchers: '{}',
      processorChain: ['template_render', 'webhook_update'],
      channelRefs: [],
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

  function openChannelEditor(record: NotificationChannel | null) {
    setEditingChannel(record)
    setChannelOpen(true)
    channelForm.setFieldsValue(record ? {
      ...record,
      config: prettyJson(record.config ?? {}),
    } : {
      name: '',
      channelType: 'webhook',
      config: '{\n  "url": "https://example.com/webhook"\n}',
      enabled: true,
    })
  }

  function openRouteEditor(record: NotificationRoute | null) {
    setEditingRoute(record)
    setRouteOpen(true)
    routeForm.setFieldsValue(record ? {
      ...record,
      matchers: prettyJson(record.matchers ?? {}),
      channelIds: record.channelIds ?? [],
    } : {
      name: '',
      matchers: '{\n  "severity": "critical"\n}',
      channelIds: [],
      enabled: true,
    })
  }

  function openSilenceEditor(record: Silence | null) {
    setEditingSilence(record)
    setSilenceOpen(true)
    const now = Date.now()
    silenceForm.setFieldsValue(record ? {
      ...record,
      matchers: prettyJson(record.matchers ?? {}),
    } : {
      name: '',
      matchers: '{\n  "severity": "warning"\n}',
      reason: '',
      startsAt: new Date(now).toISOString(),
      endsAt: new Date(now + 60 * 60 * 1000).toISOString(),
      enabled: true,
    })
  }

  function submitPolicy(values: Record<string, unknown>) {
    try {
      const payload = {
        name: values.name,
        matchers: safeParseJson(String(values.matchers || '{}'), {}),
        processorChain: splitList(values.processorChain),
        channelRefs: splitList(values.channelRefs),
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
        headers: safeParseJson(String(values.headers || '{}'), {}),
        queryParams: safeParseJson(String(values.queryParams || '{}'), {}),
        samplePayload: safeParseJson(String(values.samplePayload || '{}'), {}),
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

  function submitChannel(values: Record<string, unknown>) {
    try {
      const payload = {
        name: values.name,
        channelType: values.channelType,
        config: safeParseJson(String(values.config || '{}'), {}),
        enabled: Boolean(values.enabled),
      }
      if (editingChannel?.id) {
        updateChannel.mutate({ id: editingChannel.id, payload })
        return
      }
      createChannel.mutate(payload)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  function submitRoute(values: Record<string, unknown>) {
    try {
      const payload = {
        name: values.name,
        matchers: safeParseJson(String(values.matchers || '{}'), {}),
        channelIds: splitList(values.channelIds),
        enabled: Boolean(values.enabled),
      }
      if (editingRoute?.id) {
        updateRoute.mutate({ id: editingRoute.id, payload })
        return
      }
      createRoute.mutate(payload)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  function submitSilence(values: Record<string, unknown>) {
    try {
      const payload = {
        name: values.name,
        matchers: safeParseJson(String(values.matchers || '{}'), {}),
        reason: values.reason,
        startsAt: parseIsoTime(values.startsAt, '开始时间'),
        endsAt: parseIsoTime(values.endsAt, '结束时间'),
        enabled: Boolean(values.enabled),
      }
      if (editingSilence?.id) {
        updateSilence.mutate({ id: editingSilence.id, payload })
        return
      }
      createSilence.mutate(payload)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  return (
    <div className="kc-page">
      <PageHeader
        title="通知策略"
        description="维护通知策略、模板、渠道、路由规则与静默策略。"
        actions={canManageNotifications ? (
          <Space>
            <Button icon={<PlusOutlined />} onClick={() => openSilenceEditor(null)}>新建静默</Button>
            <Button icon={<PlusOutlined />} onClick={() => openChannelEditor(null)}>新建渠道</Button>
            <Button icon={<PlusOutlined />} onClick={() => openTemplateEditor(null)}>新建模板</Button>
            <Button icon={<PlusOutlined />} type="primary" onClick={() => openPolicyEditor(null)}>新建策略</Button>
          </Space>
        ) : null}
      />
      <Tabs
        items={[
          {
            key: 'policies',
            label: '通知策略',
            children: <AdminTable shellClassName="is-panel" columns={policyColumns} dataSource={policiesQuery.data?.data ?? []} rowKey="id" loading={policiesQuery.isLoading} />,
          },
          {
            key: 'templates',
            label: '通知模板',
            children: <AdminTable shellClassName="is-panel" columns={templateColumns} dataSource={templatesQuery.data?.data ?? []} rowKey="id" loading={templatesQuery.isLoading} />,
          },
          {
            key: 'channels',
            label: '通知渠道',
            children: <AdminTable shellClassName="is-panel" columns={channelColumns} dataSource={channelsQuery.data?.data ?? []} rowKey="id" loading={channelsQuery.isLoading} />,
          },
          {
            key: 'routes',
            label: '路由规则',
            children: (
              <AdminTable
                shellClassName="is-panel"
                columns={routeColumns}
                dataSource={routesQuery.data?.data ?? []}
                rowKey="id"
                loading={routesQuery.isLoading}
                headerExtra={<Space><Text data-testid="notification-route-compat-note" type="secondary">兼容 `/alert-routes`，保存后同步到通知策略。</Text>{canManageNotifications ? <Button size="small" icon={<PlusOutlined />} onClick={() => openRouteEditor(null)}>新建路由</Button> : null}</Space>}
              />
            ),
          },
          {
            key: 'silences',
            label: '静默规则',
            children: <AdminTable shellClassName="is-panel" columns={silenceColumns} dataSource={silencesQuery.data?.data ?? []} rowKey="id" loading={silencesQuery.isLoading} />,
          },
        ]}
      />
      <Modal title={editingPolicy ? '编辑通知策略' : '新建通知策略'} open={policyOpen} onCancel={() => setPolicyOpen(false)} footer={null} destroyOnHidden width={760}>
        <Form layout="vertical" form={policyForm} onFinish={submitPolicy} initialValues={{ sendResolved: false, cooldownSeconds: 0, enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="matchers" label="匹配器(JSON)"><Input.TextArea rows={4} /></Form.Item>
          <Form.Item name="processorChain" label="处理链"><Select mode="tags" options={[{ value: 'template_render', label: 'template_render' }, { value: 'webhook_update', label: 'webhook_update' }]} /></Form.Item>
          <Form.Item name="channelRefs" label="渠道引用"><Select mode="multiple" allowClear options={channelOptions} /></Form.Item>
          <Form.Item name="oncallRef" label="OnCall 引用"><Select allowClear options={oncallOptions} /></Form.Item>
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
      <Modal title={editingChannel ? '编辑通知渠道' : '新建通知渠道'} open={channelOpen} onCancel={() => setChannelOpen(false)} footer={null} destroyOnHidden width={760}>
        <Form layout="vertical" form={channelForm} onFinish={submitChannel} initialValues={{ channelType: 'webhook', enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="channelType" label="渠道类型" rules={[{ required: true }]}>
            <Select options={[
              { value: 'webhook', label: 'webhook' },
              { value: 'slack', label: 'slack' },
              { value: 'feishu', label: 'feishu' },
              { value: 'dingtalk', label: 'dingtalk' },
              { value: 'wechat', label: 'wechat' },
            ]} />
          </Form.Item>
          <Form.Item name="config" label="配置(JSON)" rules={[{ required: true }]}><Input.TextArea rows={7} /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Space><Button type="primary" htmlType="submit" loading={createChannel.isPending || updateChannel.isPending}>保存</Button><Button onClick={() => setChannelOpen(false)}>取消</Button></Space>
        </Form>
      </Modal>
      <Modal title={editingRoute ? '编辑路由规则' : '新建路由规则'} open={routeOpen} onCancel={() => setRouteOpen(false)} footer={null} destroyOnHidden width={760}>
        <Form layout="vertical" form={routeForm} onFinish={submitRoute} initialValues={{ enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="matchers" label="匹配器(JSON)"><Input.TextArea rows={5} /></Form.Item>
          <Form.Item name="channelIds" label="接收渠道"><Select mode="multiple" allowClear options={channelOptions} /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Space><Button type="primary" htmlType="submit" loading={createRoute.isPending || updateRoute.isPending}>保存</Button><Button onClick={() => setRouteOpen(false)}>取消</Button></Space>
        </Form>
      </Modal>
      <Modal title={editingSilence ? '编辑静默规则' : '新建静默规则'} open={silenceOpen} onCancel={() => setSilenceOpen(false)} footer={null} destroyOnHidden width={760}>
        <Form layout="vertical" form={silenceForm} onFinish={submitSilence} initialValues={{ enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="matchers" label="匹配器(JSON)" rules={[{ required: true }]}><Input.TextArea rows={5} /></Form.Item>
          <Form.Item name="reason" label="静默原因"><Input.TextArea rows={3} /></Form.Item>
          <Space size={16} style={{ width: '100%' }}>
            <Form.Item name="startsAt" label="开始时间(ISO)" rules={[{ required: true }]} style={{ flex: 1 }}><Input /></Form.Item>
            <Form.Item name="endsAt" label="结束时间(ISO)" rules={[{ required: true }]} style={{ flex: 1 }}><Input /></Form.Item>
          </Space>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          <Space><Button type="primary" htmlType="submit" loading={createSilence.isPending || updateSilence.isPending}>保存</Button><Button onClick={() => setSilenceOpen(false)}>取消</Button></Space>
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
        <Space orientation="vertical" style={{ width: '100%' }} size={16}>
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
      <AdminTable shellClassName="is-panel" columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} pageSize={50} />
    </div>
  )
}
