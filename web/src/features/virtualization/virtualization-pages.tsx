import { useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
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

function OperationsTable({ assetType }: { assetType?: string }) {
  const [selectedOperation, setSelectedOperation] = useState<VirtualizationOperation | null>(null)
  const { canManageOperations } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
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
    { title: '资源', dataIndex: 'targetName', render: (value, record) => value || record.targetType || record.assetType || '-' },
    { title: '连接', dataIndex: 'connectionName', render: (value, record) => value || record.connectionId || '-' },
    { ...tableColumnPresets.status, title: '状态', dataIndex: 'status', render: statusTag },
    { title: '操作者', dataIndex: 'actor', render: (value) => value || '-' },
    { ...tableColumnPresets.datetime, title: '开始时间', dataIndex: 'startedAt', render: (_value, record) => formatDateTime(operationTime(record)) },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_value, record) => {
        const canCancel = canManageOperations && hasAllowedAction(record.allowedActions, 'cancel')
        const canRetry = canManageOperations && hasAllowedAction(record.allowedActions, 'retry')
        return (
          <Space>
            <Button size="small" type="text" icon={<FileTextOutlined />} onClick={() => setSelectedOperation(record)}>
              日志
            </Button>
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

  return (
    <>
      <Table
        rowKey="id"
        size="small"
        loading={operationsQuery.isLoading}
        dataSource={operations}
        columns={columns}
        scroll={{ x: 940 }}
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
        </Descriptions>
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
  const { canSync } = useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [syncTaskId, setSyncTaskId] = useState<string | null>(null)
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
  const overview: VirtualizationOverview = overviewQuery.data?.data ?? {}
  const stats = overview.stats ?? {}
  const health = stats.connections

  return (
    <div className="space-y-4">
      <PageHeader
        title="总览"
        description="虚拟化连接健康、资源统计和近期任务"
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
      <div className="grid gap-3 md:grid-cols-3 xl:grid-cols-6">
        <StatCard label="连接" value={health?.total ?? 0} extra={`健康 ${health?.healthy ?? 0} / 异常 ${(health?.degraded ?? 0) + (health?.unavailable ?? 0)}`} />
        <StatCard label="虚拟机" value={stats.vmCount ?? 0} extra={`运行 ${stats.runningVmCount ?? 0} / 停止 ${stats.stoppedVmCount ?? 0}`} />
        <StatCard label="镜像" value={stats.imageCount ?? 0} />
        <StatCard label="规格" value={stats.flavorCount ?? 0} />
        <StatCard label="待处理任务" value={stats.pendingTaskCount ?? 0} />
        <StatCard label="失败任务" value={stats.failedTaskCount ?? 0} />
      </div>
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

  const columns: ColumnsType<VirtualizationCluster> = [
    { title: '名称', dataIndex: 'name', fixed: 'left', width: 180 },
    { title: 'Provider', dataIndex: 'provider', render: (value) => value || '-' },
    { title: '接入目标', render: (_value, record) => record.provider === 'kubevirt' ? record.kubernetesClusterId || '-' : record.endpoint || '-', ellipsis: true },
    { title: '健康', dataIndex: 'health', render: (value, record) => statusTag(value || record.status), width: 120 },
    { title: '版本', dataIndex: 'version', render: (value) => value || '-' },
    { ...tableColumnPresets.datetime, title: '最近同步', dataIndex: 'lastSyncedAt', render: formatDateTime },
    {
      ...tableColumnPresets.action,
      title: '操作',
      width: 260,
      render: (_value, record) => (
        <Space>
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
      <PageHeader title="虚拟化集群" description="虚拟化后端连接管理、健康测试和资产同步" showResourceScope={false} actions={canManageClusters ? <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor()}>新增连接</Button> : null} />
      <Card size="small">
        <Table rowKey="id" size="small" loading={clustersQuery.isLoading} dataSource={clustersQuery.data?.data ?? []} columns={columns} scroll={{ x: 1120 }} />
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
  return (
    <div className="space-y-4">
      <PageHeader title="操作记录" description="虚拟化任务与操作日志" showResourceScope={false} />
      <Card size="small">
        <OperationsTable />
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
