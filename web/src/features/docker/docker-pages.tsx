import type { ComponentProps, ReactNode } from 'react'
import { useEffect, useMemo, useState } from 'react'
import { App, Alert, Badge, Button, Card, Descriptions, Drawer, Form, Input, InputNumber, Popconfirm, Segmented, Select, Space, Switch, Tabs, Tag, Typography } from 'antd'
import type { FormInstance } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { CloudServerOutlined, DeleteOutlined, DockerOutlined, EditOutlined, FileTextOutlined, MinusCircleOutlined, PlayCircleOutlined, PlusOutlined, PoweroffOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Navigate, useParams } from 'react-router-dom'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { formatDateTime } from '@/utils/time'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementDensityButton,
  ManagementDetailHeader,
  ManagementIconButton,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import { virtualizationApi } from '@/features/virtualization/virtualization-api'
import type { VirtualizationCluster, VirtualizationFlavor, VirtualizationImage, VirtualizationPage } from '@/features/virtualization/virtualization-types'
import '@/features/virtualization/virtualization-workbench.css'
import { dockerApi } from './docker-api'
import { DockerProjectLogsPanel, DockerProjectTerminalPanel, DockerProjectVolumesPanel } from './docker-runtime-panels'
import './docker-pages.css'
import type { DockerContainerStartInput, DockerHost, DockerHostInput, DockerListParams, DockerOperation, DockerOperationLog, DockerPage, DockerPortMapping, DockerPortMappingInput, DockerProject, DockerProjectInput, DockerQuickCreateHostInput, DockerService, DockerTemplate, DockerTemplateInput } from './docker-types'

const { Text } = Typography
const { TextArea } = Input

type OverviewTone = 'default' | 'success' | 'warning' | 'danger'
type OperationPreset = 'all' | 'pending' | 'abnormal' | 'host' | 'project' | 'service'

interface DockerFilterState extends DockerListParams {
  operationKind?: string
  abnormal?: boolean
  pending?: boolean
  kind?: string
  enabled?: boolean
}

type DockerProjectSourceKind = 'compose' | 'single_container'

interface HostFormValues extends DockerHostInput {
  memoryGiB?: number
  diskGiB?: number
}

interface QuickCreateHostFormValues extends DockerQuickCreateHostInput {
  memoryGiB?: number
  diskGiB?: number
}

interface DockerPortFormValues extends Omit<DockerPortMappingInput, 'expiresAt'> {
  expiresAt?: string
}

type ContainerStartResourceFormValues = NonNullable<DockerContainerStartInput['resources']> & {
  memoryMiB?: number
  memoryReservationMiB?: number
}

interface ContainerStartFormValues extends Omit<DockerContainerStartInput, 'resources'> {
  resources?: ContainerStartResourceFormValues
}

const STATUS_COLORS: Record<string, string> = {
  active: 'green',
  completed: 'green',
  defined: 'blue',
  degraded: 'gold',
  disabled: 'default',
  draft: 'default',
  error: 'red',
  exited: 'default',
  failed: 'red',
  healthy: 'green',
  offline: 'default',
  online: 'green',
  pending: 'gold',
  provisioning: 'blue',
  queued: 'gold',
  ready: 'green',
  released: 'default',
  running: 'green',
  stopped: 'default',
  timeout: 'red',
  callback_timeout: 'red',
  unavailable: 'red',
  unknown: 'default',
}

const DEFAULT_COMPOSE = `services:\n  web:\n    image: nginx:alpine\n    ports:\n      - "8080:80"\n`

const DOCKER_QUERY_ROOT = ['docker'] as const
const ARCHITECTURE_OPTIONS = [
  { value: 'amd64', label: 'x86_64 / amd64' },
  { value: 'arm64', label: 'ARM64 / aarch64' },
]
const DEFAULT_CONTAINER_PORTS = [{ name: 'http', hostIp: '0.0.0.0', containerPort: 80, hostPort: 18080, protocol: 'tcp', exposureScope: 'internal', domainScheme: 'http', domainTlsEnabled: false }]
const VIRTUALIZATION_PROVIDER_LABELS: Record<string, string> = {
  kubevirt: 'KubeVirt',
  pve: 'PVE',
}

type AdminTableProps = ComponentProps<typeof AdminTable>

const DOCKER_PAGINATION_SUMMARY: NonNullable<AdminTableProps['paginationSummary']> = (total, range) => {
  if (total <= 0) return '当前 0 / 0 条'
  return `当前 ${range[0]}-${range[1]} / ${total} 条`
}

function classNames(...items: Array<string | false | null | undefined>) {
  return items.filter(Boolean).join(' ')
}

function statusTag(value?: string) {
  if (!value) return <Text type="secondary">-</Text>
  const key = value.toLowerCase()
  return <Tag color={STATUS_COLORS[key] ?? 'default'}>{value}</Tag>
}

function boolTag(value?: boolean) {
  return <Tag color={value ? 'green' : 'default'}>{value ? '启用' : '停用'}</Tag>
}

function architectureTag(value?: string) {
  if (!value) return <Text type="secondary">-</Text>
  const normalized = String(value).toLowerCase()
  const color = normalized === 'arm64' || normalized === 'aarch64' ? 'cyan' : 'geekblue'
  return <Tag color={color}>{normalized === 'amd64' ? 'x86_64' : normalized}</Tag>
}

function formatBytes(value?: number) {
  if (!value || value <= 0) return '-'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${size >= 10 || index === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[index]}`
}

function configTextValue(config: Record<string, unknown> | undefined, key: string) {
  const value = config?.[key]
  if (typeof value === 'string') return value.trim() || '-'
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return '-'
}

function configArrayCount(config: Record<string, unknown> | undefined, key: string) {
  const value = config?.[key]
  return Array.isArray(value) ? value.length : 0
}

function formatPercent(value?: number) {
  if (value === undefined || value === null) return '-'
  return `${value.toFixed(value >= 10 ? 0 : 1)}%`
}

function formatPort(record: DockerPortMapping) {
  const host = `${record.hostIp || '0.0.0.0'}:${record.hostPort}`
  return `${host} -> ${record.containerPort}/${record.protocol || 'tcp'}`
}

function stringValue(value: unknown) {
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

function numberValue(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : undefined
  }
  return undefined
}

function projectPortItems(record: DockerProject) {
  const config = record.config ?? {}
  const rawPorts = Array.isArray(config.ports) ? config.ports : []
  const ports = rawPorts
    .map((item) => item && typeof item === 'object' ? item as Record<string, unknown> : undefined)
    .filter((item): item is Record<string, unknown> => Boolean(item))
  if (ports.length > 0) {
    return ports
  }
  const hostPort = numberValue(config.hostPort)
  const containerPort = numberValue(config.containerPort)
  if (!hostPort && !containerPort) {
    return []
  }
  return [{
    hostIp: config.hostIp,
    hostPort,
    containerPort,
    protocol: config.protocol,
    domainName: config.domainName,
    domainScheme: config.domainScheme,
    domainTlsEnabled: config.domainTlsEnabled,
    exposureScope: config.exposureScope,
  }]
}

function formatProjectPortItem(item: Record<string, unknown>) {
  const hostPort = numberValue(item.hostPort)
  const containerPort = numberValue(item.containerPort)
  const protocol = stringValue(item.protocol) || 'tcp'
  const hostIp = stringValue(item.hostIp) || '0.0.0.0'
  if (!hostPort && !containerPort) {
    return '-'
  }
  return `${hostPort ? `${hostIp}:${hostPort}` : '-'} -> ${containerPort || '-'}/${protocol}`
}

function renderProjectPortSummary(record: DockerProject) {
  const ports = projectPortItems(record)
  if (ports.length === 0) {
    return <Text type="secondary">-</Text>
  }
  return (
    <Space orientation="vertical" size={0} className="soha-docker-port-summary">
      {ports.slice(0, 2).map((item, index) => {
        const domainName = stringValue(item.domainName)
        return (
          <span key={`${formatProjectPortItem(item)}-${index}`} className="soha-docker-port-summary-item">
            <Text code>{formatProjectPortItem(item)}</Text>
            {domainName ? <Text type="secondary">{domainName}</Text> : null}
          </span>
        )
      })}
      {ports.length > 2 ? <Tag>+{ports.length - 2}</Tag> : null}
    </Space>
  )
}

function formatAccessURL(record: DockerPortMapping) {
  if (record.accessUrl) return record.accessUrl
  if (record.domainName) return `${record.domainScheme || (record.domainTlsEnabled ? 'https' : 'http')}://${record.domainName}`
  return ''
}

function compactRecord<T extends object>(values: T): T {
  return Object.fromEntries(
    Object.entries(values).filter(([, value]) => value !== undefined && value !== '' && value !== null),
  ) as T
}

function bytesFromMiB(value?: number) {
  return value && value > 0 ? Math.round(value * 1024 ** 2) : undefined
}

function normalizePage<T>(data: DockerPage<T> | undefined, fallbackPage: number, fallbackPageSize: number): DockerPage<T> {
  return data ?? { items: [], total: 0, page: fallbackPage, pageSize: fallbackPageSize }
}

function queryData<T>(response: { data: T } | undefined, fallback: T) {
  return response?.data ?? fallback
}

function virtualizationItems<T>(data: VirtualizationPage<T> | T[] | undefined): T[] {
  if (!data) return []
  return Array.isArray(data) ? data : data.items ?? []
}

function stringConfigValue(config: Record<string, unknown> | undefined, key: string) {
  const value = config?.[key]
  return typeof value === 'string' ? value.trim() : ''
}

function providerLabel(provider?: string) {
  return VIRTUALIZATION_PROVIDER_LABELS[String(provider || '').toLowerCase()] ?? provider ?? '-'
}

function isProvisionConnection(item: VirtualizationCluster) {
  const provider = String(item.provider || '').toLowerCase()
  return item.enabled !== false && (provider === 'pve' || provider === 'kubevirt')
}

function isProvisionImage(item: VirtualizationImage) {
  const provider = String(item.provider || '').toLowerCase()
  const sourceKind = String(item.sourceKind || item.assetKind || '').toLowerCase()
  if (provider === 'pve') {
    return sourceKind === '' || sourceKind === 'template' || sourceKind === 'iso'
  }
  if (provider === 'kubevirt') {
    return sourceKind === '' || sourceKind === 'datasource' || sourceKind === 'pvc' || sourceKind === 'containerdisk' || sourceKind === 'container_disk'
  }
  return false
}

function pageTablePagination<T>(
  page: DockerPage<T>,
  embedded: boolean,
  setFilters: React.Dispatch<React.SetStateAction<DockerFilterState>>,
) {
  if (embedded) return false
  return {
    current: page.page,
    pageSize: page.pageSize,
    total: page.total,
    onPageChange: (pageNumber: number) => setFilters((current) => ({ ...current, page: pageNumber })),
    onPageSizeChange: (pageSize: number) => setFilters((current) => ({ ...current, page: 1, pageSize })),
  }
}

function refreshDocker(queryClient: ReturnType<typeof useQueryClient>) {
  return queryClient.invalidateQueries({ queryKey: DOCKER_QUERY_ROOT })
}

function isPendingOperation(status?: string) {
  return ['queued', 'running'].includes(String(status || '').toLowerCase())
}

function isAbnormalOperation(status?: string) {
  return ['failed', 'callback_timeout', 'timeout', 'error'].includes(String(status || '').toLowerCase())
}

function badgeStatusForTone(tone: OverviewTone): 'success' | 'warning' | 'error' | 'default' {
  if (tone === 'success') return 'success'
  if (tone === 'warning') return 'warning'
  if (tone === 'danger') return 'error'
  return 'default'
}

function operationTone(record: DockerOperation): OverviewTone {
  if (isAbnormalOperation(record.status)) return 'danger'
  if (isPendingOperation(record.status)) return 'warning'
  if (record.status === 'completed') return 'success'
  return 'default'
}

