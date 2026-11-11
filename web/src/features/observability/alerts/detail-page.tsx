import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { getAIWorkbenchPathForMode, useAIPageContext } from '@/features/copilot'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import {
  ApartmentOutlined,
  DashboardOutlined,
  FileSearchOutlined,
  LineChartOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Descriptions, Drawer, Modal, Select, Space, Tabs, Typography } from 'antd'
import { useEffect, useState, type ReactNode } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import '../observability-pages.css'
import { observabilityRuleQueries } from '../rules'
import { alertDiagnosticPaths, alertDisplayStatus, stringifyAlertPayload } from './model'
import { observabilityAlertMutations } from './mutations'
import { observabilityAlertQueries } from './queries'
import type { AlertDeliveryMetadata, HealingRun } from './types'

const { Paragraph } = Typography

function useAlertEventDetailController(eventId: string, enabled: boolean) {
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canAcknowledge = hasPermission(permissionSnapshotQuery.data?.data, 'observe.alerts.ack')
  const canResolve = hasPermission(permissionSnapshotQuery.data?.data, 'observe.alerts.update')
  const canHeal = hasPermission(permissionSnapshotQuery.data?.data, 'observe.healing.heal')
  const [healOpen, setHealOpen] = useState(false)
  const [healingPolicyId, setHealingPolicyId] = useState('')
  const eventQuery = useQuery({
    ...observabilityAlertQueries.detail(eventId),
    enabled: enabled && eventId !== '',
  })
  const ruleId = readableQueryData(eventQuery)?.ruleId ?? ''
  const ruleQuery = useQuery({
    ...observabilityRuleQueries.detail(ruleId),
    enabled: enabled && ruleId !== '',
  })
  const ruleRunsQuery = useQuery({
    ...observabilityRuleQueries.runs(ruleId),
    enabled: enabled && ruleId !== '',
  })
  const healingRunsQuery = useQuery({
    ...observabilityAlertQueries.healingRuns(eventId),
    enabled: enabled && !queryAccessDenied(eventQuery) && eventId !== '',
  })
  const notificationPolicyId = readableQueryData(ruleQuery)?.notificationPolicyId ?? ''
  const previewQuery = useQuery({
    ...observabilityAlertQueries.preview(eventId, notificationPolicyId),
    enabled: enabled && eventId !== '' && notificationPolicyId !== '',
  })
  const deliveryLogsQuery = useQuery({
    ...observabilityAlertQueries.deliveryLogs(eventId),
    enabled: enabled && !queryAccessDenied(eventQuery) && eventId !== '',
  })
  const healingPoliciesQuery = useQuery({
    ...observabilityAlertQueries.healingPolicies(),
    enabled: enabled && canHeal && !queryAccessDenied(eventQuery),
  })
  const acknowledgeMutation = useOperationMutation(
    observabilityAlertMutations.acknowledge(queryClient),
    {
      label: ['确认告警', 'Acknowledge alert'],
      target: (id) => id,
      receipt: () => ({ message: '告警确认记录已返回' }),
    },
  )
  const resolveMutation = useOperationMutation(observabilityAlertMutations.resolve(queryClient), {
    label: ['恢复告警', 'Resolve alert'],
    target: (id) => id,
    receipt: () => ({ message: '告警恢复记录已返回' }),
  })
  const healMutation = useOperationMutation(observabilityAlertMutations.heal(queryClient), {
    label: ['发起自愈', 'Start healing'],
    target: ({ eventId, policyId }) => `${eventId} · ${policyId}`,
    receipt: (result) => ({
      message: '自愈请求已返回',
      evidence: (
        <Space>
          <span>运行 {result.id}</span>
          <a href="/monitoring-workbench/healing">查看执行记录</a>
        </Space>
      ),
    }),
  })
  const denied = queryAccessDenied(eventQuery)
  const blocked =
    !readableQueryData(eventQuery) ||
    acknowledgeMutation.blocked ||
    resolveMutation.blocked ||
    healMutation.blocked
  useEffect(() => {
    if (denied || !enabled) {
      setHealOpen(false)
      setHealingPolicyId('')
    }
  }, [denied, enabled, eventId])

  return {
    acknowledge: () => {
      if (!blocked && canAcknowledge) acknowledgeMutation.mutate(eventId)
    },
    acknowledgeMutation,
    canAcknowledge,
    canHeal,
    canResolve,
    deliveryLogsQuery,
    event: readableQueryData(eventQuery),
    blocked,
    ruleQuery,
    eventQuery,
    heal: () => {
      if (blocked || !canHeal || !healingPolicyId || !readableQueryData(healingPoliciesQuery))
        return
      healMutation.mutate(
        { eventId, policyId: healingPolicyId },
        {
          onSuccess: () => {
            setHealOpen(false)
          },
        },
      )
    },
    healMutation,
    healOpen,
    healingPoliciesQuery,
    healingPolicyId,
    healingRunsQuery,
    openHealModal: () => {
      if (blocked || !canHeal) return
      healMutation.reset()
      setHealingPolicyId(readableQueryData(ruleQuery)?.healingPolicyIds?.[0] || '')
      setHealOpen(true)
    },
    previewQuery,
    resolve: () => {
      if (!blocked && canResolve) resolveMutation.mutate(eventId)
    },
    resolveMutation,
    rule: readableQueryData(ruleQuery),
    ruleRunsQuery,
    setHealOpen,
    setHealingPolicyId,
  }
}

