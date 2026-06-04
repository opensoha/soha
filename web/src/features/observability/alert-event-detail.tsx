import { useState } from 'react'
import { App, Button, Card, Descriptions, Drawer, Modal, Select, Space, Tabs, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { ManagementDetailHeader, ManagementState } from '@/components/management-list'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { getAIWorkbenchPathForMode } from '@/features/copilot/workbench-navigation'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import { formatDateTime } from '@/utils/time'

const { Paragraph } = Typography

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

interface HealingPolicy {
  id: string
  name: string
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

interface AlertEvent {
  id: string
  ruleId?: string
  sourceType: string
  sourceSystem?: string
  fingerprint: string
  title: string
  summary: string
  severity: string
  status: string
  clusterId?: string
  namespace?: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  receiver?: string
  generatorUrl?: string
  currentState?: string
  lastNotificationAt?: string
  startsAt?: string
  endsAt?: string
  lastSeenAt?: string
  createdAt: string
  updatedAt: string
}

function eventDisplayStatus(event?: AlertEvent) {
  return event?.currentState || event?.status || ''
}

interface AlertDeliveryLog {
  id: string
  alertId: string
  channelId?: string
  status: string
  summary?: string
  metadata?: Record<string, unknown>
  createdAt: string
}

interface AlertRuleRunRecord {
  id: string
  status: string
  matched: boolean
  summary?: string
  durationMs: number
  error?: string
  createdAt: string
}

function stringifyPayload(payload: unknown) {
  return JSON.stringify(payload ?? {}, null, 2)
}

function useAlertEventDetailController(eventId: string, enabled: boolean) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canAcknowledge = hasPermission(permissionSnapshotQuery.data?.data, 'observe.alerts.ack')
  const canManageAlerts = hasPermission(permissionSnapshotQuery.data?.data, 'observe.alerts.manage')
  const canHeal = hasPermission(permissionSnapshotQuery.data?.data, 'observe.healing.manage')
  const [healOpen, setHealOpen] = useState(false)
  const [healingPolicyId, setHealingPolicyId] = useState<string>('')

  const eventQuery = useQuery({
    queryKey: ['alert-event-detail', eventId],
    queryFn: () => api.get<ApiResponse<AlertEvent>>(`/alert-events/${eventId}`),
    enabled: enabled && eventId !== '',
  })
  const ruleQuery = useQuery({
    queryKey: ['alert-event-detail-rule', eventQuery.data?.data?.ruleId ?? ''],
    queryFn: () => api.get<ApiResponse<AlertRule>>(`/alert-rules/${eventQuery.data?.data?.ruleId}`),
    enabled: enabled && Boolean(eventQuery.data?.data?.ruleId),
  })
  const ruleRunsQuery = useQuery({
    queryKey: ['alert-event-detail-runs', eventQuery.data?.data?.ruleId ?? ''],
    queryFn: () => api.get<ApiResponse<AlertRuleRunRecord[]>>(`/alert-rule-runs?ruleId=${encodeURIComponent(eventQuery.data?.data?.ruleId ?? '')}`),
    enabled: enabled && Boolean(eventQuery.data?.data?.ruleId),
  })
  const healingRunsQuery = useQuery({
    queryKey: ['alert-event-detail-healing-runs', eventId],
    queryFn: () => api.get<ApiResponse<HealingRun[]>>(`/healing-runs?eventId=${encodeURIComponent(eventId)}`),
    enabled: enabled && eventId !== '',
  })
  const previewQuery = useQuery({
    queryKey: ['alert-event-detail-preview', eventId, ruleQuery.data?.data?.notificationPolicyId ?? ''],
    queryFn: () => api.get<ApiResponse<Array<Record<string, unknown>>>>(`/notification-policies/${encodeURIComponent(ruleQuery.data?.data?.notificationPolicyId ?? '')}/preview?eventId=${encodeURIComponent(eventId)}`),
    enabled: enabled && Boolean(eventId && ruleQuery.data?.data?.notificationPolicyId),
  })
  const deliveryLogsQuery = useQuery({
    queryKey: ['alert-event-detail-delivery-logs', eventId],
    queryFn: () => api.get<ApiResponse<AlertDeliveryLog[]>>(`/alert-delivery-logs?alertId=${encodeURIComponent(eventId)}`),
    enabled: enabled && eventId !== '',
  })
  const healingPoliciesQuery = useQuery({
    queryKey: ['alert-event-detail-healing-policies'],
    queryFn: () => api.get<ApiResponse<HealingPolicy[]>>('/healing-policies'),
    enabled,
  })

  const acknowledgeMutation = useMutation({
    mutationFn: () => api.post(`/alert-events/${eventId}/acknowledge`),
    onSuccess: () => {
      void message.success('告警已确认')
      void queryClient.invalidateQueries({ queryKey: ['alert-event-detail', eventId] })
      void queryClient.invalidateQueries({ queryKey: ['alert-events'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const resolveMutation = useMutation({
    mutationFn: () => api.post(`/alert-events/${eventId}/resolve`),
    onSuccess: () => {
      void message.success('告警已恢复')
      void queryClient.invalidateQueries({ queryKey: ['alert-event-detail', eventId] })
      void queryClient.invalidateQueries({ queryKey: ['alert-events'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const healMutation = useMutation({
    mutationFn: (policyId: string) => api.post(`/alert-events/${eventId}/heal?policyId=${encodeURIComponent(policyId)}`),
    onSuccess: () => {
      void message.success('自愈运行已创建')
      void queryClient.invalidateQueries({ queryKey: ['alert-event-detail-healing-runs', eventId] })
      void queryClient.invalidateQueries({ queryKey: ['healing-runs'] })
      setHealOpen(false)
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const event = eventQuery.data?.data
  const rule = ruleQuery.data?.data
  const previewRows = previewQuery.data?.data ?? []
  const deliveryLogs = deliveryLogsQuery.data?.data ?? []

  function openHealModal() {
    setHealingPolicyId(rule?.healingPolicyIds?.[0] || '')
    setHealOpen(true)
  }

  return {
    acknowledgeMutation,
    canAcknowledge,
    canHeal,
    canManageAlerts,
    deliveryLogs,
    deliveryLogsQuery,
    event,
    eventQuery,
    healMutation,
    healOpen,
    healingPoliciesQuery,
    healingPolicyId,
    healingRunsQuery,
    openHealModal,
    previewQuery,
    previewRows,
    resolveMutation,
    rule,
    ruleQuery,
    ruleRunsQuery,
    setHealOpen,
    setHealingPolicyId,
  }
}

type AlertEventDetailController = ReturnType<typeof useAlertEventDetailController>

function AlertEventDetailBody({ detail, inDrawer = false }: { detail: AlertEventDetailController; inDrawer?: boolean }) {
  const {
    deliveryLogs,
    deliveryLogsQuery,
    event,
    eventQuery,
    healMutation,
    healOpen,
    healingPoliciesQuery,
    healingPolicyId,
    healingRunsQuery,
    previewQuery,
    previewRows,
    rule,
    ruleRunsQuery,
    setHealOpen,
    setHealingPolicyId,
  } = detail

  return (
    <div data-testid={inDrawer ? 'alert-event-drawer-content' : 'alert-event-page-content'}>
      <Card title="事件摘要" loading={eventQuery.isLoading}>
        <Descriptions bordered size="small" column={inDrawer ? 1 : 2}>
          <Descriptions.Item label="事件ID">{event?.id || '-'}</Descriptions.Item>
          <Descriptions.Item label="状态">{event ? <StatusTag value={event.status} /> : '-'}</Descriptions.Item>
          <Descriptions.Item label="来源">{event?.sourceSystem || event?.sourceType || '-'}</Descriptions.Item>
          <Descriptions.Item label="严重度">{event ? <StatusTag value={event.severity} /> : '-'}</Descriptions.Item>
          <Descriptions.Item label="范围">{[event?.clusterId, event?.namespace].filter(Boolean).join(' / ') || '-'}</Descriptions.Item>
          <Descriptions.Item label="最近命中">{formatDateTime(event?.lastSeenAt)}</Descriptions.Item>
          <Descriptions.Item label="规则ID">{event?.ruleId || '-'}</Descriptions.Item>
          <Descriptions.Item label="当前态">{event?.currentState || '-'}</Descriptions.Item>
          <Descriptions.Item label="接收器">{event?.receiver || '-'}</Descriptions.Item>
          <Descriptions.Item label="最近通知">{formatDateTime(event?.lastNotificationAt)}</Descriptions.Item>
          <Descriptions.Item label="触发时间">{formatDateTime(event?.startsAt)}</Descriptions.Item>
          <Descriptions.Item label="恢复时间">{formatDateTime(event?.endsAt)}</Descriptions.Item>
          <Descriptions.Item label="标题" span={inDrawer ? 1 : 2}>{event?.title || '-'}</Descriptions.Item>
          <Descriptions.Item label="摘要" span={inDrawer ? 1 : 2}>{event?.summary || '-'}</Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title="规则与通知上下文" style={{ marginTop: 16 }}>
        <Descriptions bordered size="small" column={inDrawer ? 1 : 2}>
          <Descriptions.Item label="规则名称">{rule?.name || '-'}</Descriptions.Item>
          <Descriptions.Item label="规则类型">{rule?.ruleType || '-'}</Descriptions.Item>
          <Descriptions.Item label="通知策略">{rule?.notificationPolicyId || '-'}</Descriptions.Item>
          <Descriptions.Item label="自愈策略">{rule?.healingPolicyIds?.length ? rule.healingPolicyIds.join(', ') : '-'}</Descriptions.Item>
          <Descriptions.Item label="聚合维度" span={inDrawer ? 1 : 2}>{rule?.groupBy?.length ? rule.groupBy.join(', ') : '-'}</Descriptions.Item>
          <Descriptions.Item label="生成链接" span={inDrawer ? 1 : 2}>
            {event?.generatorUrl ? <a href={event.generatorUrl} target="_blank" rel="noreferrer">{event.generatorUrl}</a> : '-'}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Tabs
        style={{ marginTop: 16 }}
        items={[
          {
            key: 'runs',
            label: '规则运行',
            children: (
              <AdminTable
                shellClassName="soha-management-table-shell"
                rowKey="id"
                loading={ruleRunsQuery.isLoading}
                dataSource={ruleRunsQuery.data?.data ?? []}
                pagination={{ pageSize: 10 }}
                columns={[
                  { title: '运行ID', dataIndex: 'id' },
                  { title: '状态', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
                  { title: '命中', dataIndex: 'matched', render: (value: boolean) => <BooleanTag value={value} trueLabel="命中" falseLabel="未命中" /> },
                  { title: '耗时(ms)', dataIndex: 'durationMs' },
                  { title: '摘要', dataIndex: 'summary', ellipsis: true },
                  { title: '错误', dataIndex: 'error', render: (value: string) => value || '-' },
                  { title: '时间', dataIndex: 'createdAt', render: (value: string) => formatDateTime(value) },
                ]}
              />
            ),
          },
          {
            key: 'healing',
            label: '自愈运行',
            children: (
              <AdminTable
                shellClassName="soha-management-table-shell"
                rowKey="id"
                loading={healingRunsQuery.isLoading}
                dataSource={healingRunsQuery.data?.data ?? []}
                pagination={{ pageSize: 10 }}
                columns={[
                  { title: '运行ID', dataIndex: 'id' },
                  { title: '状态', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
                  { title: '审批', dataIndex: 'approvalStatus', render: (value: string) => value ? <StatusTag value={value} /> : '-' },
                  { title: 'Workflow', dataIndex: 'workflowStatus', render: (value: string, record: HealingRun) => value ? <StatusTag value={value} /> : record.workflowRunId || '-' },
                  { title: '摘要', dataIndex: 'workflowSummary', ellipsis: true, render: (value: string) => value || '-' },
                  { title: '审批人', dataIndex: 'approvedBy', render: (value: string) => value || '-' },
                  { title: '创建时间', dataIndex: 'createdAt', render: (value: string) => formatDateTime(value) },
                ]}
              />
            ),
          },
          {
            key: 'preview',
            label: '通知预览',
            children: (
              <AdminTable
                shellClassName="soha-management-table-shell"
                rowKey={(record) => `${String(record.channelId || 'channel')}:${String(record.templateId || 'template')}:${String(record.url || 'url')}`}
                loading={previewQuery.isLoading}
                dataSource={previewRows}
                pagination={false}
                columns={[
                  { title: '渠道', dataIndex: 'channelId', render: (value: string) => value || '-' },
                  { title: '模板', dataIndex: 'templateId', render: (value: string) => value || '-' },
                  { title: 'URL', dataIndex: 'url', ellipsis: true },
                  { title: 'Method', dataIndex: 'method', render: (value: string) => value || '-' },
                  { title: 'Content-Type', dataIndex: 'contentType', render: (value: string) => value || '-' },
                  { title: 'Body', dataIndex: 'body', render: (value: string) => <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{String(value || '')}</pre> },
                ]}
              />
            ),
          },
          {
            key: 'delivery',
            label: '投递日志',
            children: (
              <AdminTable
                shellClassName="soha-management-table-shell"
                rowKey="id"
                loading={deliveryLogsQuery.isLoading}
                dataSource={deliveryLogs}
                pagination={{ pageSize: 10 }}
                columns={[
                  { title: '时间', dataIndex: 'createdAt', render: (value: string) => formatDateTime(value) },
                  { title: '渠道', dataIndex: 'channelId', render: (value: string) => value || '-' },
                  { title: '状态', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
                  { title: '摘要', dataIndex: 'summary', render: (value: string) => value || '-' },
                  { title: '元数据', dataIndex: 'metadata', render: (value: Record<string, unknown>) => <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{stringifyPayload(value)}</pre> },
                ]}
              />
            ),
          },
          {
            key: 'raw',
            label: '原始载荷',
            children: (
              <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                <Card size="small" title="Labels">
                  <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{stringifyPayload(event?.labels)}</pre>
                </Card>
                <Card size="small" title="Annotations">
                  <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{stringifyPayload(event?.annotations)}</pre>
                </Card>
                <Card size="small" title="Rule JSON">
                  <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{stringifyPayload(rule)}</pre>
                </Card>
                <Card size="small" title="Event JSON">
                  <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{stringifyPayload(event)}</pre>
                </Card>
              </Space>
            ),
          },
        ]}
      />

      <Modal
        title="发起自愈"
        open={healOpen}
        onCancel={() => setHealOpen(false)}
        onOk={() => healMutation.mutate(healingPolicyId)}
        okButtonProps={{ disabled: !healingPolicyId, loading: healMutation.isPending }}
        destroyOnHidden
      >
        <Select
          style={{ width: '100%' }}
          placeholder="选择自愈策略"
          value={healingPolicyId}
          onChange={(value) => setHealingPolicyId(String(value))}
          options={(healingPoliciesQuery.data?.data ?? []).filter((item) => item.enabled && rule?.healingPolicyIds?.includes(item.id)).map((item) => ({ value: item.id, label: item.name }))}
        />
      </Modal>
    </div>
  )
}

function AlertEventDetailActions({
  detail,
  extra,
}: {
  detail: AlertEventDetailController
  extra?: React.ReactNode
}) {
  const { acknowledgeMutation, canAcknowledge, canHeal, canManageAlerts, event, openHealModal, resolveMutation, rule } = detail
  const navigate = useNavigate()
  const aiWorkbenchSearch = new URLSearchParams()
  aiWorkbenchSearch.set('timeRangeMinutes', '60')
  if (event) {
    aiWorkbenchSearch.set('alertId', event.id)
    if (event.clusterId) aiWorkbenchSearch.set('clusterId', event.clusterId)
    if (event.namespace) aiWorkbenchSearch.set('namespace', event.namespace)
    const workload = event.labels?.workload || event.labels?.deployment || event.labels?.app || event.labels?.service
    if (workload) aiWorkbenchSearch.set('workload', workload)
  }
  const aiWorkbenchPath = getAIWorkbenchPathForMode('root_cause', aiWorkbenchSearch)

  return (
    <Space wrap>
      {extra}
      <Button onClick={() => navigate(aiWorkbenchPath)}>AI 调查</Button>
      {canAcknowledge && eventDisplayStatus(event) !== 'acknowledged' ? <Button loading={acknowledgeMutation.isPending} onClick={() => acknowledgeMutation.mutate()}>确认</Button> : null}
      {canManageAlerts && eventDisplayStatus(event) !== 'resolved' ? <Button loading={resolveMutation.isPending} onClick={() => resolveMutation.mutate()}>恢复</Button> : null}
      {canHeal && (rule?.healingPolicyIds?.length ?? 0) > 0 ? <Button type="primary" onClick={openHealModal}>发起自愈</Button> : null}
    </Space>
  )
}

export function AlertEventDetailPageContent({
  eventId,
  onBack,
}: {
  eventId: string
  onBack: () => void
}) {
  const detail = useAlertEventDetailController(eventId, eventId !== '')

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title={detail.event?.title || '告警事件详情'}
        description={detail.event ? `${detail.event.sourceSystem || detail.event.sourceType} · ${detail.event.status}` : '查看告警事件、规则运行、自愈与通知链路'}
        actions={(
          <AlertEventDetailActions
            detail={detail}
            extra={<Button onClick={onBack}>返回列表</Button>}
          />
        )}
      />
      <AlertEventDetailBody detail={detail} />
    </div>
  )
}

export function AlertEventDetailDrawer({
  eventId,
  open,
  onClose,
  onOpenStandalone,
}: {
  eventId: string
  open: boolean
  onClose: () => void
  onOpenStandalone: (eventId: string) => void
}) {
  const detail = useAlertEventDetailController(eventId, open && eventId !== '')

  return (
    <Drawer
      data-testid="alert-event-drawer"
      open={open}
      onClose={onClose}
      destroyOnHidden
      size="large"
      title={detail.event?.title || '告警事件详情'}
      extra={(
        <AlertEventDetailActions
          detail={detail}
          extra={<Button onClick={() => onOpenStandalone(eventId)}>在详情页打开</Button>}
        />
      )}
    >
      {eventId ? (
        <>
          <Paragraph type="secondary" style={{ marginTop: 0 }}>
            {detail.event ? `${detail.event.sourceSystem || detail.event.sourceType} · ${detail.event.status}` : '查看告警事件、规则运行、自愈与通知链路'}
          </Paragraph>
          <AlertEventDetailBody detail={detail} inDrawer />
        </>
      ) : <ManagementState bordered={false} compact kind="select-scope" description="请选择一条告警事件。" />}
    </Drawer>
  )
}