function operationActionLabel(action: string) {
  return ({
    deploy: '部署',
    redeploy: '重新部署',
    start: '启动',
    stop: '停止',
    restart: '重启',
    down: 'Down',
    pull: 'Pull',
    build: 'Build',
    destroy: '销毁',
  } as Record<string, string>)[action] ?? action
}

function useDockerPermissions() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  return {
    canManageHosts: hasPermission(snapshot, 'docker.hosts.manage'),
    canManageProjects: hasPermission(snapshot, 'docker.projects.manage'),
    canDeployProjects: hasPermission(snapshot, 'docker.projects.deploy'),
    canViewServices: hasPermission(snapshot, 'docker.services.view'),
    canManageServices: hasPermission(snapshot, 'docker.services.manage'),
    canViewPorts: hasPermission(snapshot, 'docker.ports.view'),
    canManagePorts: hasPermission(snapshot, 'docker.ports.manage'),
    canManageTemplates: hasPermission(snapshot, 'docker.templates.manage'),
    canManageOperations: hasPermission(snapshot, 'docker.operations.manage'),
  }
}

function DockerTableHeader({
  actions,
  meta = [],
  status,
  title,
  tone = 'default',
}: {
  actions?: ReactNode
  meta?: string[]
  status?: string
  title: string
  tone?: OverviewTone
}) {
  return (
    <ManagementDetailHeader
      title={(
        <Space size={8} wrap>
          <span>{title}</span>
          {status ? <Badge status={badgeStatusForTone(tone)} text={status} /> : null}
        </Space>
      )}
      meta={meta.length > 0 ? meta.map((item) => <span key={item}>{item}</span>) : undefined}
      actions={actions ? <ManagementTableToolbar>{actions}</ManagementTableToolbar> : undefined}
    />
  )
}

function DockerFilterActions({ loading, onReset }: { loading?: boolean; onReset: () => void }) {
  return (
    <Space size={8}>
      <Button htmlType="submit" icon={<SearchOutlined />} loading={loading} type="primary">查询</Button>
      <Button htmlType="button" onClick={onReset}>重置</Button>
    </Space>
  )
}

type DockerAdminTableProps = Omit<
  AdminTableProps,
  | 'columnSettingIconOnly'
  | 'columnSettingPlacement'
  | 'headerExtra'
  | 'paginationSummary'
  | 'shellClassName'
  | 'tableSize'
  | 'toolbarExtra'
> & {
  actions?: ReactNode
  enableDensity?: boolean
  paginationSummary?: AdminTableProps['paginationSummary']
  refreshing?: boolean
  showColumnSettings?: boolean
  showRefresh?: boolean
  shellClassName?: string
  onRefresh?: () => void
}

function DockerAdminTable({
  actions,
  className,
  enableDensity = true,
  onRefresh,
  paginationSummary = DOCKER_PAGINATION_SUMMARY,
  refreshing,
  scroll,
  shellClassName,
  showColumnSettings = true,
  showRefresh = true,
  title,
  ...tableProps
}: DockerAdminTableProps) {
  const [tableSize, setTableSize] = useState<NonNullable<AdminTableProps['tableSize']>>('small')
  const toolbarExtra = actions || enableDensity || (showRefresh && onRefresh) ? (
    <ManagementTableToolbar>
      {actions}
      {enableDensity ? (
        <ManagementDensityButton
          aria-label="切换表格密度"
          size="small"
          tooltip={tableSize === 'small' ? '切换为宽松密度' : '切换为紧凑密度'}
          onClick={() => setTableSize((current) => current === 'small' ? 'middle' : 'small')}
        />
      ) : null}
      {showRefresh && onRefresh ? (
        <ManagementRefreshButton
          aria-label="刷新列表"
          loading={refreshing}
          size="small"
          tooltip="刷新"
          onClick={onRefresh}
        />
      ) : null}
    </ManagementTableToolbar>
  ) : undefined

  return (
    <AdminTable
      {...tableProps}
      className={classNames('soha-vrt-table', className)}
      columnSettingIconOnly
      columnSettingPlacement={showColumnSettings ? (title ? 'header' : 'toolbar') : 'hidden'}
      paginationSummary={paginationSummary}
      scroll={scroll}
      shellClassName={classNames('soha-management-table-shell', 'soha-docker-table-shell', shellClassName)}
      tableSize={tableSize}
      title={title}
      toolbarExtra={toolbarExtra}
    />
  )
}

function MetricCard({ label, value, helper, tone = 'default', onClick }: { label: string; value: number | string; helper?: string; tone?: OverviewTone; onClick?: () => void }) {
  return (
    <Card size="small" variant="outlined" className={`soha-vrt-metric-card is-${tone}`}>
      <button type="button" className="soha-vrt-metric-card-button" onClick={onClick} disabled={!onClick}>
        <span className="soha-overview-metric-label">{label}</span>
        <span className="soha-vrt-stat-value">{value}</span>
        {helper ? <span className="soha-overview-metric-helper">{helper}</span> : null}
      </button>
    </Card>
  )
}

function SummaryChips({ counts, compact = false }: { counts: Array<{ key: string; label: string; value?: number; tone?: OverviewTone }>; compact?: boolean }) {
  return (
    <div className={`soha-vrt-chip-grid${compact ? ' is-compact' : ''}`}>
      {counts.map((item) => (
        <div key={item.key} className={`soha-vrt-chip is-${item.tone ?? 'default'}`}>
          <span className="soha-vrt-chip-label">{item.label}</span>
          <span className="soha-vrt-chip-value">{item.value ?? 0}</span>
        </div>
      ))}
    </div>
  )
}

function DrawerFooter({ form, loading, onCancel, submitLabel = '提交' }: { form: FormInstance; loading?: boolean; onCancel: () => void; submitLabel?: string }) {
  return (
    <Space>
      <Button type="primary" loading={loading} onClick={() => form.submit()}>{submitLabel}</Button>
      <Button onClick={onCancel}>取消</Button>
    </Space>
  )
}

function useDockerOptions() {
  const hostsQuery = useQuery({ queryKey: ['docker', 'hosts', 'options'], queryFn: () => dockerApi.hosts({ page: 1, pageSize: 200 }) })
  const projectsQuery = useQuery({ queryKey: ['docker', 'projects', 'options'], queryFn: () => dockerApi.projects({ page: 1, pageSize: 200 }) })
  const servicesQuery = useQuery({ queryKey: ['docker', 'services', 'options'], queryFn: () => dockerApi.services({ page: 1, pageSize: 300 }) })
  const hosts = normalizePage(hostsQuery.data?.data, 1, 200).items
  const projects = normalizePage(projectsQuery.data?.data, 1, 200).items
  const services = normalizePage(servicesQuery.data?.data, 1, 300).items
  return {
    hosts,
    projects,
    services,
    hostOptions: hosts.map((item) => ({ value: item.id, label: item.name || item.id })),
    projectOptions: projects.map((item) => ({ value: item.id, label: item.name || item.id })),
    serviceOptions: services.map((item) => ({ value: item.id, label: item.name || item.id })),
  }
}

function useVirtualizationProvisionOptions(enabled: boolean) {
  const clustersQuery = useQuery({
    queryKey: ['virtualization', 'clusters', 'docker-provision-options'],
    queryFn: virtualizationApi.clusters,
    enabled,
  })
  const imagesQuery = useQuery({
    queryKey: ['virtualization', 'images', 'docker-provision-options'],
    queryFn: () => virtualizationApi.images({ page: 1, pageSize: 500 }),
    enabled,
  })
  const flavorsQuery = useQuery({
    queryKey: ['virtualization', 'flavors', 'docker-provision-options'],
    queryFn: virtualizationApi.flavors,
    enabled,
  })
  const connections = queryData(clustersQuery.data, [] as VirtualizationCluster[]).filter(isProvisionConnection)
  const images = virtualizationItems(imagesQuery.data?.data).filter(isProvisionImage)
  const flavors = queryData(flavorsQuery.data, [] as VirtualizationFlavor[]).filter((item) => item.enabled !== false)
  return {
    connections,
    images,
    flavors,
    loading: clustersQuery.isLoading || imagesQuery.isLoading || flavorsQuery.isLoading,
  }
}

function buildHostPayload(values: HostFormValues): DockerHostInput {
  return compactRecord({
    ...values,
    memoryBytes: values.memoryGiB ? Math.round(values.memoryGiB * 1024 ** 3) : values.memoryBytes,
    diskBytes: values.diskGiB ? Math.round(values.diskGiB * 1024 ** 3) : values.diskBytes,
    memoryGiB: undefined,
    diskGiB: undefined,
  })
}

function hostToForm(record?: DockerHost): Partial<HostFormValues> {
  if (!record) return { status: 'pending', architecture: 'amd64', availablePortStart: 20000, availablePortEnd: 39999 }
  return {
    ...record,
    memoryGiB: record.memoryBytes ? Math.round((record.memoryBytes / 1024 ** 3) * 10) / 10 : undefined,
    diskGiB: record.diskBytes ? Math.round((record.diskBytes / 1024 ** 3) * 10) / 10 : undefined,
  }
}

export function buildQuickHostPayload(values: QuickCreateHostFormValues): DockerQuickCreateHostInput {
  return compactRecord({
    ...values,
    memoryBytes: values.memoryGiB ? Math.round(values.memoryGiB * 1024 ** 3) : values.memoryBytes,
    diskBytes: values.diskGiB ? Math.round(values.diskGiB * 1024 ** 3) : values.diskBytes,
    memoryGiB: undefined,
    diskGiB: undefined,
  })
}

function buildProjectPayload(values: DockerProjectInput): DockerProjectInput {
  return compactRecord({
    ...values,
    composeContent: values.composeContent || DEFAULT_COMPOSE,
    status: values.status || 'draft',
    sourceKind: values.sourceKind || 'inline_compose',
  })
}

function buildPortPayload(values: DockerPortFormValues): DockerPortMappingInput {
  return compactRecord({
    ...values,
    protocol: values.protocol || 'tcp',
    exposureScope: values.exposureScope || 'internal',
    status: values.status || 'active',
  })
}

export function buildContainerStartPayload(values: ContainerStartFormValues): DockerContainerStartInput {
  const { ports: formPorts, volumes: formVolumes, environmentVariables: formEnvVars, resources: formResources, ...rest } = values
  const ports = (formPorts ?? [])
    .map((port) => compactRecord({
      ...port,
      protocol: port.protocol || rest.protocol || 'tcp',
      exposureScope: port.exposureScope || rest.exposureScope || 'internal',
      domainScheme: port.domainName ? port.domainScheme || (port.domainTlsEnabled ? 'https' : 'http') : undefined,
      domainTlsEnabled: port.domainName ? Boolean(port.domainTlsEnabled) : undefined,
    }))
    .filter((port) => port.hostPort || port.containerPort)
  const volumes = (formVolumes ?? [])
    .map((volume) => compactRecord({
      ...volume,
      type: volume.type || undefined,
      readOnly: Boolean(volume.readOnly),
    }))
    .filter((volume) => volume.source || volume.target)
  const environmentVariables = (formEnvVars ?? [])
    .map((item) => compactRecord(item))
    .filter((item) => item.name || item.value)
  const resources = compactRecord({
    cpus: formResources?.cpus && formResources.cpus > 0 ? formResources.cpus : undefined,
    memoryBytes: bytesFromMiB(formResources?.memoryMiB) ?? formResources?.memoryBytes,
    memoryReservationBytes: bytesFromMiB(formResources?.memoryReservationMiB) ?? formResources?.memoryReservationBytes,
  })
  const primaryPort = ports[0]
  return compactRecord({
    ...rest,
    architecture: rest.architecture || undefined,
    containerPort: primaryPort?.containerPort ?? rest.containerPort,
    hostIp: primaryPort?.hostIp ?? rest.hostIp,
    hostPort: primaryPort?.hostPort ?? rest.hostPort,
    protocol: primaryPort?.protocol || rest.protocol || 'tcp',
    exposureScope: primaryPort?.exposureScope || rest.exposureScope || 'internal',
    domainName: primaryPort?.domainName ?? rest.domainName,
    domainScheme: primaryPort?.domainName ? primaryPort.domainScheme || (primaryPort.domainTlsEnabled ? 'https' : 'http') : undefined,
    domainTlsEnabled: primaryPort?.domainName ? Boolean(primaryPort.domainTlsEnabled) : undefined,
    restartPolicy: rest.restartPolicy || 'unless-stopped',
    ports: ports.length ? ports : undefined,
    volumes: volumes.length ? volumes : undefined,
    environmentVariables: environmentVariables.length ? environmentVariables : undefined,
    resources: Object.keys(resources).length ? resources : undefined,
  })
}

