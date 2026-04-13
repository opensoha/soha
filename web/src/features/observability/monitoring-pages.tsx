import { Card, Tag, Tabs, TabPane, Button, Toast, Typography } from '@douyinfe/semi-ui'
import { IconPulse, IconAlertTriangle } from '@douyinfe/semi-icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { StatGrid } from '@/components/stat-grid'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse } from '@/types'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'

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
        { label: '总告警数', value: summary.totalCount, icon: <IconPulse size="extra-large" /> },
        { label: '活跃告警', value: summary.firingCount, icon: <IconAlertTriangle size="extra-large" /> },
        { label: '已恢复', value: summary.resolvedCount, icon: <IconPulse size="extra-large" /> },
        { label: '严重告警', value: summary.criticalCount, icon: <IconAlertTriangle size="extra-large" /> },
        { label: '通知渠道', value: summary.channelCount, icon: <IconPulse size="extra-large" /> },
      ]
    : []

  return (
    <div className="kc-page">
      <PageHeader title="告警中心概览" description="查看告警压力、通知覆盖度与整体运行态势。" />

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
            <IconPulse size="extra-large" />
            <Paragraph type="tertiary" className="mt-2">
              Critical {summary?.criticalCount ?? 0} / Warning {summary?.warningCount ?? 0} / Info {summary?.infoCount ?? 0}
            </Paragraph>
            <Paragraph type="tertiary">
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
  name: string
  severity: string
  status: string
  source: string
  owner: string
  message: string
  startsAt: string
}

export function AlertsPage() {
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['alerts'],
    queryFn: () => api.get<ApiResponse<Alert[]>>('/alerts'),
  })

  const ackMutation = useMutation({
    mutationFn: (id: string) => api.post(`/alerts/${id}/acknowledge`),
    onSuccess: () => {
      Toast.success('告警已确认')
      queryClient.invalidateQueries({ queryKey: ['alerts'] })
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  const columns: ColumnProps<Alert>[] = [
    { title: '名称', dataIndex: 'name' },
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
    { title: '来源', dataIndex: 'source' },
    { title: '负责人', dataIndex: 'owner' },
    { title: '消息', dataIndex: 'message', ellipsis: true },
    { ...tableColumnPresets.datetime, title: '触发时间', dataIndex: 'startsAt', render: (value: string) => formatDateTime(value) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: Alert) =>
        record.status !== 'acknowledged' ? (
          <Button size="small" theme="borderless" onClick={() => ackMutation.mutate(record.id)}>
            确认
          </Button>
        ) : (
          <Text type="tertiary" size="small">已确认</Text>
        ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader title="活跃告警" description="查看当前告警严重程度、负责人、来源以及确认状态。" />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="id" loading={isLoading} pageSize={20} />
    </div>
  )
}

/* ─── Notifications ─── */

interface NotificationChannel {
  id: string
  name: string
  type: string
  endpoint: string
  enabled: boolean
}

interface NotificationRoute {
  id: string
  name: string
  matcher: string
  receiver: string
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

export function NotificationsPage() {
  const channelsQuery = useQuery({
    queryKey: ['notification-channels'],
    queryFn: () => api.get<ApiResponse<NotificationChannel[]>>('/notification-channels'),
  })
  const routesQuery = useQuery({
    queryKey: ['notification-routes'],
    queryFn: () => api.get<ApiResponse<NotificationRoute[]>>('/alert-routes'),
  })
  const silencesQuery = useQuery({
    queryKey: ['notification-silences'],
    queryFn: () => api.get<ApiResponse<Silence[]>>('/alert-silences'),
  })

  const channelColumns: ColumnProps<NotificationChannel>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: '类型', dataIndex: 'type', render: (t: string) => <Tag>{t}</Tag> },
    { title: 'Endpoint', dataIndex: 'endpoint', ellipsis: true },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'enabled',
      render: (v: boolean) => <BooleanTag value={v} trueLabel="启用" falseLabel="禁用" />,
    },
  ]

  const routeColumns: ColumnProps<NotificationRoute>[] = [
    { title: '名称', dataIndex: 'name' },
    { title: '匹配规则', dataIndex: 'matcher' },
    { title: '接收器', dataIndex: 'receiver' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'enabled',
      render: (v: boolean) => <BooleanTag value={v} trueLabel="启用" falseLabel="禁用" />,
    },
  ]

  const silenceColumns: ColumnProps<Silence>[] = [
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

  return (
    <div className="kc-page">
      <PageHeader title="通知策略" description="维护通知渠道、路由规则与静默策略。" />
      <Tabs type="line">
        <TabPane tab="通知渠道" itemKey="channels">
          <AdminTable columns={channelColumns} dataSource={channelsQuery.data?.data ?? []} rowKey="id" loading={channelsQuery.isLoading} />
        </TabPane>
        <TabPane tab="路由规则" itemKey="routes">
          <AdminTable columns={routeColumns} dataSource={routesQuery.data?.data ?? []} rowKey="id" loading={routesQuery.isLoading} />
        </TabPane>
        <TabPane tab="静默规则" itemKey="silences">
          <AdminTable columns={silenceColumns} dataSource={silencesQuery.data?.data ?? []} rowKey="id" loading={silencesQuery.isLoading} />
        </TabPane>
      </Tabs>
    </div>
  )
}

/* ─── Events ─── */

interface K8sEvent {
  id: string
  timestamp: string
  type: string
  reason: string
  source: string
  object: string
  message: string
}

export function EventsPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['events'],
    queryFn: () => api.get<ApiResponse<K8sEvent[]>>('/events'),
  })

  const columns: ColumnProps<K8sEvent>[] = [
    { ...tableColumnPresets.datetime, title: '时间', dataIndex: 'timestamp', render: (value: string) => formatDateTime(value) },
    { ...tableColumnPresets.status, title: '类型', dataIndex: 'type', render: (t: string) => <StatusTag value={t} /> },
    { title: 'Reason', dataIndex: 'reason', width: 140 },
    { title: '来源', dataIndex: 'source', width: 140 },
    { title: '对象', dataIndex: 'object', width: 200 },
    { title: '消息', dataIndex: 'message', ellipsis: true },
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
          <IconAlertTriangle size="extra-large" />
          <Title heading={5} type="tertiary" style={{ marginTop: 16 }}>值班协同</Title>
          <Paragraph type="tertiary">
            配置值班轮换计划、升级策略和通知规则。该功能即将上线。
          </Paragraph>
        </div>
      </Card>
    </div>
  )
}
