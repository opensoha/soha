import { useMemo, useState } from 'react'
import { App, Descriptions, Drawer, Form, Segmented, Space, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { FileTextOutlined, PoweroffOutlined, ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementQueryPanel,
} from '@/components/management-list'
import { formatDateTime } from '@/utils/time'
import { localeText, useI18n } from '@/i18n'
import { dockerApi } from '../docker-api'
import { dockerQueries } from '../queries'
import type { DockerOperation, DockerOperationLog } from '../docker-types'
import {
  DockerAdminTable,
  isAbnormalOperation,
  isPendingOperation,
  normalizePage,
  operationTone,
  pageTablePagination,
  queryData,
  refreshDocker,
  statusTag,
  type DockerFilterState,
  useDockerPermissions,
} from '../shared/ui'

const { Text } = Typography
type OperationPreset = 'all' | 'pending' | 'abnormal' | 'host' | 'project' | 'service'

function operationKindLabel(value: string, localeCode: 'zh_CN' | 'en_US') {
  const labels: Record<string, [string, string]> = {
    host_provision: ['主机构建', 'Host provisioning'],
    project_deploy: ['项目部署', 'Project deployment'],
    service_action: ['服务操作', 'Service action'],
  }
  const label = labels[value]
  return label ? localeText(localeCode, label[0], label[1]) : value
}

function OperationLogDrawer({
  operation,
  logs,
  loading,
  open,
  onClose,
}: {
  operation?: DockerOperation | null
  logs: DockerOperationLog[]
  loading?: boolean
  open: boolean
  onClose: () => void
}) {
  const { localeCode } = useI18n()
  const text = logs.length
    ? logs
        .map(
          (item) =>
            `[${formatDateTime(item.createdAt)}] ${item.logLevel || 'info'} ${item.message}`,
        )
        .join('\n')
    : JSON.stringify(operation?.payload ?? {}, null, 2)
  return (
    <Drawer
      title={localeText(localeCode, '操作日志', 'Operation logs')}
      size="large"
      open={open}
      onClose={onClose}
    >
      {operation ? (
        <Descriptions size="small" column={2} bordered className="mb-3">
          <Descriptions.Item label={localeText(localeCode, '任务 ID', 'Task ID')}>
            {operation.id}
          </Descriptions.Item>
          <Descriptions.Item label={localeText(localeCode, '状态', 'Status')}>
            {statusTag(operation.status)}
          </Descriptions.Item>
          <Descriptions.Item label={localeText(localeCode, '类型', 'Type')}>
            {operationKindLabel(operation.operationKind ?? '-', localeCode)}
          </Descriptions.Item>
          <Descriptions.Item label={localeText(localeCode, '发起人', 'Requested by')}>
            {operation.requestedBy || '-'}
          </Descriptions.Item>
        </Descriptions>
      ) : null}
      <pre className="max-h-[560px] overflow-auto rounded border border-[var(--soha-border-color)] bg-[var(--soha-bg-surface-muted)] p-3 text-xs">
        {loading
          ? localeText(localeCode, '日志加载中', 'Loading logs')
          : text || localeText(localeCode, '暂无日志', 'No logs')}
      </pre>
    </Drawer>
  )
}

export function OperationsTable({
  embedded = false,
  initialPreset = 'all' as OperationPreset,
}: {
  embedded?: boolean
  initialPreset?: OperationPreset
}) {
  const [preset, setPreset] = useState<OperationPreset>(initialPreset)
  const [filters, setFilters] = useState<DockerFilterState>({
    page: 1,
    pageSize: embedded ? 6 : 15,
  })
  const [filterForm] = Form.useForm<DockerFilterState>()
  const [selectedOperation, setSelectedOperation] = useState<DockerOperation | null>(null)
  const { dockerModuleEnabled, canCancelOperations, canRetryOperations } = useDockerPermissions()
  const { localeCode } = useI18n()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const presetFilter = useMemo<DockerFilterState>(() => {
    if (preset === 'pending') return { pending: true }
    if (preset === 'abnormal') return { abnormal: true }
    if (preset === 'host') return { operationKind: 'host_provision' }
    if (preset === 'project') return { operationKind: 'project_deploy' }
    if (preset === 'service') return { operationKind: 'service_action' }
    return {}
  }, [preset])
  const queryFilters = { ...filters, ...presetFilter }
  const operationsQuery = useQuery(dockerQueries.operations(queryFilters, dockerModuleEnabled))
  const logsQuery = useQuery(
    dockerQueries.operationLogs(selectedOperation?.id ?? '', dockerModuleEnabled),
  )
  const cancelMutation = useMutation({
    mutationFn: dockerApi.cancelOperation,
    onSuccess: () => {
      message.success(localeText(localeCode, '已请求取消任务', 'Task cancellation requested'))
      refreshDocker(queryClient)
    },
  })
  const retryMutation = useMutation({
    mutationFn: dockerApi.retryOperation,
    onSuccess: () => {
      message.success(localeText(localeCode, '重试任务已提交', 'Retry task submitted'))
      refreshDocker(queryClient)
    },
  })
  const page = normalizePage(
    operationsQuery.data,
    filters.page ?? 1,
    filters.pageSize ?? (embedded ? 6 : 15),
  )
  const logs = queryData(logsQuery.data, [])
  const columns: ColumnsType<DockerOperation> = [
    {
      title: localeText(localeCode, '任务', 'Task'),
      dataIndex: 'operationKind',
      fixed: 'left',
      width: 190,
      render: (value, record) => (
        <Space orientation="vertical" size={0}>
          <Text strong>{operationKindLabel(value, localeCode)}</Text>
          <Text type="secondary">{record.id}</Text>
        </Space>
      ),
    },
    {
      title: localeText(localeCode, '状态', 'Status'),
      dataIndex: 'status',
      width: 115,
      render: (value) => statusTag(value, localeCode),
    },
    {
      title: localeText(localeCode, '关联对象', 'Related resources'),
      width: 240,
      render: (_value, record) =>
        [record.hostId, record.projectId, record.serviceId].filter(Boolean).join(' / ') || '-',
    },
    {
      title: localeText(localeCode, '发起人', 'Requested by'),
      dataIndex: 'requestedBy',
      width: 130,
      render: (value) => value || '-',
    },
    {
      title: localeText(localeCode, '尝试', 'Attempts'),
      width: 90,
      render: (_value, record) => `${record.attemptCount ?? 0}/${record.maxRetries ?? 0}`,
    },
    {
      title: 'Worker',
      dataIndex: 'claimedByWorkerId',
      width: 150,
      render: (value) => value || '-',
    },
    {
      title: localeText(localeCode, '开始', 'Started at'),
      dataIndex: 'startedAt',
      width: 155,
      render: formatDateTime,
    },
    {
      title: localeText(localeCode, '结束', 'Finished at'),
      dataIndex: 'finishedAt',
      width: 155,
      render: formatDateTime,
    },
    {
      title: localeText(localeCode, '操作', 'Actions'),
      align: 'center',
      className: 'soha-table-actions-column',
      fixed: 'right',
      width: 116,
      render: (_value, record) => (
        <Space className="soha-row-action-icons">
          <ManagementIconButton
            aria-label={localeText(localeCode, '查看日志', 'View logs')}
            size="small"
            tooltip={localeText(localeCode, '日志', 'Logs')}
            icon={<FileTextOutlined />}
            onClick={() => setSelectedOperation(record)}
          />
          {canCancelOperations &&
          (record.operationState?.cancelable ?? isPendingOperation(record.status)) ? (
            <ManagementIconButton
              aria-label={localeText(localeCode, '取消任务', 'Cancel task')}
              size="small"
              tooltip={localeText(localeCode, '取消', 'Cancel')}
              danger
              icon={<PoweroffOutlined />}
              loading={cancelMutation.isPending}
              onClick={() => cancelMutation.mutate(record.id)}
            />
          ) : null}
          {canRetryOperations &&
          (record.operationState?.retryable ?? isAbnormalOperation(record.status)) ? (
            <ManagementIconButton
              aria-label={localeText(localeCode, '重试任务', 'Retry task')}
              size="small"
              tooltip={localeText(localeCode, '重试', 'Retry')}
              icon={<ReloadOutlined />}
              loading={retryMutation.isPending}
              onClick={() => retryMutation.mutate(record.id)}
            />
          ) : null}
        </Space>
      ),
    },
  ]
  return (
    <>
      {!embedded ? (
        <div className="soha-vrt-query">
          <ManagementQueryPanel
            form={filterForm}
            actions={
              <ManagementQueryActions
                loading={operationsQuery.isFetching}
                onReset={() => {
                  filterForm.resetFields()
                  setPreset(initialPreset)
                  setFilters({ page: 1, pageSize: filters.pageSize ?? (embedded ? 6 : 15) })
                }}
              />
            }
            onFinish={(values) => setFilters((current) => ({ ...current, ...values, page: 1 }))}
          >
            <ManagementQueryField
              minWidth={360}
              width={460}
              label={localeText(localeCode, '任务视图', 'Task view')}
            >
              <Segmented<OperationPreset>
                value={preset}
                onChange={(value) => {
                  setPreset(value)
                  setFilters((current) => ({ ...current, page: 1 }))
                }}
                options={[
                  { value: 'all', label: localeText(localeCode, '全部', 'All') },
                  { value: 'pending', label: localeText(localeCode, '待处理', 'Pending') },
                  { value: 'abnormal', label: localeText(localeCode, '异常', 'Failed') },
                  { value: 'host', label: localeText(localeCode, '主机构建', 'Host provisioning') },
                  { value: 'project', label: 'Compose' },
                  { value: 'service', label: localeText(localeCode, '服务', 'Service') },
                ]}
              />
            </ManagementQueryField>
            <ManagementKeywordField
              placeholder={localeText(
                localeCode,
                '任务 ID、类型或发起人',
                'Task ID, type, or requester',
              )}
            />
          </ManagementQueryPanel>
        </div>
      ) : null}
      <DockerAdminTable
        rowKey="id"
        enableColumnSelection={!embedded}
        loading={operationsQuery.isLoading}
        dataSource={page.items}
        columns={columns}
        rowClassName={(record: DockerOperation) => `soha-vrt-row-tone-${operationTone(record)}`}
        scroll={{ x: 1280 }}
        pagination={pageTablePagination(page, embedded, setFilters)}
        title={
          embedded ? (
            <Text strong>
              {initialPreset === 'pending'
                ? localeText(localeCode, '待处理任务', 'Pending tasks')
                : localeText(localeCode, '操作记录', 'Operations')}
            </Text>
          ) : undefined
        }
        enableDensity={!embedded}
        refreshing={operationsQuery.isFetching}
        showColumnSettings={!embedded}
        showRefresh={!embedded}
        onRefresh={() => operationsQuery.refetch()}
      />
      <OperationLogDrawer
        operation={selectedOperation}
        logs={logs}
        loading={logsQuery.isLoading}
        open={Boolean(selectedOperation)}
        onClose={() => setSelectedOperation(null)}
      />
    </>
  )
}