function buildTemplatePayload(values: DockerTemplateInput): DockerTemplateInput {
  return compactRecord({
    ...values,
    templateKind: values.templateKind || 'compose',
    enabled: values.enabled !== false,
  })
}

function OperationLogDrawer({ operation, logs, loading, open, onClose }: { operation?: DockerOperation | null; logs: DockerOperationLog[]; loading?: boolean; open: boolean; onClose: () => void }) {
  const text = logs.length
    ? logs.map((item) => `[${formatDateTime(item.createdAt)}] ${item.logLevel || 'info'} ${item.message}`).join('\n')
    : JSON.stringify(operation?.payload ?? {}, null, 2)
  return (
    <Drawer title="操作日志" size="large" open={open} onClose={onClose}>
      {operation ? (
        <Descriptions size="small" column={2} bordered className="mb-3">
          <Descriptions.Item label="任务 ID">{operation.id}</Descriptions.Item>
          <Descriptions.Item label="状态">{statusTag(operation.status)}</Descriptions.Item>
          <Descriptions.Item label="类型">{operation.operationKind}</Descriptions.Item>
          <Descriptions.Item label="发起人">{operation.requestedBy || '-'}</Descriptions.Item>
        </Descriptions>
      ) : null}
      <pre className="max-h-[560px] overflow-auto rounded border border-[var(--soha-border-color)] bg-[var(--soha-bg-surface-muted)] p-3 text-xs">
        {loading ? '日志加载中' : (text || '暂无日志')}
      </pre>
    </Drawer>
  )
}

function HostsTable({ embedded = false }: { embedded?: boolean }) {
  const [filters, setFilters] = useState<DockerFilterState>({ page: 1, pageSize: embedded ? 5 : 10 })
  const [filterForm] = Form.useForm<DockerFilterState>()
  const [form] = Form.useForm<HostFormValues>()
  const [quickForm] = Form.useForm<QuickCreateHostFormValues>()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [quickDrawerOpen, setQuickDrawerOpen] = useState(false)
  const [editing, setEditing] = useState<DockerHost | null>(null)
  const { canManageHosts } = useDockerPermissions()
  const provisionOptions = useVirtualizationProvisionOptions(canManageHosts)
  const selectedProvisionConnectionID = Form.useWatch('virtualizationConnectionId', quickForm)
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const hostsQuery = useQuery({ queryKey: ['docker', 'hosts', filters], queryFn: () => dockerApi.hosts(filters) })
  const createMutation = useMutation({
    mutationFn: (values: HostFormValues) => editing ? dockerApi.updateHost(editing.id, buildHostPayload(values)) : dockerApi.createHost(buildHostPayload(values)),
    onSuccess: () => {
      message.success(editing ? '主机已更新' : '主机已创建')
      setDrawerOpen(false)
      setEditing(null)
      form.resetFields()
      refreshDocker(queryClient)
    },
  })
  const quickCreateMutation = useMutation({
    mutationFn: (values: QuickCreateHostFormValues) => dockerApi.quickCreateHost(buildQuickHostPayload(values)),
    onSuccess: () => {
      message.success('虚拟化构建任务已提交')
      setQuickDrawerOpen(false)
      quickForm.resetFields()
      refreshDocker(queryClient)
    },
  })
  const deleteMutation = useMutation({
    mutationFn: dockerApi.deleteHost,
    onSuccess: () => {
      message.success('主机已删除')
      refreshDocker(queryClient)
    },
  })
  const page = normalizePage(hostsQuery.data?.data, filters.page ?? 1, filters.pageSize ?? 10)
  const selectedProvisionConnection = provisionOptions.connections.find((item) => item.id === selectedProvisionConnectionID)
  const selectedProvisionProvider = String(selectedProvisionConnection?.provider || '').toLowerCase()
  const quickConnectionOptions = provisionOptions.connections.map((item) => ({
    value: item.id,
    label: `${item.name || item.id} (${providerLabel(item.provider)})`,
  }))
  const quickImageOptions = provisionOptions.images
    .filter((item) => !selectedProvisionConnectionID || item.connectionId === selectedProvisionConnectionID)
    .map((item) => ({
      value: item.id,
      label: [item.name || item.id, item.sourceKind || item.assetKind, item.sourceRef].filter(Boolean).join(' / '),
    }))
  const quickFlavorOptions = provisionOptions.flavors.map((item) => ({
    value: item.id,
    label: `${item.name || item.id} (${item.cpu}C / ${formatBytes(item.memoryMiB * 1024 ** 2)} / ${item.diskGiB || '-'}GiB)`,
  }))
  const applyProvisionConnectionDefaults = (connectionID?: string) => {
    const connection = provisionOptions.connections.find((item) => item.id === connectionID)
    const provider = String(connection?.provider || '').toLowerCase()
    quickForm.setFieldsValue({
      imageId: undefined,
      vmTemplateId: undefined,
      network: provider === 'pve' ? stringConfigValue(connection?.config, 'defaultBridge') || undefined : undefined,
    })
  }
  const applyProvisionImageDefaults = (imageID?: string) => {
    const image = provisionOptions.images.find((item) => item.id === imageID)
    const sourceKind = String(image?.sourceKind || image?.assetKind || '').toLowerCase()
    quickForm.setFieldsValue({
      vmTemplateId: image?.provider === 'pve' && sourceKind === 'template' && image.sourceRef ? image.sourceRef : undefined,
    })
  }
  const columns: ColumnsType<DockerHost> = [
    { title: '名称', dataIndex: 'name', fixed: 'left', width: 190, render: (value, record) => <Text strong>{value || record.id}</Text> },
    { title: '状态', dataIndex: 'status', width: 110, render: statusTag },
    { title: '架构', dataIndex: 'architecture', width: 120, render: architectureTag },
    { title: 'Endpoint', dataIndex: 'endpoint', width: 220, render: (value, record) => value || record.ipAddress || '-' },
    { title: '环境/归属', width: 180, render: (_value, record) => [record.environment, record.owner || record.team].filter(Boolean).join(' / ') || '-' },
    { title: 'VM', width: 180, render: (_value, record) => record.vmName || record.vmId || record.virtualizationConnectionId || '-' },
    { title: '规格', width: 180, render: (_value, record) => `${record.cpuCoreCount || '-'}C / ${formatBytes(record.memoryBytes)} / ${formatBytes(record.diskBytes)}` },
    { title: '端口池', width: 140, render: (_value, record) => record.availablePortStart && record.availablePortEnd ? `${record.availablePortStart}-${record.availablePortEnd}` : '-' },
    { title: '心跳', dataIndex: 'lastHeartbeatAt', width: 155, render: formatDateTime },
    {
      title: '操作',
      align: 'center',
      fixed: 'right',
      width: 96,
      render: (_value, record) => canManageHosts ? (
        <Space className="soha-row-action-icons">
          <ManagementIconButton aria-label="编辑主机" size="small" tooltip="编辑" icon={<EditOutlined />} onClick={() => { setEditing(record); form.setFieldsValue(hostToForm(record)); setDrawerOpen(true) }} />
          <Popconfirm title="确认删除 Docker 主机？" onConfirm={() => deleteMutation.mutate(record.id)}>
            <ManagementIconButton aria-label="删除主机" size="small" tooltip="删除" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ) : null,
    },
  ]
  return (
    <>
      {!embedded ? (
        <div className="soha-vrt-query">
          <ManagementQueryPanel
            form={filterForm}
            actions={<DockerFilterActions loading={hostsQuery.isFetching} onReset={() => { filterForm.resetFields(); setFilters({ page: 1, pageSize: filters.pageSize ?? 10 }) }} />}
            onFinish={(values) => setFilters((current) => ({ ...current, ...values, page: 1 }))}
          >
            <ManagementQueryField grow minWidth={260} width={360} name="search" label="关键词"><Input allowClear prefix={<SearchOutlined />} placeholder="主机、Endpoint、VM 或 IP" /></ManagementQueryField>
            <ManagementQueryField minWidth={132} width={150} name="status" label="状态"><Select allowClear placeholder="全部" options={['online', 'ready', 'provisioning', 'degraded', 'offline', 'unavailable'].map((item) => ({ value: item, label: item }))} /></ManagementQueryField>
            <ManagementQueryField minWidth={148} width={170} name="architecture" label="架构"><Select allowClear placeholder="全部" options={ARCHITECTURE_OPTIONS} /></ManagementQueryField>
            <ManagementQueryField minWidth={150} width={180} name="environment" label="环境"><Input allowClear placeholder="dev / test" /></ManagementQueryField>
          </ManagementQueryPanel>
        </div>
      ) : null}
      <DockerAdminTable
        rowKey="id"
        enableColumnSelection={!embedded}
        loading={hostsQuery.isLoading}
        dataSource={page.items}
        columns={columns}
        scroll={{ x: 1340 }}
        pagination={pageTablePagination(page, embedded, setFilters)}
        actions={canManageHosts && !embedded ? (
          <>
            <Button icon={<CloudServerOutlined />} onClick={() => { quickForm.setFieldsValue({ architecture: 'amd64', availablePortStart: 20000, availablePortEnd: 39999, cpuCoreCount: 4, memoryGiB: 8, diskGiB: 80 }); setQuickDrawerOpen(true) }}>虚拟化快速构建</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditing(null); form.setFieldsValue(hostToForm()); setDrawerOpen(true) }}>接入主机</Button>
          </>
        ) : null}
        enableDensity={!embedded}
        refreshing={hostsQuery.isFetching}
        showColumnSettings={!embedded}
        showRefresh={!embedded}
        onRefresh={() => hostsQuery.refetch()}
      />
      <Drawer title={editing ? '编辑 Docker 主机' : '接入 Docker 主机'} size="large" open={drawerOpen} onClose={() => setDrawerOpen(false)} extra={<DrawerFooter form={form} loading={createMutation.isPending} onCancel={() => setDrawerOpen(false)} />}>
        <Form form={form} layout="vertical" onFinish={(values) => createMutation.mutate(values)}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="status" label="状态"><Select options={['pending', 'online', 'ready', 'provisioning', 'degraded', 'offline'].map((item) => ({ value: item, label: item }))} /></Form.Item>
            <Form.Item name="endpoint" label="Endpoint"><Input placeholder="tcp://10.0.0.10:2376" /></Form.Item>
            <Form.Item name="architecture" label="架构"><Select allowClear options={ARCHITECTURE_OPTIONS} /></Form.Item>
            <Form.Item name="agentId" label="Agent ID"><Input /></Form.Item>
            <Form.Item name="ipAddress" label="IP 地址"><Input /></Form.Item>
            <Form.Item name="environment" label="环境"><Input /></Form.Item>
            <Form.Item name="owner" label="负责人"><Input /></Form.Item>
            <Form.Item name="team" label="团队"><Input /></Form.Item>
            <Form.Item name="virtualizationConnectionId" label="虚拟化连接 ID"><Input /></Form.Item>
            <Form.Item name="vmId" label="VM ID"><Input /></Form.Item>
            <Form.Item name="vmName" label="VM 名称"><Input /></Form.Item>
            <Form.Item name="cpuCoreCount" label="CPU 核数"><InputNumber min={0} className="w-full" /></Form.Item>
            <Form.Item name="memoryGiB" label="内存 GiB"><InputNumber min={0} className="w-full" /></Form.Item>
            <Form.Item name="diskGiB" label="磁盘 GiB"><InputNumber min={0} className="w-full" /></Form.Item>
            <Form.Item name="availablePortStart" label="端口池起始"><InputNumber min={1} max={65535} className="w-full" /></Form.Item>
            <Form.Item name="availablePortEnd" label="端口池结束"><InputNumber min={1} max={65535} className="w-full" /></Form.Item>
            <Form.Item name="dockerVersion" label="Docker 版本"><Input /></Form.Item>
            <Form.Item name="composeVersion" label="Compose 版本"><Input /></Form.Item>
          </div>
        </Form>
      </Drawer>
      <Drawer title="虚拟化快速构建 Docker 主机" size="large" open={quickDrawerOpen} onClose={() => setQuickDrawerOpen(false)} extra={<DrawerFooter form={quickForm} loading={quickCreateMutation.isPending} onCancel={() => setQuickDrawerOpen(false)} submitLabel="提交构建" />}>
        <Form form={quickForm} layout="vertical" onFinish={(values) => quickCreateMutation.mutate(values)}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="virtualizationConnectionId" label="虚拟化连接" rules={[{ required: true }]}>
              <Select
                allowClear
                showSearch
                optionFilterProp="label"
                loading={provisionOptions.loading}
                options={quickConnectionOptions}
                placeholder="选择 PVE 或 KubeVirt 连接"
                onChange={applyProvisionConnectionDefaults}
              />
            </Form.Item>
            <Form.Item name="imageId" label="镜像 / 模板" rules={[{ required: true }]}>
              <Select
                allowClear
                showSearch
                optionFilterProp="label"
                disabled={!selectedProvisionConnectionID}
                loading={provisionOptions.loading}
                options={quickImageOptions}
                placeholder={selectedProvisionConnectionID ? '选择已同步镜像、ISO 或模板' : '先选择虚拟化连接'}
                onChange={applyProvisionImageDefaults}
              />
            </Form.Item>
            <Form.Item name="flavorId" label="规格">
              <Select allowClear showSearch optionFilterProp="label" loading={provisionOptions.loading} options={quickFlavorOptions} placeholder="选择规格或手动填写资源" />
            </Form.Item>
            <Form.Item name="architecture" label="架构"><Select options={ARCHITECTURE_OPTIONS} /></Form.Item>
            <Form.Item name="network" label={selectedProvisionProvider === 'kubevirt' ? '网络' : 'PVE 网桥'}>
              <Input disabled={selectedProvisionProvider === 'kubevirt'} placeholder={selectedProvisionProvider === 'kubevirt' ? 'KubeVirt 使用默认 Pod 网络' : 'vmbr0'} />
            </Form.Item>
            <Form.Item name="vmTemplateId" hidden><Input /></Form.Item>
            <Form.Item name="environment" label="环境"><Input /></Form.Item>
            <Form.Item name="owner" label="负责人"><Input /></Form.Item>
            <Form.Item name="team" label="团队"><Input /></Form.Item>
            <Form.Item name="cpuCoreCount" label="CPU 核数"><InputNumber min={1} className="w-full" /></Form.Item>
            <Form.Item name="memoryGiB" label="内存 GiB"><InputNumber min={1} className="w-full" /></Form.Item>
            <Form.Item name="diskGiB" label="磁盘 GiB"><InputNumber min={1} className="w-full" /></Form.Item>
            <Form.Item name="ttlSeconds" label="有效期秒数"><InputNumber min={0} className="w-full" /></Form.Item>
            <Form.Item name="availablePortStart" label="端口池起始"><InputNumber min={1} max={65535} className="w-full" /></Form.Item>
            <Form.Item name="availablePortEnd" label="端口池结束"><InputNumber min={1} max={65535} className="w-full" /></Form.Item>
            {selectedProvisionProvider === 'pve' ? <Form.Item name={['config', 'providerParams', 'snippetStorage']} label="PVE Snippet Storage"><Input placeholder="local" /></Form.Item> : null}
          </div>
          <Form.Item name="cloudInit" label="Cloud-init 用户数据"><TextArea rows={8} spellCheck={false} placeholder="#cloud-config" /></Form.Item>
        </Form>
      </Drawer>
    </>
  )
}

