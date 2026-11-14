import { WorkerReadiness } from '../clusters/worker-pools'
import { useEffect, useMemo, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Alert,
  Button,
  Descriptions,
  Drawer,
  Popconfirm,
  Segmented,
  Space,
  Typography,
} from 'antd'
import type { DrawerProps } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { Key } from 'react'
import {
  FileTextOutlined,
  PoweroffOutlined,
  ReloadOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import { hasAllowedAction } from '@/features/auth'
import { getAIWorkbenchPathForMode } from '@/features/copilot'
import { localeText, useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import {
  ManagementIconButton,
  ManagementQueryField,
  ManagementQueryPanel,
} from '@/components/management-list'
import {
  virtualizationMutations,
  withVirtualizationMutationSuccess,
} from '@/features/virtualization/mutations'
import { virtualizationQueries } from '@/features/virtualization/queries'
import { useVirtualizationPermissions } from '@/features/virtualization/shared/use-virtualization-permissions'
import { VirtualizationAdminTable } from '@/features/virtualization/shared/ui'
import { TableCellText } from '@/components/table-cell-content'
import {
  OPERATION_FILTER_PRESETS,
  buildOperationFilter,
  bulkActionSummary,
  formatOperationDuration,
  isAbnormalOperation,
  isPendingOperation,
  isSyncOperation,
  isVMOperation,
  latestNonEmptyOperationMessage,
  localTableSummary,
  nextOperationSearch,
  operationKindLabel,
  operationParamsFromSearch,
  operationTime,
  selectableOperationIds,
} from '@/features/virtualization/virtualization-model'
import type {
  OperationFilterPreset,
  OverviewTone,
} from '@/features/virtualization/virtualization-model'
import '@/features/virtualization/virtualization-workbench.css'
import type { VirtualizationOperation } from '@/features/virtualization/virtualization-types'

const { Text } = Typography

const stableDrawerMotion = null as unknown as DrawerProps['motion']

const tableEllipsis = { showTitle: false } as const

function statusTag(value?: string) {
  if (!value) return <Text type="secondary">-</Text>
  return <StatusTag value={value} />
}

function tableTooltipText(value: unknown) {
  const text = String(value ?? '').trim() || '-'
  return <TableCellText className="soha-vrt-table-tooltip-text" value={text} />
}

function buildInvestigationPath(params: {
  clusterId?: string
  namespace?: string
  workload?: string
  connectionId?: string
  vmId?: string
  provider?: string
  timeRangeMinutes?: number
}) {
  const search = new URLSearchParams()
  search.set('mode', 'root_cause')
  search.set('timeRangeMinutes', String(params.timeRangeMinutes ?? 60))
  if (params.clusterId) search.set('clusterId', params.clusterId)
  if (params.namespace) search.set('namespace', params.namespace)
  if (params.workload) search.set('workload', params.workload)
  if (params.connectionId) search.set('connectionId', params.connectionId)
  if (params.vmId) search.set('vmId', params.vmId)
  if (params.provider) search.set('provider', params.provider)
  return getAIWorkbenchPathForMode('root_cause', search)
}

export function OperationsTable({
  assetType,
  initialPreset = 'all',
  toolbarExtra,
}: {
  assetType?: string
  initialPreset?: OperationFilterPreset
  toolbarExtra?: React.ReactNode
}) {
  const location = useLocation()
  const navigate = useNavigate()
  const parsedSearch = useMemo(() => operationParamsFromSearch(location.search), [location.search])
  const [selectedOperation, setSelectedOperation] = useState<VirtualizationOperation | null>(null)
  const [preset, setPreset] = useState<OperationFilterPreset>(initialPreset)
  const [selectedTaskRowKeys, setSelectedTaskRowKeys] = useState<React.Key[]>([])
  const { virtualizationModuleEnabled, canCancelOperations, canRetryOperations } =
    useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const { localeCode } = useI18n()
  const operationParams = {
    assetType: assetType ?? parsedSearch.query.assetType,
    taskKind: assetType ? undefined : parsedSearch.query.taskKind,
    abnormal: parsedSearch.query.abnormal,
    pending: parsedSearch.query.pending,
    statuses: parsedSearch.query.statuses,
    connectionId: parsedSearch.query.connectionId,
    vmId: parsedSearch.query.vmId,
    search: parsedSearch.query.search,
  }
  const operationsQuery = useQuery(
    virtualizationQueries.operations(operationParams, virtualizationModuleEnabled),
  )
  const logsQuery = useQuery(
    virtualizationQueries.operationLogs(
      selectedOperation?.id ?? '',
      virtualizationModuleEnabled && Boolean(selectedOperation?.id),
    ),
  )

  useEffect(() => {
    setPreset(assetType ? 'asset_sync' : parsedSearch.preset || initialPreset)
  }, [assetType, initialPreset, parsedSearch.preset])

  const operations = operationsQuery.data ?? []
  const hasServerFilters = Boolean(
    parsedSearch.query.abnormal ||
    parsedSearch.query.pending ||
    parsedSearch.query.connectionId ||
    parsedSearch.query.vmId ||
    parsedSearch.query.search ||
    parsedSearch.query.statuses?.length ||
    parsedSearch.query.taskKind ||
    parsedSearch.query.assetType,
  )
  const filteredOperations = useMemo(
    () => (hasServerFilters ? operations : buildOperationFilter(operations, preset)),
    [hasServerFilters, operations, preset],
  )
  const logs = logsQuery.data ?? []
  const cancelMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.cancelOperation(queryClient), () =>
      message.success('取消请求已提交'),
    ),
  )
  const retryMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.retryOperation(queryClient), () =>
      message.success('重试任务已提交'),
    ),
  )
  const batchCancelMutation = useMutation(
    withVirtualizationMutationSuccess(
      virtualizationMutations.cancelOperations(queryClient),
      (_response, ids) => {
        message.success(
          localeText(
            localeCode,
            `已提交 ${ids.length} 个任务的取消请求`,
            `Cancellation requested for ${ids.length} tasks`,
          ),
        )
        setSelectedTaskRowKeys([])
      },
    ),
  )
  const batchRetryMutation = useMutation(
    withVirtualizationMutationSuccess(
      virtualizationMutations.retryOperations(queryClient),
      (_response, ids) => {
        message.success(
          localeText(
            localeCode,
            `已提交 ${ids.length} 个任务的重试请求`,
            `Retry requested for ${ids.length} tasks`,
          ),
        )
        setSelectedTaskRowKeys([])
      },
    ),
  )
  const columns: ColumnsType<VirtualizationOperation> = [
    {
      title: localeText(localeCode, '类型', 'Type'),
      dataIndex: 'operationType',
      render: (_value, record) => tableTooltipText(operationKindLabel(record, localeCode)),
      ellipsis: tableEllipsis,
      width: 140,
    },
    {
      title: localeText(localeCode, '资源', 'Resource'),
      dataIndex: 'targetName',
      render: (value, record) =>
        tableTooltipText(value || record.targetType || record.assetType || '-'),
      ellipsis: tableEllipsis,
      width: 180,
    },
    {
      title: localeText(localeCode, '连接', 'Connection'),
      dataIndex: 'connectionName',
      render: (value, record) => tableTooltipText(value || record.connectionId || '-'),
      ellipsis: tableEllipsis,
      width: 200,
    },
    {
      ...tableColumnPresets.status,
      title: localeText(localeCode, '状态', 'Status'),
      dataIndex: 'status',
      render: statusTag,
      width: 120,
    },
    {
      title: localeText(localeCode, '异常摘要', 'Failure summary'),
      dataIndex: 'message',
      render: (_value, record) => tableTooltipText(latestNonEmptyOperationMessage(record)),
      ellipsis: tableEllipsis,
      width: 320,
    },
    {
      title: localeText(localeCode, '运行时长', 'Duration'),
      render: (_value, record) => tableTooltipText(formatOperationDuration(record)),
      ellipsis: tableEllipsis,
      width: 140,
    },
    {
      ...tableColumnPresets.datetime,
      title: localeText(localeCode, '最近心跳', 'Last heartbeat'),
      dataIndex: 'lastHeartbeatAt',
      render: (value) => tableTooltipText(formatDateTime(value)),
      ellipsis: tableEllipsis,
      width: 180,
    },
    {
      ...tableColumnPresets.datetime,
      title: localeText(localeCode, '开始时间', 'Started at'),
      dataIndex: 'startedAt',
      render: (_value, record) => tableTooltipText(formatDateTime(operationTime(record))),
      ellipsis: tableEllipsis,
      width: 180,
    },
    {
      ...tableColumnPresets.action,
      title: localeText(localeCode, '操作', 'Actions'),
      dataIndex: 'id',
      width: 176,
      render: (_value, record) => {
        const canCancel = canCancelOperations && hasAllowedAction(record.allowedActions, 'cancel')
        const canRetry = canRetryOperations && hasAllowedAction(record.allowedActions, 'retry')
        return (
          <Space className="soha-row-action-icons" wrap>
            <ManagementIconButton
              aria-label={localeText(localeCode, '查看日志', 'View logs')}
              size="small"
              tooltip={localeText(localeCode, '日志', 'Logs')}
              icon={<FileTextOutlined />}
              onClick={() => setSelectedOperation(record)}
            />
            <ManagementIconButton
              aria-label={localeText(localeCode, 'AI 调查', 'AI investigation')}
              size="small"
              tooltip={localeText(localeCode, 'AI 调查', 'AI investigation')}
              icon={<SearchOutlined />}
              onClick={() =>
                navigate(
                  buildInvestigationPath({
                    connectionId: record.connectionId,
                    vmId: record.vmId,
                    workload: record.targetName || record.vmId || record.connectionId,
                    timeRangeMinutes: 60,
                  }),
                )
              }
            />
            {record.vmId ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '查看虚拟机', 'View virtual machine')}
                size="small"
                tooltip="VM"
                icon={<FileTextOutlined />}
                onClick={() =>
                  navigate(
                    `/compute/virtualization/vms/${encodeURIComponent(record.vmId || '')}?focus=operations`,
                  )
                }
              />
            ) : null}
            {canCancel ? (
              <Popconfirm
                title={localeText(localeCode, '确认取消任务？', 'Cancel this task?')}
                onConfirm={() => cancelMutation.mutate(record.id)}
              >
                <ManagementIconButton
                  aria-label={localeText(localeCode, '取消任务', 'Cancel task')}
                  size="small"
                  tooltip={localeText(localeCode, '取消', 'Cancel')}
                  danger
                  icon={<PoweroffOutlined />}
                />
              </Popconfirm>
            ) : null}
            {canRetry ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '重试任务', 'Retry task')}
                size="small"
                tooltip={localeText(localeCode, '重试', 'Retry')}
                icon={<ReloadOutlined />}
                onClick={() => retryMutation.mutate(record.id)}
              />
            ) : null}
          </Space>
        )
      },
    },
  ]

  const counts = {
    pending: operations.filter((record) => isPendingOperation(record.status)).length,
    abnormal: operations.filter((record) => isAbnormalOperation(record.status)).length,
    sync: operations.filter((record) => isSyncOperation(record)).length,
    vm: operations.filter((record) => isVMOperation(record)).length,
  }
  const statusSummary = [
    {
      key: 'pending',
      label: localeText(localeCode, '待处理', 'Pending'),
      value: counts.pending,
      tone: counts.pending > 0 ? 'warning' : 'default',
    },
    {
      key: 'abnormal',
      label: localeText(localeCode, '失败/超时', 'Failed / Timed out'),
      value: counts.abnormal,
      tone: counts.abnormal > 0 ? 'danger' : 'default',
    },
    { key: 'sync', label: localeText(localeCode, '同步任务', 'Sync tasks'), value: counts.sync },
    { key: 'vm', label: localeText(localeCode, 'VM 任务', 'VM tasks'), value: counts.vm },
  ] satisfies Array<{ key: string; label: string; value: number; tone?: OverviewTone }>
  const selectPreset = (nextPreset: OperationFilterPreset) => {
    setPreset(nextPreset)
    navigate({
      pathname: location.pathname,
      search: nextOperationSearch(nextPreset, {
        connectionId: parsedSearch.query.connectionId,
        vmId: parsedSearch.query.vmId,
        taskKind: parsedSearch.query.taskKind,
        search: parsedSearch.query.search,
        statuses: parsedSearch.query.statuses,
      }),
    })
  }
  const resetOperationFilters = () => {
    const nextPreset: OperationFilterPreset = assetType ? 'asset_sync' : 'all'
    setSelectedTaskRowKeys([])
    setPreset(nextPreset)
    navigate({
      pathname: location.pathname,
      search: assetType ? '' : nextOperationSearch(nextPreset, {}),
    })
  }

  return (
    <>
      <div className="soha-vrt-query soha-vrt-operations-query">
        <ManagementQueryPanel
          collapsible
          actions={
            <Button onClick={resetOperationFilters}>
              {localeText(localeCode, '重置', 'Reset')}
            </Button>
          }
        >
          <ManagementQueryField
            label={localeText(localeCode, '任务视图', 'Task view')}
            minWidth={300}
            width={360}
          >
            {assetType === 'asset_sync' ? (
              <MetadataTag tone="blue" label={localeText(localeCode, '同步任务', 'Sync tasks')} />
            ) : (
              <Segmented
                size="small"
                value={preset}
                options={OPERATION_FILTER_PRESETS.map((item) => ({
                  label:
                    item.key === 'all'
                      ? localeText(localeCode, '全部任务', 'All tasks')
                      : item.key === 'pending'
                        ? localeText(localeCode, '待处理', 'Pending')
                        : item.key === 'abnormal'
                          ? localeText(localeCode, '失败/超时', 'Failed / Timed out')
                          : item.key === 'asset_sync'
                            ? localeText(localeCode, '同步任务', 'Sync tasks')
                            : localeText(localeCode, 'VM 任务', 'VM tasks'),
                  value: item.key,
                }))}
                onChange={(value) => selectPreset(value as OperationFilterPreset)}
              />
            )}
          </ManagementQueryField>
          <ManagementQueryField
            grow
            label={localeText(localeCode, '任务统计', 'Task summary')}
            minWidth={420}
            width={560}
          >
            <div className="soha-vrt-commandbar-meta">
              {statusSummary.map((item) => (
                <span key={item.key}>
                  {item.label}
                  <Text strong>{item.value}</Text>
                </span>
              ))}
            </div>
          </ManagementQueryField>
        </ManagementQueryPanel>
      </div>
      <VirtualizationAdminTable
        rowKey="id"
        actions={toolbarExtra}
        toolbarExtra={
          selectedTaskRowKeys.length > 0 ? (
            <div className="soha-vrt-selection-bar">
              <Text type="secondary">
                {localeText(
                  localeCode,
                  `已选择 ${selectedTaskRowKeys.length} 个任务`,
                  `${selectedTaskRowKeys.length} tasks selected`,
                )}
              </Text>
              <Space wrap>
                {canCancelOperations ? (
                  <Popconfirm
                    title={localeText(localeCode, '确认批量取消任务？', 'Cancel selected tasks?')}
                    description={bulkActionSummary(
                      '将取消',
                      filteredOperations
                        .filter((record) => selectedTaskRowKeys.includes(record.id))
                        .map((record) => record.targetName || record.id),
                    )}
                    onConfirm={() => batchCancelMutation.mutate(selectedTaskRowKeys.map(String))}
                  >
                    <Button
                      danger
                      disabled={selectedTaskRowKeys.some(
                        (id) =>
                          !selectableOperationIds(filteredOperations, 'cancel').includes(
                            String(id),
                          ),
                      )}
                      loading={batchCancelMutation.isPending}
                    >
                      {localeText(localeCode, '批量取消', 'Cancel selected')}
                    </Button>
                  </Popconfirm>
                ) : null}
                {canRetryOperations ? (
                  <Popconfirm
                    title={localeText(localeCode, '确认批量重试任务？', 'Retry selected tasks?')}
                    description={bulkActionSummary(
                      '将重试',
                      filteredOperations
                        .filter((record) => selectedTaskRowKeys.includes(record.id))
                        .map((record) => record.targetName || record.id),
                    )}
                    onConfirm={() => batchRetryMutation.mutate(selectedTaskRowKeys.map(String))}
                  >
                    <Button
                      type="primary"
                      disabled={selectedTaskRowKeys.some(
                        (id) =>
                          !selectableOperationIds(filteredOperations, 'retry').includes(String(id)),
                      )}
                      loading={batchRetryMutation.isPending}
                    >
                      {localeText(localeCode, '批量重试', 'Retry selected')}
                    </Button>
                  </Popconfirm>
                ) : null}
                <Button onClick={() => setSelectedTaskRowKeys([])}>
                  {localeText(localeCode, '清空选择', 'Clear selection')}
                </Button>
              </Space>
            </div>
          ) : null
        }
        rowSelection={{
          selectedRowKeys: selectedTaskRowKeys,
          onChange: (keys: Key[]) => setSelectedTaskRowKeys(keys),
        }}
        loading={operationsQuery.isLoading}
        refreshing={operationsQuery.isFetching}
        onRefresh={() => void operationsQuery.refetch()}
        dataSource={filteredOperations}
        columns={columns}
        paginationSummary={localeText(
          localeCode,
          localTableSummary(filteredOperations.length, operations.length),
          `${filteredOperations.length} of ${operations.length}`,
        )}
        scroll={{ x: 1640 }}
      />
      <Drawer
        title={localeText(localeCode, '任务日志', 'Task logs')}
        size="large"
        motion={stableDrawerMotion}
        open={Boolean(selectedOperation)}
        onClose={() => setSelectedOperation(null)}
      >
        <Descriptions size="small" column={1} bordered>
          <Descriptions.Item label={localeText(localeCode, '任务 ID', 'Task ID')}>
            {selectedOperation?.id}
          </Descriptions.Item>
          <Descriptions.Item label={localeText(localeCode, '类型', 'Type')}>
            {selectedOperation ? operationKindLabel(selectedOperation, localeCode) : '-'}
          </Descriptions.Item>
          <Descriptions.Item label={localeText(localeCode, '状态', 'Status')}>
            {statusTag(selectedOperation?.status)}
          </Descriptions.Item>
          <Descriptions.Item label={localeText(localeCode, '资源', 'Resource')}>
            {selectedOperation?.targetName || selectedOperation?.targetType || '-'}
          </Descriptions.Item>
          <Descriptions.Item label={localeText(localeCode, '连接', 'Connection')}>
            {selectedOperation?.connectionName || selectedOperation?.connectionId || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="VM">{selectedOperation?.vmId || '-'}</Descriptions.Item>
          <Descriptions.Item label={localeText(localeCode, '开始时间', 'Started at')}>
            {formatDateTime(selectedOperation?.startedAt || selectedOperation?.createdAt)}
          </Descriptions.Item>
          <Descriptions.Item label={localeText(localeCode, '最近心跳', 'Last heartbeat')}>
            {formatDateTime(selectedOperation?.lastHeartbeatAt)}
          </Descriptions.Item>
          <Descriptions.Item label={localeText(localeCode, '完成时间', 'Completed at')}>
            {formatDateTime(selectedOperation?.completedAt)}
          </Descriptions.Item>
        </Descriptions>
        {selectedOperation?.payload?.workerPoolId && (
          <div className="mt-4">
            <WorkerReadiness operationId={selectedOperation.id} />
          </div>
        )}
        {selectedOperation?.message ? (
          <Alert
            className="mt-4"
            type={isAbnormalOperation(selectedOperation.status) ? 'error' : 'info'}
            title={selectedOperation.message}
          />
        ) : null}
        <div className="mt-4 flex justify-end">
          <Button
            size="small"
            onClick={async () => {
              const text =
                (logs.length
                  ? logs
                      .map(
                        (item) =>
                          `[${formatDateTime(item.createdAt)}] ${item.logLevel ?? 'info'} ${item.message}`,
                      )
                      .join('\n')
                  : selectedOperation?.logs?.length
                    ? selectedOperation.logs.join('\n')
                    : selectedOperation?.logText) ||
                selectedOperation?.message ||
                ''
              if (!text) return
              await navigator.clipboard.writeText(text)
              message.success(localeText(localeCode, '日志已复制', 'Logs copied'))
            }}
          >
            {localeText(localeCode, '复制日志', 'Copy logs')}
          </Button>
        </div>
        <pre className="mt-4 max-h-[520px] overflow-auto rounded border border-[var(--soha-border-color)] bg-[var(--soha-bg-surface-muted)] p-3 text-xs">
          {(logs.length
            ? logs
                .map(
                  (item) =>
                    `[${formatDateTime(item.createdAt)}] ${item.logLevel ?? 'info'} ${item.message}`,
                )
                .join('\n')
            : selectedOperation?.logs?.length
              ? selectedOperation.logs.join('\n')
              : selectedOperation?.logText) ||
            selectedOperation?.message ||
            (logsQuery.isLoading
              ? localeText(localeCode, '日志加载中', 'Loading logs')
              : localeText(localeCode, '暂无日志', 'No logs'))}
        </pre>
      </Drawer>
    </>
  )
}
