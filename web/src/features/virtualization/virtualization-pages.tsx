import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Alert,
  Badge,
  Button,
  Card,
  Descriptions,
  Drawer,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Progress,
  Segmented,
  Select,
  Space,
  Spin,
  Switch,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import type { DrawerProps } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { ComponentProps, Key } from 'react'
import {
  CheckCircleOutlined,
  CloudSyncOutlined,
  CloseCircleOutlined,
  DeleteOutlined,
  EditOutlined,
  EyeOutlined,
  FileTextOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  PoweroffOutlined,
  ReloadOutlined,
  SearchOutlined,
  ThunderboltOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { getAIWorkbenchPathForMode } from '@/features/copilot/workbench-navigation'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { api } from '@/services/api-client'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementDetailHeader,
  ManagementIconButton,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import type { ApiResponse, Cluster } from '@/types'
import { virtualizationApi } from './virtualization-api'
import {
  ENABLED_FILTER_OPTIONS,
  OPERATION_FILTER_PRESETS,
  STATUS_COLORS,
  VIRTUALIZATION_PROVIDER_FILTER_OPTIONS,
  VIRTUALIZATION_PROVIDER_OPTIONS,
  badgeStatusForTone,
  buildClusterPayload,
  buildCreateVmPayload,
  buildImagePayload,
  buildOperationFilter,
  bulkActionSummary,
  classNames,
  clusterRiskScore,
  formatOperationDuration,
  isAbnormalOperation,
  isPendingOperation,
  isSyncOperation,
  isVMOperation,
  latestNonEmptyOperationMessage,
  localTableSummary,
  nextOperationSearch,
  normalizePage,
  operationKind,
  operationParamsFromSearch,
  operationPresetFromSearch,
  operationTime,
  providerLabel,
  riskReasons,
  selectableOperationIds,
  stringifyRaw,
  virtualizationPageSummary,
} from './virtualization-model'
import type {
  EnabledFilter,
  OperationFilterPreset,
  OverviewTone,
  ProviderFilter,
  VirtualMachineFormValues,
  VirtualizationClusterFormValues,
} from './virtualization-model'
export { buildClusterPayload, buildCreateVmPayload } from './virtualization-model'
import { useTaskStream } from './use-task-stream'
import { LineChart } from '@visactor/react-vchart'
import {
  buildCompactChartSpec,
  compactMetricColors,
  formatMetricValue,
  type CompactChartLine,
} from '@/components/resource-metrics-panel'
import './virtualization-workbench.css'
import type {
  VirtualMachine,
  VirtualMachinePowerAction,
  VirtualizationCluster,
  VirtualizationFlavor,
  VirtualizationFlavorInput,
  VirtualizationImage,
  VirtualizationImageInput,
  VirtualizationListParams,
  VirtualizationOperation,
  VirtualizationOverview,
  VirtualizationPage,
  VirtualizationVMMetrics,
} from './virtualization-types'

const { Text } = Typography
const stableDrawerMotion = null as unknown as DrawerProps['motion']

const VMConsole = lazy(() =>
  import('./vm-console').then((module) => ({ default: module.VMConsole })),
)



function statusTag(value?: string) {
  if (!value) return <Text type="secondary">-</Text>
  const key = value.toLowerCase()
  return <Tag color={STATUS_COLORS[key] ?? 'default'}>{value}</Tag>
}



function OperationStatusChips({ counts, compact = false }: { counts: Array<{ key: string; label: string; value: number; tone?: OverviewTone }>; compact?: boolean }) {
  return (
    <div className={`soha-vrt-chip-grid${compact ? ' is-compact' : ''}`}>
      {counts.map((item) => (
        <div key={item.key} className={`soha-vrt-chip is-${item.tone ?? 'default'}`}>
          <span className="soha-vrt-chip-label">{item.label}</span>
          <span className="soha-vrt-chip-value">{item.value}</span>
        </div>
      ))}
    </div>
  )
}

function AttentionList({
  title,
  description,
  emptyText,
  action,
  items,
  renderMeta,
  renderActions,
  tone = 'default',
}: {
  title: string
  description: string
  emptyText: string
  action?: React.ReactNode
  items: Array<{ id: string; title: string; status?: string; message?: string }>
  renderMeta?: (item: { id: string; title: string; status?: string; message?: string }) => React.ReactNode
  renderActions?: (item: { id: string; title: string; status?: string; message?: string }) => React.ReactNode
  tone?: OverviewTone
}) {
  return (
    <section className={`soha-vrt-lane is-${tone}`}>
      <div className="soha-vrt-lane-head">
        <div>
          <div className="soha-vrt-lane-title">{title}</div>
          <div className="soha-vrt-lane-description">{description}</div>
        </div>
        {action ? <div className="soha-vrt-lane-action">{action}</div> : null}
      </div>
      {items.length === 0 ? (
        <div className="soha-vrt-empty">
          <ManagementState bordered={false} compact title={emptyText} description={description} />
        </div>
      ) : (
        <div className="soha-vrt-lane-items">
          {items.map((item) => (
            <div key={item.id} className="soha-vrt-attention-row">
              <div className="soha-vrt-attention-row-head">
                <Space wrap>
                  <Text strong>{item.title}</Text>
                  {statusTag(item.status)}
                </Space>
                {renderActions ? <Space wrap className="soha-vrt-row-actions">{renderActions(item)}</Space> : null}
              </div>
              <div className="soha-vrt-attention-message">{item.message || '-'}</div>
              {renderMeta ? <div className="soha-vrt-attention-meta">{renderMeta(item)}</div> : null}
            </div>
          ))}
        </div>
      )}
    </section>
  )
}



function refreshVirtualization(queryClient: ReturnType<typeof useQueryClient>) {
  return queryClient.invalidateQueries({ queryKey: ['virtualization'] })
}

const VM_METRIC_COLOR_MAP: Record<string, string> = {
  cpu: compactMetricColors.cpu,
  memory: compactMetricColors.memory,
  networkRx: compactMetricColors.networkRx,
  networkTx: compactMetricColors.networkTx,
}

function vmMetricColor(key: string): string {
  return VM_METRIC_COLOR_MAP[key] ?? compactMetricColors.default
}

function VMMetricsChart({ data }: { data: VirtualizationVMMetrics }) {
  if (!data.ready || data.message) {
    return <Alert type="info" message={data.message || '当前暂无可用指标数据'} />
  }
  const series = data.series ?? []
  if (series.length === 0) {
    return <ManagementState bordered={false} compact title="暂无指标数据" description="Provider 暂未返回可展示的指标序列。" />
  }
  return (
    <div className="grid gap-4 md:grid-cols-2">
      {series.map((item) => {
        const points = (item.points ?? []).map((point) => ({
          timestamp: new Date(point.timestamp * 1000).toISOString(),
          value: point.value,
        }))
        const lines: CompactChartLine[] = [
          {
            color: vmMetricColor(item.key),
            fill: true,
            key: item.key,
            label: item.label,
            points,
            unit: item.unit,
          },
        ]
        const latest = points.length > 0 ? points[points.length - 1].value : null
        return (
          <Card key={item.key} size="small" title={item.label} extra={
            <Text type="secondary">
              最新: {latest !== null ? formatMetricValue(latest, item.unit) : '-'}
            </Text>
          }>
            <div style={{ height: 240 }}>
              <LineChart spec={buildCompactChartSpec(lines, item.unit, 'zh_CN')} />
            </div>
          </Card>
        )
      })}
    </div>
  )
}

interface TaskProgressBannerProps {
  task: VirtualizationOperation | null
  status: 'idle' | 'streaming' | 'done' | 'error'
  title: string
  onCancel?: () => void
  cancelling?: boolean
}

function TaskProgressBanner({ task, status, title, onCancel, cancelling }: TaskProgressBannerProps) {
  if (status === 'idle' || status === 'done') return null
  const isError = status === 'error'
  const description = task?.message || (isError ? '与服务器的实时连接已断开' : '正在等待任务完成...')
  const taskStatus = task?.status ? <Tag color={STATUS_COLORS[task.status] ?? 'blue'}>{task.status}</Tag> : null
  return (
    <Alert
      className="soha-vrt-task-banner"
      type={isError ? 'warning' : 'info'}
      showIcon
      icon={isError ? undefined : <Spin size="small" />}
      message={
        <Space>
          <span>{title}</span>
          {taskStatus}
        </Space>
      }
      description={description}
      action={
        onCancel && task?.id ? (
          <Button size="small" danger onClick={onCancel} loading={cancelling}>取消任务</Button>
        ) : null
      }
    />
  )
}



function VirtualizationFilterActions({
  loading,
  onReset,
}: {
  loading?: boolean
  onReset: () => void
}) {
  return (
    <Space size={8}>
      <Button type="primary" htmlType="submit" icon={<SearchOutlined />} loading={loading}>筛选</Button>
      <Button onClick={onReset}>重置</Button>
    </Space>
  )
}

function VirtualizationAdminTable({
  className,
  columnSettingIconOnly = true,
  columnSettingPlacement = 'header',
  shellClassName,
  tableSize = 'small',
  ...props
}: ComponentProps<typeof AdminTable>) {
  return (
    <AdminTable
      {...props}
      className={classNames('soha-vrt-table', className)}
      columnSettingIconOnly={columnSettingIconOnly}
      columnSettingPlacement={columnSettingPlacement}
      shellClassName={classNames('soha-management-table-shell', shellClassName)}
      tableSize={tableSize}
    />
  )
}



function pageTablePagination<T>(
  page: VirtualizationPage<T>,
  setFilters: React.Dispatch<React.SetStateAction<VirtualizationListParams>>,
) {
  return {
    current: page.page,
    pageSize: page.pageSize,
    total: page.total,
    onPageChange: (pageNumber: number) => setFilters((current) => ({ ...current, page: pageNumber })),
    onPageSizeChange: (pageSize: number) => setFilters((current) => ({ ...current, page: 1, pageSize })),
  }
}



function useVirtualizationPermissions() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  return {
    canManage: hasPermission(snapshot, 'virtualization.manage'),
    canManageVMs: hasPermission(snapshot, 'virtualization.vms.manage') || hasPermission(snapshot, 'virtualization.manage'),
    canManageClusters: hasPermission(snapshot, 'virtualization.clusters.manage') || hasPermission(snapshot, 'virtualization.manage'),
    canManageImages: hasPermission(snapshot, 'virtualization.images.manage') || hasPermission(snapshot, 'virtualization.manage'),
    canManageFlavors: hasPermission(snapshot, 'virtualization.flavors.manage') || hasPermission(snapshot, 'virtualization.manage'),
    canManageOperations: hasPermission(snapshot, 'virtualization.operations.manage') || hasPermission(snapshot, 'virtualization.manage'),
    canSync: hasPermission(snapshot, 'virtualization.sync.manage') || hasPermission(snapshot, 'virtualization.manage'),
    canViewMetrics: hasPermission(snapshot, 'virtualization.vms.metrics') || hasPermission(snapshot, 'virtualization.vms.view') || hasPermission(snapshot, 'virtualization.manage'),
    canAccessConsole: hasPermission(snapshot, 'virtualization.vms.console') || hasPermission(snapshot, 'virtualization.vms.view') || hasPermission(snapshot, 'virtualization.manage'),
  }
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



function OperationsTable({
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
  const { canManageOperations } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const operationsQuery = useQuery({
    queryKey: ['virtualization', 'operations', assetType ?? 'all', parsedSearch.query],
    queryFn: () => virtualizationApi.operations({
      assetType: assetType ?? parsedSearch.query.assetType,
      taskKind: assetType ? undefined : parsedSearch.query.taskKind,
      abnormal: parsedSearch.query.abnormal,
      pending: parsedSearch.query.pending,
      statuses: parsedSearch.query.statuses,
      connectionId: parsedSearch.query.connectionId,
      vmId: parsedSearch.query.vmId,
      search: parsedSearch.query.search,
    } as Parameters<typeof virtualizationApi.operations>[0]),
  })
  const logsQuery = useQuery({
    queryKey: ['virtualization', 'operations', selectedOperation?.id, 'logs'],
    queryFn: () => virtualizationApi.operationLogs(selectedOperation?.id ?? ''),
    enabled: Boolean(selectedOperation?.id),
  })

  useEffect(() => {
    setPreset(assetType ? 'asset_sync' : parsedSearch.preset || initialPreset)
  }, [assetType, initialPreset, parsedSearch.preset])

  const operations = operationsQuery.data?.data ?? []
  const hasServerFilters = Boolean(parsedSearch.query.abnormal || parsedSearch.query.pending || parsedSearch.query.connectionId || parsedSearch.query.vmId || parsedSearch.query.search || parsedSearch.query.statuses?.length || parsedSearch.query.taskKind || parsedSearch.query.assetType)
  const filteredOperations = useMemo(() => (hasServerFilters ? operations : buildOperationFilter(operations, preset)), [hasServerFilters, operations, preset])
  const logs = logsQuery.data?.data ?? []
  const cancelMutation = useMutation({
    mutationFn: virtualizationApi.cancelOperation,
    onSuccess: () => {
      message.success('取消请求已提交')
      refreshVirtualization(queryClient)
    },
  })
  const retryMutation = useMutation({
    mutationFn: virtualizationApi.retryOperation,
    onSuccess: () => {
      message.success('重试任务已提交')
      refreshVirtualization(queryClient)
    },
  })
  const batchCancelMutation = useMutation({
    mutationFn: async (ids: string[]) => Promise.all(ids.map((id) => virtualizationApi.cancelOperation(id))),
    onSuccess: (_response, ids) => {
      message.success(`已提交 ${ids.length} 个任务的取消请求`)
      setSelectedTaskRowKeys([])
      refreshVirtualization(queryClient)
    },
  })
  const batchRetryMutation = useMutation({
    mutationFn: async (ids: string[]) => Promise.all(ids.map((id) => virtualizationApi.retryOperation(id))),
    onSuccess: (_response, ids) => {
      message.success(`已提交 ${ids.length} 个任务的重试请求`)
      setSelectedTaskRowKeys([])
      refreshVirtualization(queryClient)
    },
  })
  const columns: ColumnsType<VirtualizationOperation> = [
    { title: '类型', dataIndex: 'operationType', render: (_value, record) => operationKind(record), width: 150 },
    { title: '资源', dataIndex: 'targetName', render: (value, record) => value || record.targetType || record.assetType || '-', width: 160 },
    { title: '连接', dataIndex: 'connectionName', render: (value, record) => value || record.connectionId || '-', width: 160 },
    { ...tableColumnPresets.status, title: '状态', dataIndex: 'status', render: statusTag, width: 120 },
    { title: '异常摘要', dataIndex: 'message', render: (_value, record) => latestNonEmptyOperationMessage(record), ellipsis: true },
    { title: '运行时长', render: (_value, record) => formatOperationDuration(record), width: 140 },
    { ...tableColumnPresets.datetime, title: '最近心跳', dataIndex: 'lastHeartbeatAt', render: formatDateTime, width: 180 },
    { ...tableColumnPresets.datetime, title: '开始时间', dataIndex: 'startedAt', render: (_value, record) => formatDateTime(operationTime(record)), width: 180 },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      width: 176,
      render: (_value, record) => {
        const canCancel = canManageOperations && hasAllowedAction(record.allowedActions, 'cancel')
        const canRetry = canManageOperations && hasAllowedAction(record.allowedActions, 'retry')
        return (
          <Space className="soha-row-action-icons" wrap>
            <ManagementIconButton aria-label="查看日志" size="small" tooltip="日志" icon={<FileTextOutlined />} onClick={() => setSelectedOperation(record)} />
            <ManagementIconButton
              aria-label="AI 调查"
              size="small"
              tooltip="AI 调查"
              icon={<SearchOutlined />}
              onClick={() => navigate(buildInvestigationPath({
                connectionId: record.connectionId,
                vmId: record.vmId,
                workload: record.targetName || record.vmId || record.connectionId,
                timeRangeMinutes: 60,
              }))}
            />
            {record.vmId ? <ManagementIconButton aria-label="查看虚拟机" size="small" tooltip="VM" icon={<FileTextOutlined />} onClick={() => navigate(`/virtualization/vms/${encodeURIComponent(record.vmId || '')}?focus=operations`)} /> : null}
            {canCancel ? (
              <Popconfirm title="确认取消任务？" onConfirm={() => cancelMutation.mutate(record.id)}>
                <ManagementIconButton aria-label="取消任务" size="small" tooltip="取消" danger icon={<PoweroffOutlined />} />
              </Popconfirm>
            ) : null}
            {canRetry ? <ManagementIconButton aria-label="重试任务" size="small" tooltip="重试" icon={<ReloadOutlined />} onClick={() => retryMutation.mutate(record.id)} /> : null}
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
    { key: 'pending', label: '待处理', value: counts.pending, tone: counts.pending > 0 ? 'warning' : 'default' },
    { key: 'abnormal', label: '失败/超时', value: counts.abnormal, tone: counts.abnormal > 0 ? 'danger' : 'default' },
    { key: 'sync', label: '同步任务', value: counts.sync },
    { key: 'vm', label: 'VM 任务', value: counts.vm },
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
          actions={(
            <Space size={8}>
              <Button onClick={resetOperationFilters}>重置</Button>
              <Button icon={<ReloadOutlined />} loading={operationsQuery.isFetching} onClick={() => operationsQuery.refetch()}>刷新</Button>
            </Space>
          )}
        >
          <ManagementQueryField label="任务视图" minWidth={300} width={360}>
            {assetType === 'asset_sync' ? (
              <Tag color="blue">同步任务</Tag>
            ) : (
              <Segmented
                size="small"
                value={preset}
                options={OPERATION_FILTER_PRESETS.map((item) => ({ label: item.label, value: item.key }))}
                onChange={(value) => selectPreset(value as OperationFilterPreset)}
              />
            )}
          </ManagementQueryField>
          <ManagementQueryField grow label="任务统计" minWidth={420} width={560}>
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
        headerExtra={toolbarExtra ? <ManagementTableToolbar>{toolbarExtra}</ManagementTableToolbar> : null}
        toolbarExtra={selectedTaskRowKeys.length > 0 ? (
          <div className="soha-vrt-selection-bar">
            <Text type="secondary">已选择 {selectedTaskRowKeys.length} 个任务</Text>
            <Space wrap>
              {canManageOperations ? (
                <Popconfirm
                  title="确认批量取消任务？"
                  description={bulkActionSummary('将取消', filteredOperations.filter((record) => selectedTaskRowKeys.includes(record.id)).map((record) => record.targetName || record.id))}
                  onConfirm={() => batchCancelMutation.mutate(selectedTaskRowKeys.map(String))}
                >
                  <Button danger disabled={selectedTaskRowKeys.some((id) => !selectableOperationIds(filteredOperations, 'cancel').includes(String(id)))} loading={batchCancelMutation.isPending}>批量取消</Button>
                </Popconfirm>
              ) : null}
              {canManageOperations ? (
                <Popconfirm
                  title="确认批量重试任务？"
                  description={bulkActionSummary('将重试', filteredOperations.filter((record) => selectedTaskRowKeys.includes(record.id)).map((record) => record.targetName || record.id))}
                  onConfirm={() => batchRetryMutation.mutate(selectedTaskRowKeys.map(String))}
                >
                  <Button type="primary" disabled={selectedTaskRowKeys.some((id) => !selectableOperationIds(filteredOperations, 'retry').includes(String(id)))} loading={batchRetryMutation.isPending}>批量重试</Button>
                </Popconfirm>
              ) : null}
              <Button onClick={() => setSelectedTaskRowKeys([])}>清空选择</Button>
            </Space>
          </div>
        ) : null}
        rowSelection={{
          selectedRowKeys: selectedTaskRowKeys,
          onChange: (keys: Key[]) => setSelectedTaskRowKeys(keys),
        }}
        loading={operationsQuery.isLoading}
        dataSource={filteredOperations}
        columns={columns}
        paginationSummary={localTableSummary(filteredOperations.length, operations.length)}
        scroll={{ x: 1360 }}
      />
      <Drawer
        title="任务日志"
        size="large"
        motion={stableDrawerMotion}
        open={Boolean(selectedOperation)}
        onClose={() => setSelectedOperation(null)}
      >
        <Descriptions size="small" column={1} bordered>
          <Descriptions.Item label="任务 ID">{selectedOperation?.id}</Descriptions.Item>
          <Descriptions.Item label="类型">{selectedOperation ? operationKind(selectedOperation) : '-'}</Descriptions.Item>
          <Descriptions.Item label="状态">{statusTag(selectedOperation?.status)}</Descriptions.Item>
          <Descriptions.Item label="资源">{selectedOperation?.targetName || selectedOperation?.targetType || '-'}</Descriptions.Item>
          <Descriptions.Item label="连接">{selectedOperation?.connectionName || selectedOperation?.connectionId || '-'}</Descriptions.Item>
          <Descriptions.Item label="VM">{selectedOperation?.vmId || '-'}</Descriptions.Item>
          <Descriptions.Item label="开始时间">{formatDateTime(selectedOperation?.startedAt || selectedOperation?.createdAt)}</Descriptions.Item>
          <Descriptions.Item label="最近心跳">{formatDateTime(selectedOperation?.lastHeartbeatAt)}</Descriptions.Item>
          <Descriptions.Item label="完成时间">{formatDateTime(selectedOperation?.completedAt)}</Descriptions.Item>
        </Descriptions>
        {selectedOperation?.message ? <Alert className="mt-4" type={isAbnormalOperation(selectedOperation.status) ? 'error' : 'info'} message={selectedOperation.message} /> : null}
        <div className="mt-4 flex justify-end">
          <Button size="small" onClick={async () => {
            const text = (logs.length
              ? logs.map((item) => `[${formatDateTime(item.createdAt)}] ${item.logLevel ?? 'info'} ${item.message}`).join('\n')
              : selectedOperation?.logs?.length
                ? selectedOperation.logs.join('\n')
                : selectedOperation?.logText) || selectedOperation?.message || ''
            if (!text) return
            await navigator.clipboard.writeText(text)
            message.success('日志已复制')
          }}>
            复制日志
          </Button>
        </div>
        <pre className="mt-4 max-h-[520px] overflow-auto rounded border border-[var(--soha-border)] bg-[var(--soha-surface-muted)] p-3 text-xs">
          {(logs.length
            ? logs.map((item) => `[${formatDateTime(item.createdAt)}] ${item.logLevel ?? 'info'} ${item.message}`).join('\n')
            : selectedOperation?.logs?.length
              ? selectedOperation.logs.join('\n')
              : selectedOperation?.logText) || selectedOperation?.message || (logsQuery.isLoading ? '日志加载中' : '暂无日志')}
        </pre>
      </Drawer>
    </>
  )
}


export function VirtualizationOverviewPage() {
  const { canManageClusters, canManageOperations, canSync } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { message } = App.useApp()
  const [syncTaskId, setSyncTaskId] = useState<string | null>(null)
  const [selectedOperation, setSelectedOperation] = useState<VirtualizationOperation | null>(null)
  const [selectedCluster, setSelectedCluster] = useState<VirtualizationCluster | null>(null)
  const { task: syncTask, status: syncStreamStatus } = useTaskStream(syncTaskId)

  useEffect(() => {
    if (syncStreamStatus === 'done') {
      const success = syncTask?.status === 'completed'
      message[success ? 'success' : 'error'](success ? '同步完成' : `同步失败: ${syncTask?.message ?? '未知错误'}`)
      setSyncTaskId(null)
      refreshVirtualization(queryClient)
    }
  }, [syncStreamStatus, syncTask, message, queryClient])

  const cancelSyncMutation = useMutation({
    mutationFn: virtualizationApi.cancelOperation,
    onSuccess: () => {
      message.info('已请求取消同步任务')
    },
  })

  const overviewQuery = useQuery({
    queryKey: ['virtualization', 'overview'],
    queryFn: virtualizationApi.overview,
  })
  const logsQuery = useQuery({
    queryKey: ['virtualization', 'operations', selectedOperation?.id, 'logs', 'overview'],
    queryFn: () => virtualizationApi.operationLogs(selectedOperation?.id ?? ''),
    enabled: Boolean(selectedOperation?.id),
  })
  const syncMutation = useMutation({
    mutationFn: virtualizationApi.syncAll,
    onSuccess: (response) => {
      const taskId = response?.data?.id
      if (taskId) {
        message.info('同步任务已提交，正在跟踪进度...')
        setSyncTaskId(taskId)
      } else {
        message.success('同步任务已提交')
      }
      refreshVirtualization(queryClient)
    },
  })
  const retryMutation = useMutation({
    mutationFn: virtualizationApi.retryOperation,
    onSuccess: () => {
      message.success('重试任务已提交')
      refreshVirtualization(queryClient)
    },
  })
  const testMutation = useMutation({
    mutationFn: virtualizationApi.testCluster,
    onSuccess: () => {
      message.success('测试任务已提交')
      refreshVirtualization(queryClient)
    },
  })
  const syncClusterMutation = useMutation({
    mutationFn: virtualizationApi.syncCluster,
    onSuccess: () => {
      message.success('同步任务已提交')
      refreshVirtualization(queryClient)
    },
  })

  const overview: VirtualizationOverview = overviewQuery.data?.data ?? {}
  const stats = overview.stats ?? {}
  const health = stats.connections
  const operations = overview.recentOperations ?? []
  const attention = overview.attention
  const connectionSummary = overview.connectionSummary
  const taskSummary = overview.taskSummary
  const providerSummary = overview.providerSummary ?? []
  const abnormalClusters = useMemo(
    () => [...(attention?.riskyConnections ?? [])].sort((left, right) => clusterRiskScore(left) - clusterRiskScore(right)).slice(0, 5),
    [attention?.riskyConnections],
  )
  const failedSyncOperations = useMemo(
    () => attention?.failedSyncTasks ?? [],
    [attention?.failedSyncTasks],
  )
  const failedOperations = useMemo(
    () => attention?.failedOperations ?? [],
    [attention?.failedOperations],
  )
  const recentAbnormal = useMemo(
    () => (failedOperations.length ? failedOperations : operations.filter((record) => isAbnormalOperation(record.status))).slice(0, 8),
    [failedOperations, operations],
  )
  const logs = logsQuery.data?.data ?? []
  const totalConnections = connectionSummary?.total ?? health?.total ?? 0
  const healthyConnections = connectionSummary?.healthy ?? health?.healthy ?? 0
  const degradedConnections = connectionSummary?.degraded ?? health?.degraded ?? 0
  const unavailableConnections = connectionSummary?.unavailable ?? health?.unavailable ?? 0
  const unhealthyConnections = degradedConnections + unavailableConnections
  const pendingTasks = stats.pendingTaskCount ?? ((taskSummary?.queued ?? 0) + (taskSummary?.running ?? 0))
  const failedTaskTotal = stats.failedTaskCount ?? ((taskSummary?.failed ?? 0) + (taskSummary?.timeout ?? 0) || failedOperations.length)
  const runningVmCount = stats.runningVmCount ?? 0
  const vmCount = stats.vmCount ?? 0
  const hasInventory = totalConnections > 0 || vmCount > 0 || providerSummary.length > 0
  const lastSyncTone: OverviewTone = !overview.lastSyncTask
    ? (hasInventory ? 'warning' : 'default')
    : overview.lastSyncTask.status === 'completed'
      ? 'success'
      : isAbnormalOperation(overview.lastSyncTask.status)
        ? 'danger'
        : 'warning'
  const overviewTone: OverviewTone = !hasInventory
    ? 'default'
    : unhealthyConnections > 0 || failedTaskTotal > 0
      ? 'danger'
      : pendingTasks > 0 || lastSyncTone === 'warning'
        ? 'warning'
        : 'success'
  const overviewStatusLabel = overviewTone === 'danger'
    ? '存在风险'
    : overviewTone === 'warning'
      ? '需要关注'
      : overviewTone === 'success'
        ? '运行正常'
        : '待接入'
  const providerRows = useMemo(
    () => providerSummary.map((item) => {
      const connections = item.connections ?? 0
      const healthy = item.healthy ?? 0
      const degraded = item.degraded ?? 0
      const unavailable = item.unavailable ?? 0
      const healthPercent = connections > 0 ? Math.round((healthy / connections) * 100) : 0
      const tone: OverviewTone = unavailable > 0 ? 'danger' : degraded > 0 ? 'warning' : 'success'
      return { ...item, connections, healthy, degraded, unavailable, healthPercent, tone }
    }),
    [providerSummary],
  )
  const operationStats: Array<{ key: string; label: string; value: number; tone?: OverviewTone }> = [
    { key: 'credentialMissing', label: '凭证缺失', value: connectionSummary?.credentialMissing ?? 0, tone: (connectionSummary?.credentialMissing ?? 0) > 0 ? 'warning' : 'default' },
    { key: 'neverSynced', label: '从未同步', value: connectionSummary?.neverSynced ?? 0, tone: (connectionSummary?.neverSynced ?? 0) > 0 ? 'warning' : 'default' },
    { key: 'completed', label: '已完成任务', value: taskSummary?.completed ?? 0 },
    { key: 'providers', label: 'Provider 数', value: providerSummary.length },
  ]
  const metricStats: Array<{
    key: string
    label: string
    value: React.ReactNode
    helper: React.ReactNode
    tone: OverviewTone
    icon: React.ReactNode
    action: () => void
  }> = [
    {
      key: 'connections',
      label: '异常连接',
      value: unhealthyConnections,
      helper: `健康 ${healthyConnections} / 总计 ${totalConnections}`,
      tone: unhealthyConnections > 0 ? 'danger' : totalConnections > 0 ? 'success' : 'default',
      icon: unhealthyConnections > 0 ? <WarningOutlined /> : <CheckCircleOutlined />,
      action: () => navigate('/virtualization/clusters'),
    },
    {
      key: 'pending',
      label: '待处理任务',
      value: pendingTasks,
      helper: '排队与执行中任务',
      tone: pendingTasks > 0 ? 'warning' : 'default',
      icon: <CloudSyncOutlined />,
      action: () => navigate('/virtualization/operations?pending=true'),
    },
    {
      key: 'failed',
      label: '失败任务',
      value: failedTaskTotal,
      helper: '失败与回调超时',
      tone: failedTaskTotal > 0 ? 'danger' : 'default',
      icon: failedTaskTotal > 0 ? <CloseCircleOutlined /> : <CheckCircleOutlined />,
      action: () => navigate('/virtualization/operations?abnormal=true'),
    },
    {
      key: 'sync',
      label: '最近同步状态',
      value: overview.lastSyncTask?.status === 'completed' ? '正常' : overview.lastSyncTask?.status ? '异常' : '未同步',
      helper: overview.lastSyncTask ? `${operationKind(overview.lastSyncTask)} · ${formatDateTime(operationTime(overview.lastSyncTask))}` : '尚无同步记录',
      tone: lastSyncTone,
      icon: <ReloadOutlined />,
      action: () => navigate('/virtualization/sync'),
    },
    {
      key: 'vm',
      label: '运行中 VM',
      value: `${runningVmCount} / ${vmCount}`,
      helper: `停机 ${stats.stoppedVmCount ?? 0}`,
      tone: 'default',
      icon: <PlayCircleOutlined />,
      action: () => navigate('/virtualization/vms'),
    },
  ]

  return (
    <div className="soha-page soha-virtualization-overview">
      <ManagementDetailHeader
        title={(
          <Space size={8} wrap>
            <span>虚拟化总览</span>
            <Badge status={badgeStatusForTone(overviewTone)} text={overviewStatusLabel} />
          </Space>
        )}
        meta={(
          <>
            <span>连接 {healthyConnections}/{totalConnections}</span>
            <span>Provider {providerSummary.length}</span>
            <span>VM {runningVmCount}/{vmCount}</span>
            <span>最近同步 {overview.lastSyncTask ? formatDateTime(operationTime(overview.lastSyncTask)) : '未同步'}</span>
          </>
        )}
        actions={canSync ? (
          <>
            <Link to="/virtualization/sync">
              <Button icon={<CloudSyncOutlined />}>同步任务</Button>
            </Link>
            <Button type="primary" icon={<ReloadOutlined />} loading={syncMutation.isPending} onClick={() => syncMutation.mutate()}>
              立即同步
            </Button>
          </>
        ) : undefined}
      />
      <div className="soha-overview-metric-grid soha-vrt-metric-grid" aria-label="虚拟化运行指标">
        {metricStats.map((item) => (
          <Card key={item.key} size="small" variant="outlined" className={`soha-overview-metric-card soha-vrt-metric-card is-${item.tone}`}>
            <button type="button" className="soha-vrt-metric-card-button" onClick={item.action}>
              <div className="soha-overview-metric-card-head">
                <div className="soha-overview-metric-copy">
                  <Text className="soha-overview-metric-label">{item.label}</Text>
                  <span className="soha-vrt-stat-value">{item.value}</span>
                </div>
                <span className="soha-overview-metric-icon">{item.icon}</span>
              </div>
              <Text className="soha-overview-metric-helper">{item.helper}</Text>
            </button>
          </Card>
        ))}
      </div>
      <TaskProgressBanner
        task={syncTask}
        status={syncStreamStatus}
        title="正在同步虚拟化资源"
        onCancel={syncTask?.id ? () => cancelSyncMutation.mutate(syncTask.id) : undefined}
        cancelling={cancelSyncMutation.isPending}
      />

      <div className="soha-vrt-workbench-grid">
        <div className="soha-vrt-workbench-main">
          <div className="soha-vrt-lane-grid">
            <AttentionList
              title="高风险连接"
              description="不可用、降级、凭证缺失、未同步"
              emptyText="暂无高风险连接"
              tone={abnormalClusters.length > 0 ? 'danger' : 'default'}
              action={<ManagementIconButton aria-label="查看全部连接" tooltip="查看全部" icon={<EyeOutlined />} size="small" onClick={() => navigate('/virtualization/clusters')} />}
              items={abnormalClusters.map((item) => ({
                id: item.id,
                title: item.name,
                status: item.health || item.status,
                message: riskReasons(item).join(' / '),
              }))}
              renderMeta={(item) => {
                const cluster = abnormalClusters.find((record) => record.id === item.id)
                return cluster ? `Provider: ${cluster.provider || '-'} · 最近同步: ${formatDateTime(cluster.lastSyncedAt)}` : null
              }}
              renderActions={(item) => {
                const cluster = abnormalClusters.find((record) => record.id === item.id)
                if (!cluster) return null
                return (
                  <>
                    <ManagementIconButton aria-label="查看连接详情" tooltip="详情" icon={<EyeOutlined />} size="small" onClick={() => setSelectedCluster(cluster)} />
                    <ManagementIconButton aria-label="发起 AI 调查" tooltip="AI调查" icon={<PlayCircleOutlined />} size="small" onClick={() => navigate(buildInvestigationPath({ connectionId: cluster.id, provider: cluster.provider, clusterId: cluster.kubernetesClusterId, namespace: cluster.defaultNamespace, workload: cluster.name, timeRangeMinutes: 60 }))} />
                    {canManageClusters ? <ManagementIconButton aria-label="测试连接" tooltip="测试" icon={<ThunderboltOutlined />} size="small" onClick={() => testMutation.mutate(cluster.id)} loading={testMutation.isPending} /> : null}
                    {canSync ? <ManagementIconButton aria-label="同步连接" tooltip="同步" icon={<CloudSyncOutlined />} size="small" onClick={() => syncClusterMutation.mutate(cluster.id)} loading={syncClusterMutation.isPending} /> : null}
                  </>
                )
              }}
            />
            <AttentionList
              title="最近失败同步"
              description="asset_sync 失败或超时"
              emptyText="暂无失败同步"
              tone={failedSyncOperations.length > 0 ? 'danger' : 'default'}
              action={<ManagementIconButton aria-label="进入同步任务" tooltip="进入同步任务" icon={<CloudSyncOutlined />} size="small" onClick={() => navigate('/virtualization/sync')} />}
              items={failedSyncOperations.map((item) => ({ id: item.id, title: item.targetName || item.connectionId || item.id, status: item.status, message: latestNonEmptyOperationMessage(item) }))}
              renderMeta={(item) => {
                const operation = failedSyncOperations.find((record) => record.id === item.id)
                return operation ? `开始时间: ${formatDateTime(operationTime(operation))}` : null
              }}
              renderActions={(item) => {
                const operation = failedSyncOperations.find((record) => record.id === item.id)
                return operation ? (
                  <>
                    <ManagementIconButton aria-label="查看同步日志" tooltip="日志" icon={<FileTextOutlined />} size="small" onClick={() => setSelectedOperation(operation)} />
                    <ManagementIconButton aria-label="发起 AI 调查" tooltip="AI调查" icon={<PlayCircleOutlined />} size="small" onClick={() => navigate(buildInvestigationPath({ connectionId: operation.connectionId, vmId: operation.vmId, workload: operation.targetName || operation.vmId || operation.connectionId, timeRangeMinutes: 60 }))} />
                    {canManageOperations && hasAllowedAction(operation.allowedActions, 'retry') ? <ManagementIconButton aria-label="重试同步任务" tooltip="重试" icon={<ReloadOutlined />} size="small" onClick={() => retryMutation.mutate(operation.id)} loading={retryMutation.isPending} /> : null}
                  </>
                ) : null
              }}
            />
            <AttentionList
              title="失败与超时任务"
              description="生命周期任务失败或超时"
              emptyText="暂无失败任务"
              tone={failedOperations.length > 0 ? 'danger' : 'default'}
              action={<ManagementIconButton aria-label="进入任务中心" tooltip="任务中心" icon={<FileTextOutlined />} size="small" onClick={() => navigate('/virtualization/operations')} />}
              items={failedOperations.map((item) => ({ id: item.id, title: item.targetName || item.vmId || item.id, status: item.status, message: latestNonEmptyOperationMessage(item) }))}
              renderMeta={(item) => {
                const operation = failedOperations.find((record) => record.id === item.id)
                return operation ? `类型: ${operationKind(operation)} · 连接: ${operation.connectionId || '-'}` : null
              }}
              renderActions={(item) => {
                const operation = failedOperations.find((record) => record.id === item.id)
                return operation ? (
                  <>
                    <ManagementIconButton aria-label="查看任务日志" tooltip="日志" icon={<FileTextOutlined />} size="small" onClick={() => setSelectedOperation(operation)} />
                    <ManagementIconButton aria-label="发起 AI 调查" tooltip="AI调查" icon={<PlayCircleOutlined />} size="small" onClick={() => navigate(buildInvestigationPath({ connectionId: operation.connectionId, vmId: operation.vmId, workload: operation.targetName || operation.vmId || operation.connectionId, timeRangeMinutes: 60 }))} />
                    {operation.vmId ? <ManagementIconButton aria-label="查看 VM" tooltip="VM" icon={<EyeOutlined />} size="small" onClick={() => navigate(`/virtualization/vms/${encodeURIComponent(operation.vmId || '')}?focus=operations`)} /> : null}
                  </>
                ) : null
              }}
            />
          </div>

          <section className="soha-vrt-panel soha-vrt-table-panel">
            <div className="soha-vrt-panel-head">
              <div>
                <div className="soha-vrt-panel-title">任务流水</div>
                <div className="soha-vrt-panel-caption">最近操作与异常快照</div>
              </div>
              <ManagementIconButton aria-label="查看全部任务" tooltip="查看全部" icon={<EyeOutlined />} size="small" onClick={() => navigate('/virtualization/operations')} />
            </div>
            <div className="soha-vrt-panel-body">
              <AdminTable
                rowKey="id"
                tableSize="small"
                pagination={false}
                loading={overviewQuery.isLoading}
                dataSource={operations.length > 0 ? operations.slice(0, 8) : recentAbnormal}
                className="soha-vrt-overview-table"
                empty="暂无任务记录"
                enableColumnSelection={false}
                scroll={{ x: 920 }}
                columns={[
                  { title: '类型', render: (_value: unknown, record: VirtualizationOperation) => operationKind(record), width: 150 },
                  { title: '资源', dataIndex: 'targetName', render: (value: string, record: VirtualizationOperation) => value || record.targetType || '-', width: 180 },
                  { title: '连接', dataIndex: 'connectionId', render: (value: string) => value || '-', width: 180 },
                  { title: '状态', dataIndex: 'status', render: statusTag, width: 120 },
                  { title: '摘要', render: (_value: unknown, record: VirtualizationOperation) => latestNonEmptyOperationMessage(record), ellipsis: true },
                  { title: '时间', render: (_value: unknown, record: VirtualizationOperation) => formatDateTime(operationTime(record)), width: 180 },
                  {
                    ...tableColumnPresets.action,
                    title: '操作',
                    render: (_value: unknown, record: VirtualizationOperation) => (
                      <ManagementIconButton
                        aria-label="查看任务日志"
                        tooltip="日志"
                        icon={<FileTextOutlined />}
                        size="small"
                        onClick={() => setSelectedOperation(record)}
                      />
                    ),
                    width: 100,
                  },
                ]}
              />
            </div>
          </section>
        </div>

        <aside className="soha-vrt-side-stack">
          <section className="soha-vrt-panel">
            <div className="soha-vrt-panel-head">
              <div>
                <div className="soha-vrt-panel-title">连接健康态势</div>
                <div className="soha-vrt-panel-caption">纳管连接状态</div>
              </div>
              <ManagementIconButton aria-label="打开连接页" tooltip="连接页" icon={<EyeOutlined />} size="small" onClick={() => navigate('/virtualization/clusters')} />
            </div>
            <div className="soha-vrt-panel-body">
              <OperationStatusChips compact counts={[
                { key: 'healthy', label: '健康连接', value: healthyConnections, tone: healthyConnections > 0 ? 'success' : 'default' },
                { key: 'degraded', label: '降级连接', value: degradedConnections, tone: degradedConnections > 0 ? 'warning' : 'default' },
                { key: 'unavailable', label: '不可用连接', value: unavailableConnections, tone: unavailableConnections > 0 ? 'danger' : 'default' },
                { key: 'neverSynced', label: '未同步连接', value: connectionSummary?.neverSynced ?? 0, tone: (connectionSummary?.neverSynced ?? 0) > 0 ? 'warning' : 'default' },
              ]} />
            </div>
          </section>

          <section className="soha-vrt-panel">
            <div className="soha-vrt-panel-head">
              <div>
                <div className="soha-vrt-panel-title">任务处置态势</div>
                <div className="soha-vrt-panel-caption">队列、执行、失败</div>
              </div>
              <ManagementIconButton aria-label="打开任务中心" tooltip="任务中心" icon={<FileTextOutlined />} size="small" onClick={() => navigate('/virtualization/operations')} />
            </div>
            <div className="soha-vrt-panel-body">
              <OperationStatusChips compact counts={[
                { key: 'queued', label: '排队中', value: taskSummary?.queued ?? 0, tone: (taskSummary?.queued ?? 0) > 0 ? 'warning' : 'default' },
                { key: 'running', label: '执行中', value: taskSummary?.running ?? 0, tone: (taskSummary?.running ?? 0) > 0 ? 'warning' : 'default' },
                { key: 'failed', label: '失败/超时', value: (taskSummary?.failed ?? 0) + (taskSummary?.timeout ?? 0), tone: ((taskSummary?.failed ?? 0) + (taskSummary?.timeout ?? 0)) > 0 ? 'danger' : 'default' },
                { key: 'sync', label: '最近同步任务', value: overview.lastSyncTask ? 1 : 0, tone: overview.lastSyncTask ? lastSyncTone : 'default' },
              ]} />
            </div>
          </section>

          <section className="soha-vrt-panel">
            <div className="soha-vrt-panel-head">
              <div>
                <div className="soha-vrt-panel-title">Provider 分布</div>
                <div className="soha-vrt-panel-caption">连接健康与 VM 覆盖</div>
              </div>
            </div>
            <div className="soha-vrt-panel-body">
              {providerRows.length > 0 ? (
                <div className="soha-vrt-provider-list">
                  {providerRows.map((item) => (
                    <div key={item.provider} className={`soha-vrt-provider-row is-${item.tone}`}>
                      <div className="soha-vrt-provider-row-head">
                        <Text strong>{item.provider.toUpperCase()}</Text>
                        <Text type="secondary">{item.runningVms ?? 0}/{item.vms ?? 0} VM</Text>
                      </div>
                      <Progress
                        percent={item.healthPercent}
                        showInfo={false}
                        size="small"
                        status={item.tone === 'danger' ? 'exception' : item.tone === 'success' ? 'success' : 'normal'}
                        strokeColor={item.tone === 'warning' ? '#f97316' : undefined}
                      />
                      <div className="soha-vrt-provider-meta">
                        <span>健康 {item.healthy}</span>
                        <span>降级 {item.degraded}</span>
                        <span>不可用 {item.unavailable}</span>
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="soha-vrt-empty">
                  <ManagementState bordered={false} compact title="暂无 Provider 数据" description="虚拟化连接同步完成后会在这里展示 Provider 分布。" />
                </div>
              )}
            </div>
          </section>

          <section className="soha-vrt-panel">
            <div className="soha-vrt-panel-head">
              <div>
                <div className="soha-vrt-panel-title">运维统计摘要</div>
                <div className="soha-vrt-panel-caption">凭证、同步、Provider</div>
              </div>
            </div>
            <div className="soha-vrt-panel-body">
              <OperationStatusChips compact counts={operationStats} />
            </div>
          </section>
        </aside>
      </div>
      <Drawer title="异常任务详情" size="large" motion={stableDrawerMotion} open={Boolean(selectedOperation)} onClose={() => setSelectedOperation(null)}>
        <Descriptions size="small" column={1} bordered>
          <Descriptions.Item label="任务 ID">{selectedOperation?.id}</Descriptions.Item>
          <Descriptions.Item label="类型">{selectedOperation ? operationKind(selectedOperation) : '-'}</Descriptions.Item>
          <Descriptions.Item label="状态">{statusTag(selectedOperation?.status)}</Descriptions.Item>
          <Descriptions.Item label="资源">{selectedOperation?.targetName || selectedOperation?.targetType || '-'}</Descriptions.Item>
          <Descriptions.Item label="连接">{selectedOperation?.connectionId || '-'}</Descriptions.Item>
          <Descriptions.Item label="VM">{selectedOperation?.vmId || '-'}</Descriptions.Item>
          <Descriptions.Item label="开始时间">{formatDateTime(selectedOperation?.startedAt || selectedOperation?.createdAt)}</Descriptions.Item>
          <Descriptions.Item label="最近心跳">{formatDateTime(selectedOperation?.lastHeartbeatAt)}</Descriptions.Item>
        </Descriptions>
        {selectedOperation?.message ? <Alert className="mt-4" type={isAbnormalOperation(selectedOperation.status) ? 'error' : 'info'} message={selectedOperation.message} /> : null}
        <pre className="mt-4 max-h-[520px] overflow-auto rounded border border-[var(--soha-border)] bg-[var(--soha-surface-muted)] p-3 text-xs">
          {(logs.length
            ? logs.map((item) => `[${formatDateTime(item.createdAt)}] ${item.logLevel ?? 'info'} ${item.message}`).join('\n')
            : selectedOperation?.message) || (logsQuery.isLoading ? '日志加载中' : '暂无日志')}
        </pre>
      </Drawer>
      <Drawer title="连接风险详情" size="large" motion={stableDrawerMotion} open={Boolean(selectedCluster)} onClose={() => setSelectedCluster(null)}>
        <Descriptions size="small" column={1} bordered>
          <Descriptions.Item label="连接名称">{selectedCluster?.name || '-'}</Descriptions.Item>
          <Descriptions.Item label="Provider">{selectedCluster?.provider || '-'}</Descriptions.Item>
          <Descriptions.Item label="健康状态">{statusTag(selectedCluster?.health || selectedCluster?.status)}</Descriptions.Item>
          <Descriptions.Item label="风险等级">{selectedCluster?.riskLevel || '-'}</Descriptions.Item>
          <Descriptions.Item label="风险原因">{selectedCluster ? riskReasons(selectedCluster).join(' / ') || '正常' : '-'}</Descriptions.Item>
          <Descriptions.Item label="接入目标">{selectedCluster?.provider === 'kubevirt' ? selectedCluster?.kubernetesClusterId || '-' : selectedCluster?.endpoint || '-'}</Descriptions.Item>
          <Descriptions.Item label="默认命名空间">{selectedCluster?.defaultNamespace || '-'}</Descriptions.Item>
          <Descriptions.Item label="最近同步">{formatDateTime(selectedCluster?.lastSyncedAt)}</Descriptions.Item>
        </Descriptions>
        <Space className="mt-4">
          {selectedCluster && canManageClusters ? <Button onClick={() => testMutation.mutate(selectedCluster.id)} loading={testMutation.isPending}>测试连接</Button> : null}
          {selectedCluster && canSync ? <Button onClick={() => syncClusterMutation.mutate(selectedCluster.id)} loading={syncClusterMutation.isPending}>重新同步</Button> : null}
          {selectedCluster ? <Button onClick={() => navigate(buildInvestigationPath({ connectionId: selectedCluster.id, provider: selectedCluster.provider, clusterId: selectedCluster.kubernetesClusterId, namespace: selectedCluster.defaultNamespace, workload: selectedCluster.name, timeRangeMinutes: 60 }))}>AI调查</Button> : null}
          {selectedCluster ? <Button onClick={() => navigate('/virtualization/clusters')}>前往连接页</Button> : null}
        </Space>
      </Drawer>
    </div>
  )
}

export function VirtualizationVmsPage() {
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [filters, setFilters] = useState<VirtualizationListParams>({ page: 1, pageSize: 10 })
  const [filterForm] = Form.useForm<VirtualizationListParams>()
  const [form] = Form.useForm<VirtualMachineFormValues>()
  const [pendingTaskId, setPendingTaskId] = useState<string | null>(null)
  const { canManageVMs } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const createProvider = Form.useWatch('provider', form) ?? 'kubevirt'
  const createSourceMode = Form.useWatch('sourceMode', form) ?? (createProvider === 'pve' ? 'template_clone' : 'datasource_clone')
  const { task: streamedTask, status: streamStatus } = useTaskStream(pendingTaskId)

  useEffect(() => {
    if (streamStatus === 'done') {
      const success = streamedTask?.status === 'completed'
      message[success ? 'success' : 'error'](success ? '虚拟机创建完成' : `虚拟机创建失败: ${streamedTask?.message ?? '未知错误'}`)
      setPendingTaskId(null)
      refreshVirtualization(queryClient)
    }
  }, [streamStatus, streamedTask, message, queryClient])
  const cancelCreateMutation = useMutation({
    mutationFn: virtualizationApi.cancelOperation,
    onSuccess: () => {
      message.info('已请求取消创建任务')
    },
  })
  const vmsQuery = useQuery({
    queryKey: ['virtualization', 'vms', filters],
    queryFn: () => virtualizationApi.vms(filters),
  })
  const clustersQuery = useQuery({ queryKey: ['virtualization', 'clusters'], queryFn: virtualizationApi.clusters })
  const imagesQuery = useQuery({ queryKey: ['virtualization', 'images', 'create-options'], queryFn: () => virtualizationApi.images() })
  const flavorsQuery = useQuery({ queryKey: ['virtualization', 'flavors'], queryFn: virtualizationApi.flavors })
  const createMutation = useMutation({
    mutationFn: (values: VirtualMachineFormValues) => virtualizationApi.createVm(buildCreateVmPayload(values)),
    onSuccess: (response) => {
      const taskId = response?.data?.id
      if (taskId) {
        message.info('虚拟机创建任务已提交，正在跟踪进度...')
        setPendingTaskId(taskId)
      } else {
        message.success('虚拟机创建任务已提交')
      }
      setDrawerOpen(false)
      form.resetFields()
      refreshVirtualization(queryClient)
    },
  })
  const powerMutation = useMutation({
    mutationFn: ({ id, action }: { id: string; action: VirtualMachinePowerAction }) => virtualizationApi.powerVm(id, action),
    onSuccess: () => {
      message.success('电源操作已提交')
      refreshVirtualization(queryClient)
    },
  })
  const clusters = clustersQuery.data?.data ?? []
  const images = normalizePage(imagesQuery.data?.data, 1, 200).items
  const flavors = flavorsQuery.data?.data ?? []
  const pveNodeOptions = useMemo(() => images.filter((item) => item.provider === 'pve' && item.node).map((item) => item.node as string).filter((value, index, items) => items.indexOf(value) === index).map((value) => ({ value, label: value })), [images])
  const pveStorageOptions = useMemo(() => images.filter((item) => item.provider === 'pve' && item.assetKind === 'storage' && item.name).map((item) => item.name).filter((value, index, items) => items.indexOf(value) === index).map((value) => ({ value, label: value })), [images])
  const pveBridgeOptions = useMemo(() => ['vmbr0', 'vmbr1'].map((value) => ({ value, label: value })), [])
  const vmPage = normalizePage(vmsQuery.data?.data, filters.page ?? 1, filters.pageSize ?? 10)
  const selectedFlavorId = Form.useWatch('flavorId', form)
  const selectedFlavor = flavors.find((item) => item.id === selectedFlavorId)
  const columns: ColumnsType<VirtualMachine> = [
    {
      title: '名称',
      dataIndex: 'name',
      fixed: 'left',
      width: 190,
      render: (value, record) => <Link to={`/virtualization/vms/${encodeURIComponent(record.id)}`}>{value}</Link>,
    },
    { title: 'Provider', dataIndex: 'provider', render: (value) => value || '-' },
    { title: '连接', dataIndex: 'connectionName', render: (value, record) => value || record.connectionId || '-' },
    { title: '命名空间/节点', render: (_value, record) => [record.namespace, record.node].filter(Boolean).join(' / ') || '-' },
    { title: '电源', dataIndex: 'powerState', render: (value, record) => statusTag(value || record.status), width: 120 },
    { title: '规格', render: (_value, record) => record.flavorName || `${record.cpu ?? '-'}C / ${record.memoryMiB ?? '-'}MiB / ${record.diskGiB ?? '-'}GiB` },
    { title: '镜像', dataIndex: 'bootImageName', render: (value, record) => value || record.bootImageId || '-' },
    { title: '地址', dataIndex: 'ipAddresses', render: (value: string[]) => value?.join(', ') || '-' },
    { ...tableColumnPresets.datetime, title: '创建时间', dataIndex: 'createdAt', render: formatDateTime },
    {
      ...tableColumnPresets.action,
      title: '操作',
      width: 130,
      render: (_value, record) => {
        if (!canManageVMs) return null
        const canPower = (action: string) => !record.allowedActions || hasAllowedAction(record.allowedActions, action)
        return (
          <Space className="soha-row-action-icons">
            {canPower('start') ? <ManagementIconButton aria-label="启动虚拟机" size="small" tooltip="启动" icon={<PlayCircleOutlined />} onClick={() => powerMutation.mutate({ id: record.id, action: 'start' })} /> : null}
            {canPower('stop') ? <ManagementIconButton aria-label="停止虚拟机" size="small" tooltip="停止" icon={<PoweroffOutlined />} onClick={() => powerMutation.mutate({ id: record.id, action: 'stop' })} /> : null}
            {canPower('restart') ? <ManagementIconButton aria-label="重启虚拟机" size="small" tooltip="重启" icon={<ReloadOutlined />} onClick={() => powerMutation.mutate({ id: record.id, action: 'restart' })} /> : null}
            {canPower('delete') ? (
              <Popconfirm title="确认删除虚拟机？" onConfirm={() => powerMutation.mutate({ id: record.id, action: 'delete' })}>
                <ManagementIconButton aria-label="删除虚拟机" size="small" tooltip="删除" danger icon={<DeleteOutlined />} />
              </Popconfirm>
            ) : null}
          </Space>
        )
      },
    },
  ]

  return (
    <div className="soha-page soha-virtualization-page">
      <TaskProgressBanner
        task={streamedTask}
        status={streamStatus}
        title="正在创建虚拟机"
        onCancel={streamedTask?.id ? () => cancelCreateMutation.mutate(streamedTask.id) : undefined}
        cancelling={cancelCreateMutation.isPending}
      />
      <div className="soha-vrt-query soha-vrt-vms-query">
        <ManagementQueryPanel
          collapsible
          form={filterForm}
          actions={(
            <VirtualizationFilterActions
              loading={vmsQuery.isFetching}
              onReset={() => {
                filterForm.resetFields()
                setFilters((current) => ({ page: 1, pageSize: current.pageSize ?? 10 }))
              }}
            />
          )}
          onFinish={(values) => setFilters((current) => ({ ...current, ...values, page: 1 }))}
        >
          <ManagementQueryField minWidth={260} name="search" label="关键字" width={260}>
            <Input allowClear prefix={<SearchOutlined />} placeholder="搜索名称、IP 或节点" />
          </ManagementQueryField>
          <ManagementQueryField minWidth={180} name="connectionId" label="连接" width={180}>
            <Select
              allowClear
              showSearch
              optionFilterProp="label"
              placeholder="全部连接"
              options={clusters.map((item) => ({ value: item.id, label: item.name }))}
            />
          </ManagementQueryField>
          <ManagementQueryField minWidth={136} name="status" label="状态" width={136}>
            <Select
              allowClear
              placeholder="全部状态"
              options={['running', 'stopped', 'pending', 'failed'].map((item) => ({ value: item, label: item }))}
            />
          </ManagementQueryField>
          <ManagementQueryField minWidth={160} name="provider" label="Provider" width={160}>
            <Select allowClear placeholder="全部 Provider" options={VIRTUALIZATION_PROVIDER_OPTIONS} />
          </ManagementQueryField>
        </ManagementQueryPanel>
      </div>
      <VirtualizationAdminTable
        rowKey="id"
        headerExtra={canManageVMs ? (
          <ManagementTableToolbar>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setDrawerOpen(true)}>
              创建虚拟机
            </Button>
          </ManagementTableToolbar>
        ) : null}
        loading={vmsQuery.isLoading}
        dataSource={vmPage.items}
        columns={columns}
        scroll={{ x: 1340 }}
        pagination={pageTablePagination(vmPage, setFilters)}
        paginationSummary={virtualizationPageSummary}
      />
      <Drawer title="创建虚拟机" size="large" motion={stableDrawerMotion} open={drawerOpen} onClose={() => setDrawerOpen(false)}>
        <Form form={form} layout="vertical" initialValues={{ provider: 'kubevirt', sourceMode: 'datasource_clone', startAfterCreate: true }} onFinish={(values) => createMutation.mutate(values)}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="provider" label="Provider" rules={[{ required: true }]}>
              <Select options={[{ value: 'kubevirt', label: 'KubeVirt' }, { value: 'pve', label: 'PVE' }]} />
            </Form.Item>
            <Form.Item name="connectionId" label="连接" rules={[{ required: true }]}>
              <Select
                showSearch
                optionFilterProp="label"
                options={clusters
                  .filter((item) => !createProvider || item.provider === createProvider)
                  .map((item) => ({ value: item.id, label: item.name }))}
              />
            </Form.Item>
          </div>
          <Form.Item name="sourceMode" label="创建模式" rules={[{ required: true }]}>
            <Select
              options={createProvider === 'pve'
                ? [
                    { value: 'template_clone', label: '模板克隆' },
                    { value: 'iso_install', label: 'ISO 安装' },
                  ]
                : [
                    { value: 'datasource_clone', label: 'DataSource 克隆' },
                    { value: 'pvc_clone', label: 'PVC 克隆' },
                  ]}
            />
          </Form.Item>
          <Form.Item name="flavorId" label="规格" rules={[{ required: true }]}>
            <Select
              showSearch
              optionFilterProp="label"
              options={flavors.filter((item) => item.enabled !== false).map((item) => ({
                value: item.id,
                label: `${item.name} (${item.cpu}C / ${item.memoryMiB}MiB / ${item.diskGiB}GiB)`,
              }))}
            />
          </Form.Item>
          {selectedFlavor ? (
            <Alert
              className="mb-3"
              type="info"
              showIcon
              title={`已选择 ${selectedFlavor.name}: ${selectedFlavor.cpu}C / ${selectedFlavor.memoryMiB}MiB / ${selectedFlavor.diskGiB}GiB`}
            />
          ) : null}
          <Form.Item name="bootImageId" label={createProvider === 'pve' ? (createSourceMode === 'iso_install' ? '安装 ISO' : '模板') : '启动镜像'} rules={[{ required: true }]}>
            <Select
              showSearch
              optionFilterProp="label"
              options={images
                .filter((item) => !createProvider || item.provider === createProvider || !item.provider)
                .filter((item) => createProvider !== 'pve' || (createSourceMode === 'iso_install' ? item.assetKind === 'iso' || item.sourceKind === 'iso' : item.assetKind === 'template' || item.sourceKind === 'template'))
                .map((item) => ({ value: item.id, label: item.connectionName ? `${item.name} (${item.connectionName})` : item.name }))}
            />
          </Form.Item>
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="namespace" label="命名空间">
              <Input />
            </Form.Item>
            <Form.Item name="node" label="节点">
              {createProvider === 'pve' && pveNodeOptions.length > 0 ? <Select allowClear options={pveNodeOptions} /> : <Input disabled={createProvider === 'kubevirt'} placeholder={createProvider === 'kubevirt' ? '当前由集群调度' : undefined} />}
            </Form.Item>
          </div>
          <Form.Item name="network" label="网络">
            <Input disabled={createProvider === 'kubevirt'} placeholder={createProvider === 'kubevirt' ? '当前仅支持默认 Pod 网络' : undefined} />
          </Form.Item>
          {createProvider === 'pve' ? (
            <>
              <div className="grid gap-3 md:grid-cols-3">
                <Form.Item name="pveStorage" label="PVE 存储">
                  {pveStorageOptions.length > 0 ? <Select allowClear options={pveStorageOptions} /> : <Input placeholder="local-lvm" />}
                </Form.Item>
                <Form.Item name="pveBridge" label="PVE 网桥">
                  <Select allowClear options={pveBridgeOptions} placeholder="vmbr0" />
                </Form.Item>
                {createSourceMode === 'iso_install' ? (
                  <Form.Item name="pveIso" label="安装 ISO">
                    <Input placeholder="local:iso/ubuntu.iso" />
                  </Form.Item>
                ) : (
                  <Form.Item label="模板模式">
                    <Alert type="info" showIcon message="当前将按模板克隆模式创建 VM，启动镜像字段会作为模板来源。" />
                  </Form.Item>
                )}
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <Form.Item name="pveCloudInitUser" label="PVE Cloud-Init 用户名">
                  <Input placeholder="ubuntu" />
                </Form.Item>
                <Form.Item name="pveCloudInitSSHKeys" label="PVE Cloud-Init SSH Keys">
                  <Input.TextArea rows={3} placeholder="ssh-rsa AAAA..." />
                </Form.Item>
              </div>
            </>
          ) : (
            <div className="grid gap-3 md:grid-cols-2">
              <Form.Item name="kubevirtStorageClass" label="StorageClass">
                <Input placeholder="fast-ssd" />
              </Form.Item>
              {createSourceMode === 'pvc_clone' ? (
                <Form.Item name="kubevirtDataVolumeName" label="PVC 名称">
                  <Input placeholder="existing-root-pvc" />
                </Form.Item>
              ) : (
                <Form.Item name="kubevirtDataVolumeName" label="DataVolume 名称">
                  <Input placeholder="demo-rootdisk" />
                </Form.Item>
              )}
            </div>
          )}
          <Form.Item name="cloudInit" label={createProvider === 'pve' ? 'KubeVirt 以外场景不建议使用原始 Cloud Init 文本' : 'Cloud Init userData'}>
            <Input.TextArea rows={5} disabled={createProvider === 'pve'} placeholder={createProvider === 'pve' ? 'PVE 改用上方用户名 / SSH Keys 字段' : '#cloud-config'} />
          </Form.Item>
          <Form.Item name="startAfterCreate" label="创建后启动" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={createMutation.isPending}>提交</Button>
            <Button onClick={() => setDrawerOpen(false)}>取消</Button>
          </Space>
        </Form>
      </Drawer>
    </div>
  )
}

export function VirtualizationVmDetailPage() {
  const navigate = useNavigate()
  const { id } = useParams()
  const location = useLocation()
  const pathParts = location.pathname.split('/').filter(Boolean)
  const vmId = id ?? decodeURIComponent(pathParts[pathParts.length - 1] ?? '')
  const { canViewMetrics, canAccessConsole } = useVirtualizationPermissions()
  const [metricsRange, setMetricsRange] = useState(60)
  const [activeTab, setActiveTab] = useState(() => {
    const params = new URLSearchParams(location.search)
    if (params.get('focus') === 'operations' || params.get('focus') === 'logs') {
      return params.get('focus') || 'operations'
    }
    return 'raw'
  })
  const detailQuery = useQuery({
    queryKey: ['virtualization', 'vms', vmId, 'detail'],
    queryFn: () => virtualizationApi.vmDetail(vmId),
    enabled: Boolean(vmId),
  })
  const detail = detailQuery.data?.data
  const vm = detail?.vm
  const providerRaw = stringifyRaw(detail?.providerRaw)
  const sortedOperations = useMemo(() => {
    const records = [...(detail?.operations ?? [])]
    return records.sort((left, right) => {
      const leftAbnormal = isAbnormalOperation(left.status) ? 0 : 1
      const rightAbnormal = isAbnormalOperation(right.status) ? 0 : 1
      if (leftAbnormal !== rightAbnormal) return leftAbnormal - rightAbnormal
      return (operationTime(right) || '').localeCompare(operationTime(left) || '')
    })
  }, [detail?.operations])
  const latestAbnormalOperation = sortedOperations.find((item) => isAbnormalOperation(item.status))

  const isRunning = vm?.powerState === 'running' || vm?.status === 'running'

  const metricsQuery = useQuery({
    queryKey: ['virtualization', 'vm-metrics', vmId, metricsRange],
    queryFn: () => virtualizationApi.vmMetrics(vmId, metricsRange, metricsRange <= 60 ? 60 : 300),
    refetchInterval: 30000,
    enabled: Boolean(vmId) && isRunning,
  })

  return (
    <div className="soha-page soha-virtualization-page">
      <ManagementDetailHeader
        title={vm?.name ?? '虚拟机详情'}
        description={
          <Space size={8} wrap>
            <Badge
              status={badgeStatusForTone(latestAbnormalOperation ? 'danger' : isRunning ? 'success' : 'default')}
              text={vm?.powerState || vm?.status || (detailQuery.isLoading ? '加载中' : '-')}
            />
          </Space>
        }
        meta={
          <>
            <span>{`Provider ${vm?.provider ?? '-'}`}</span>
            <span>{`连接 ${vm?.connectionName || vm?.connectionId || '-'}`}</span>
            <span>{`节点 ${vm?.node || '-'}`}</span>
          </>
        }
        actions={<Link to="/virtualization/vms"><Button>返回列表</Button></Link>}
      />
      {!vm && !detailQuery.isLoading ? (
        <Card size="small" variant="outlined" className="soha-management-panel-card">
          <ManagementState compact kind="not-found" title="未找到虚拟机详情" description="目标虚拟机不存在，或当前账号无法访问该资源。" />
        </Card>
      ) : null}
      {latestAbnormalOperation ? (
        <Alert
          type="error"
          showIcon
          title={`最近异常任务：${operationKind(latestAbnormalOperation)}`}
          description={latestNonEmptyOperationMessage(latestAbnormalOperation)}
          action={<Space><Button size="small" onClick={() => setActiveTab('operations')}>查看任务历史</Button><Button size="small" onClick={() => navigate(buildInvestigationPath({ connectionId: vm?.connectionId, vmId: vm?.id, namespace: vm?.namespace, workload: vm?.name, timeRangeMinutes: 60 }))}>AI调查</Button></Space>}
        />
      ) : null}
      <Card size="small" variant="outlined" className="soha-management-panel-card" loading={detailQuery.isLoading}>
        <Descriptions size="small" column={{ xs: 1, md: 2, xl: 3 }} bordered>
          <Descriptions.Item label="ID">{vm?.id ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="Provider">{vm?.provider ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="连接">{vm?.connectionName || vm?.connectionId || '-'}</Descriptions.Item>
          <Descriptions.Item label="状态">{statusTag(vm?.powerState || vm?.status)}</Descriptions.Item>
          <Descriptions.Item label="命名空间">{vm?.namespace || '-'}</Descriptions.Item>
          <Descriptions.Item label="节点">{vm?.node || '-'}</Descriptions.Item>
          <Descriptions.Item label="规格">{vm?.flavorName || vm?.flavorId || '-'}</Descriptions.Item>
          <Descriptions.Item label="来源模式">{vm?.sourceMode || '-'}</Descriptions.Item>
          <Descriptions.Item label="来源引用">{vm?.sourceRef || '-'}</Descriptions.Item>
          <Descriptions.Item label="来源资产">{detail?.image?.name || vm?.bootImageName || vm?.bootImageId || '-'}</Descriptions.Item>
          <Descriptions.Item label="资产类型">{detail?.image?.assetKind || detail?.image?.sourceKind || '-'}</Descriptions.Item>
          <Descriptions.Item label="StorageClass / 存储">{detail?.image ? [detail.image.storageClass, detail.image.storage].filter(Boolean).join(' / ') || '-' : '-'}</Descriptions.Item>
          <Descriptions.Item label="CPU">{vm?.cpu ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="内存">{vm?.memoryMiB ? `${vm.memoryMiB} MiB` : '-'}</Descriptions.Item>
          <Descriptions.Item label="磁盘">{vm?.diskGiB ? `${vm.diskGiB} GiB` : '-'}</Descriptions.Item>
          <Descriptions.Item label="镜像">{vm?.bootImageName || vm?.bootImageId || '-'}</Descriptions.Item>
          <Descriptions.Item label="网络">{vm?.network || '-'}</Descriptions.Item>
          <Descriptions.Item label="IP">{vm?.ipAddresses?.join(', ') || '-'}</Descriptions.Item>
          <Descriptions.Item label="创建时间">{formatDateTime(vm?.createdAt)}</Descriptions.Item>
          <Descriptions.Item label="更新时间">{formatDateTime(vm?.updatedAt)}</Descriptions.Item>
        </Descriptions>
        <div className="mt-4 grid gap-3 md:grid-cols-2">
          <Card size="small" title="Console 能力摘要">
            <Text type="secondary">{canAccessConsole ? `已启用控制台入口（Provider: ${vm?.provider || '-'})，运行中时可尝试建立连接。` : '当前角色无控制台权限。'}</Text>
          </Card>
          <Card size="small" title="Metrics 能力摘要">
            <Text type="secondary">{canViewMetrics ? `已启用指标入口（Provider: ${vm?.provider || '-'})，是否可用取决于 provider 与连接配置。` : '当前角色无指标查看权限。'}</Text>
          </Card>
        </div>
      </Card>
      <Tabs
        activeKey={activeTab}
        onChange={setActiveTab}
        items={[
          {
            key: 'raw',
            label: 'Provider Raw',
            forceRender: true,
            children: (
              <Card size="small">
                <pre className="max-h-[520px] overflow-auto rounded border border-[var(--soha-border)] bg-[var(--soha-surface-muted)] p-3 text-xs">
                  {providerRaw || '暂无 provider raw 数据'}
                </pre>
              </Card>
            ),
          },
          {
            key: 'operations',
            label: '任务历史',
            forceRender: true,
            children: (
              <Card size="small">
                <AdminTable
                  rowKey="id"
                  tableSize="small"
                  dataSource={sortedOperations}
                  pageSize={10}
                  columnSettingIconOnly
                  columnSettingPlacement="header"
                  columns={[
                    { title: '类型', render: (_value: unknown, record: VirtualizationOperation) => operationKind(record), width: 150 },
                    { title: '状态', dataIndex: 'status', render: statusTag, width: 120 },
                    { title: '消息', dataIndex: 'message', render: (value: string) => value || '-' },
                    { title: '时间', render: (_value: unknown, record: VirtualizationOperation) => formatDateTime(operationTime(record)), width: 180 },
                    {
                      ...tableColumnPresets.action,
                      title: '操作',
                      render: () => (
                        <ManagementIconButton
                          aria-label="发起 AI 调查"
                          tooltip="AI调查"
                          icon={<PlayCircleOutlined />}
                          size="small"
                          onClick={() => navigate(buildInvestigationPath({ connectionId: vm?.connectionId, vmId: vm?.id, namespace: vm?.namespace, workload: vm?.name, timeRangeMinutes: 60 }))}
                        />
                      ),
                      width: 100,
                    },
                  ]}
                />
              </Card>
            ),
          },
          {
            key: 'logs',
            label: '日志',
            forceRender: true,
            children: (
              <Card size="small">
                <pre className="max-h-[520px] overflow-auto rounded border border-[var(--soha-border)] bg-[var(--soha-surface-muted)] p-3 text-xs">
                  {(detail?.logs ?? []).map((item) => `[${formatDateTime(item.createdAt)}] ${item.logLevel ?? 'info'} ${item.message}`).join('\n') || '暂无日志'}
                </pre>
              </Card>
            ),
          },
          canViewMetrics ? {
            key: 'metrics',
            label: '监控指标',
            forceRender: true,
            children: isRunning ? (
              <Card
                size="small"
                loading={metricsQuery.isLoading}
                extra={
                  <Select
                    value={metricsRange}
                    onChange={setMetricsRange}
                    style={{ width: 180 }}
                    options={[
                      { value: 15, label: '最近 15 分钟' },
                      { value: 60, label: '最近 1 小时' },
                      { value: 360, label: '最近 6 小时' },
                      { value: 1440, label: '最近 24 小时' },
                    ]}
                  />
                }
              >
                {metricsQuery.data?.data ? (
                  <VMMetricsChart data={metricsQuery.data.data} />
                ) : (
                  <ManagementState bordered={false} compact title="暂无指标数据" description="指标查询完成后这里会展示 VM 运行曲线。" />
                )}
              </Card>
            ) : (
              <Card size="small">
                <ManagementState bordered={false} compact kind="unsupported" title="VM 未运行" description="VM 进入运行状态后才能采集指标数据。" />
              </Card>
            ),
          } : null,
          canAccessConsole ? {
            key: 'console',
            label: '控制台',
            children: isRunning ? (
              <Suspense fallback={<Card size="small"><Spin tip="正在加载控制台..." /></Card>}>
                <VMConsole vmId={vmId} />
              </Suspense>
            ) : (
              <Card size="small">
                <ManagementState bordered={false} compact kind="unsupported" title="VM 未运行" description="VM 进入运行状态后才能访问控制台。" />
              </Card>
            ),
          } : null,
        ].filter((item): item is NonNullable<typeof item> => item !== null)}
      />
    </div>
  )
}

export function VirtualizationClustersPage() {
  const navigate = useNavigate()
  const [editing, setEditing] = useState<VirtualizationCluster | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [form] = Form.useForm<VirtualizationClusterFormValues>()
  const [showOnlyAbnormal, setShowOnlyAbnormal] = useState(false)
  const [enabledFilter, setEnabledFilter] = useState<EnabledFilter>('all')
  const [providerFilter, setProviderFilter] = useState<ProviderFilter>('all')
  const [showNeverSynced, setShowNeverSynced] = useState(false)
  const [selectedConnectionOperation, setSelectedConnectionOperation] = useState<VirtualizationOperation | null>(null)
  const [selectedClusterRowKeys, setSelectedClusterRowKeys] = useState<React.Key[]>([])
  const { canManageClusters, canSync } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const provider = Form.useWatch('provider', form) ?? 'kubevirt'
  const clustersQuery = useQuery({ queryKey: ['virtualization', 'clusters'], queryFn: virtualizationApi.clusters })
  const clusterOperationsQuery = useQuery({
    queryKey: ['virtualization', 'operations', 'clusters-page'],
    queryFn: () => virtualizationApi.operations(),
  })
  const platformClustersQuery = useQuery({
    queryKey: ['clusters'],
    queryFn: () => api.get<ApiResponse<Cluster[]>>('/clusters'),
  })
  const saveMutation = useMutation({
    mutationFn: (values: VirtualizationClusterFormValues) => {
      const payload = buildClusterPayload(values)
      return editing ? virtualizationApi.updateCluster(editing.id, payload) : virtualizationApi.createCluster(payload)
    },
    onSuccess: () => {
      message.success('连接已保存')
      setDrawerOpen(false)
      setEditing(null)
      form.resetFields()
      refreshVirtualization(queryClient)
    },
  })
  const deleteMutation = useMutation({
    mutationFn: virtualizationApi.deleteCluster,
    onSuccess: () => {
      message.success('连接已删除')
      refreshVirtualization(queryClient)
    },
  })
  const testMutation = useMutation({ mutationFn: virtualizationApi.testCluster, onSuccess: () => { message.success('测试任务已提交'); refreshVirtualization(queryClient) } })
  const syncMutation = useMutation({ mutationFn: virtualizationApi.syncCluster, onSuccess: () => { message.success('同步任务已提交'); refreshVirtualization(queryClient) } })
  const batchSyncMutation = useMutation({
    mutationFn: async (ids: string[]) => Promise.all(ids.map((id) => virtualizationApi.syncCluster(id))),
    onSuccess: (_response, ids) => {
      message.success(`已提交 ${ids.length} 个连接的同步任务`)
      setSelectedClusterRowKeys([])
      refreshVirtualization(queryClient)
    },
  })
  const batchTestMutation = useMutation({
    mutationFn: async (ids: string[]) => Promise.all(ids.map((id) => virtualizationApi.testCluster(id))),
    onSuccess: (_response, ids) => {
      message.success(`已提交 ${ids.length} 个连接的测试任务`)
      setSelectedClusterRowKeys([])
      refreshVirtualization(queryClient)
    },
  })

  function openEditor(record?: VirtualizationCluster) {
    setEditing(record ?? null)
    form.setFieldsValue(record ? {
      name: record.name,
      provider: record.provider === 'pve' ? 'pve' : 'kubevirt',
      endpoint: record.endpoint,
      kubernetesClusterId: record.kubernetesClusterId,
      defaultNamespace: record.defaultNamespace,
      enabled: record.enabled !== false,
      verifyTls: record.verifyTls !== false,
      region: record.region,
      description: record.description,
      defaultNode: typeof record.config?.defaultNode === 'string' ? record.config.defaultNode : undefined,
      defaultStorage: typeof record.config?.defaultStorage === 'string' ? record.config.defaultStorage : undefined,
      defaultBridge: typeof record.config?.defaultBridge === 'string' ? record.config.defaultBridge : undefined,
      backendUrl: typeof record.config?.backendUrl === 'string' ? record.config.backendUrl : undefined,
      prometheusUrl: typeof record.config?.prometheusUrl === 'string' ? record.config.prometheusUrl : undefined,
      prometheusBearerToken: typeof record.config?.prometheusBearerToken === 'string' ? record.config.prometheusBearerToken : undefined,
    } : { provider: 'kubevirt', enabled: true, verifyTls: true })
    setDrawerOpen(true)
  }

  const clusterRows = useMemo(() => {
    const records = clustersQuery.data?.data ?? []
    return [...records]
      .filter((record) => !showOnlyAbnormal || riskReasons(record).length > 0)
      .filter((record) => enabledFilter === 'all' || (enabledFilter === 'enabled' ? record.enabled !== false : record.enabled === false))
      .filter((record) => providerFilter === 'all' || record.provider === providerFilter)
      .filter((record) => !showNeverSynced || !record.lastSyncedAt)
      .sort((left, right) => clusterRiskScore(left) - clusterRiskScore(right) || left.name.localeCompare(right.name))
  }, [clustersQuery.data?.data, enabledFilter, providerFilter, showNeverSynced, showOnlyAbnormal])
  const clusterOperations = clusterOperationsQuery.data?.data ?? []
  function operationsForConnection(connectionId?: string) {
    if (!connectionId) return []
    return clusterOperations.filter((item) => item.connectionId === connectionId)
  }

  function failedSyncForConnection(connectionId?: string) {
    return operationsForConnection(connectionId).find((item) => isSyncOperation(item) && isAbnormalOperation(item.status))
  }

  function latestAbnormalForConnection(connectionId?: string) {
    return operationsForConnection(connectionId).find((item) => isAbnormalOperation(item.status))
  }

  function resetClusterFilters() {
    setShowOnlyAbnormal(false)
    setEnabledFilter('all')
    setProviderFilter('all')
    setShowNeverSynced(false)
  }

  const columns: ColumnsType<VirtualizationCluster> = [
    { title: '名称', dataIndex: 'name', fixed: 'left', width: 180 },
    { title: 'Provider', dataIndex: 'provider', render: providerLabel, width: 120 },
    { title: '接入目标', render: (_value, record) => record.provider === 'kubevirt' ? record.kubernetesClusterId || '-' : record.endpoint || '-', ellipsis: true },
    { title: '健康', dataIndex: 'health', render: (value, record) => statusTag(value || record.status), width: 120 },
    { title: '风险', render: (_value, record) => riskReasons(record).join(' / ') || '正常', width: 220 },
    { title: '风险等级', dataIndex: 'riskLevel', render: (value: string | undefined) => value ? <Tag color={value === 'critical' ? 'red' : value === 'warning' ? 'gold' : value === 'attention' ? 'blue' : 'default'}>{value}</Tag> : '-', width: 120 },
    { title: '最近失败同步', render: (_value, record) => {
      const failedSync = failedSyncForConnection(record.id)
      return failedSync ? (
        <Button size="small" type="link" onClick={() => setSelectedConnectionOperation(failedSync)}>{latestNonEmptyOperationMessage(failedSync)}</Button>
      ) : '-'
    }, width: 200 },
    { title: '最近异常任务', render: (_value, record) => {
      const abnormal = latestAbnormalForConnection(record.id)
      return abnormal ? (
        <Button size="small" type="link" onClick={() => setSelectedConnectionOperation(abnormal)}>{operationKind(abnormal)}</Button>
      ) : '-'
    }, width: 160 },
    { title: '凭证', dataIndex: 'credentialConfigured', render: (value: boolean | undefined) => value === false ? <Tag color="red">未配置</Tag> : <Tag color="green">已配置</Tag>, width: 120 },
    { ...tableColumnPresets.datetime, title: '最近同步', dataIndex: 'lastSyncedAt', render: formatDateTime, width: 180 },
    {
      ...tableColumnPresets.action,
      title: '操作',
      width: 130,
      render: (_value, record) => (
        <Space className="soha-row-action-icons" wrap>
          {canManageClusters ? <ManagementIconButton aria-label="测试连接" size="small" tooltip="测试" icon={<ThunderboltOutlined />} onClick={() => testMutation.mutate(record.id)} /> : null}
          {canSync ? <ManagementIconButton aria-label="同步连接" size="small" tooltip="同步" icon={<CloudSyncOutlined />} onClick={() => syncMutation.mutate(record.id)} /> : null}
          {canManageClusters ? <ManagementIconButton aria-label="编辑连接" size="small" tooltip="编辑" icon={<EditOutlined />} onClick={() => openEditor(record)} /> : null}
          {canManageClusters ? (
            <Popconfirm title="确认删除连接？" onConfirm={() => deleteMutation.mutate(record.id)}>
              <ManagementIconButton aria-label="删除连接" size="small" tooltip="删除" danger icon={<DeleteOutlined />} />
            </Popconfirm>
          ) : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="soha-page soha-virtualization-page">
      <div className="soha-vrt-query">
        <ManagementQueryPanel
          collapsible
          actions={<Button onClick={resetClusterFilters}>重置</Button>}
        >
          <ManagementQueryField label="异常过滤" minWidth={180} width={180}>
            <Switch checked={showOnlyAbnormal} onChange={setShowOnlyAbnormal} checkedChildren="仅异常" unCheckedChildren="全部" />
          </ManagementQueryField>
          <ManagementQueryField label="启用状态" minWidth={180} width={180}>
            <Select value={enabledFilter} onChange={setEnabledFilter} options={ENABLED_FILTER_OPTIONS} />
          </ManagementQueryField>
          <ManagementQueryField label="Provider" minWidth={160} width={160}>
            <Select value={providerFilter} onChange={setProviderFilter} options={VIRTUALIZATION_PROVIDER_FILTER_OPTIONS} />
          </ManagementQueryField>
          <ManagementQueryField label="同步状态" minWidth={180} width={180}>
            <Switch checked={showNeverSynced} onChange={setShowNeverSynced} checkedChildren="未同步" unCheckedChildren="全部" />
          </ManagementQueryField>
        </ManagementQueryPanel>
      </div>
      <VirtualizationAdminTable
        rowKey="id"
        headerExtra={canManageClusters ? (
          <ManagementTableToolbar>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor()}>新增连接</Button>
          </ManagementTableToolbar>
        ) : null}
        toolbarExtra={selectedClusterRowKeys.length > 0 ? (
          <div className="soha-vrt-selection-bar">
            <Text type="secondary">已选择 {selectedClusterRowKeys.length} 个连接</Text>
            <Space wrap>
              {canManageClusters ? (
                <Popconfirm
                  title="确认批量测试连接？"
                  description={bulkActionSummary('将测试', clusterRows.filter((record) => selectedClusterRowKeys.includes(record.id)).map((record) => record.name))}
                  onConfirm={() => batchTestMutation.mutate(selectedClusterRowKeys.map(String))}
                >
                  <Button loading={batchTestMutation.isPending}>批量测试</Button>
                </Popconfirm>
              ) : null}
              {canSync ? (
                <Popconfirm
                  title="确认批量同步连接？"
                  description={bulkActionSummary('将同步', clusterRows.filter((record) => selectedClusterRowKeys.includes(record.id)).map((record) => record.name))}
                  onConfirm={() => batchSyncMutation.mutate(selectedClusterRowKeys.map(String))}
                >
                  <Button type="primary" loading={batchSyncMutation.isPending}>批量同步</Button>
                </Popconfirm>
              ) : null}
              <Button onClick={() => setSelectedClusterRowKeys([])}>清空选择</Button>
            </Space>
          </div>
        ) : null}
        rowSelection={{
          selectedRowKeys: selectedClusterRowKeys,
          onChange: (keys: Key[]) => setSelectedClusterRowKeys(keys),
        }}
        tableSize="small"
        loading={clustersQuery.isLoading || clusterOperationsQuery.isLoading}
        dataSource={clusterRows}
        columns={columns}
        scroll={{ x: 1320 }}
        paginationSummary={localTableSummary(clusterRows.length, clustersQuery.data?.data?.length ?? 0)}
        expandable={{
          expandedRowRender: (record: VirtualizationCluster) => {
            const failedSync = failedSyncForConnection(record.id)
            const latestAbnormal = latestAbnormalForConnection(record.id)
            return (
              <Descriptions size="small" column={{ xs: 1, md: 2 }} bordered>
                <Descriptions.Item label="Endpoint / Cluster">{record.provider === 'kubevirt' ? record.kubernetesClusterId || '-' : record.endpoint || '-'}</Descriptions.Item>
                <Descriptions.Item label="默认命名空间">{record.defaultNamespace || '-'}</Descriptions.Item>
                <Descriptions.Item label="校验 TLS">{record.verifyTls === false ? '关闭' : '开启'}</Descriptions.Item>
                <Descriptions.Item label="最近同步">{formatDateTime(record.lastSyncedAt)}</Descriptions.Item>
                <Descriptions.Item label="Region">{record.region || '-'}</Descriptions.Item>
                <Descriptions.Item label="风险说明">{riskReasons(record).join(' / ') || '正常'}</Descriptions.Item>
                <Descriptions.Item label="Console Backend">{record.provider === 'kubevirt' ? String(record.config?.backendUrl || record.endpoint || '-') : '-'}</Descriptions.Item>
                <Descriptions.Item label="Prometheus">{record.provider === 'kubevirt' ? String(record.config?.prometheusUrl || '-') : '-'}</Descriptions.Item>
                <Descriptions.Item label="PVE 默认网桥">{record.provider === 'pve' ? String(record.config?.defaultBridge || '-') : '-'}</Descriptions.Item>
                <Descriptions.Item label="最近失败同步">{failedSync ? `${operationKind(failedSync)} · ${latestNonEmptyOperationMessage(failedSync)}` : '-'}</Descriptions.Item>
                <Descriptions.Item label="最近异常任务">{latestAbnormal ? `${operationKind(latestAbnormal)} · ${latestNonEmptyOperationMessage(latestAbnormal)}` : '-'}</Descriptions.Item>
              </Descriptions>
            )
          },
        }}
      />
      <Drawer title={editing ? '编辑连接' : '新增连接'} size="large" motion={stableDrawerMotion} open={drawerOpen} onClose={() => setDrawerOpen(false)}>
        <Form form={form} layout="vertical" initialValues={{ provider: 'kubevirt', enabled: true, verifyTls: true }} onFinish={(values) => saveMutation.mutate(values)}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="provider" label="Provider" rules={[{ required: true }]}>
            <Select options={[{ value: 'kubevirt', label: 'KubeVirt' }, { value: 'pve', label: 'PVE' }]} />
          </Form.Item>
          {provider === 'kubevirt' ? (
            <>
              <Form.Item name="kubernetesClusterId" label="Kubernetes 集群" rules={[{ required: true }]}>
                <Select
                  showSearch
                  loading={platformClustersQuery.isLoading}
                  optionFilterProp="label"
                  options={(platformClustersQuery.data?.data ?? []).map((item) => ({ value: item.id, label: `${item.name} (${item.id})` }))}
                />
              </Form.Item>
              <Form.Item name="defaultNamespace" label="默认命名空间">
                <Input />
              </Form.Item>
              <Form.Item name="backendUrl" label="Console Backend URL">
                <Input placeholder="https://kube-api.example:6443" />
              </Form.Item>
              <div className="grid gap-3 md:grid-cols-2">
                <Form.Item name="prometheusUrl" label="Prometheus URL">
                  <Input placeholder="https://prometheus.example" />
                </Form.Item>
                <Form.Item name="prometheusBearerToken" label="Prometheus Bearer Token">
                  <Input.Password />
                </Form.Item>
              </div>
            </>
          ) : (
            <>
              <Form.Item name="endpoint" label="Endpoint" rules={[{ required: true }]}>
                <Input placeholder="https://pve.example:8006" />
              </Form.Item>
              <div className="grid gap-3 md:grid-cols-2">
                <Form.Item name="tokenID" label="Token ID">
                  <Input />
                </Form.Item>
                <Form.Item name="tokenSecret" label="Token Secret">
                  <Input.Password />
                </Form.Item>
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <Form.Item name="ticket" label="Ticket">
                  <Input.Password />
                </Form.Item>
                <Form.Item name="csrfToken" label="CSRF Token">
                  <Input.Password />
                </Form.Item>
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <Form.Item name="defaultNode" label="默认节点">
                  <Input />
                </Form.Item>
                <Form.Item name="defaultStorage" label="默认存储">
                  <Input />
                </Form.Item>
              </div>
              <Form.Item name="defaultBridge" label="默认网桥">
                <Input placeholder="vmbr0" />
              </Form.Item>
            </>
          )}
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="verifyTls" label="校验 TLS" valuePropName="checked">
              <Switch />
            </Form.Item>
          </div>
          <Form.Item name="region" label="Region">
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={saveMutation.isPending}>保存</Button>
            <Button onClick={() => setDrawerOpen(false)}>取消</Button>
          </Space>
        </Form>
      </Drawer>
      <Drawer title="连接关联异常任务" size="large" motion={stableDrawerMotion} open={Boolean(selectedConnectionOperation)} onClose={() => setSelectedConnectionOperation(null)}>
        <Descriptions size="small" column={1} bordered>
          <Descriptions.Item label="任务 ID">{selectedConnectionOperation?.id}</Descriptions.Item>
          <Descriptions.Item label="类型">{selectedConnectionOperation ? operationKind(selectedConnectionOperation) : '-'}</Descriptions.Item>
          <Descriptions.Item label="状态">{statusTag(selectedConnectionOperation?.status)}</Descriptions.Item>
          <Descriptions.Item label="连接">{selectedConnectionOperation?.connectionId || '-'}</Descriptions.Item>
          <Descriptions.Item label="摘要">{selectedConnectionOperation?.message || '-'}</Descriptions.Item>
          <Descriptions.Item label="开始时间">{formatDateTime(operationTime(selectedConnectionOperation || {} as VirtualizationOperation))}</Descriptions.Item>
        </Descriptions>
        {selectedConnectionOperation?.message ? <Alert className="mt-4" type={isAbnormalOperation(selectedConnectionOperation.status) ? 'error' : 'info'} message={selectedConnectionOperation.message} /> : null}
        <div className="mt-4">
          <Button onClick={() => navigate(`/virtualization/operations?connectionId=${encodeURIComponent(selectedConnectionOperation?.connectionId || '')}&abnormal=true`)}>查看该连接全部异常任务</Button>
        </div>
      </Drawer>
    </div>
  )
}

export function VirtualizationImagesPage() {
  const [editing, setEditing] = useState<VirtualizationImage | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [filters, setFilters] = useState<VirtualizationListParams>({ page: 1, pageSize: 10 })
  const [filterForm] = Form.useForm<VirtualizationListParams>()
  const [form] = Form.useForm<VirtualizationImageInput>()
  const { canManageImages } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const imageProvider = Form.useWatch('provider', form) ?? 'kubevirt'
  const imagesQuery = useQuery({
    queryKey: ['virtualization', 'images', filters],
    queryFn: () => virtualizationApi.images(filters),
  })
  const clustersQuery = useQuery({ queryKey: ['virtualization', 'clusters'], queryFn: virtualizationApi.clusters })
  const imagesPage = normalizePage(imagesQuery.data?.data, filters.page ?? 1, filters.pageSize ?? 10)
  const clusters = clustersQuery.data?.data ?? []
  const saveMutation = useMutation({
    mutationFn: (values: VirtualizationImageInput) => editing
      ? virtualizationApi.updateImage(editing.id, buildImagePayload(values))
      : virtualizationApi.createImage(buildImagePayload(values)),
    onSuccess: () => {
      message.success('镜像入口已保存')
      setDrawerOpen(false)
      setEditing(null)
      form.resetFields()
      refreshVirtualization(queryClient)
    },
  })
  const deleteMutation = useMutation({
    mutationFn: virtualizationApi.deleteImage,
    onSuccess: () => {
      message.success('镜像入口已删除')
      refreshVirtualization(queryClient)
    },
  })
  function openImageEditor(record?: VirtualizationImage) {
    setEditing(record ?? null)
    form.setFieldsValue(record ? {
      name: record.name,
      provider: record.provider ?? 'kubevirt',
      connectionId: record.connectionId,
      namespace: record.namespace,
      sourceKind: record.sourceKind ?? record.source,
      sourceRef: record.sourceRef,
      source: record.source,
      osType: record.osType,
      sizeGiB: record.sizeGiB,
      description: record.description,
    } : { provider: 'kubevirt', sourceKind: 'datasource' })
    setDrawerOpen(true)
  }
  const columns: ColumnsType<VirtualizationImage> = [
    { title: '名称', dataIndex: 'name', fixed: 'left', width: 180 },
    { title: 'Provider', dataIndex: 'provider', render: providerLabel },
    { title: '连接', dataIndex: 'connectionName', render: (value, record) => value || record.connectionId || '-' },
    { title: '命名空间', dataIndex: 'namespace', render: (value) => value || '-' },
    { title: '来源', render: (_value, record) => record.assetKind || record.sourceKind || record.source || '-' },
    { title: '引用', dataIndex: 'sourceRef', render: (value) => value || '-' },
    { title: '节点/存储', render: (_value, record) => [record.node, record.storage].filter(Boolean).join(' / ') || '-' },
    { title: 'StorageClass', dataIndex: 'storageClass', render: (value) => value || '-' },
    { title: '可用性', render: (_value, record) => record.ready === false ? <Tag color="red">不可用</Tag> : <Tag color="green">可用</Tag> },
    { title: '系统', dataIndex: 'osType', render: (value) => value || '-' },
    { title: '大小', dataIndex: 'sizeGiB', render: (value) => value ? `${value} GiB` : '-' },
    { title: '状态', dataIndex: 'status', render: statusTag },
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: formatDateTime },
    {
      ...tableColumnPresets.action,
      title: '操作',
      render: (_value, record) => canManageImages ? (
        <Space className="soha-row-action-icons">
          <ManagementIconButton aria-label="编辑镜像" size="small" tooltip="编辑" icon={<EditOutlined />} onClick={() => openImageEditor(record)} />
          <Popconfirm title="确认删除镜像入口？" onConfirm={() => deleteMutation.mutate(record.id)}>
            <ManagementIconButton aria-label="删除镜像" size="small" tooltip="删除" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ) : null,
    },
  ]
  return (
    <div className="soha-page soha-virtualization-page">
      <div className="soha-vrt-query">
        <ManagementQueryPanel
          collapsible
          form={filterForm}
          actions={(
            <VirtualizationFilterActions
              loading={imagesQuery.isFetching}
              onReset={() => {
                filterForm.resetFields()
                setFilters((current) => ({ page: 1, pageSize: current.pageSize ?? 10 }))
              }}
            />
          )}
          onFinish={(values) => setFilters((current) => ({ ...current, ...values, page: 1 }))}
        >
          <ManagementQueryField minWidth={260} name="search" label="关键字" width={260}>
            <Input allowClear prefix={<SearchOutlined />} placeholder="搜索镜像、模板或 ISO" />
          </ManagementQueryField>
          <ManagementQueryField minWidth={180} name="connectionId" label="连接" width={180}>
            <Select
              allowClear
              showSearch
              optionFilterProp="label"
              placeholder="全部连接"
              options={clusters.map((item) => ({ value: item.id, label: item.name }))}
            />
          </ManagementQueryField>
          <ManagementQueryField minWidth={160} name="provider" label="Provider" width={160}>
            <Select allowClear placeholder="全部 Provider" options={VIRTUALIZATION_PROVIDER_OPTIONS} />
          </ManagementQueryField>
        </ManagementQueryPanel>
      </div>
      <VirtualizationAdminTable
        rowKey="id"
        headerExtra={canManageImages ? (
          <ManagementTableToolbar>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => openImageEditor()}>新增镜像入口</Button>
          </ManagementTableToolbar>
        ) : null}
        loading={imagesQuery.isLoading}
        dataSource={imagesPage.items}
        columns={columns}
        scroll={{ x: 1220 }}
        pagination={pageTablePagination(imagesPage, setFilters)}
        paginationSummary={virtualizationPageSummary}
      />
      <Drawer title={editing ? '编辑镜像入口' : '新增镜像入口'} size="large" motion={stableDrawerMotion} open={drawerOpen} onClose={() => setDrawerOpen(false)}>
        <Form form={form} layout="vertical" initialValues={{ provider: 'kubevirt', sourceKind: 'datasource' }} onFinish={(values) => saveMutation.mutate(values)}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="provider" label="Provider" rules={[{ required: true }]}>
              <Select options={[{ value: 'kubevirt', label: 'KubeVirt' }, { value: 'pve', label: 'PVE' }]} />
            </Form.Item>
            <Form.Item name="connectionId" label="连接" rules={[{ required: true }]}>
              <Select
                showSearch
                optionFilterProp="label"
                options={clusters
                  .filter((item) => !imageProvider || item.provider === imageProvider)
                  .map((item) => ({ value: item.id, label: item.name }))}
              />
            </Form.Item>
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="sourceKind" label="来源类型" rules={[{ required: true }]}>
              <Select
                options={imageProvider === 'pve'
                  ? [{ value: 'template', label: 'PVE template' }, { value: 'iso', label: 'PVE ISO' }]
                  : [{ value: 'datasource', label: 'KubeVirt DataSource' }, { value: 'pvc', label: 'PVC' }]}
              />
            </Form.Item>
            <Form.Item name="sourceRef" label="来源引用" rules={[{ required: true }]}>
              <Input placeholder={imageProvider === 'pve' ? 'local:vztmpl/ubuntu.tar.zst 或 local:iso/ubuntu.iso' : 'namespace/name'} />
            </Form.Item>
          </div>
          {imageProvider === 'kubevirt' ? (
            <Form.Item name="namespace" label="命名空间">
              <Input />
            </Form.Item>
          ) : null}
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="osType" label="操作系统">
              <Input placeholder="ubuntu / windows / centos" />
            </Form.Item>
            <Form.Item name="sizeGiB" label="大小 GiB">
              <InputNumber min={1} className="w-full" />
            </Form.Item>
          </div>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={saveMutation.isPending}>保存</Button>
            <Button onClick={() => setDrawerOpen(false)}>取消</Button>
          </Space>
        </Form>
      </Drawer>
    </div>
  )
}

export function VirtualizationFlavorsPage() {
  const [editing, setEditing] = useState<VirtualizationFlavor | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [flavorFilters, setFlavorFilters] = useState<{ enabled?: EnabledFilter; search?: string }>({ enabled: 'all' })
  const [filterForm] = Form.useForm<{ enabled?: EnabledFilter; search?: string }>()
  const [form] = Form.useForm<VirtualizationFlavorInput>()
  const { canManageFlavors } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const flavorsQuery = useQuery({ queryKey: ['virtualization', 'flavors'], queryFn: virtualizationApi.flavors })
  const flavors = flavorsQuery.data?.data ?? []
  const flavorRows = useMemo(() => {
    const search = flavorFilters.search?.trim().toLowerCase()
    return flavors.filter((record) => {
      const matchesSearch = !search || [record.name, record.description]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(search))
      const matchesEnabled = flavorFilters.enabled === 'disabled'
        ? record.enabled === false
        : flavorFilters.enabled === 'enabled'
          ? record.enabled !== false
          : true
      return matchesSearch && matchesEnabled
    })
  }, [flavorFilters.enabled, flavorFilters.search, flavors])
  const saveMutation = useMutation({
    mutationFn: (values: VirtualizationFlavorInput) => editing ? virtualizationApi.updateFlavor(editing.id, values) : virtualizationApi.createFlavor(values),
    onSuccess: () => {
      message.success('规格已保存')
      setDrawerOpen(false)
      setEditing(null)
      form.resetFields()
      refreshVirtualization(queryClient)
    },
  })
  const deleteMutation = useMutation({
    mutationFn: virtualizationApi.deleteFlavor,
    onSuccess: () => {
      message.success('规格已删除')
      refreshVirtualization(queryClient)
    },
  })
  function openEditor(record?: VirtualizationFlavor) {
    setEditing(record ?? null)
    form.setFieldsValue(record ?? { enabled: true })
    setDrawerOpen(true)
  }
  const columns: ColumnsType<VirtualizationFlavor> = [
    { title: '名称', dataIndex: 'name', fixed: 'left', width: 180 },
    { title: 'CPU', dataIndex: 'cpu', width: 90 },
    { title: '内存 MiB', dataIndex: 'memoryMiB', width: 120 },
    { title: '磁盘 GiB', dataIndex: 'diskGiB', width: 120 },
    { title: '状态', dataIndex: 'enabled', render: (value) => value === false ? <Tag>禁用</Tag> : <Tag color="green">启用</Tag>, width: 100 },
    { title: '描述', dataIndex: 'description', render: (value) => value || '-' },
    {
      ...tableColumnPresets.action,
      title: '操作',
      render: (_value, record) => canManageFlavors ? (
        <Space className="soha-row-action-icons">
          <ManagementIconButton aria-label="编辑规格" size="small" tooltip="编辑" icon={<EditOutlined />} onClick={() => openEditor(record)} />
          <Popconfirm title="确认删除规格？" onConfirm={() => deleteMutation.mutate(record.id)}>
            <ManagementIconButton aria-label="删除规格" size="small" tooltip="删除" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ) : null,
    },
  ]
  return (
    <div className="soha-page soha-virtualization-page">
      <div className="soha-vrt-query">
        <ManagementQueryPanel
          collapsible
          form={filterForm}
          initialValues={{ enabled: 'all' }}
          actions={(
            <VirtualizationFilterActions
              loading={flavorsQuery.isFetching}
              onReset={() => {
                filterForm.resetFields()
                setFlavorFilters({ enabled: 'all' })
              }}
            />
          )}
          onFinish={(values) => setFlavorFilters({ enabled: values.enabled ?? 'all', search: values.search })}
        >
          <ManagementQueryField minWidth={260} name="search" label="关键字" width={260}>
            <Input allowClear prefix={<SearchOutlined />} placeholder="搜索规格名称或描述" />
          </ManagementQueryField>
          <ManagementQueryField minWidth={180} name="enabled" label="启用状态" width={180}>
            <Select options={ENABLED_FILTER_OPTIONS} />
          </ManagementQueryField>
        </ManagementQueryPanel>
      </div>
      <VirtualizationAdminTable
        rowKey="id"
        headerExtra={canManageFlavors ? (
          <ManagementTableToolbar>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor()}>新增规格</Button>
          </ManagementTableToolbar>
        ) : null}
        loading={flavorsQuery.isLoading}
        dataSource={flavorRows}
        columns={columns}
        paginationSummary={localTableSummary(flavorRows.length, flavors.length)}
        scroll={{ x: 900 }}
      />
      <Drawer title={editing ? '编辑规格' : '新增规格'} size="large" motion={stableDrawerMotion} open={drawerOpen} onClose={() => setDrawerOpen(false)}>
        <Form form={form} layout="vertical" initialValues={{ cpu: 2, memoryMiB: 4096, diskGiB: 40, enabled: true }} onFinish={(values) => saveMutation.mutate(values)}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <div className="grid gap-3 md:grid-cols-3">
            <Form.Item name="cpu" label="CPU" rules={[{ required: true }]}>
              <InputNumber min={1} className="w-full" />
            </Form.Item>
            <Form.Item name="memoryMiB" label="内存 MiB" rules={[{ required: true }]}>
              <InputNumber min={128} className="w-full" />
            </Form.Item>
            <Form.Item name="diskGiB" label="磁盘 GiB" rules={[{ required: true }]}>
              <InputNumber min={1} className="w-full" />
            </Form.Item>
          </div>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={saveMutation.isPending}>保存</Button>
            <Button onClick={() => setDrawerOpen(false)}>取消</Button>
          </Space>
        </Form>
      </Drawer>
    </div>
  )
}

export function VirtualizationOperationsPage() {
  const location = useLocation()
  const preset = operationPresetFromSearch(location.search)
  return (
    <div className="soha-page soha-virtualization-page">
      <OperationsTable initialPreset={preset} />
    </div>
  )
}

export function VirtualizationSyncPage() {
  const { canSync } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const syncMutation = useMutation({
    mutationFn: virtualizationApi.syncAll,
    onSuccess: () => {
      message.success('同步任务已提交')
      refreshVirtualization(queryClient)
    },
  })
  const headerActions = useMemo(() => canSync ? (
    <Button type="primary" icon={<CloudSyncOutlined />} loading={syncMutation.isPending} onClick={() => syncMutation.mutate()}>
      新建同步任务
    </Button>
  ) : null, [canSync, syncMutation])
  return (
    <div className="soha-page soha-virtualization-page">
      <OperationsTable assetType="asset_sync" toolbarExtra={headerActions} />
    </div>
  )
}