function ProjectsTable({ embedded = false, sourceKind = 'compose' as DockerProjectSourceKind }: { embedded?: boolean; sourceKind?: DockerProjectSourceKind }) {
  const [filters, setFilters] = useState<DockerFilterState>({ page: 1, pageSize: embedded ? 5 : 10, sourceKind })
  const [filterForm] = Form.useForm<DockerFilterState>()
  const [form] = Form.useForm<DockerProjectInput>()
  const [containerForm] = Form.useForm<ContainerStartFormValues>()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [containerDrawerOpen, setContainerDrawerOpen] = useState(false)
  const [editing, setEditing] = useState<DockerProject | null>(null)
  const { canManageProjects, canDeployProjects, canManagePorts } = useDockerPermissions()
  const { hosts, hostOptions } = useDockerOptions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const projectsQuery = useQuery({ queryKey: ['docker', 'projects', filters], queryFn: () => dockerApi.projects(filters) })
  const saveMutation = useMutation({
    mutationFn: (values: DockerProjectInput) => editing ? dockerApi.updateProject(editing.id, buildProjectPayload(values)) : dockerApi.createProject(buildProjectPayload(values)),
    onSuccess: () => {
      message.success(editing ? '项目已更新' : '项目已创建')
      setDrawerOpen(false)
      setEditing(null)
      form.resetFields()
      refreshDocker(queryClient)
    },
  })
  const containerStartMutation = useMutation({
    mutationFn: (values: ContainerStartFormValues) => dockerApi.startContainer(buildContainerStartPayload(values)),
    onSuccess: () => {
      message.success('单容器启动任务已提交')
      setContainerDrawerOpen(false)
      containerForm.resetFields()
      refreshDocker(queryClient)
    },
  })
  const deleteMutation = useMutation({ mutationFn: dockerApi.deleteProject, onSuccess: () => { message.success('项目已删除'); refreshDocker(queryClient) } })
  const deployMutation = useMutation({
    mutationFn: ({ id, action }: { id: string; action: string }) => dockerApi.deployProject(id, action),
    onSuccess: (_response, variables) => {
      message.success(`${operationActionLabel(variables.action)}任务已提交`)
      refreshDocker(queryClient)
    },
  })
  const page = normalizePage(projectsQuery.data?.data, filters.page ?? 1, filters.pageSize ?? 10)
  const canStartContainer = canManageProjects && canDeployProjects && canManagePorts
  const isSingleContainer = sourceKind === 'single_container'
  const applyContainerHostDefaults = (hostID?: string) => {
    const host = hosts.find((item) => item.id === hostID)
    if (host?.architecture) {
      containerForm.setFieldsValue({ architecture: host.architecture })
    }
  }
  const columns: ColumnsType<DockerProject> = [
    { title: isSingleContainer ? '单容器服务' : 'Compose', dataIndex: 'name', fixed: 'left', width: 210, render: (value, record) => <Space orientation="vertical" size={0}><Link to={`/docker/projects/${record.id}`}><Text strong>{value}</Text></Link><Text type="secondary">{record.slug}</Text></Space> },
    { title: '状态', dataIndex: 'status', width: 110, render: statusTag },
    { title: '主机', dataIndex: 'hostId', width: 190, render: (value) => hostOptions.find((item) => item.value === value)?.label || value },
    { title: '来源', width: 160, render: (_value, record) => record.sourceKind || record.templateId || 'inline_compose' },
    ...(isSingleContainer ? [{ title: '端口映射', width: 260, render: (_value, record) => renderProjectPortSummary(record) }] satisfies ColumnsType<DockerProject> : []),
    { title: '环境/归属', width: 180, render: (_value, record) => [record.environment, record.owner || record.team].filter(Boolean).join(' / ') || '-' },
    { title: '目标态', dataIndex: 'desiredState', width: 120, render: (value) => value || '-' },
    { title: '到期', dataIndex: 'expiresAt', width: 155, render: formatDateTime },
    { title: '部署时间', dataIndex: 'lastDeployedAt', width: 155, render: formatDateTime },
    {
      title: '操作',
      align: 'center',
      fixed: 'right',
      width: 160,
      render: (_value, record) => (
        <Space className="soha-row-action-icons">
          {canDeployProjects ? <ManagementIconButton aria-label="部署项目" size="small" tooltip="部署" icon={<PlayCircleOutlined />} loading={deployMutation.isPending} onClick={() => deployMutation.mutate({ id: record.id, action: 'deploy' })} /> : null}
          {canDeployProjects ? <ManagementIconButton aria-label="重启项目" size="small" tooltip="重启" icon={<ReloadOutlined />} loading={deployMutation.isPending} onClick={() => deployMutation.mutate({ id: record.id, action: 'restart' })} /> : null}
          {canDeployProjects ? <ManagementIconButton aria-label="停止项目" size="small" tooltip="停止" icon={<PoweroffOutlined />} loading={deployMutation.isPending} onClick={() => deployMutation.mutate({ id: record.id, action: 'down' })} /> : null}
          <Link to={`/docker/projects/${record.id}`}><ManagementIconButton aria-label="查看容器详情" size="small" tooltip="详情" icon={<FileTextOutlined />} /></Link>
          {canManageProjects ? <ManagementIconButton aria-label="编辑项目" size="small" tooltip="编辑" icon={<EditOutlined />} onClick={() => { setEditing(record); form.setFieldsValue(record); setDrawerOpen(true) }} /> : null}
          {canManageProjects ? <Popconfirm title={isSingleContainer ? '确认删除单容器服务？' : '确认删除 Compose 项目？'} onConfirm={() => deleteMutation.mutate(record.id)}><ManagementIconButton aria-label="删除项目" size="small" tooltip="删除" danger icon={<DeleteOutlined />} /></Popconfirm> : null}
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
            actions={<DockerFilterActions loading={projectsQuery.isFetching} onReset={() => { filterForm.resetFields(); setFilters({ page: 1, pageSize: filters.pageSize ?? 10, sourceKind }) }} />}
            onFinish={(values) => setFilters((current) => ({ ...current, ...values, sourceKind, page: 1 }))}
          >
            <ManagementQueryField grow minWidth={260} width={360} name="search" label="关键词"><Input allowClear prefix={<SearchOutlined />} placeholder="项目、Slug 或来源" /></ManagementQueryField>
            <ManagementQueryField minWidth={180} width={220} name="hostId" label="主机"><Select allowClear showSearch optionFilterProp="label" placeholder="全部主机" options={hostOptions} /></ManagementQueryField>
            <ManagementQueryField minWidth={132} width={150} name="status" label="状态"><Select allowClear placeholder="全部" options={['draft', 'defined', 'running', 'stopped', 'failed'].map((item) => ({ value: item, label: item }))} /></ManagementQueryField>
            <ManagementQueryField minWidth={150} width={180} name="environment" label="环境"><Input allowClear placeholder="dev / test" /></ManagementQueryField>
          </ManagementQueryPanel>
        </div>
      ) : null}
      <DockerAdminTable
        rowKey="id"
        enableColumnSelection={!embedded}
        loading={projectsQuery.isLoading}
        dataSource={page.items}
        columns={columns}
        scroll={{ x: isSingleContainer ? 1690 : 1430 }}
        pagination={pageTablePagination(page, embedded, setFilters)}
        actions={!embedded ? (
          <>
            {isSingleContainer && canStartContainer ? <Button type="primary" icon={<PlayCircleOutlined />} onClick={() => { containerForm.setFieldsValue({ architecture: 'amd64', protocol: 'tcp', exposureScope: 'internal', restartPolicy: 'unless-stopped', domainScheme: 'http', domainTlsEnabled: false, ports: DEFAULT_CONTAINER_PORTS }); setContainerDrawerOpen(true) }}>快速启动</Button> : null}
            {!isSingleContainer && canManageProjects ? <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditing(null); form.setFieldsValue({ composeContent: DEFAULT_COMPOSE, status: 'draft', sourceKind: 'inline_compose' }); setDrawerOpen(true) }}>创建 Compose</Button> : null}
          </>
        ) : null}
        enableDensity={!embedded}
        refreshing={projectsQuery.isFetching}
        showColumnSettings={!embedded}
        showRefresh={!embedded}
        onRefresh={() => projectsQuery.refetch()}
      />
      <Drawer title={editing ? '编辑 Compose 项目' : '创建 Compose 项目'} size="large" open={drawerOpen} onClose={() => setDrawerOpen(false)} extra={<DrawerFooter form={form} loading={saveMutation.isPending} onCancel={() => setDrawerOpen(false)} />}>
        <Form form={form} layout="vertical" onFinish={(values) => saveMutation.mutate(values)}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="hostId" label="Docker 主机" rules={[{ required: true }]}><Select showSearch optionFilterProp="label" options={hostOptions} /></Form.Item>
            <Form.Item name="slug" label="Slug"><Input /></Form.Item>
            <Form.Item name="environment" label="环境"><Input /></Form.Item>
            <Form.Item name="owner" label="负责人"><Input /></Form.Item>
            <Form.Item name="team" label="团队"><Input /></Form.Item>
            <Form.Item name="status" label="状态"><Select options={['draft', 'defined', 'running', 'stopped', 'failed'].map((item) => ({ value: item, label: item }))} /></Form.Item>
            <Form.Item name="desiredState" label="目标态"><Select allowClear options={['running', 'stopped'].map((item) => ({ value: item, label: item }))} /></Form.Item>
            <Form.Item name="ttlSeconds" label="TTL 秒数"><InputNumber min={0} className="w-full" /></Form.Item>
            <Form.Item name="sourceKind" label="来源类型"><Select options={['inline_compose', 'git', 'template'].map((item) => ({ value: item, label: item }))} /></Form.Item>
            <Form.Item name="sourceRef" label="来源引用"><Input /></Form.Item>
            <Form.Item name="templateId" label="模板 ID"><Input /></Form.Item>
          </div>
          <Form.Item name="description" label="描述"><Input /></Form.Item>
          <Tabs
            items={[
              { key: 'compose', label: 'Compose', children: <Form.Item name="composeContent" rules={[{ required: true }]}><TextArea rows={16} spellCheck={false} /></Form.Item> },
              { key: 'env', label: '.env', children: <Form.Item name="envContent"><TextArea rows={12} spellCheck={false} /></Form.Item> },
            ]}
          />
        </Form>
      </Drawer>
      <Drawer title="快速启动 Docker 应用" size="large" open={containerDrawerOpen} onClose={() => setContainerDrawerOpen(false)} extra={<DrawerFooter form={containerForm} loading={containerStartMutation.isPending} onCancel={() => setContainerDrawerOpen(false)} submitLabel="启动" />}>
        <Form form={containerForm} layout="vertical" onFinish={(values) => containerStartMutation.mutate(values)}>
          <Form.Item name="name" label="容器名称" rules={[{ required: true }]}><Input placeholder="preview-api" /></Form.Item>
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="hostId" label="Docker 主机" rules={[{ required: true }]}><Select showSearch optionFilterProp="label" options={hostOptions} onChange={applyContainerHostDefaults} /></Form.Item>
            <Form.Item name="image" label="镜像" rules={[{ required: true }]}><Input placeholder="nginx:alpine" /></Form.Item>
            <Form.Item name="architecture" label="架构"><Select options={ARCHITECTURE_OPTIONS} /></Form.Item>
            <Form.Item name="restartPolicy" label="重启策略"><Select options={['unless-stopped', 'always', 'on-failure', 'no'].map((item) => ({ value: item, label: item }))} /></Form.Item>
            <Form.Item name="environment" label="环境"><Input /></Form.Item>
            <Form.Item name="owner" label="负责人"><Input /></Form.Item>
            <Form.Item name="team" label="团队"><Input /></Form.Item>
            <Form.Item name="ttlSeconds" label="TTL 秒数"><InputNumber min={0} className="w-full" /></Form.Item>
            <Form.Item name="command" label="启动命令"><Input /></Form.Item>
            <Form.Item name="entrypoint" label="Entrypoint"><Input /></Form.Item>
          </div>
          <Form.List name="ports">
            {(fields, { add, remove }) => (
              <div className="mb-3 space-y-3">
                {fields.map((field, index) => (
                  <div key={field.key} className="rounded border border-[var(--soha-border-color)] p-3">
                    <div className="mb-2 flex items-center justify-between">
                      <Text strong>端口 {index + 1}</Text>
                      {fields.length > 1 ? <Button type="text" danger icon={<MinusCircleOutlined />} onClick={() => remove(field.name)} /> : null}
                    </div>
                    <div className="grid gap-3 md:grid-cols-4">
                      <Form.Item name={[field.name, 'name']} label="名称"><Input placeholder="http" /></Form.Item>
                      <Form.Item name={[field.name, 'containerPort']} label="容器端口" rules={[{ required: true }]}><InputNumber min={1} max={65535} className="w-full" /></Form.Item>
                      <Form.Item name={[field.name, 'hostPort']} label="主机端口" rules={[{ required: true }]}><InputNumber min={1} max={65535} className="w-full" /></Form.Item>
                      <Form.Item name={[field.name, 'hostIp']} label="监听 IP"><Input placeholder="0.0.0.0" /></Form.Item>
                      <Form.Item name={[field.name, 'protocol']} label="协议"><Select options={[{ value: 'tcp', label: 'tcp' }, { value: 'udp', label: 'udp' }]} /></Form.Item>
                      <Form.Item name={[field.name, 'exposureScope']} label="暴露范围"><Select options={['internal', 'vpn', 'public'].map((item) => ({ value: item, label: item }))} /></Form.Item>
                      <Form.Item name={[field.name, 'domainName']} label="访问域名"><Input placeholder="preview.internal.example.com" /></Form.Item>
                      <Form.Item name={[field.name, 'domainScheme']} label="域名协议"><Select options={[{ value: 'http', label: 'http' }, { value: 'https', label: 'https' }]} /></Form.Item>
                      <Form.Item name={[field.name, 'domainTlsEnabled']} label="TLS" valuePropName="checked"><Switch /></Form.Item>
                    </div>
                  </div>
                ))}
                <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ hostIp: '0.0.0.0', protocol: 'tcp', exposureScope: 'internal', domainScheme: 'http', domainTlsEnabled: false })}>添加端口</Button>
              </div>
            )}
          </Form.List>
          <Form.List name="volumes">
            {(fields, { add, remove }) => (
              <div className="mb-3 space-y-3">
                {fields.map((field, index) => (
                  <div key={field.key} className="rounded border border-[var(--soha-border-color)] p-3">
                    <div className="mb-2 flex items-center justify-between">
                      <Text strong>卷 {index + 1}</Text>
                      <Button type="text" danger icon={<MinusCircleOutlined />} onClick={() => remove(field.name)} />
                    </div>
                    <div className="grid gap-3 md:grid-cols-4">
                      <Form.Item name={[field.name, 'name']} label="名称"><Input placeholder="data" /></Form.Item>
                      <Form.Item name={[field.name, 'type']} label="类型"><Select options={[{ value: 'bind', label: 'bind' }, { value: 'volume', label: 'volume' }]} /></Form.Item>
                      <Form.Item name={[field.name, 'source']} label="来源"><Input placeholder="/data/app 或 app-data" /></Form.Item>
                      <Form.Item name={[field.name, 'target']} label="挂载路径"><Input placeholder="/var/lib/app" /></Form.Item>
                      <Form.Item name={[field.name, 'readOnly']} label="只读" valuePropName="checked"><Switch /></Form.Item>
                    </div>
                  </div>
                ))}
                <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ type: 'bind', readOnly: false })}>添加卷</Button>
              </div>
            )}
          </Form.List>
          <Form.List name="environmentVariables">
            {(fields, { add, remove }) => (
              <div className="mb-3 space-y-3">
                {fields.map((field, index) => (
                  <div key={field.key} className="grid items-start gap-3 md:grid-cols-[1fr_1fr_40px]">
                    <Form.Item name={[field.name, 'name']} label={index === 0 ? '变量名' : undefined}><Input placeholder="APP_ENV" /></Form.Item>
                    <Form.Item name={[field.name, 'value']} label={index === 0 ? '变量值' : undefined}><Input /></Form.Item>
                    <Button className={index === 0 ? 'mt-8' : undefined} type="text" danger icon={<MinusCircleOutlined />} onClick={() => remove(field.name)} />
                  </div>
                ))}
                <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({})}>添加环境变量</Button>
              </div>
            )}
          </Form.List>
          <div className="grid gap-3 md:grid-cols-3">
            <Form.Item name={['resources', 'cpus']} label="CPU 限制"><InputNumber min={0} step={0.1} className="w-full" /></Form.Item>
            <Form.Item name={['resources', 'memoryMiB']} label="内存限制 MiB"><InputNumber min={0} className="w-full" /></Form.Item>
            <Form.Item name={['resources', 'memoryReservationMiB']} label="内存预留 MiB"><InputNumber min={0} className="w-full" /></Form.Item>
          </div>
          <Form.Item name="network" label="外部网络"><Input placeholder="traefik 或已有 Docker network" /></Form.Item>
          <Form.Item name="imagePullPolicy" label="拉取策略"><Select allowClear options={['always', 'missing', 'never', 'build'].map((item) => ({ value: item, label: item }))} /></Form.Item>
          <Form.Item name="envContent" label=".env"><TextArea rows={8} spellCheck={false} placeholder="KEY=value" /></Form.Item>
        </Form>
      </Drawer>
    </>
  )
}

