import { Alert, Button, Descriptions, Space, Typography } from 'antd'
import { AdminTable } from '@/components/admin-table'
import type { TableColumnsType } from 'antd'
import { RedoOutlined } from '@ant-design/icons'
import { ManagementState } from '@/components/management-list'
import { BooleanTag, MetadataTag, StatusTag } from '@/components/status-tag'
import { formatDateTime } from '@/utils/time'
import type { DirectoryConnection, DirectoryEvent, DirectoryRuntimeStatus } from './types'

const { Text } = Typography

function runTime(run?: DirectoryRuntimeStatus['lastIncrementalRun']) {
  return formatDateTime(run?.finishedAt ?? run?.startedAt)
}

export function DirectoryRuntimePanel({
  canRetry,
  connection,
  events,
  eventsLoading,
  eventsUnavailable = false,
  onRetry,
  retryingEventId,
  status,
  statusError,
  statusLoading,
  statusUnavailable = false,
}: {
  canRetry: boolean
  connection: DirectoryConnection
  events: DirectoryEvent[]
  eventsLoading: boolean
  eventsUnavailable?: boolean
  onRetry: (eventId: string) => void
  retryingEventId?: string
  status?: DirectoryRuntimeStatus
  statusError?: string
  statusLoading: boolean
  statusUnavailable?: boolean
}) {
  const callbackUrl = status?.callbackUrl
    ? new URL(status.callbackUrl, window.location.origin).toString()
    : undefined
  const columns: TableColumnsType<DirectoryEvent> = [
    {
      title: '事件时间',
      dataIndex: 'occurredAt',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '事件类型',
      dataIndex: 'eventType',
      width: 220,
      ellipsis: true,
    },
    {
      title: '上游事件 ID',
      dataIndex: 'providerEventId',
      width: 200,
      ellipsis: true,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
    { title: '尝试次数', dataIndex: 'attempts', width: 90 },
    {
      title: '错误',
      dataIndex: 'errorSummary',
      ellipsis: true,
      render: (value?: string) => (value ? <Text type="danger">{value}</Text> : '-'),
    },
    {
      title: '操作',
      key: 'actions',
      width: 90,
      fixed: 'right',
      render: (_value, event) =>
        canRetry && event.status === 'failed' ? (
          <Button
            size="small"
            icon={<RedoOutlined />}
            loading={retryingEventId === event.id}
            onClick={() => onRetry(event.id)}
          >
            重试
          </Button>
        ) : null,
    },
  ]

  if (statusError)
    return <Alert showIcon type="error" title="目录事件状态加载失败" description={statusError} />

  return (
    <Space
      orientation="vertical"
      size={12}
      style={{ minWidth: 0, width: '100%' }}
      styles={{ item: { minWidth: 0, width: '100%' } }}
    >
      {connection.policy.mode !== 'scheduled_and_realtime' ? (
        <Alert
          showIcon
          type="info"
          title="实时事件未启用"
          description="编辑连接并选择“定时 + 实时事件”后，才会接收增量组织与人员变更。"
        />
      ) : null}
      {statusUnavailable ? null : !status ? (
        <ManagementState
          compact
          kind={statusLoading ? 'loading' : 'empty'}
          title="尚无目录运行状态"
          description="读取到运行状态后展示回调、队列与对账结果。"
        />
      ) : (
        <Descriptions
          bordered
          column={{ xs: 1, sm: 1, md: 2 }}
          size="small"
          items={[
            {
              key: 'callbackUrl',
              label: '事件回调地址',
              span: 2,
              children: callbackUrl ? (
                <Text code copyable={{ text: callbackUrl, tooltips: ['复制地址', '已复制'] }}>
                  {callbackUrl}
                </Text>
              ) : (
                '-'
              ),
            },
            {
              key: 'callbackStatus',
              label: '回调验证',
              children: (
                <BooleanTag
                  value={status?.callbackStatus === 'verified'}
                  trueLabel="已验证"
                  falseLabel={status?.callbackConfigured ? '等待首个事件' : '未配置'}
                  falseColor="warning"
                />
              ),
            },
            {
              key: 'callbackVerifiedAt',
              label: '验证时间',
              children: formatDateTime(status?.callbackVerifiedAt),
            },
            {
              key: 'queue',
              label: '事件队列',
              span: 2,
              children: (
                <Space wrap size={4}>
                  <MetadataTag label={`待处理 ${status?.queuedEvents ?? 0}`} tone="blue" />
                  <MetadataTag label={`失败 ${status?.failedEvents ?? 0}`} tone="orange" />
                </Space>
              ),
            },
            {
              key: 'lastEventAt',
              label: '最近事件',
              children: formatDateTime(status?.lastEventAt),
            },
            {
              key: 'lastIncrementalRun',
              label: '最近增量同步',
              children: runTime(status?.lastIncrementalRun),
            },
            {
              key: 'lastFullRun',
              label: '最近全量对账',
              children: runTime(status?.lastFullRun),
            },
            {
              key: 'reconcileRequired',
              label: '对账状态',
              children: (
                <Space size={4}>
                  <BooleanTag
                    value={!status?.needsFullReconcile}
                    trueLabel="无需对账"
                    falseLabel="需要对账"
                    falseColor="warning"
                  />
                  {status?.reconcileReason ? (
                    <Text type="secondary">{status.reconcileReason}</Text>
                  ) : null}
                </Space>
              ),
            },
            {
              key: 'disablePolicy',
              label: '离职处理',
              children:
                connection.policy.userDisablePolicy === 'never'
                  ? '保留账号，仅移除成员关系'
                  : '停用目录托管账号并移除成员关系',
            },
            {
              key: 'archivePolicy',
              label: '组织归档',
              children: '上游缺失时归档，不直接删除',
            },
          ]}
        />
      )}
      {eventsUnavailable ? null : !eventsLoading && events.length === 0 ? (
        <ManagementState
          compact
          kind="empty"
          title="暂无目录事件"
          description="收到目录变更后会显示在这里。"
        />
      ) : (
        <AdminTable
          enableColumnSelection={false}
          rowKey="id"
          tableSize="small"
          loading={statusLoading || eventsLoading}
          columns={columns}
          dataSource={events}
          pagination={false}
          scroll={{ x: 980 }}
        />
      )}
    </Space>
  )
}