type DetailController = ReturnType<typeof useAlertEventDetailController>

function AlertEventDetailBody({
  actions,
  detail,
  inDrawer = false,
}: {
  actions?: ReactNode
  detail: DetailController
  inDrawer?: boolean
}) {
  const { event, rule } = detail
  const operationFeedback = (
    <>
      {detail.acknowledgeMutation.feedback}
      {detail.resolveMutation.feedback}
      {!detail.healOpen ? detail.healMutation.feedback : null}
    </>
  )
  if (!event)
    return (
      <>
        <ReadFeedback label="告警事件" query={detail.eventQuery} />
        {operationFeedback}
        {actions}
        {detail.eventQuery.isLoading ? (
          <ManagementState kind="loading" title="正在读取告警事件" />
        ) : !detail.eventQuery.isError ? (
          <ManagementState description="未读取到告警事件" />
        ) : null}
      </>
    )
  return (
    <div data-testid={inDrawer ? 'alert-event-drawer-content' : 'alert-event-page-content'}>
      <ReadFeedback label="告警事件" query={detail.eventQuery} />
      {operationFeedback}
      <Card title="事件摘要" extra={actions} loading={detail.eventQuery.isLoading}>
        <Descriptions bordered size="small" column={inDrawer ? 1 : 2}>
          <Descriptions.Item label="事件ID">{event?.id || '-'}</Descriptions.Item>
          <Descriptions.Item label="状态">
            {event ? <StatusTag value={event.status} /> : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="来源">
            {event?.sourceSystem || event?.sourceType || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="严重度">
            {event ? <StatusTag value={event.severity} /> : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="范围">
            {[event?.clusterId, event?.namespace].filter(Boolean).join(' / ') || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="最近命中">
            {formatDateTime(event?.lastSeenAt)}
          </Descriptions.Item>
          <Descriptions.Item label="规则ID">{event?.ruleId || '-'}</Descriptions.Item>
          <Descriptions.Item label="当前态">{event?.currentState || '-'}</Descriptions.Item>
          <Descriptions.Item label="接收器">{event?.receiver || '-'}</Descriptions.Item>
          <Descriptions.Item label="最近通知">
            {formatDateTime(event?.lastNotificationAt)}
          </Descriptions.Item>
          <Descriptions.Item label="触发时间">{formatDateTime(event?.startsAt)}</Descriptions.Item>
          <Descriptions.Item label="恢复时间">{formatDateTime(event?.endsAt)}</Descriptions.Item>
          <Descriptions.Item label="标题" span={inDrawer ? 1 : 2}>
            {event?.title || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="摘要" span={inDrawer ? 1 : 2}>
            {event?.summary || '-'}
          </Descriptions.Item>
        </Descriptions>
      </Card>
      <Card title="规则与通知上下文" style={{ marginTop: 16 }}>
        <ReadFeedback label="告警规则上下文" query={detail.ruleQuery} />
        <Descriptions bordered size="small" column={inDrawer ? 1 : 2}>
          <Descriptions.Item label="规则名称">{rule?.name || '-'}</Descriptions.Item>
          <Descriptions.Item label="规则类型">{rule?.ruleType || '-'}</Descriptions.Item>
          <Descriptions.Item label="通知策略">
            {rule?.notificationPolicyId || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="自愈策略">
            {rule?.healingPolicyIds?.join(', ') || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="聚合维度" span={inDrawer ? 1 : 2}>
            {rule?.groupBy?.join(', ') || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="生成链接" span={inDrawer ? 1 : 2}>
            {event?.generatorUrl ? (
              <a href={event.generatorUrl} target="_blank" rel="noreferrer">
                {event.generatorUrl}
              </a>
            ) : (
              '-'
            )}
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
                enableDensity
                error={detail.ruleRunsQuery.error}
                hasReadData={detail.ruleRunsQuery.data !== undefined}
                refreshing={detail.ruleRunsQuery.isFetching}
                onRefresh={() => void detail.ruleRunsQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                rowKey="id"
                loading={detail.ruleRunsQuery.isLoading}
                dataSource={detail.ruleRunsQuery.data ?? []}
                pagination={{ pageSize: 10 }}
                columns={[
                  { title: '运行ID', dataIndex: 'id' },
                  {
                    title: '状态',
                    dataIndex: 'status',
                    render: (value: string) => <StatusTag value={value} />,
                  },
                  {
                    title: '命中',
                    dataIndex: 'matched',
                    render: (value: boolean) => (
                      <BooleanTag value={value} trueLabel="命中" falseLabel="未命中" />
                    ),
                  },
                  { title: '耗时(ms)', dataIndex: 'durationMs' },
                  { title: '摘要', dataIndex: 'summary', ellipsis: true },
                  { title: '错误', dataIndex: 'error', render: (value: string) => value || '-' },
                  { title: '时间', dataIndex: 'createdAt', render: formatDateTime },
                ]}
              />
            ),
          },
          {
            key: 'healing',
            label: '自愈运行',
            children: (
              <AdminTable
                enableDensity
                error={detail.healingRunsQuery.error}
                hasReadData={detail.healingRunsQuery.data !== undefined}
                refreshing={detail.healingRunsQuery.isFetching}
                onRefresh={() => void detail.healingRunsQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                rowKey="id"
                loading={detail.healingRunsQuery.isLoading}
                dataSource={detail.healingRunsQuery.data ?? []}
                pagination={{ pageSize: 10 }}
                columns={[
                  { title: '运行ID', dataIndex: 'id' },
                  {
                    title: '状态',
                    dataIndex: 'status',
                    render: (value: string) => <StatusTag value={value} />,
                  },
                  {
                    title: '审批',
                    dataIndex: 'approvalStatus',
                    render: (value: string) => (value ? <StatusTag value={value} /> : '-'),
                  },
                  {
                    title: 'Workflow',
                    dataIndex: 'workflowStatus',
                    render: (value: string, record: HealingRun) =>
                      value ? <StatusTag value={value} /> : record.workflowRunId || '-',
                  },
                  {
                    title: '摘要',
                    dataIndex: 'workflowSummary',
                    ellipsis: true,
                    render: (value: string) => value || '-',
                  },
                  {
                    title: '审批人',
                    dataIndex: 'approvedBy',
                    render: (value: string) => value || '-',
                  },
                  { title: '创建时间', dataIndex: 'createdAt', render: formatDateTime },
                ]}
              />
            ),
          },
          {
            key: 'preview',
            label: '通知预览',
            children: (
              <AdminTable
                enableDensity
                error={detail.previewQuery.error}
                hasReadData={detail.previewQuery.data !== undefined}
                refreshing={detail.previewQuery.isFetching}
                onRefresh={() => void detail.previewQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                rowKey={(record) =>
                  `${String(record.channelId || 'channel')}:${String(record.templateId || 'template')}:${String(record.url || 'url')}`
                }
                loading={detail.previewQuery.isLoading}
                dataSource={detail.previewQuery.data ?? []}
                pagination={false}
                columns={[
                  {
                    title: '渠道',
                    dataIndex: 'channelId',
                    render: (value: string) => value || '-',
                  },
                  {
                    title: '模板',
                    dataIndex: 'templateId',
                    render: (value: string) => value || '-',
                  },
                  { title: 'URL', dataIndex: 'url', ellipsis: true },
                  { title: 'Method', dataIndex: 'method', render: (value: string) => value || '-' },
                  {
                    title: 'Content-Type',
                    dataIndex: 'contentType',
                    render: (value: string) => value || '-',
                  },
                  {
                    title: 'Body',
                    dataIndex: 'body',
                    render: (value: string) => (
                      <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{String(value || '')}</pre>
                    ),
                  },
                ]}
              />
            ),
          },
          {
            key: 'delivery',
            label: '投递日志',
            children: (
              <AdminTable
                enableDensity
                error={detail.deliveryLogsQuery.error}
                hasReadData={detail.deliveryLogsQuery.data !== undefined}
                refreshing={detail.deliveryLogsQuery.isFetching}
                onRefresh={() => void detail.deliveryLogsQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                rowKey="id"
                loading={detail.deliveryLogsQuery.isLoading}
                dataSource={detail.deliveryLogsQuery.data ?? []}
                pagination={{ pageSize: 10 }}
                columns={[
                  { title: '时间', dataIndex: 'createdAt', render: formatDateTime },
                  {
                    title: '渠道',
                    dataIndex: 'channelId',
                    render: (value: string) => value || '-',
                  },
                  {
                    title: '状态',
                    dataIndex: 'status',
                    render: (value: string) => <StatusTag value={value} />,
                  },
                  { title: '摘要', dataIndex: 'summary', render: (value: string) => value || '-' },
                  {
                    title: '元数据',
                    dataIndex: 'metadata',
                    render: (value: AlertDeliveryMetadata) => (
                      <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>
                        {stringifyAlertPayload(value)}
                      </pre>
                    ),
                  },
                ]}
              />
            ),
          },
          {
            key: 'raw',
            label: '原始载荷',
            children: (
              <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                {[
                  ['Labels', event?.labels],
                  ['Annotations', event?.annotations],
                  ['Rule JSON', rule],
                  ['Event JSON', event],
                ].map(([title, value]) => (
                  <Card size="small" title={title as string} key={title as string}>
                    <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>
                      {stringifyAlertPayload(value)}
                    </pre>
                  </Card>
                ))}
              </Space>
            ),
          },
        ]}
      />
      <Modal
        className="soha-observability-modal"
        style={{ top: 32 }}
        classNames={{
          body: 'soha-observability-modal-body',
          header: 'soha-observability-modal-header',
        }}
        title="发起自愈"
        open={detail.healOpen && Boolean(event)}
        closable={!detail.healMutation.blocked}
        keyboard={!detail.healMutation.blocked}
        cancelButtonProps={{ disabled: detail.healMutation.blocked }}
        mask={{ closable: false }}
        onCancel={() => {
          if (!detail.healMutation.blocked) detail.setHealOpen(false)
        }}
        onOk={detail.heal}
        okButtonProps={{
          disabled:
            detail.blocked ||
            !detail.healingPolicyId ||
            !readableQueryData(detail.healingPoliciesQuery),
          loading: detail.healMutation.isPending,
        }}
        destroyOnHidden
      >
        {detail.healMutation.feedback}
        <ReadFeedback label="自愈策略选项" query={detail.healingPoliciesQuery} />
        <p>告警：{event.id}</p>
        <Select
          style={{ width: '100%' }}
          disabled={detail.blocked || !readableQueryData(detail.healingPoliciesQuery)}
          placeholder="选择自愈策略"
          value={detail.healingPolicyId || undefined}
          onChange={(value) => detail.setHealingPolicyId(String(value))}
          options={(readableQueryData(detail.healingPoliciesQuery) ?? [])
            .filter((item) => item.enabled && rule?.healingPolicyIds?.includes(item.id))
            .map((item) => ({ value: item.id, label: item.name }))}
        />
      </Modal>
    </div>
  )
}

function AlertEventDetailActions({
  detail,
  extra,
}: {
  detail: DetailController
  extra?: ReactNode
}) {
  const navigate = useNavigate()
  const { event, rule } = detail
  const snapshot =
    event?.querySnapshot ??
    readableQueryData(detail.ruleRunsQuery)?.find((run) => run.querySnapshot)?.querySnapshot
  const diagnosticPaths = event ? alertDiagnosticPaths(event, snapshot) : undefined
  const search = new URLSearchParams()
  search.set('timeRangeMinutes', '60')
  if (event) {
    search.set('alertId', event.id)
    if (event.clusterId) search.set('clusterId', event.clusterId)
    if (event.namespace) search.set('namespace', event.namespace)
    const workload =
      event.labels?.workload ||
      event.labels?.deployment ||
      event.labels?.app ||
      event.labels?.service
    if (workload) search.set('workload', workload)
  }
  return (
    <Space wrap>
      {extra}
      <Button
        aria-label="刷新告警详情"
        loading={detail.eventQuery.isFetching || detail.ruleQuery.isFetching}
        onClick={() => {
          void detail.eventQuery.refetch()
          if (event?.ruleId) {
            void detail.ruleQuery.refetch()
            void detail.ruleRunsQuery.refetch()
          }
          if (event) {
            void detail.healingRunsQuery.refetch()
            void detail.deliveryLogsQuery.refetch()
            if (rule?.notificationPolicyId) void detail.previewQuery.refetch()
          }
        }}
      >
        刷新
      </Button>
      <Button
        disabled={!event}
        onClick={() => navigate(getAIWorkbenchPathForMode('root_cause', search))}
      >
        AI 调查
      </Button>
      {diagnosticPaths ? (
        <>
          <Button icon={<LineChartOutlined />} onClick={() => navigate(diagnosticPaths.metrics)}>
            指标
          </Button>
          <Button icon={<ApartmentOutlined />} onClick={() => navigate(diagnosticPaths.traces)}>
            链路
          </Button>
          <Button icon={<FileSearchOutlined />} onClick={() => navigate(diagnosticPaths.logs)}>
            日志
          </Button>
          <Button icon={<DashboardOutlined />} onClick={() => navigate(diagnosticPaths.dashboards)}>
            Dashboard
          </Button>
        </>
      ) : null}
      {detail.canAcknowledge && alertDisplayStatus(event) !== 'acknowledged' ? (
        <Button
          disabled={detail.blocked}
          loading={detail.acknowledgeMutation.isPending}
          onClick={detail.acknowledge}
        >
          确认
        </Button>
      ) : null}
      {detail.canResolve && alertDisplayStatus(event) !== 'resolved' ? (
        <Button
          disabled={detail.blocked}
          loading={detail.resolveMutation.isPending}
          onClick={detail.resolve}
        >
          恢复
        </Button>
      ) : null}
      {detail.canHeal && (rule?.healingPolicyIds?.length ?? 0) > 0 ? (
        <Button disabled={detail.blocked} type="primary" onClick={detail.openHealModal}>
          发起自愈
        </Button>
      ) : null}
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
  const event = detail.event
  const workload =
    event?.labels?.workload ||
    event?.labels?.deployment ||
    event?.labels?.app ||
    event?.labels?.service
  useAIPageContext({
    sourceWorkbench: 'monitoring',
    sourceTitle: event?.title || '告警事件详情',
    entityKind: 'monitoring.alert',
    entityName: event?.title || eventId,
    alertId: event?.id || eventId,
    clusterId: event?.clusterId,
    namespace: event?.namespace,
    workload,
    service: event?.labels?.service,
    timeRangeMinutes: 60,
    pinnedData: {
      severity: event?.severity,
      status: alertDisplayStatus(event),
      sourceType: event?.sourceType,
      sourceSystem: event?.sourceSystem,
      startsAt: event?.startsAt,
      lastSeenAt: event?.lastSeenAt,
    },
    promptHint: `排查告警 ${event?.title || eventId} 的触发原因、影响范围、关联资源和恢复建议。`,
  })
  return (
    <div className="soha-page">
      <AlertEventDetailBody
        detail={detail}
        actions={
          <AlertEventDetailActions
            detail={detail}
            extra={<Button onClick={onBack}>返回列表</Button>}
          />
        }
      />
    </div>
  )
}

export function AlertEventDetailPage() {
  const { eventId = '' } = useParams()
  const navigate = useNavigate()
  return (
    <AlertEventDetailPageContent
      key={eventId}
      eventId={eventId}
      onBack={() => navigate('/monitoring-workbench/alerts')}
    />
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
      extra={
        <AlertEventDetailActions
          detail={detail}
          extra={<Button onClick={() => onOpenStandalone(eventId)}>在详情页打开</Button>}
        />
      }
    >
      {eventId ? (
        <>
          <Paragraph type="secondary" style={{ marginTop: 0 }}>
            {detail.event
              ? `${detail.event.sourceSystem || detail.event.sourceType} · ${detail.event.status}`
              : '查看告警事件、规则运行、自愈与通知链路'}
          </Paragraph>
          <AlertEventDetailBody detail={detail} inDrawer />
        </>
      ) : (
        <ManagementState
          bordered={false}
          compact
          kind="select-scope"
          description="请选择一条告警事件。"
        />
      )}
    </Drawer>
  )
}