function ServicesTable({ embedded = false, fixedProjectId }: { embedded?: boolean; fixedProjectId?: string }) {
  const [filters, setFilters] = useState<DockerFilterState>({ page: 1, pageSize: embedded ? 5 : 10, projectId: fixedProjectId })
  const [filterForm] = Form.useForm<DockerFilterState>()
  const { canManageServices } = useDockerPermissions()
  const { hostOptions, projectOptions } = useDockerOptions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const servicesQuery = useQuery({ queryKey: ['docker', 'services', filters], queryFn: () => dockerApi.services(filters) })
  const actionMutation = useMutation({
    mutationFn: ({ id, action }: { id: string; action: string }) => dockerApi.serviceAction(id, action),
    onSuccess: (_response, variables) => { message.success(`${variables.action} 任务已提交`); refreshDocker(queryClient) },
  })
  const page = normalizePage(servicesQuery.data?.data, filters.page ?? 1, filters.pageSize ?? 10)
  const columns: ColumnsType<DockerService> = [
    { title: '服务', dataIndex: 'name', fixed: 'left', width: 180, render: (value, record) => <Space orientation="vertical" size={0}><Text strong>{value}</Text><Text type="secondary">{record.containerId || record.id}</Text></Space> },
    { title: '状态', dataIndex: 'status', width: 110, render: statusTag },
    { title: '镜像', dataIndex: 'image', width: 240, render: (value) => value || '-' },
    { title: '项目', dataIndex: 'projectId', width: 180, render: (value) => projectOptions.find((item) => item.value === value)?.label || value },
    { title: '主机', dataIndex: 'hostId', width: 170, render: (value) => hostOptions.find((item) => item.value === value)?.label || value },
    { title: 'CPU', dataIndex: 'cpuPercent', width: 90, render: formatPercent },
    { title: '内存', dataIndex: 'memoryBytes', width: 110, render: formatBytes },
    { title: '网络', width: 150, render: (_value, record) => `${formatBytes(record.networkRxBytes)} / ${formatBytes(record.networkTxBytes)}` },
    { title: '重启', dataIndex: 'restartCount', width: 80 },
    { title: '最近同步', dataIndex: 'lastSeenAt', width: 155, render: formatDateTime },
    {
      title: '操作',
      align: 'center',
      fixed: 'right',
      width: 130,
      render: (_value, record) => canManageServices ? (
        <Space className="soha-row-action-icons">
          {['restart', 'start', 'stop'].map((action) => (
            <ManagementIconButton
              key={action}
              aria-label={operationActionLabel(action)}
              size="small"
              tooltip={operationActionLabel(action)}
              icon={action === 'restart' ? <ReloadOutlined /> : action === 'start' ? <PlayCircleOutlined /> : <PoweroffOutlined />}
              loading={actionMutation.isPending}
              onClick={() => actionMutation.mutate({ id: record.id, action })}
            />
          ))}
          <ManagementIconButton aria-label="查看日志" size="small" tooltip="日志" icon={<FileTextOutlined />} loading={actionMutation.isPending} onClick={() => actionMutation.mutate({ id: record.id, action: 'logs' })} />
        </Space>
      ) : null,
    },
  ]
  return (
    <>
      {!embedded ? (
        <div className="soha-vrt-query">
          <ManagementQueryPanel
            form={filterForm}
            actions={<DockerFilterActions loading={servicesQuery.isFetching} onReset={() => { filterForm.resetFields(); setFilters({ page: 1, pageSize: filters.pageSize ?? 10, projectId: fixedProjectId }) }} />}
            onFinish={(values) => setFilters((current) => ({ ...current, ...values, projectId: fixedProjectId, page: 1 }))}
          >
            <ManagementQueryField grow minWidth={260} width={360} name="search" label="关键词"><Input allowClear prefix={<SearchOutlined />} placeholder="服务、镜像或容器" /></ManagementQueryField>
            <ManagementQueryField minWidth={180} width={220} name="hostId" label="主机"><Select allowClear showSearch optionFilterProp="label" placeholder="全部主机" options={hostOptions} /></ManagementQueryField>
            {!fixedProjectId ? <ManagementQueryField minWidth={180} width={220} name="projectId" label="项目"><Select allowClear showSearch optionFilterProp="label" placeholder="全部项目" options={projectOptions} /></ManagementQueryField> : null}
            <ManagementQueryField minWidth={132} width={150} name="status" label="状态"><Select allowClear placeholder="全部" options={['defined', 'running', 'exited', 'failed', 'unknown'].map((item) => ({ value: item, label: item }))} /></ManagementQueryField>
          </ManagementQueryPanel>
        </div>
      ) : null}
      <DockerAdminTable
        rowKey="id"
        enableColumnSelection={!embedded}
        loading={servicesQuery.isLoading}
        dataSource={page.items}
        columns={columns}
        scroll={{ x: 1440 }}
        pagination={pageTablePagination(page, embedded, setFilters)}
        enableDensity={!embedded}
        refreshing={servicesQuery.isFetching}
        showColumnSettings={!embedded}
        showRefresh={!embedded}
        onRefresh={() => servicesQuery.refetch()}
      />
    </>
  )
}

