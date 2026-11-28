import { ContextFeedback } from '@/components/context-feedback'
import { useMemo } from 'react'
import { App, Button, Space, Tag, Typography } from 'antd'
import { ArrowRightOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { TableRowActionMenu } from '@/components/table-row-action-menu'
import { MutationFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { OverviewMetricCard, type OverviewMetricItem } from '@/components/overview-visuals'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { DeliveryTable } from '@/features/delivery/delivery-table'
import {
  canCancelExecutionTask,
  canRetryExecutionTask,
  summarizeExecutionTaskArtifacts,
  summarizeExecutionTaskStatus,
} from '@/features/delivery/delivery-status'
import { deliveryMutations } from '@/features/delivery/mutations'
import { deliveryQueries } from '@/features/delivery/queries'
import type { ExecutionArtifact, ExecutionTask } from '@/features/delivery/types'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { summarizeDeliveryGovernance } from '../workbench/governance'

const { Text } = Typography

export function ExecutionTasksPage() {
  const { modal } = App.useApp()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const focusedExecutionTaskId = searchParams.get('executionTaskId')?.trim() ?? ''
  const focusedReleaseBundleId = searchParams.get('releaseBundleId')?.trim() ?? ''
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canCancel = hasPermission(permissionSnapshot, 'delivery.execution-tasks.cancel')
  const canRetry = hasPermission(permissionSnapshot, 'delivery.execution-tasks.retry')
  const tasksQuery = useQuery(deliveryQueries.executionTasks.list({ refetchInterval: 5000 }))
  const cancelMutation = useMutation(deliveryMutations.executionTasks.cancel(queryClient))
  const retryMutation = useMutation(deliveryMutations.executionTasks.retry(queryClient))
  const taskData = readableQueryData(tasksQuery)
  const executionTasks = taskData ?? []
  const requestLocked =
    cancelMutation.isPending ||
    retryMutation.isPending ||
    writeOutcomeUnknown(cancelMutation.error) ||
    writeOutcomeUnknown(retryMutation.error)
  const focusedTask = focusedExecutionTaskId
    ? executionTasks.find((item) => item.id === focusedExecutionTaskId)
    : undefined
  const executionSummary = useMemo(
    () => summarizeExecutionTaskStatus(executionTasks),
    [executionTasks],
  )
  const summaryMetrics: OverviewMetricItem[] = [
    {
      key: 'total',
      label: '任务总数',
      value: taskData === undefined ? '—' : executionSummary.total,
      helper: taskData === undefined ? '执行状态不可用' : `${executionSummary.active} 个执行中`,
    },
    {
      key: 'blocked',
      label: '阻塞任务',
      value: taskData === undefined ? '—' : executionSummary.blocked,
      helper: taskData === undefined ? '重试状态不可用' : `${executionSummary.retryable} 个可重试`,
      tone:
        taskData === undefined ? undefined : executionSummary.blocked > 0 ? 'danger' : 'success',
    },
    {
      key: 'artifacts',
      label: '交付物线索',
      value: taskData === undefined ? '—' : executionSummary.artifacts,
      helper: '来自任务结果',
    },
    {
      key: 'callbacks',
      label: '回调可用',
      value: taskData === undefined ? '—' : executionSummary.callbackReady,
      helper: 'agent / callback token',
      tone: taskData === undefined ? undefined : 'success',
    },
  ]

  function handleCancel(task: ExecutionTask) {
    if (requestLocked) return
    cancelMutation.mutate({ id: task.id, reason: 'Canceled from execution tasks console' })
  }

  function handleRetry(task: ExecutionTask) {
    if (requestLocked) return
    retryMutation.mutate({ id: task.id, reason: 'Retried from execution tasks console' })
  }

  return (
    <div className="soha-page">
      <MutationFeedback
        operations={[
          {
            label: '取消执行任务',
            mutation: cancelMutation,
            target: cancelMutation.variables?.id,
            receipt: { message: '取消请求已提交，请核对任务状态' },
          },
          {
            label: '重试执行任务',
            mutation: retryMutation,
            target: retryMutation.variables?.id,
            receipt: { message: '任务已重新入队，请查看执行记录' },
          },
        ]}
      />
      {focusedExecutionTaskId || focusedReleaseBundleId ? (
        <ContextFeedback
          showIcon
          title={focusedTask ? `已定位执行任务 ${focusedTask.id}` : '执行任务定位'}
          description={[
            focusedExecutionTaskId ? `executionTaskId=${focusedExecutionTaskId}` : '',
            focusedReleaseBundleId ? `releaseBundleId=${focusedReleaseBundleId}` : '',
          ]
            .filter(Boolean)
            .join(' / ')}
          type={focusedTask || tasksQuery.isLoading ? 'info' : 'warning'}
        />
      ) : null}
      <div className="soha-overview-metric-grid">
        {summaryMetrics.map(({ key, ...item }) => (
          <OverviewMetricCard key={key} {...item} loading={tasksQuery.isLoading} />
        ))}
      </div>
      <DeliveryTable
        rowKey="id"
        refreshing={tasksQuery.isFetching}
        onRefresh={() => tasksQuery.refetch()}
        isError={tasksQuery.isError}
        error={tasksQuery.error}
        hasReadData={tasksQuery.data !== undefined}
        errorDescription="暂时无法读取执行任务。"
        onRetry={() => tasksQuery.refetch()}
        localSorting
        loading={tasksQuery.isLoading}
        dataSource={executionTasks}
        columns={[
          {
            title: '任务',
            dataIndex: 'taskKind',
            render: (value: string, record: ExecutionTask) => (
              <Space orientation="vertical" size={0}>
                <Space size={6} wrap>
                  <Button
                    type="link"
                    style={{ padding: 0, height: 'auto', fontWeight: 600 }}
                    onClick={() => navigate(`/delivery/execution-tasks/${record.id}`)}
                  >
                    {value}
                  </Button>
                  {record.id === focusedExecutionTaskId ? <Tag color="blue">已定位</Tag> : null}
                </Space>
                <Text type="secondary">{record.id}</Text>
              </Space>
            ),
          },
          { title: '执行器', dataIndex: 'providerKind' },
          { title: '目标类型', dataIndex: 'targetKind' },
          {
            title: '应用',
            dataIndex: 'applicationId',
            render: (value: string, record: ExecutionTask) => (
              <Space orientation="vertical" size={0}>
                <Text>{value}</Text>
                <Text type="secondary">{record.applicationEnvironmentId || '-'}</Text>
              </Space>
            ),
          },
          {
            title: '版本包',
            dataIndex: 'releaseBundleId',
            render: (value: string) =>
              value ? (
                <Button
                  type="link"
                  size="small"
                  aria-label={`查看版本包 ${value}`}
                  onClick={() => navigate(`/delivery/release-bundles/${value}`)}
                >
                  {value}
                </Button>
              ) : (
                '-'
              ),
          },
          {
            title: '交付物',
            dataIndex: 'artifacts',
            render: (value?: ExecutionArtifact[]) => summarizeExecutionTaskArtifacts(value),
          },
          {
            title: '状态',
            dataIndex: 'status',
            render: (value: string) => <StatusTag value={value} />,
          },
          {
            title: '治理证据',
            dataIndex: 'releaseBundleId',
            render: (value: string, record: ExecutionTask) => {
              if (!value) return '-'
              const bundle = {
                id: value,
                applicationId: record.applicationId,
                version: '-',
                sourceType: '-',
                status: 'completed',
                createdAt: record.createdAt,
                updatedAt: record.updatedAt,
              }
              const governance = summarizeDeliveryGovernance(bundle, [record])
              return (
                <Space size={4} wrap>
                  <StatusTag value={governance.label} />
                  {governance.aiAuditRefs.length ? <Tag color="purple">AI审计</Tag> : null}
                  {governance.approvalRequestId ? <Tag color="gold">审批</Tag> : null}
                </Space>
              )
            },
          },
          {
            title: '重试',
            dataIndex: 'attemptCount',
            render: (value: number, record: ExecutionTask) => `${value}/${record.maxRetries}`,
          },
          { title: '超时（秒）', dataIndex: 'timeoutSeconds' },
          {
            ...tableColumnPresets.datetime,
            title: '心跳',
            dataIndex: 'lastHeartbeatAt',
            render: (value?: string) => (value ? formatDateTime(value) : '-'),
          },
          {
            ...tableColumnPresets.datetime,
            title: '更新时间',
            dataIndex: 'updatedAt',
            render: (value: string) => formatDateTime(value),
          },
          {
            ...tableColumnPresets.action,
            title: '操作',
            dataIndex: 'id',
            render: (_: unknown, record: ExecutionTask) => (
              <TableRowActionMenu
                targetLabel={record.id}
                items={[
                  {
                    key: 'detail',
                    label: '查看执行详情',
                    icon: <ArrowRightOutlined />,
                    onClick: () => navigate(`/delivery/execution-tasks/${record.id}`),
                  },
                  canRetry && canRetryExecutionTask(record)
                    ? {
                        key: 'retry',
                        label: '重试执行任务',
                        icon: <ReloadOutlined />,
                        disabled: requestLocked,
                        onClick: () => handleRetry(record),
                      }
                    : null,
                  canCancel && canCancelExecutionTask(record) ? { type: 'divider' } : null,
                  canCancel && canCancelExecutionTask(record)
                    ? {
                        key: 'cancel',
                        label: '取消执行任务',
                        icon: <StopOutlined />,
                        danger: true,
                        disabled: requestLocked,
                        onClick: () => {
                          modal.confirm({
                            title: '确认取消该任务？',
                            content: `任务：${record.id}`,
                            okText: '确认取消',
                            okButtonProps: { danger: true },
                            onOk: () => handleCancel(record),
                          })
                        },
                      }
                    : null,
                ]}
              />
            ),
          },
        ]}
      />
    </div>
  )
}
