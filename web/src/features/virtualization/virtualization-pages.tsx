import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Alert,
  Button,
  Card,
  Descriptions,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  CloudSyncOutlined,
  DeleteOutlined,
  EditOutlined,
  FileTextOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  PoweroffOutlined,
  ReloadOutlined,
  SearchOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { PageHeader } from '@/components/page-header'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { api } from '@/services/api-client'
import type { ApiResponse, Cluster } from '@/types'
import { virtualizationApi } from './virtualization-api'
import { VMConsole } from './vm-console'
import { useTaskStream } from './use-task-stream'
import { LineChart } from '@visactor/react-vchart'
import {
  buildCompactChartSpec,
  compactMetricColors,
  formatMetricValue,
  type CompactChartLine,
} from '@/components/resource-metrics-panel'
import type {
  CreateVirtualMachineInput,
  VirtualMachine,
  VirtualMachineDetail,
  VirtualMachinePowerAction,
  VirtualizationCluster,
  VirtualizationClusterInput,
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

const { Paragraph, Text } = Typography

const STATUS_COLORS: Record<string, string> = {
  healthy: 'green',
  ready: 'green',
  running: 'green',
  success: 'green',
  completed: 'green',
  synced: 'green',
  degraded: 'gold',
  pending: 'gold',
  queued: 'gold',
  syncing: 'blue',
  running_task: 'blue',
  failed: 'red',
  error: 'red',
  callback_timeout: 'red',
  canceled: 'default',
  stale: 'default',
  unavailable: 'red',
  stopped: 'default',
  stopped_vm: 'default',
}

function statusTag(value?: string) {
  if (!value) return <Text type="secondary">-</Text>
  const key = value.toLowerCase()
  return <Tag color={STATUS_COLORS[key] ?? 'default'}>{value}</Tag>
}

const OPERATION_FILTER_PRESETS = [
  { key: 'all', label: '全部任务' },
  { key: 'pending', label: '待处理' },
  { key: 'abnormal', label: '失败/超时' },
  { key: 'asset_sync', label: '同步任务' },
  { key: 'vm', label: 'VM 任务' },
] as const

type OperationFilterPreset = (typeof OPERATION_FILTER_PRESETS)[number]['key']

function isAbnormalOperation(status?: string) {
  return ['failed', 'callback_timeout'].includes(String(status || '').toLowerCase())
}

function isPendingOperation(status?: string) {
  return ['queued', 'running'].includes(String(status || '').toLowerCase())
}

function isSyncOperation(record: VirtualizationOperation) {
  return operationKind(record) === 'asset_sync'
}

function isVMOperation(record: VirtualizationOperation) {
  return ['vm_create', 'vm_action'].includes(operationKind(record))
}

function formatOperationDuration(record: VirtualizationOperation) {
  const startedAt = record.startedAt || record.createdAt
  const endedAt = record.completedAt || record.updatedAt
  if (!startedAt) return '-'
  const start = new Date(startedAt).getTime()
  const end = endedAt ? new Date(endedAt).getTime() : Date.now()
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return '-'
  const minutes = Math.floor((end - start) / 60000)
  if (minutes < 1) return '少于 1 分钟'
  if (minutes < 60) return `${minutes} 分钟`
  const hours = Math.floor(minutes / 60)
  const restMinutes = minutes % 60
  return restMinutes > 0 ? `${hours} 小时 ${restMinutes} 分钟` : `${hours} 小时`
}

function buildOperationFilter(records: VirtualizationOperation[], preset: OperationFilterPreset) {
  switch (preset) {
    case 'pending':
      return records.filter((record) => isPendingOperation(record.status))
    case 'abnormal':
      return records.filter((record) => isAbnormalOperation(record.status))
    case 'asset_sync':
      return records.filter((record) => isSyncOperation(record))
    case 'vm':
      return records.filter((record) => isVMOperation(record))
    default:
      return records
  }
}

function riskReasons(record: VirtualizationCluster) {
  if (record.riskReasons?.length) {
    return record.riskReasons
  }
  const reasons: string[] = []
  const health = String(record.health || record.status || '').toLowerCase()
  if (health === 'unavailable') {
    reasons.push('连接不可用')
  } else if (health === 'degraded') {
    reasons.push('连接降级')
  }
  if (record.enabled !== false && record.credentialConfigured === false) {
    reasons.push('未配置凭证')
  }
  if (!record.lastSyncedAt) {
    reasons.push('尚未同步')
  }
  return reasons
}

function clusterRiskScore(record: VirtualizationCluster) {
  const health = String(record.health || record.status || '').toLowerCase()
  if (health === 'unavailable') return 0
  if (health === 'degraded') return 1
  if (record.enabled !== false && record.credentialConfigured === false) return 2
  if (!record.lastSyncedAt) return 3
  return 4
}

function latestNonEmptyOperationMessage(record: VirtualizationOperation) {
  return record.message || '-'
}

function OperationStatusChips({ counts }: { counts: Array<{ key: string; label: string; value: number; tone?: string }> }) {
  return (
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
      {counts.map((item) => (
        <div key={item.key} className="rounded border border-[var(--kc-border)] bg-[var(--kc-surface-muted)] p-3">
          <div className="text-xs text-[var(--kc-text-secondary)]">{item.label}</div>
          <div className={`mt-2 text-2xl font-semibold ${item.tone === 'danger' ? 'text-red-500' : item.tone === 'warning' ? 'text-amber-500' : ''}`}>{item.value}</div>
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
}: {
  title: string
  description: string
  emptyText: string
  action?: React.ReactNode
  items: Array<{ id: string; title: string; status?: string; message?: string }>
  renderMeta?: (item: { id: string; title: string; status?: string; message?: string }) => React.ReactNode
  renderActions?: (item: { id: string; title: string; status?: string; message?: string }) => React.ReactNode
}) {
  return (
    <Card size="small" title={title} extra={action}>
      <div className="mb-3 text-xs text-[var(--kc-text-secondary)]">{description}</div>
      {items.length === 0 ? (
        <Empty description={emptyText} image={Empty.PRESENTED_IMAGE_SIMPLE} />
      ) : (
        <div className="space-y-3">
          {items.map((item) => (
            <div key={item.id} className="rounded border border-[var(--kc-border)] p-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <Space wrap>
                  <Text strong>{item.title}</Text>
                  {statusTag(item.status)}
                </Space>
                {renderActions ? <Space wrap>{renderActions(item)}</Space> : null}
              </div>
              <div className="mt-2 text-xs text-[var(--kc-text-secondary)]">{item.message || '-'}</div>
              {renderMeta ? <div className="mt-2 text-xs text-[var(--kc-text-secondary)]">{renderMeta(item)}</div> : null}
            </div>
          ))}
        </div>
      )}
    </Card>
  )
}

function operationKind(record: VirtualizationOperation) {
  return record.operationType || record.type || record.action || '-'
}

function operationTime(record: VirtualizationOperation) {
  return record.startedAt || record.createdAt || record.updatedAt
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
  if (data.message) {
    return <Alert type="info" message={data.message} />
  }
  const series = data.series ?? []
  if (series.length === 0) {
    return <Empty description="暂无指标数据" />
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

function normalizePage<T>(data: VirtualizationPage<T> | T[] | undefined, fallbackPage: number, fallbackPageSize: number): VirtualizationPage<T> {
  if (Array.isArray(data)) {
    return { items: data, total: data.length, page: fallbackPage, pageSize: fallbackPageSize }
  }
  return data ?? { items: [], total: 0, page: fallbackPage, pageSize: fallbackPageSize }
}

function compactRecord(values: Record<string, unknown>) {
  return Object.fromEntries(
    Object.entries(values).filter(([, value]) => value !== undefined && value !== '' && value !== null),
  )
}

function stringifyRaw(value: VirtualMachineDetail['providerRaw'] | undefined) {
  if (!value) return ''
  if (typeof value === 'string') return value
  return JSON.stringify(value, null, 2)
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

interface VirtualizationClusterFormValues {
  name: string
  provider?: 'kubevirt' | 'pve'
  endpoint?: string
  kubernetesClusterId?: string
  defaultNamespace?: string
  enabled?: boolean
  verifyTls?: boolean
  region?: string
  description?: string
  tokenID?: string
  tokenSecret?: string
  ticket?: string
  csrfToken?: string
  defaultNode?: string
  defaultStorage?: string
}

function operationPresetFromSearch(search: string): OperationFilterPreset {
  const params = new URLSearchParams(search)
  if (params.get('pending') === 'true') {
    return 'pending'
  }
  if (params.get('abnormal') === 'true') {
    return 'abnormal'
  }
  const taskKind = params.get('taskKind') || params.get('assetType')
  if (taskKind === 'asset_sync') {
    return 'asset_sync'
  }
  if (taskKind === 'vm_create' || taskKind === 'vm_action') {
    return 'vm'
  }
  return 'all'
}

function buildVmPayload(values: CreateVirtualMachineInput): CreateVirtualMachineInput {
  return {
    name: values.name,
    connectionId: values.connectionId,
    flavorId: values.flavorId,
    namespace: values.namespace || undefined,
    node: values.node || undefined,
    cpu: values.cpu,
    memoryMiB: values.memoryMiB,
    bootImageId: values.bootImageId,
    diskGiB: values.diskGiB,
    network: values.network || undefined,
    cloudInit: values.cloudInit || undefined,
    providerParams: values.providerParams,
    startAfterCreate: Boolean(values.startAfterCreate),
  }
}

interface VirtualMachineFormValues extends CreateVirtualMachineInput {
  provider?: string
  pveStorage?: string
  pveBridge?: string
  pveIso?: string
  kubevirtStorageClass?: string
  kubevirtDataVolumeName?: string
}

function buildCreateVmPayload(values: VirtualMachineFormValues): CreateVirtualMachineInput {
  const providerParams = compactRecord({
    storage: values.pveStorage,
    bridge: values.pveBridge,
    iso: values.pveIso,
    storageClass: values.kubevirtStorageClass,
    dataVolumeName: values.kubevirtDataVolumeName,
  })
  return buildVmPayload({
    ...values,
    providerParams: Object.keys(providerParams).length ? providerParams : undefined,
  })
}

function buildImagePayload(values: VirtualizationImageInput): VirtualizationImageInput {
  return {
    name: values.name,
    provider: values.provider,
    connectionId: values.connectionId || undefined,
    namespace: values.namespace || undefined,
    sourceKind: values.sourceKind,
    sourceRef: values.sourceRef || undefined,
    source: values.source || undefined,
    osType: values.osType || undefined,
    sizeGiB: values.sizeGiB,
    description: values.description || undefined,
  }
}

function buildClusterPayload(values: VirtualizationClusterFormValues): VirtualizationClusterInput {
  const provider = values.provider ?? 'kubevirt'
  const config: Record<string, unknown> = {}
  const credential: Record<string, unknown> = {}
  if (values.region) config.region = values.region
  if (values.description) config.description = values.description
  if (provider === 'pve') {
    if (values.defaultNode) config.defaultNode = values.defaultNode
    if (values.defaultStorage) config.defaultStorage = values.defaultStorage
    if (values.tokenID) credential.tokenID = values.tokenID
    if (values.tokenSecret) credential.tokenSecret = values.tokenSecret
    if (values.ticket) credential.ticket = values.ticket
    if (values.csrfToken) credential.csrfToken = values.csrfToken
  }
  return {
    name: values.name,
    provider,
    endpoint: provider === 'pve' ? values.endpoint : undefined,
    kubernetesClusterId: provider === 'kubevirt' ? values.kubernetesClusterId : undefined,
    defaultNamespace: values.defaultNamespace || undefined,
    enabled: values.enabled !== false,
    verifyTls: values.verifyTls !== false,
    region: values.region || undefined,
    description: values.description || undefined,
    config: Object.keys(config).length ? config : undefined,
    credential: Object.keys(credential).length ? credential : undefined,
  }
}

function OperationsTable({ assetType, initialPreset = 'all' }: { assetType?: string; initialPreset?: OperationFilterPreset }) {
  const [selectedOperation, setSelectedOperation] = useState<VirtualizationOperation | null>(null)
  const [preset, setPreset] = useState<OperationFilterPreset>(initialPreset)
  const { canManageOperations } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const operationsQuery = useQuery({
    queryKey: ['virtualization', 'operations', assetType ?? 'all'],
    queryFn: () => virtualizationApi.operations({ assetType }),
  })
  const logsQuery = useQuery({
    queryKey: ['virtualization', 'operations', selectedOperation?.id, 'logs'],
    queryFn: () => virtualizationApi.operationLogs(selectedOperation?.id ?? ''),
    enabled: Boolean(selectedOperation?.id),
  })
  const operations = operationsQuery.data?.data ?? []
  const filteredOperations = useMemo(() => buildOperationFilter(operations, preset), [operations, preset])
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
      width: 240,
      render: (_value, record) => {
        const canCancel = canManageOperations && hasAllowedAction(record.allowedActions, 'cancel')
        const canRetry = canManageOperations && hasAllowedAction(record.allowedActions, 'retry')
        return (
          <Space wrap>
            <Button size="small" type="text" icon={<FileTextOutlined />} onClick={() => setSelectedOperation(record)}>
              日志
            </Button>
            {record.vmId ? <Button size="small" type="text" onClick={() => navigate(`/virtualization/vms/${encodeURIComponent(record.vmId || '')}`)}>VM</Button> : null}
            {canCancel ? (
              <Popconfirm title="确认取消任务？" onConfirm={() => cancelMutation.mutate(record.id)}>
                <Button size="small" type="text" danger>取消</Button>
              </Popconfirm>
            ) : null}
            {canRetry ? <Button size="small" type="text" icon={<ReloadOutlined />} onClick={() => retryMutation.mutate(record.id)}>重试</Button> : null}
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

  return (
    <>
      <Space wrap className="mb-3">
        {OPERATION_FILTER_PRESETS.map((item) => (
          <Button key={item.key} type={preset === item.key ? 'primary' : 'default'} onClick={() => setPreset(item.key)}>
            {item.label}
          </Button>
        ))}
      </Space>
      <OperationStatusChips counts={[
        { key: 'pending', label: '待处理', value: counts.pending, tone: counts.pending > 0 ? 'warning' : 'default' },
        { key: 'abnormal', label: '失败/超时', value: counts.abnormal, tone: counts.abnormal > 0 ? 'danger' : 'default' },
        { key: 'sync', label: '同步任务', value: counts.sync },
        { key: 'vm', label: 'VM 任务', value: counts.vm },
      ]} />
      <Table
        rowKey="id"
        size="small"
        loading={operationsQuery.isLoading}
        dataSource={filteredOperations}
        columns={columns}
        scroll={{ x: 1360 }}
      />
      <Drawer
        title="任务日志"
        size="large"
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
        <pre className="mt-4 max-h-[520px] overflow-auto rounded border border-[var(--kc-border)] bg-[var(--kc-surface-muted)] p-3 text-xs">
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

function StatCard({ label, value, extra }: { label: string; value: number | string; extra?: string }) {
  return (
    <Card size="small">
      <Text type="secondary">{label}</Text>
      <div className="mt-2 text-2xl font-semibold">{value}</div>
      {extra ? <Text type="secondary">{extra}</Text> : null}
    </Card>
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
  const clustersQuery = useQuery({
    queryKey: ['virtualization', 'clusters', 'overview'],
    queryFn: virtualizationApi.clusters,
  })
  const operationsQuery = useQuery({
    queryKey: ['virtualization', 'operations', 'overview'],
    queryFn: () => virtualizationApi.operations(),
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
  const clusters = clustersQuery.data?.data ?? []
  const operations = operationsQuery.data?.data ?? []
  const attention = overview.attention
  const connectionSummary = overview.connectionSummary
  const taskSummary = overview.taskSummary
  const abnormalClusters = useMemo(
    () => (attention?.riskyConnections?.length ? attention.riskyConnections : [...clusters].filter((record) => riskReasons(record).length > 0).sort((left, right) => clusterRiskScore(left) - clusterRiskScore(right)).slice(0, 4)),
    [attention?.riskyConnections, clusters],
  )
  const failedSyncOperations = useMemo(
    () => (attention?.failedSyncTasks?.length ? attention.failedSyncTasks : operations.filter((record) => isSyncOperation(record) && isAbnormalOperation(record.status)).slice(0, 5)),
    [attention?.failedSyncTasks, operations],
  )
  const failedOperations = useMemo(
    () => (attention?.failedOperations?.length ? attention.failedOperations : operations.filter((record) => isAbnormalOperation(record.status)).slice(0, 5)),
    [attention?.failedOperations, operations],
  )
  const pendingOperations = useMemo(
    () => operations.filter((record) => isPendingOperation(record.status)).slice(0, 5),
    [operations],
  )
  const recentAbnormal = useMemo(
    () => operations.filter((record) => isAbnormalOperation(record.status)).slice(0, 8),
    [operations],
  )
  const logs = logsQuery.data?.data ?? []
  const heroStats = [
    {
      key: 'connections',
      label: '异常连接',
      value: (connectionSummary?.degraded ?? health?.degraded ?? 0) + (connectionSummary?.unavailable ?? health?.unavailable ?? 0),
      helper: `健康 ${connectionSummary?.healthy ?? health?.healthy ?? 0} / 总计 ${connectionSummary?.total ?? health?.total ?? 0}`,
      tone: ((connectionSummary?.degraded ?? health?.degraded ?? 0) + (connectionSummary?.unavailable ?? health?.unavailable ?? 0)) > 0 ? 'danger' : 'default',
      action: () => navigate('/virtualization/clusters'),
    },
    {
      key: 'pending',
      label: '待处理任务',
      value: stats.pendingTaskCount ?? ((taskSummary?.queued ?? 0) + (taskSummary?.running ?? 0) || pendingOperations.length),
      helper: '正在运行或排队中的任务',
      tone: (stats.pendingTaskCount ?? ((taskSummary?.queued ?? 0) + (taskSummary?.running ?? 0) || pendingOperations.length)) > 0 ? 'warning' : 'default',
      action: () => navigate('/virtualization/operations?pending=true'),
    },
    {
      key: 'failed',
      label: '失败任务',
      value: stats.failedTaskCount ?? ((taskSummary?.failed ?? 0) + (taskSummary?.timeout ?? 0) || failedOperations.length),
      helper: '失败或超时任务需优先处理',
      tone: (stats.failedTaskCount ?? ((taskSummary?.failed ?? 0) + (taskSummary?.timeout ?? 0) || failedOperations.length)) > 0 ? 'danger' : 'default',
      action: () => navigate('/virtualization/operations?abnormal=true'),
    },
    {
      key: 'sync',
      label: '最近同步状态',
      value: overview.lastSyncTask?.status === 'completed' ? '正常' : overview.lastSyncTask?.status ? '异常' : '未同步',
      helper: overview.lastSyncTask ? `${operationKind(overview.lastSyncTask)} · ${formatDateTime(operationTime(overview.lastSyncTask))}` : '尚无同步记录',
      tone: overview.lastSyncTask?.status === 'completed' ? 'default' : 'warning',
      action: () => navigate('/virtualization/sync'),
    },
    {
      key: 'vm',
      label: '运行中 VM',
      value: `${stats.runningVmCount ?? 0} / ${stats.vmCount ?? 0}`,
      helper: `停机 ${stats.stoppedVmCount ?? 0}`,
      tone: 'default',
      action: () => navigate('/virtualization/vms'),
    },
  ]

  return (
    <div className="space-y-4">
      <PageHeader
        title="总览"
        description="优先聚焦异常连接、失败任务和同步风险的虚拟化运维总览。"
        showResourceScope={false}
        actions={
          canSync ? (
            <Space>
              <Link to="/virtualization/sync">
                <Button icon={<CloudSyncOutlined />}>同步任务</Button>
              </Link>
              <Button type="primary" icon={<ReloadOutlined />} loading={syncMutation.isPending} onClick={() => syncMutation.mutate()}>
                立即同步
              </Button>
            </Space>
          ) : null
        }
      />
      <TaskProgressBanner
        task={syncTask}
        status={syncStreamStatus}
        title="正在同步虚拟化资源"
        onCancel={syncTask?.id ? () => cancelSyncMutation.mutate(syncTask.id) : undefined}
        cancelling={cancelSyncMutation.isPending}
      />
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5">
        {heroStats.map((item) => (
          <Card key={item.key} size="small" hoverable onClick={item.action} className="cursor-pointer">
            <Text type="secondary">{item.label}</Text>
            <div className={`mt-2 text-2xl font-semibold ${item.tone === 'danger' ? 'text-red-500' : item.tone === 'warning' ? 'text-amber-500' : ''}`}>{item.value}</div>
            <Text type="secondary">{item.helper}</Text>
          </Card>
        ))}
      </div>
      <div className="grid gap-4 xl:grid-cols-3">
        <AttentionList
          title="高风险连接"
          description="优先处理不可用、降级、未配置凭证或尚未同步的连接。"
          emptyText="暂无高风险连接"
          action={<Button type="link" onClick={() => navigate('/virtualization/clusters')}>查看全部</Button>}
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
                <Button size="small" onClick={() => setSelectedCluster(cluster)}>详情</Button>
                {canManageClusters ? <Button size="small" onClick={() => testMutation.mutate(cluster.id)} loading={testMutation.isPending}>测试</Button> : null}
                {canSync ? <Button size="small" onClick={() => syncClusterMutation.mutate(cluster.id)} loading={syncClusterMutation.isPending}>同步</Button> : null}
              </>
            )
          }}
        />
        <AttentionList
          title="最近失败同步"
          description="快速定位失败或超时的资产同步任务。"
          emptyText="暂无失败同步"
          action={<Button type="link" onClick={() => navigate('/virtualization/sync')}>进入同步任务</Button>}
          items={failedSyncOperations.map((item) => ({ id: item.id, title: item.targetName || item.connectionId || item.id, status: item.status, message: latestNonEmptyOperationMessage(item) }))}
          renderMeta={(item) => {
            const operation = failedSyncOperations.find((record) => record.id === item.id)
            return operation ? `开始时间: ${formatDateTime(operationTime(operation))}` : null
          }}
          renderActions={(item) => {
            const operation = failedSyncOperations.find((record) => record.id === item.id)
            return operation ? (
              <>
                <Button size="small" onClick={() => setSelectedOperation(operation)}>日志</Button>
                {canManageOperations && hasAllowedAction(operation.allowedActions, 'retry') ? <Button size="small" onClick={() => retryMutation.mutate(operation.id)} loading={retryMutation.isPending}>重试</Button> : null}
              </>
            ) : null
          }}
        />
        <AttentionList
          title="失败与超时任务"
          description="失败任务会影响生命周期动作与资源一致性。"
          emptyText="暂无失败任务"
          action={<Button type="link" onClick={() => navigate('/virtualization/operations')}>查看任务中心</Button>}
          items={failedOperations.map((item) => ({ id: item.id, title: item.targetName || item.vmId || item.id, status: item.status, message: latestNonEmptyOperationMessage(item) }))}
          renderMeta={(item) => {
            const operation = failedOperations.find((record) => record.id === item.id)
            return operation ? `类型: ${operationKind(operation)} · 连接: ${operation.connectionId || '-'}` : null
          }}
          renderActions={(item) => {
            const operation = failedOperations.find((record) => record.id === item.id)
            return operation ? (
              <>
                <Button size="small" onClick={() => setSelectedOperation(operation)}>日志</Button>
                {operation.vmId ? <Button size="small" onClick={() => navigate(`/virtualization/vms/${encodeURIComponent(operation.vmId || '')}`)}>VM</Button> : null}
              </>
            ) : null
          }}
        />
      </div>
      <div className="grid gap-4 xl:grid-cols-2">
        <Card size="small" title="连接健康态势" extra={<Button type="link" onClick={() => navigate('/virtualization/clusters')}>连接页</Button>}>
          <OperationStatusChips counts={[
            { key: 'healthy', label: '健康连接', value: health?.healthy ?? 0 },
            { key: 'degraded', label: '降级连接', value: health?.degraded ?? 0, tone: (health?.degraded ?? 0) > 0 ? 'warning' : 'default' },
            { key: 'unavailable', label: '不可用连接', value: health?.unavailable ?? 0, tone: (health?.unavailable ?? 0) > 0 ? 'danger' : 'default' },
            { key: 'neverSynced', label: '未同步连接', value: clusters.filter((item) => !item.lastSyncedAt).length, tone: clusters.some((item) => !item.lastSyncedAt) ? 'warning' : 'default' },
          ]} />
        </Card>
        <Card size="small" title="任务处置态势" extra={<Button type="link" onClick={() => navigate('/virtualization/operations')}>任务中心</Button>}>
          <OperationStatusChips counts={[
            { key: 'queued', label: '排队中', value: operations.filter((item) => item.status === 'queued').length, tone: operations.some((item) => item.status === 'queued') ? 'warning' : 'default' },
            { key: 'running', label: '执行中', value: operations.filter((item) => item.status === 'running').length, tone: operations.some((item) => item.status === 'running') ? 'warning' : 'default' },
            { key: 'failed', label: '失败/超时', value: operations.filter((item) => isAbnormalOperation(item.status)).length, tone: operations.some((item) => isAbnormalOperation(item.status)) ? 'danger' : 'default' },
            { key: 'sync', label: '同步任务', value: operations.filter((item) => isSyncOperation(item)).length },
          ]} />
        </Card>
      </div>
      <Card size="small" title="最近异常" extra={<Button type="link" onClick={() => navigate('/virtualization/operations')}>查看全部异常</Button>} loading={operationsQuery.isLoading}>
        <Table
          rowKey="id"
          size="small"
          pagination={false}
          dataSource={recentAbnormal}
          locale={{ emptyText: '暂无异常任务' }}
          columns={[
            { title: '类型', render: (_value, record: VirtualizationOperation) => operationKind(record), width: 150 },
            { title: '资源', dataIndex: 'targetName', render: (value: string, record: VirtualizationOperation) => value || record.targetType || '-', width: 160 },
            { title: '连接', dataIndex: 'connectionId', render: (value: string) => value || '-', width: 160 },
            { title: '状态', dataIndex: 'status', render: statusTag, width: 120 },
            { title: '摘要', render: (_value, record: VirtualizationOperation) => latestNonEmptyOperationMessage(record), ellipsis: true },
            { title: '时间', render: (_value, record: VirtualizationOperation) => formatDateTime(operationTime(record)), width: 180 },
            { title: '操作', render: (_value, record: VirtualizationOperation) => <Button size="small" onClick={() => setSelectedOperation(record)}>日志</Button>, width: 100 },
          ]}
        />
      </Card>
      <Card size="small" title="最近操作" loading={overviewQuery.isLoading}>
        <Table
          rowKey="id"
          size="small"
          pagination={false}
          dataSource={overview.recentOperations ?? []}
          columns={[
            { title: '类型', render: (_value, record: VirtualizationOperation) => operationKind(record), width: 150 },
            { title: '资源', dataIndex: 'targetName', render: (value: string, record: VirtualizationOperation) => value || record.targetType || '-' },
            { title: '状态', dataIndex: 'status', render: statusTag, width: 120 },
            { title: '时间', render: (_value, record: VirtualizationOperation) => formatDateTime(operationTime(record)), width: 180 },
          ]}
        />
      </Card>
      <Drawer title="异常任务详情" size="large" open={Boolean(selectedOperation)} onClose={() => setSelectedOperation(null)}>
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
        <pre className="mt-4 max-h-[520px] overflow-auto rounded border border-[var(--kc-border)] bg-[var(--kc-surface-muted)] p-3 text-xs">
          {(logs.length
            ? logs.map((item) => `[${formatDateTime(item.createdAt)}] ${item.logLevel ?? 'info'} ${item.message}`).join('\n')
            : selectedOperation?.message) || (logsQuery.isLoading ? '日志加载中' : '暂无日志')}
        </pre>
      </Drawer>
      <Drawer title="连接风险详情" size="large" open={Boolean(selectedCluster)} onClose={() => setSelectedCluster(null)}>
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
      width: 220,
      render: (_value, record) => {
        if (!canManageVMs) return null
        const canPower = (action: string) => !record.allowedActions || hasAllowedAction(record.allowedActions, action)
        return (
          <Space>
            {canPower('start') ? <Button size="small" type="text" icon={<PlayCircleOutlined />} onClick={() => powerMutation.mutate({ id: record.id, action: 'start' })}>启动</Button> : null}
            {canPower('stop') ? <Button size="small" type="text" icon={<PoweroffOutlined />} onClick={() => powerMutation.mutate({ id: record.id, action: 'stop' })}>停止</Button> : null}
            {canPower('restart') ? <Button size="small" type="text" icon={<ReloadOutlined />} onClick={() => powerMutation.mutate({ id: record.id, action: 'restart' })}>重启</Button> : null}
            {canPower('delete') ? (
              <Popconfirm title="确认删除虚拟机？" onConfirm={() => powerMutation.mutate({ id: record.id, action: 'delete' })}>
                <Button size="small" type="text" danger icon={<DeleteOutlined />} />
              </Popconfirm>
            ) : null}
          </Space>
        )
      },
    },
  ]

  return (
    <div className="space-y-4">
      <PageHeader
        title="虚拟机"
        description="API 驱动的虚拟机实例列表与生命周期入口"
        showResourceScope={false}
        actions={canManageVMs ? <Button type="primary" icon={<PlusOutlined />} onClick={() => setDrawerOpen(true)}>创建虚拟机</Button> : null}
      />
      <TaskProgressBanner
        task={streamedTask}
        status={streamStatus}
        title="正在创建虚拟机"
        onCancel={streamedTask?.id ? () => cancelCreateMutation.mutate(streamedTask.id) : undefined}
        cancelling={cancelCreateMutation.isPending}
      />
      <Card size="small">
        <Form
          form={filterForm}
          className="mb-3"
          layout="inline"
          onFinish={(values) => setFilters((current) => ({ ...current, ...values, page: 1 }))}
        >
          <Form.Item name="search">
            <Input allowClear prefix={<SearchOutlined />} placeholder="搜索名称、IP 或节点" />
          </Form.Item>
          <Form.Item name="provider">
            <Select
              allowClear
              className="min-w-32"
              placeholder="Provider"
              options={[{ value: 'kubevirt', label: 'KubeVirt' }, { value: 'pve', label: 'PVE' }]}
            />
          </Form.Item>
          <Form.Item name="connectionId">
            <Select
              allowClear
              className="min-w-40"
              placeholder="连接"
              options={clusters.map((item) => ({ value: item.id, label: item.name }))}
            />
          </Form.Item>
          <Form.Item name="status">
            <Select
              allowClear
              className="min-w-32"
              placeholder="状态"
              options={['running', 'stopped', 'pending', 'failed'].map((item) => ({ value: item, label: item }))}
            />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" icon={<SearchOutlined />}>筛选</Button>
            <Button
              onClick={() => {
                filterForm.resetFields()
                setFilters({ page: 1, pageSize: filters.pageSize ?? 10 })
              }}
            >
              重置
            </Button>
          </Space>
        </Form>
        <Table
          rowKey="id"
          size="small"
          loading={vmsQuery.isLoading}
          dataSource={vmPage.items}
          columns={columns}
          scroll={{ x: 1340 }}
          pagination={{
            current: vmPage.page,
            pageSize: vmPage.pageSize,
            total: vmPage.total,
            showSizeChanger: true,
            onChange: (page, pageSize) => setFilters((current) => ({ ...current, page, pageSize })),
          }}
        />
      </Card>
      <Drawer title="创建虚拟机" size="large" open={drawerOpen} onClose={() => setDrawerOpen(false)}>
        <Form form={form} layout="vertical" initialValues={{ provider: 'kubevirt', startAfterCreate: true }} onFinish={(values) => createMutation.mutate(values)}>
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
              message={`已选择 ${selectedFlavor.name}: ${selectedFlavor.cpu}C / ${selectedFlavor.memoryMiB}MiB / ${selectedFlavor.diskGiB}GiB`}
            />
          ) : null}
          <Form.Item name="bootImageId" label="启动镜像" rules={[{ required: true }]}>
            <Select
              showSearch
              optionFilterProp="label"
              options={images
                .filter((item) => !createProvider || item.provider === createProvider || !item.provider)
                .map((item) => ({ value: item.id, label: item.connectionName ? `${item.name} (${item.connectionName})` : item.name }))}
            />
          </Form.Item>
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="namespace" label="命名空间">
              <Input />
            </Form.Item>
            <Form.Item name="node" label="节点">
              <Input />
            </Form.Item>
          </div>
          <Form.Item name="network" label="网络">
            <Input />
          </Form.Item>
          {createProvider === 'pve' ? (
            <div className="grid gap-3 md:grid-cols-3">
              <Form.Item name="pveStorage" label="PVE 存储">
                <Input placeholder="local-lvm" />
              </Form.Item>
              <Form.Item name="pveBridge" label="PVE 网桥">
                <Input placeholder="vmbr0" />
              </Form.Item>
              <Form.Item name="pveIso" label="ISO">
                <Input placeholder="local:iso/ubuntu.iso" />
              </Form.Item>
            </div>
          ) : (
            <div className="grid gap-3 md:grid-cols-2">
              <Form.Item name="kubevirtStorageClass" label="StorageClass">
                <Input />
              </Form.Item>
              <Form.Item name="kubevirtDataVolumeName" label="DataVolume">
                <Input />
              </Form.Item>
            </div>
          )}
          <Form.Item name="cloudInit" label="Cloud Init">
            <Input.TextArea rows={5} />
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
  const { id } = useParams()
  const location = useLocation()
  const pathParts = location.pathname.split('/').filter(Boolean)
  const vmId = id ?? decodeURIComponent(pathParts[pathParts.length - 1] ?? '')
  const { canViewMetrics, canAccessConsole } = useVirtualizationPermissions()
  const [metricsRange, setMetricsRange] = useState(60)
  const detailQuery = useQuery({
    queryKey: ['virtualization', 'vms', vmId, 'detail'],
    queryFn: () => virtualizationApi.vmDetail(vmId),
    enabled: Boolean(vmId),
  })
  const detail = detailQuery.data?.data
  const vm = detail?.vm
  const providerRaw = stringifyRaw(detail?.providerRaw)

  const isRunning = vm?.powerState === 'running' || vm?.status === 'running'

  const metricsQuery = useQuery({
    queryKey: ['virtualization', 'vm-metrics', vmId, metricsRange],
    queryFn: () => virtualizationApi.vmMetrics(vmId, metricsRange, metricsRange <= 60 ? 60 : 300),
    refetchInterval: 30000,
    enabled: Boolean(vmId) && isRunning,
  })

  return (
    <div className="space-y-4">
      <PageHeader
        title={vm?.name ?? '虚拟机详情'}
        description="规格、镜像、网络、Provider 原始视图和任务上下文"
        showResourceScope={false}
        actions={<Link to="/virtualization/vms"><Button>返回列表</Button></Link>}
      />
      {!vm && !detailQuery.isLoading ? (
        <Card size="small">
          <Empty description="未找到虚拟机详情" />
        </Card>
      ) : null}
      <Card size="small" loading={detailQuery.isLoading}>
        <Descriptions size="small" column={{ xs: 1, md: 2, xl: 3 }} bordered>
          <Descriptions.Item label="ID">{vm?.id ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="Provider">{vm?.provider ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="连接">{vm?.connectionName || vm?.connectionId || '-'}</Descriptions.Item>
          <Descriptions.Item label="状态">{statusTag(vm?.powerState || vm?.status)}</Descriptions.Item>
          <Descriptions.Item label="命名空间">{vm?.namespace || '-'}</Descriptions.Item>
          <Descriptions.Item label="节点">{vm?.node || '-'}</Descriptions.Item>
          <Descriptions.Item label="规格">{vm?.flavorName || vm?.flavorId || '-'}</Descriptions.Item>
          <Descriptions.Item label="CPU">{vm?.cpu ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="内存">{vm?.memoryMiB ? `${vm.memoryMiB} MiB` : '-'}</Descriptions.Item>
          <Descriptions.Item label="磁盘">{vm?.diskGiB ? `${vm.diskGiB} GiB` : '-'}</Descriptions.Item>
          <Descriptions.Item label="镜像">{vm?.bootImageName || vm?.bootImageId || '-'}</Descriptions.Item>
          <Descriptions.Item label="网络">{vm?.network || '-'}</Descriptions.Item>
          <Descriptions.Item label="IP">{vm?.ipAddresses?.join(', ') || '-'}</Descriptions.Item>
          <Descriptions.Item label="创建时间">{formatDateTime(vm?.createdAt)}</Descriptions.Item>
          <Descriptions.Item label="更新时间">{formatDateTime(vm?.updatedAt)}</Descriptions.Item>
        </Descriptions>
      </Card>
      <Tabs
        items={[
          {
            key: 'raw',
            label: 'Provider Raw',
            forceRender: true,
            children: (
              <Card size="small">
                <pre className="max-h-[520px] overflow-auto rounded border border-[var(--kc-border)] bg-[var(--kc-surface-muted)] p-3 text-xs">
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
                <Table
                  rowKey="id"
                  size="small"
                  dataSource={detail?.operations ?? []}
                  pagination={{ pageSize: 10 }}
                  columns={[
                    { title: '类型', render: (_value, record: VirtualizationOperation) => operationKind(record), width: 150 },
                    { title: '状态', dataIndex: 'status', render: statusTag, width: 120 },
                    { title: '消息', dataIndex: 'message', render: (value: string) => value || '-' },
                    { title: '时间', render: (_value, record: VirtualizationOperation) => formatDateTime(operationTime(record)), width: 180 },
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
                <pre className="max-h-[520px] overflow-auto rounded border border-[var(--kc-border)] bg-[var(--kc-surface-muted)] p-3 text-xs">
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
                  <Empty description="暂无指标数据" />
                )}
              </Card>
            ) : (
              <Card size="small">
                <Empty description="VM 未运行，无指标数据" />
              </Card>
            ),
          } : null,
          canAccessConsole ? {
            key: 'console',
            label: '控制台',
            children: isRunning ? (
              <VMConsole vmId={vmId} />
            ) : (
              <Card size="small">
                <Empty description="VM 未运行，无法访问控制台" />
              </Card>
            ),
          } : null,
        ].filter((item): item is NonNullable<typeof item> => item !== null)}
      />
    </div>
  )
}

export function VirtualizationClustersPage() {
  const [editing, setEditing] = useState<VirtualizationCluster | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [form] = Form.useForm<VirtualizationClusterFormValues>()
  const [showOnlyAbnormal, setShowOnlyAbnormal] = useState(false)
  const [enabledFilter, setEnabledFilter] = useState<'all' | 'enabled' | 'disabled'>('all')
  const [providerFilter, setProviderFilter] = useState<'all' | 'kubevirt' | 'pve'>('all')
  const [showNeverSynced, setShowNeverSynced] = useState(false)
  const { canManageClusters, canSync } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const provider = Form.useWatch('provider', form) ?? 'kubevirt'
  const clustersQuery = useQuery({ queryKey: ['virtualization', 'clusters'], queryFn: virtualizationApi.clusters })
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

  const columns: ColumnsType<VirtualizationCluster> = [
    { title: '名称', dataIndex: 'name', fixed: 'left', width: 180 },
    { title: 'Provider', dataIndex: 'provider', render: (value) => value || '-', width: 120 },
    { title: '接入目标', render: (_value, record) => record.provider === 'kubevirt' ? record.kubernetesClusterId || '-' : record.endpoint || '-', ellipsis: true },
    { title: '健康', dataIndex: 'health', render: (value, record) => statusTag(value || record.status), width: 120 },
    { title: '风险', render: (_value, record) => riskReasons(record).join(' / ') || '正常', width: 220 },
    { title: '凭证', dataIndex: 'credentialConfigured', render: (value: boolean | undefined) => value === false ? <Tag color="red">未配置</Tag> : <Tag color="green">已配置</Tag>, width: 120 },
    { ...tableColumnPresets.datetime, title: '最近同步', dataIndex: 'lastSyncedAt', render: formatDateTime, width: 180 },
    {
      ...tableColumnPresets.action,
      title: '操作',
      width: 260,
      render: (_value, record) => (
        <Space wrap>
          {canManageClusters ? <Button size="small" type="text" icon={<ThunderboltOutlined />} onClick={() => testMutation.mutate(record.id)}>测试</Button> : null}
          {canSync ? <Button size="small" type="text" icon={<CloudSyncOutlined />} onClick={() => syncMutation.mutate(record.id)}>同步</Button> : null}
          {canManageClusters ? <Button size="small" type="text" icon={<EditOutlined />} onClick={() => openEditor(record)}>编辑</Button> : null}
          {canManageClusters ? (
            <Popconfirm title="确认删除连接？" onConfirm={() => deleteMutation.mutate(record.id)}>
              <Button size="small" type="text" danger icon={<DeleteOutlined />} />
            </Popconfirm>
          ) : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="space-y-4">
      <PageHeader title="虚拟化集群" description="优先聚焦异常连接、同步风险与连接测试结果。" showResourceScope={false} actions={canManageClusters ? <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor()}>新增连接</Button> : null} />
      <Card size="small">
        <div className="mb-3 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <div>
            <div className="mb-1 text-xs text-[var(--kc-text-secondary)]">异常过滤</div>
            <Switch checked={showOnlyAbnormal} onChange={setShowOnlyAbnormal} checkedChildren="仅异常" unCheckedChildren="全部" />
          </div>
          <div>
            <div className="mb-1 text-xs text-[var(--kc-text-secondary)]">启用状态</div>
            <Select value={enabledFilter} onChange={setEnabledFilter} options={[{ value: 'all', label: '全部' }, { value: 'enabled', label: '仅启用' }, { value: 'disabled', label: '仅禁用' }]} />
          </div>
          <div>
            <div className="mb-1 text-xs text-[var(--kc-text-secondary)]">Provider</div>
            <Select value={providerFilter} onChange={setProviderFilter} options={[{ value: 'all', label: '全部' }, { value: 'kubevirt', label: 'KubeVirt' }, { value: 'pve', label: 'PVE' }]} />
          </div>
          <div>
            <div className="mb-1 text-xs text-[var(--kc-text-secondary)]">同步状态</div>
            <Switch checked={showNeverSynced} onChange={setShowNeverSynced} checkedChildren="未同步" unCheckedChildren="全部" />
          </div>
        </div>
        <Table
          rowKey="id"
          size="small"
          loading={clustersQuery.isLoading}
          dataSource={clusterRows}
          columns={columns}
          scroll={{ x: 1320 }}
          expandable={{
            expandedRowRender: (record) => (
              <Descriptions size="small" column={{ xs: 1, md: 2 }} bordered>
                <Descriptions.Item label="Endpoint / Cluster">{record.provider === 'kubevirt' ? record.kubernetesClusterId || '-' : record.endpoint || '-'}</Descriptions.Item>
                <Descriptions.Item label="默认命名空间">{record.defaultNamespace || '-'}</Descriptions.Item>
                <Descriptions.Item label="校验 TLS">{record.verifyTls === false ? '关闭' : '开启'}</Descriptions.Item>
                <Descriptions.Item label="最近同步">{formatDateTime(record.lastSyncedAt)}</Descriptions.Item>
                <Descriptions.Item label="Region">{record.region || '-'}</Descriptions.Item>
                <Descriptions.Item label="风险说明">{riskReasons(record).join(' / ') || '正常'}</Descriptions.Item>
              </Descriptions>
            ),
          }}
        />
      </Card>
      <Drawer title={editing ? '编辑连接' : '新增连接'} size="large" open={drawerOpen} onClose={() => setDrawerOpen(false)}>
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
    { title: 'Provider', dataIndex: 'provider', render: (value) => value || '-' },
    { title: '连接', dataIndex: 'connectionName', render: (value, record) => value || record.connectionId || '-' },
    { title: '命名空间', dataIndex: 'namespace', render: (value) => value || '-' },
    { title: '来源', render: (_value, record) => record.sourceKind || record.source || '-' },
    { title: '引用', dataIndex: 'sourceRef', render: (value) => value || '-' },
    { title: '系统', dataIndex: 'osType', render: (value) => value || '-' },
    { title: '大小', dataIndex: 'sizeGiB', render: (value) => value ? `${value} GiB` : '-' },
    { title: '状态', dataIndex: 'status', render: statusTag },
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: formatDateTime },
    {
      ...tableColumnPresets.action,
      title: '操作',
      render: (_value, record) => canManageImages ? (
        <Space>
          <Button size="small" type="text" icon={<EditOutlined />} onClick={() => openImageEditor(record)}>编辑</Button>
          <Popconfirm title="确认删除镜像入口？" onConfirm={() => deleteMutation.mutate(record.id)}>
            <Button size="small" type="text" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ) : null,
    },
  ]
  return (
    <div className="space-y-4">
      <PageHeader
        title="镜像与模板"
        description="管理 KubeVirt DataSource/PVC 与 PVE template/ISO 镜像入口"
        showResourceScope={false}
        actions={canManageImages ? <Button type="primary" icon={<PlusOutlined />} onClick={() => openImageEditor()}>新增镜像入口</Button> : null}
      />
      <Card size="small">
        <Form
          form={filterForm}
          className="mb-3"
          layout="inline"
          onFinish={(values) => setFilters((current) => ({ ...current, ...values, page: 1 }))}
        >
          <Form.Item name="search">
            <Input allowClear prefix={<SearchOutlined />} placeholder="搜索镜像、模板或 ISO" />
          </Form.Item>
          <Form.Item name="provider">
            <Select allowClear className="min-w-32" placeholder="Provider" options={[{ value: 'kubevirt', label: 'KubeVirt' }, { value: 'pve', label: 'PVE' }]} />
          </Form.Item>
          <Form.Item name="connectionId">
            <Select allowClear className="min-w-40" placeholder="连接" options={clusters.map((item) => ({ value: item.id, label: item.name }))} />
          </Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" icon={<SearchOutlined />}>筛选</Button>
            <Button
              onClick={() => {
                filterForm.resetFields()
                setFilters({ page: 1, pageSize: filters.pageSize ?? 10 })
              }}
            >
              重置
            </Button>
          </Space>
        </Form>
        <Table
          rowKey="id"
          size="small"
          loading={imagesQuery.isLoading}
          dataSource={imagesPage.items}
          columns={columns}
          scroll={{ x: 1220 }}
          pagination={{
            current: imagesPage.page,
            pageSize: imagesPage.pageSize,
            total: imagesPage.total,
            showSizeChanger: true,
            onChange: (page, pageSize) => setFilters((current) => ({ ...current, page, pageSize })),
          }}
        />
      </Card>
      <Drawer title={editing ? '编辑镜像入口' : '新增镜像入口'} size="large" open={drawerOpen} onClose={() => setDrawerOpen(false)}>
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
  const [form] = Form.useForm<VirtualizationFlavorInput>()
  const { canManageFlavors } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const flavorsQuery = useQuery({ queryKey: ['virtualization', 'flavors'], queryFn: virtualizationApi.flavors })
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
        <Space>
          <Button size="small" type="text" icon={<EditOutlined />} onClick={() => openEditor(record)}>编辑</Button>
          <Popconfirm title="确认删除规格？" onConfirm={() => deleteMutation.mutate(record.id)}>
            <Button size="small" type="text" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ) : null,
    },
  ]
  return (
    <div className="space-y-4">
      <PageHeader title="规格" description="虚拟机标准规格管理" showResourceScope={false} actions={canManageFlavors ? <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor()}>新增规格</Button> : null} />
      <Card size="small">
        <Table rowKey="id" size="small" loading={flavorsQuery.isLoading} dataSource={flavorsQuery.data?.data ?? []} columns={columns} scroll={{ x: 900 }} />
      </Card>
      <Drawer title={editing ? '编辑规格' : '新增规格'} size="large" open={drawerOpen} onClose={() => setDrawerOpen(false)}>
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
  return (
    <div className="space-y-4">
      <PageHeader title="操作记录" description="虚拟化任务与操作日志" showResourceScope={false} />
      <Card size="small">
        <OperationsTable initialPreset={operationPresetFromSearch(location.search)} />
      </Card>
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
    <div className="space-y-4">
      <PageHeader title="同步任务" description="虚拟化资产同步任务与日志" showResourceScope={false} actions={headerActions} />
      <Card size="small">
        <Paragraph type="secondary">仅展示 asset_sync 类型任务。</Paragraph>
        <OperationsTable assetType="asset_sync" />
      </Card>
    </div>
  )
}