function PortsTable({ embedded = false, fixedHostId, fixedProjectId }: { embedded?: boolean; fixedHostId?: string; fixedProjectId?: string }) {
  const [filters, setFilters] = useState<DockerFilterState>({ page: 1, pageSize: embedded ? 5 : 10, hostId: fixedHostId, projectId: fixedProjectId })
  const [filterForm] = Form.useForm<DockerFilterState>()
  const [form] = Form.useForm<DockerPortFormValues>()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editing, setEditing] = useState<DockerPortMapping | null>(null)
  const { canManagePorts } = useDockerPermissions()
  const { hostOptions, projectOptions, serviceOptions } = useDockerOptions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const portsQuery = useQuery({ queryKey: ['docker', 'ports', filters], queryFn: () => dockerApi.ports(filters) })
  const saveMutation = useMutation({
    mutationFn: (values: DockerPortFormValues) => editing ? dockerApi.updatePort(editing.id, buildPortPayload(values)) : dockerApi.createPort(buildPortPayload(values)),
    onSuccess: () => {
      message.success(editing ? '端口映射已更新' : '端口映射已创建')
      setDrawerOpen(false)
      setEditing(null)
      form.resetFields()
      refreshDocker(queryClient)
    },
  })
  const deleteMutation = useMutation({ mutationFn: dockerApi.deletePort, onSuccess: () => { message.success('端口映射已删除'); refreshDocker(queryClient) } })
  const page = normalizePage(portsQuery.data?.data, filters.page ?? 1, filters.pageSize ?? 10)
  const columns: ColumnsType<DockerPortMapping> = [
    { title: '名称', dataIndex: 'name', fixed: 'left', width: 180, render: (value) => <Text strong>{value}</Text> },
    { title: '状态', dataIndex: 'status', width: 105, render: statusTag },
    { title: '映射', width: 220, render: (_value, record) => formatPort(record) },
    { title: '域名', dataIndex: 'domainName', width: 220, render: (value, record) => value ? <Space><Text>{value}</Text>{record.domainTlsEnabled ? <Tag color="green">TLS</Tag> : null}</Space> : '-' },
    { title: '暴露范围', dataIndex: 'exposureScope', width: 110, render: (value) => value || 'internal' },
    { title: '访问地址', width: 250, render: (_value, record) => { const url = formatAccessURL(record); return url ? <Typography.Link href={url} target="_blank">{url}</Typography.Link> : '-' } },
    { title: '主机', dataIndex: 'hostId', width: 170, render: (value) => hostOptions.find((item) => item.value === value)?.label || value },
    { title: '项目/服务', width: 190, render: (_value, record) => [projectOptions.find((item) => item.value === record.projectId)?.label || record.projectId, serviceOptions.find((item) => item.value === record.serviceId)?.label || record.serviceId].filter(Boolean).join(' / ') || '-' },
    { title: '负责人', dataIndex: 'owner', width: 120, render: (value) => value || '-' },
    { title: '到期', dataIndex: 'expiresAt', width: 155, render: formatDateTime },
    {
      title: '操作',
      align: 'center',
      fixed: 'right',
      width: 96,
      render: (_value, record) => canManagePorts ? (
        <Space className="soha-row-action-icons">
          <ManagementIconButton aria-label="编辑端口映射" size="small" tooltip="编辑" icon={<EditOutlined />} onClick={() => { setEditing(record); form.setFieldsValue(record); setDrawerOpen(true) }} />
          <Popconfirm title="确认删除端口映射？" onConfirm={() => deleteMutation.mutate(record.id)}><ManagementIconButton aria-label="删除端口映射" size="small" tooltip="删除" danger icon={<DeleteOutlined />} /></Popconfirm>
        </Space>
      ) : null,
    },
  ]
  return (
    <>
      {!embedded ? (
        <div className="soha-vrt-query">
          <ManagementQueryPanel
            form={filterForm}
            actions={<DockerFilterActions loading={portsQuery.isFetching} onReset={() => { filterForm.resetFields(); setFilters({ page: 1, pageSize: filters.pageSize ?? 10, hostId: fixedHostId, projectId: fixedProjectId }) }} />}
            onFinish={(values) => setFilters((current) => ({ ...current, ...values, hostId: fixedHostId, projectId: fixedProjectId, page: 1 }))}
          >
            <ManagementQueryField grow minWidth={260} width={360} name="search" label="关键词"><Input allowClear prefix={<SearchOutlined />} placeholder="名称、访问地址或负责人" /></ManagementQueryField>
            {!fixedHostId ? <ManagementQueryField minWidth={180} width={220} name="hostId" label="主机"><Select allowClear showSearch optionFilterProp="label" placeholder="全部主机" options={hostOptions} /></ManagementQueryField> : null}
            {!fixedProjectId ? <ManagementQueryField minWidth={180} width={220} name="projectId" label="项目"><Select allowClear showSearch optionFilterProp="label" placeholder="全部项目" options={projectOptions} /></ManagementQueryField> : null}
            <ManagementQueryField minWidth={132} width={150} name="status" label="状态"><Select allowClear placeholder="全部" options={['active', 'reserved', 'released', 'expired'].map((item) => ({ value: item, label: item }))} /></ManagementQueryField>
          </ManagementQueryPanel>
        </div>
      ) : null}
      <DockerAdminTable
        rowKey="id"
        enableColumnSelection={!embedded}
        loading={portsQuery.isLoading}
        dataSource={page.items}
        columns={columns}
        scroll={{ x: 1470 }}
        pagination={pageTablePagination(page, embedded, setFilters)}
        actions={canManagePorts && !embedded ? (
          <>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditing(null); form.setFieldsValue({ hostId: fixedHostId, projectId: fixedProjectId, protocol: 'tcp', exposureScope: 'internal', status: 'active', domainScheme: 'http', domainTlsEnabled: false }); setDrawerOpen(true) }}>新增映射</Button>
          </>
        ) : null}
        enableDensity={!embedded}
        refreshing={portsQuery.isFetching}
        showColumnSettings={!embedded}
        showRefresh={!embedded}
        onRefresh={() => portsQuery.refetch()}
      />
      <Drawer title={editing ? '编辑端口映射' : '新增端口映射'} size="large" open={drawerOpen} onClose={() => setDrawerOpen(false)} extra={<DrawerFooter form={form} loading={saveMutation.isPending} onCancel={() => setDrawerOpen(false)} />}>
        <Form form={form} layout="vertical" onFinish={(values) => saveMutation.mutate(values)}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="hostId" label="Docker 主机" rules={[{ required: true }]} hidden={Boolean(fixedHostId)}><Select showSearch optionFilterProp="label" options={hostOptions} /></Form.Item>
            <Form.Item name="hostIp" label="监听 IP"><Input placeholder="0.0.0.0" /></Form.Item>
            <Form.Item name="hostPort" label="主机端口" rules={[{ required: true }]}><InputNumber min={1} max={65535} className="w-full" /></Form.Item>
            <Form.Item name="containerPort" label="容器端口" rules={[{ required: true }]}><InputNumber min={1} max={65535} className="w-full" /></Form.Item>
            <Form.Item name="protocol" label="协议"><Select options={[{ value: 'tcp', label: 'tcp' }, { value: 'udp', label: 'udp' }]} /></Form.Item>
            <Form.Item name="exposureScope" label="暴露范围"><Select options={['internal', 'vpn', 'public'].map((item) => ({ value: item, label: item }))} /></Form.Item>
            <Form.Item name="status" label="状态"><Select options={['active', 'reserved', 'released', 'expired'].map((item) => ({ value: item, label: item }))} /></Form.Item>
            <Form.Item name="owner" label="负责人"><Input /></Form.Item>
            <Form.Item name="projectId" label="项目" hidden={Boolean(fixedProjectId)}><Select allowClear showSearch optionFilterProp="label" options={projectOptions} /></Form.Item>
            <Form.Item name="serviceId" label="服务"><Select allowClear showSearch optionFilterProp="label" options={serviceOptions} /></Form.Item>
          </div>
          <div className="grid gap-3 md:grid-cols-[1fr_160px_120px]">
            <Form.Item name="domainName" label="访问域名"><Input placeholder="preview.internal.example.com" /></Form.Item>
            <Form.Item name="domainScheme" label="域名协议"><Select options={[{ value: 'http', label: 'http' }, { value: 'https', label: 'https' }]} /></Form.Item>
            <Form.Item name="domainTlsEnabled" label="TLS" valuePropName="checked"><Switch /></Form.Item>
          </div>
          <Form.Item name="accessUrl" label="访问地址"><Input placeholder="http://10.0.0.10:8080" /></Form.Item>
          <Form.Item name="expiresAt" label="到期时间"><Input placeholder="2026-06-01T10:00:00Z" /></Form.Item>
        </Form>
      </Drawer>
    </>
  )
}

function ContainerManagementPage() {
  return (
    <div className="soha-page soha-virtualization-page">
      <ManagementDetailHeader
        title="容器管理"
        meta={(
          <>
            <span>Compose</span>
            <span>单容器服务</span>
          </>
        )}
      />
      <Tabs
        className="soha-docker-management-tabs"
        items={[
          { key: 'compose', label: 'Compose', children: <ProjectsTable sourceKind="compose" /> },
          { key: 'single', label: '单容器服务', children: <ProjectsTable sourceKind="single_container" /> },
        ]}
      />
    </div>
  )
}

function ProjectDetailWorkspace() {
  const { projectId } = useParams()
  const resolvedProjectId = projectId ?? ''
  const { canViewServices, canManageServices, canViewPorts } = useDockerPermissions()
  const [runtimeServiceName, setRuntimeServiceName] = useState('')
  const projectQuery = useQuery({
    enabled: Boolean(resolvedProjectId),
    queryKey: ['docker', 'projects', resolvedProjectId],
    queryFn: () => dockerApi.project(resolvedProjectId),
  })
  const detailServicesQuery = useQuery({
    enabled: Boolean(resolvedProjectId && (canViewServices || canManageServices)),
    queryKey: ['docker', 'project-services', resolvedProjectId],
    queryFn: () => dockerApi.services({ projectId: resolvedProjectId, page: 1, pageSize: 100 }),
  })
  const project = projectQuery.data?.data
  const isSingleContainerProject = project?.sourceKind === 'single_container'
  const projectConfig = project?.config
  const runtimeServices = normalizePage(detailServicesQuery.data?.data, 1, 100).items
  const runtimeServiceOptions = useMemo(() => {
    const options = runtimeServices
      .map((service) => ({ label: service.name || service.id, value: service.name || service.id }))
      .filter((option) => option.value)
    const configServiceName = stringValue(projectConfig?.serviceName)
    if (configServiceName && !options.some((option) => option.value === configServiceName)) {
      options.push({ label: configServiceName, value: configServiceName })
    }
    return options
  }, [projectConfig, runtimeServices])
  const defaultRuntimeServiceName = runtimeServiceOptions[0]?.value || ''
  useEffect(() => {
    if (!defaultRuntimeServiceName) {
      return
    }
    if (!runtimeServiceName || !runtimeServiceOptions.some((option) => option.value === runtimeServiceName)) {
      setRuntimeServiceName(defaultRuntimeServiceName)
    }
  }, [defaultRuntimeServiceName, runtimeServiceName, runtimeServiceOptions])
  if (!resolvedProjectId) {
    return <Navigate to="/docker/projects" replace />
  }
  const runtimeConfigTab = isSingleContainerProject ? {
    key: 'config',
    label: '配置',
    children: (
      <Card loading={projectQuery.isLoading}>
        <Descriptions
          size="small"
          column={{ xs: 1, sm: 2, lg: 3 }}
          items={[
            { key: 'image', label: '镜像', children: configTextValue(projectConfig, 'image') },
            { key: 'architecture', label: '架构', children: architectureTag(configTextValue(projectConfig, 'architecture')) },
            { key: 'platform', label: '平台', children: configTextValue(projectConfig, 'platform') },
            { key: 'serviceName', label: '服务名', children: configTextValue(projectConfig, 'serviceName') },
            { key: 'restartPolicy', label: '重启策略', children: configTextValue(projectConfig, 'restartPolicy') },
            { key: 'command', label: '启动命令', children: configTextValue(projectConfig, 'command') },
            { key: 'ports', label: '端口', children: `${configArrayCount(projectConfig, 'ports')} 个` },
            { key: 'volumes', label: '卷', children: `${configArrayCount(projectConfig, 'volumes')} 个` },
          ]}
        />
        <div className="mt-4">
          <TextArea rows={12} spellCheck={false} value={JSON.stringify(projectConfig ?? {}, null, 2)} readOnly />
        </div>
      </Card>
    ),
  } : {
    key: 'compose',
    label: 'Compose',
    children: (
      <Tabs
        items={[
          { key: 'composeContent', label: 'compose.yaml', children: <TextArea rows={18} spellCheck={false} value={project?.composeContent || ''} readOnly /> },
          { key: 'envContent', label: '.env', children: <TextArea rows={12} spellCheck={false} value={project?.envContent || ''} readOnly /> },
        ]}
      />
    ),
  }
  const detailTabItems = [
    {
      key: 'info',
      label: '信息',
      children: (
        <Card loading={projectQuery.isLoading}>
          <Descriptions
            size="small"
            column={{ xs: 1, sm: 2, lg: 3 }}
            items={[
              { key: 'host', label: 'Docker 主机', children: project?.hostId || '-' },
              { key: 'environment', label: '环境', children: project?.environment || '-' },
              { key: 'owner', label: '负责人', children: project?.owner || project?.team || '-' },
              { key: 'desiredState', label: '目标态', children: project?.desiredState || '-' },
              { key: 'lastDeployedAt', label: '部署时间', children: formatDateTime(project?.lastDeployedAt) },
              { key: 'expiresAt', label: '到期', children: formatDateTime(project?.expiresAt) },
            ]}
          />
        </Card>
      ),
    },
    ...(canViewServices ? [{ key: 'services', label: '服务', children: <ServicesTable fixedProjectId={resolvedProjectId} /> }] : []),
    ...(canViewServices ? [{
      key: 'logs',
      label: '日志',
      children: (
        <DockerProjectLogsPanel
          projectId={resolvedProjectId}
          projectName={project?.name}
          serviceName={runtimeServiceName}
          serviceOptions={runtimeServiceOptions}
          servicesLoading={detailServicesQuery.isFetching}
          onServiceChange={setRuntimeServiceName}
        />
      ),
    }] : []),
    ...(canManageServices ? [{
      key: 'terminal',
      label: 'Shell',
      children: (
        <DockerProjectTerminalPanel
          projectId={resolvedProjectId}
          projectName={project?.name}
          serviceName={runtimeServiceName}
          serviceOptions={runtimeServiceOptions}
          servicesLoading={detailServicesQuery.isFetching}
          onServiceChange={setRuntimeServiceName}
        />
      ),
    }] : []),
    ...(canViewServices ? [{
      key: 'volumes',
      label: '卷文件',
      children: (
        <DockerProjectVolumesPanel
          projectId={resolvedProjectId}
          projectName={project?.name}
          serviceName={runtimeServiceName}
          serviceOptions={runtimeServiceOptions}
          servicesLoading={detailServicesQuery.isFetching}
          onServiceChange={setRuntimeServiceName}
        />
      ),
    }] : []),
    ...(canViewPorts ? [{ key: 'ports', label: '端口映射', children: <PortsTable fixedProjectId={resolvedProjectId} fixedHostId={project?.hostId} /> }] : []),
    runtimeConfigTab,
  ]
  return (
    <div className="soha-page soha-virtualization-page">
      <ManagementDetailHeader
        title={project?.name || '容器详情'}
        description={isSingleContainerProject ? '单容器服务' : 'Compose 项目'}
        meta={project ? (
          <Space size={8} wrap>
            {statusTag(project.status)}
            <Tag>{project.sourceKind || 'inline_compose'}</Tag>
            <Text type="secondary">{project.slug || project.id}</Text>
          </Space>
        ) : null}
        actions={<Link to="/docker/projects"><Button>返回</Button></Link>}
      />
      <Tabs
        className="soha-docker-management-tabs"
        items={detailTabItems}
      />
    </div>
  )
}

function TemplatesTable() {
  const [filters, setFilters] = useState<DockerFilterState>({ page: 1, pageSize: 10 })
  const [filterForm] = Form.useForm<DockerFilterState>()
  const [form] = Form.useForm<DockerTemplateInput>()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editing, setEditing] = useState<DockerTemplate | null>(null)
  const { canManageTemplates } = useDockerPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const templatesQuery = useQuery({ queryKey: ['docker', 'templates', filters], queryFn: () => dockerApi.templates(filters) })
  const saveMutation = useMutation({
    mutationFn: (values: DockerTemplateInput) => editing ? dockerApi.updateTemplate(editing.id, buildTemplatePayload(values)) : dockerApi.createTemplate(buildTemplatePayload(values)),
    onSuccess: () => {
      message.success(editing ? '模板已更新' : '模板已创建')
      setDrawerOpen(false)
      setEditing(null)
      form.resetFields()
      refreshDocker(queryClient)
    },
  })
  const deleteMutation = useMutation({ mutationFn: dockerApi.deleteTemplate, onSuccess: () => { message.success('模板已删除'); refreshDocker(queryClient) } })
  const page = normalizePage(templatesQuery.data?.data, filters.page ?? 1, filters.pageSize ?? 10)
  const columns: ColumnsType<DockerTemplate> = [
    { title: '模板', dataIndex: 'name', fixed: 'left', width: 220, render: (value, record) => <Space orientation="vertical" size={0}><Text strong>{value}</Text><Text type="secondary">{record.description || record.id}</Text></Space> },
    { title: '类型', dataIndex: 'templateKind', width: 130, render: (value) => value || 'compose' },
    { title: '状态', dataIndex: 'enabled', width: 100, render: boolTag },
    { title: '变量', dataIndex: 'variables', width: 130, render: (value) => Object.keys(value ?? {}).length },
    { title: '更新时间', dataIndex: 'updatedAt', width: 155, render: formatDateTime },
    {
      title: '操作', align: 'center', fixed: 'right', width: 96, render: (_value, record) => canManageTemplates ? (
        <Space className="soha-row-action-icons">
          <ManagementIconButton aria-label="编辑模板" size="small" tooltip="编辑" icon={<EditOutlined />} onClick={() => { setEditing(record); form.setFieldsValue(record); setDrawerOpen(true) }} />
          <Popconfirm title="确认删除模板？" onConfirm={() => deleteMutation.mutate(record.id)}><ManagementIconButton aria-label="删除模板" size="small" tooltip="删除" danger icon={<DeleteOutlined />} /></Popconfirm>
        </Space>
      ) : null,
    },
  ]
  return (
    <>
      <div className="soha-vrt-query">
        <ManagementQueryPanel
          form={filterForm}
          actions={<DockerFilterActions loading={templatesQuery.isFetching} onReset={() => { filterForm.resetFields(); setFilters({ page: 1, pageSize: filters.pageSize ?? 10 }) }} />}
          onFinish={(values) => setFilters((current) => ({ ...current, ...values, page: 1 }))}
        >
          <ManagementQueryField grow minWidth={260} width={360} name="search" label="关键词"><Input allowClear prefix={<SearchOutlined />} placeholder="模板名称或描述" /></ManagementQueryField>
          <ManagementQueryField minWidth={132} width={150} name="kind" label="类型"><Select allowClear placeholder="全部" options={[{ value: 'compose', label: 'compose' }]} /></ManagementQueryField>
          <ManagementQueryField minWidth={132} width={150} name="enabled" label="启用"><Select allowClear placeholder="全部" options={[{ value: true, label: '启用' }, { value: false, label: '停用' }]} /></ManagementQueryField>
        </ManagementQueryPanel>
      </div>
      <DockerAdminTable
        rowKey="id"
        loading={templatesQuery.isLoading}
        dataSource={page.items}
        columns={columns}
        scroll={{ x: 860 }}
        pagination={pageTablePagination(page, false, setFilters)}
        actions={canManageTemplates ? (
          <>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditing(null); form.setFieldsValue({ templateKind: 'compose', composeContent: DEFAULT_COMPOSE, enabled: true }); setDrawerOpen(true) }}>新增模板</Button>
          </>
        ) : null}
        refreshing={templatesQuery.isFetching}
        onRefresh={() => templatesQuery.refetch()}
      />
      <Drawer title={editing ? '编辑模板' : '新增模板'} size="large" open={drawerOpen} onClose={() => setDrawerOpen(false)} extra={<DrawerFooter form={form} loading={saveMutation.isPending} onCancel={() => setDrawerOpen(false)} />}>
        <Form form={form} layout="vertical" onFinish={(values) => saveMutation.mutate(values)}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <div className="grid gap-3 md:grid-cols-2">
            <Form.Item name="templateKind" label="类型"><Select options={[{ value: 'compose', label: 'compose' }]} /></Form.Item>
            <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
          </div>
          <Form.Item name="description" label="描述"><Input /></Form.Item>
          <Tabs items={[{ key: 'compose', label: 'Compose', children: <Form.Item name="composeContent"><TextArea rows={16} spellCheck={false} /></Form.Item> }, { key: 'env', label: '.env', children: <Form.Item name="envContent"><TextArea rows={10} spellCheck={false} /></Form.Item> }]} />
        </Form>
      </Drawer>
    </>
  )
}

function OperationsTable({ embedded = false, initialPreset = 'all' as OperationPreset }: { embedded?: boolean; initialPreset?: OperationPreset }) {
  const [preset, setPreset] = useState<OperationPreset>(initialPreset)
  const [filters, setFilters] = useState<DockerFilterState>({ page: 1, pageSize: embedded ? 6 : 10 })
  const [filterForm] = Form.useForm<DockerFilterState>()
  const [selectedOperation, setSelectedOperation] = useState<DockerOperation | null>(null)
  const { canManageOperations } = useDockerPermissions()
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
  const operationsQuery = useQuery({ queryKey: ['docker', 'operations', queryFilters], queryFn: () => dockerApi.operations(queryFilters) })
  const logsQuery = useQuery({ queryKey: ['docker', 'operations', selectedOperation?.id, 'logs'], queryFn: () => dockerApi.operationLogs(selectedOperation?.id ?? ''), enabled: Boolean(selectedOperation?.id) })
  const cancelMutation = useMutation({ mutationFn: dockerApi.cancelOperation, onSuccess: () => { message.success('任务已取消'); refreshDocker(queryClient) } })
  const retryMutation = useMutation({ mutationFn: dockerApi.retryOperation, onSuccess: () => { message.success('重试任务已提交'); refreshDocker(queryClient) } })
  const page = normalizePage(operationsQuery.data?.data, filters.page ?? 1, filters.pageSize ?? 10)
  const logs = queryData(logsQuery.data, [])
  const columns: ColumnsType<DockerOperation> = [
    { title: '任务', dataIndex: 'operationKind', fixed: 'left', width: 190, render: (value, record) => <Space orientation="vertical" size={0}><Text strong>{value}</Text><Text type="secondary">{record.id}</Text></Space> },
    { title: '状态', dataIndex: 'status', width: 115, render: statusTag },
    { title: '关联对象', width: 240, render: (_value, record) => [record.hostId, record.projectId, record.serviceId].filter(Boolean).join(' / ') || '-' },
    { title: '发起人', dataIndex: 'requestedBy', width: 130, render: (value) => value || '-' },
    { title: '尝试', width: 90, render: (_value, record) => `${record.attemptCount ?? 0}/${record.maxRetries ?? 0}` },
    { title: 'Worker', dataIndex: 'claimedByWorkerId', width: 150, render: (value) => value || '-' },
    { title: '开始', dataIndex: 'startedAt', width: 155, render: formatDateTime },
    { title: '结束', dataIndex: 'finishedAt', width: 155, render: formatDateTime },
    {
      title: '操作', align: 'center', fixed: 'right', width: 116, render: (_value, record) => (
        <Space className="soha-row-action-icons">
          <ManagementIconButton aria-label="查看日志" size="small" tooltip="日志" icon={<FileTextOutlined />} onClick={() => setSelectedOperation(record)} />
          {canManageOperations && isPendingOperation(record.status) ? <ManagementIconButton aria-label="取消任务" size="small" tooltip="取消" danger icon={<PoweroffOutlined />} loading={cancelMutation.isPending} onClick={() => cancelMutation.mutate(record.id)} /> : null}
          {canManageOperations && isAbnormalOperation(record.status) ? <ManagementIconButton aria-label="重试任务" size="small" tooltip="重试" icon={<ReloadOutlined />} loading={retryMutation.isPending} onClick={() => retryMutation.mutate(record.id)} /> : null}
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
            actions={<DockerFilterActions loading={operationsQuery.isFetching} onReset={() => { filterForm.resetFields(); setPreset(initialPreset); setFilters({ page: 1, pageSize: filters.pageSize ?? (embedded ? 6 : 10) }) }} />}
            onFinish={(values) => setFilters((current) => ({ ...current, ...values, page: 1 }))}
          >
            <ManagementQueryField minWidth={360} width={460} label="任务视图">
              <Segmented<OperationPreset>
                value={preset}
                onChange={(value) => { setPreset(value); setFilters((current) => ({ ...current, page: 1 })) }}
                options={[
                  { value: 'all', label: '全部' },
                  { value: 'pending', label: '待处理' },
                  { value: 'abnormal', label: '异常' },
                  { value: 'host', label: '主机构建' },
                  { value: 'project', label: 'Compose' },
                  { value: 'service', label: '服务' },
                ]}
              />
            </ManagementQueryField>
            <ManagementQueryField grow minWidth={240} width={320} name="search" label="关键词"><Input allowClear prefix={<SearchOutlined />} placeholder="任务 ID、类型或发起人" /></ManagementQueryField>
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
        title={embedded ? <Text strong>{initialPreset === 'pending' ? '待处理任务' : '操作记录'}</Text> : undefined}
        enableDensity={!embedded}
        refreshing={operationsQuery.isFetching}
        showColumnSettings={!embedded}
        showRefresh={!embedded}
        onRefresh={() => operationsQuery.refetch()}
      />
      <OperationLogDrawer operation={selectedOperation} logs={logs} loading={logsQuery.isLoading} open={Boolean(selectedOperation)} onClose={() => setSelectedOperation(null)} />
    </>
  )
}

export function DockerOverviewPage() {
  const overviewQuery = useQuery({ queryKey: ['docker', 'overview'], queryFn: dockerApi.overview })
  const overview = overviewQuery.data?.data
  const stats = overview?.stats ?? {}
  const hostSummary = overview?.hostSummary ?? {}
  const projectSummary = overview?.projectSummary ?? {}
  const serviceSummary = overview?.serviceSummary ?? {}
  const portSummary = overview?.portSummary ?? {}
  const recentOperations = overview?.recentOperations ?? []
  const expiringProjects = overview?.expiringProjects ?? []
  const overviewTone: OverviewTone = (stats.failedTaskCount ?? 0) > 0 ? 'danger' : (stats.pendingTaskCount ?? 0) > 0 || (hostSummary.provisioning ?? 0) > 0 ? 'warning' : (stats.hostCount ?? 0) > 0 ? 'success' : 'default'
  return (
    <div className="soha-page soha-virtualization-page">
      <div className={`soha-vrt-commandbar is-${overviewTone}`}>
        <div className="soha-vrt-commandbar-main">
          <div className="soha-vrt-title-row"><DockerOutlined /><h1>Docker 工作台</h1><Badge status={badgeStatusForTone(overviewTone)} text={overviewTone === 'danger' ? '存在异常任务' : overviewTone === 'warning' ? '任务处理中' : overviewTone === 'success' ? '运行中' : '未接入'} /></div>
          <div className="soha-vrt-commandbar-meta"><span>主机 {stats.hostCount ?? 0}</span><span>项目 {stats.projectCount ?? 0}</span><span>服务 {stats.serviceCount ?? 0}</span><span>端口 {stats.portMappingCount ?? 0}</span></div>
        </div>
        <div className="soha-vrt-commandbar-actions"><Link to="/docker/projects"><Button type="primary" icon={<PlusOutlined />}>创建 Compose 项目</Button></Link></div>
      </div>
      {overviewQuery.isError ? <Alert type="error" showIcon message="Docker 总览加载失败" /> : null}
      <div className="soha-vrt-metric-grid">
        <MetricCard label="在线主机" value={stats.onlineHostCount ?? 0} helper={`总计 ${stats.hostCount ?? 0}`} tone={(stats.onlineHostCount ?? 0) > 0 ? 'success' : 'default'} />
        <MetricCard label="运行项目" value={stats.runningProjectCount ?? 0} helper={`总计 ${stats.projectCount ?? 0}`} tone="success" />
        <MetricCard label="运行服务" value={stats.runningServiceCount ?? 0} helper={`总计 ${stats.serviceCount ?? 0}`} tone="success" />
        <MetricCard label="端口映射" value={stats.portMappingCount ?? 0} helper={`Public ${portSummary.public ?? 0} / VPN ${portSummary.vpn ?? 0}`} tone={(portSummary.public ?? 0) > 0 ? 'warning' : 'default'} />
        <MetricCard label="异常任务" value={stats.failedTaskCount ?? 0} helper={`处理中 ${stats.pendingTaskCount ?? 0}`} tone={(stats.failedTaskCount ?? 0) > 0 ? 'danger' : (stats.pendingTaskCount ?? 0) > 0 ? 'warning' : 'default'} />
      </div>
      <div className="soha-vrt-workbench-grid">
        <div className="soha-vrt-workbench-main">
          <Card size="small" variant="outlined" className="soha-docker-panel-card">
            <DockerTableHeader title="运行分布" />
            <SummaryChips counts={[{ key: 'hosts-online', label: '主机在线', value: hostSummary.online, tone: 'success' }, { key: 'hosts-provisioning', label: '主机构建中', value: hostSummary.provisioning, tone: 'warning' }, { key: 'projects-running', label: '项目运行', value: projectSummary.running, tone: 'success' }, { key: 'projects-pending', label: '项目待处理', value: projectSummary.pending, tone: 'warning' }, { key: 'services-running', label: '服务运行', value: serviceSummary.running, tone: 'success' }, { key: 'services-failed', label: '服务异常', value: serviceSummary.failed, tone: 'danger' }]} />
          </Card>
          <OperationsTable embedded initialPreset="pending" />
        </div>
        <div className="soha-vrt-side-stack">
          <Card size="small" variant="outlined" className="soha-docker-panel-card">
            <DockerTableHeader title="端口暴露" />
            <SummaryChips compact counts={[{ key: 'internal', label: 'Internal', value: portSummary.internal }, { key: 'vpn', label: 'VPN', value: portSummary.vpn, tone: 'warning' }, { key: 'public', label: 'Public', value: portSummary.public, tone: 'danger' }, { key: 'expired', label: 'Expired', value: portSummary.expired, tone: 'danger' }]} />
          </Card>
          <DockerAdminTable
            rowKey="id"
            title={<Text strong>即将到期项目</Text>}
            pagination={false}
            enableColumnSelection={false}
            enableDensity={false}
            showColumnSettings={false}
            showRefresh={false}
            dataSource={expiringProjects}
            empty="暂无到期项目"
            columns={[
              { title: '项目', dataIndex: 'name' },
              { title: '状态', dataIndex: 'status', render: statusTag },
              { title: '到期', dataIndex: 'expiresAt', render: formatDateTime },
            ]}
          />
          <DockerAdminTable
            rowKey="id"
            title={<Text strong>最近任务</Text>}
            pagination={false}
            enableColumnSelection={false}
            enableDensity={false}
            showColumnSettings={false}
            showRefresh={false}
            dataSource={recentOperations}
            empty="暂无任务"
            columns={[
              { title: '类型', dataIndex: 'operationKind' },
              { title: '状态', dataIndex: 'status', render: statusTag },
              { title: '创建', dataIndex: 'createdAt', render: formatDateTime },
            ]}
          />
        </div>
      </div>
    </div>
  )
}

export function DockerHostsPage() {
  return <div className="soha-page soha-virtualization-page"><HostsTable /></div>
}

export function DockerProjectsPage() {
  return <ContainerManagementPage />
}

export function DockerProjectDetailPage() {
  return <ProjectDetailWorkspace />
}

export function DockerServicesPage() {
  return <Navigate to="/docker/projects" replace />
}

export function DockerPortsPage() {
  return <Navigate to="/docker/projects" replace />
}

export function DockerTemplatesPage() {
  return <div className="soha-page soha-virtualization-page"><TemplatesTable /></div>
}

export function DockerOperationsPage() {
  return <div className="soha-page soha-virtualization-page"><OperationsTable /></div>
}
