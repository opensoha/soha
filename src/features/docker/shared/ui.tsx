import type { ComponentProps, Dispatch, ReactNode, SetStateAction } from 'react'
import { useState } from 'react'
import { Badge, Button, Card, Space, Typography } from 'antd'
import type { FormInstance } from 'antd'
import { useQuery } from '@tanstack/react-query'
import type { QueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { BooleanTag, MetadataTag, StatusTag } from '@/components/status-tag'
import {
  ManagementDensityButton,
  ManagementDetailHeader,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useWorkbenchModuleEnabled } from '@/features/modules'
import { localeText, useI18n } from '@/i18n'
import { formatStatusLabel } from '@/i18n/status'
import { invalidateDockerQueries } from '../mutations'
import { dockerQueries } from '../queries'
import type {
  DockerContainerPortInput,
  DockerListParams,
  DockerOperation,
  DockerPage,
  DockerPayloadMap,
  DockerPortMapping,
  DockerProject,
} from '../docker-types'
import './styles.css'

const { Text } = Typography

export type OverviewTone = 'default' | 'success' | 'warning' | 'danger'
export type DockerProjectSourceKind = 'compose' | 'single_container'

export interface DockerFilterState extends DockerListParams {
  operationKind?: string
  abnormal?: boolean
  pending?: boolean
  kind?: string
  enabled?: boolean
}

export const DEFAULT_COMPOSE = `services:\n  web:\n    image: nginx:alpine\n    ports:\n      - "8080:80"\n`

export const HOST_STATUS_OPTIONS = [
  'pending',
  'online',
  'ready',
  'docker_ready',
  'provisioning',
  'vm_ready',
  'provisioned_waiting_agent',
  'agent_registered',
  'agent_bootstrapping',
  'agent_failed',
  'degraded',
  'offline',
  'unavailable',
]

export const ARCHITECTURE_OPTIONS = [
  { value: 'amd64', label: 'x86_64 / amd64' },
  { value: 'arm64', label: 'ARM64 / aarch64' },
]
export const DEFAULT_CONTAINER_PORTS = [
  {
    name: 'http',
    hostIp: '0.0.0.0',
    containerPort: 80,
    hostPort: 18080,
    protocol: 'tcp',
    exposureScope: 'internal',
    domainScheme: 'http',
    domainTlsEnabled: false,
  },
]

type DockerProjectPortDisplay = Partial<DockerContainerPortInput>
type AdminTableProps = ComponentProps<typeof AdminTable>

function classNames(...items: Array<string | false | null | undefined>) {
  return items.filter(Boolean).join(' ')
}

export function statusTag(value?: string, _record?: unknown) {
  if (!value) return <Text type="secondary">-</Text>
  return <StatusTag value={value} />
}

export function boolTag(value?: boolean) {
  return <BooleanTag value={Boolean(value)} trueLabel="启用" falseLabel="停用" />
}

export function architectureTag(value?: string) {
  if (!value) return <Text type="secondary">-</Text>
  const normalized = String(value).toLowerCase()
  const tone = normalized === 'arm64' || normalized === 'aarch64' ? 'cyan' : 'blue'
  return <MetadataTag tone={tone} label={normalized === 'amd64' ? 'x86_64' : normalized} />
}

export function formatBytes(value?: number) {
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

export function configTextValue(config: DockerPayloadMap | undefined, key: string) {
  const value = config?.[key]
  if (typeof value === 'string') return value.trim() || '-'
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return '-'
}

export function configArrayCount(config: DockerPayloadMap | undefined, key: string) {
  const value = config?.[key]
  return Array.isArray(value) ? value.length : 0
}

export function formatPercent(value?: number) {
  if (value === undefined || value === null) return '-'
  return `${value.toFixed(value >= 10 ? 0 : 1)}%`
}

export function formatPort(record: DockerPortMapping) {
  const host = `${record.hostIp || '0.0.0.0'}:${record.hostPort}`
  return `${host} -> ${record.containerPort}/${record.protocol || 'tcp'}`
}

export function stringValue(value: unknown) {
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

export function numberValue(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : undefined
  }
  return undefined
}

function isDockerContainerPortItem(item: unknown): item is DockerContainerPortInput {
  return Boolean(item && typeof item === 'object' && !Array.isArray(item))
}

export function projectPortItems(record: DockerProject) {
  const config = record.config ?? {}
  const rawPorts = Array.isArray(config.ports) ? config.ports : []
  const ports = rawPorts.filter(isDockerContainerPortItem)
  if (ports.length > 0) {
    return ports
  }
  const hostPort = numberValue(config.hostPort)
  const containerPort = numberValue(config.containerPort)
  if (!hostPort && !containerPort) {
    return []
  }
  return [
    {
      hostIp: config.hostIp,
      hostPort,
      containerPort,
      protocol: config.protocol,
      domainName: config.domainName,
      domainScheme: config.domainScheme,
      domainTlsEnabled: config.domainTlsEnabled,
      exposureScope: config.exposureScope,
    },
  ]
}

function formatProjectPortItem(item: DockerProjectPortDisplay) {
  const hostPort = numberValue(item.hostPort)
  const containerPort = numberValue(item.containerPort)
  const protocol = stringValue(item.protocol) || 'tcp'
  const hostIp = stringValue(item.hostIp) || '0.0.0.0'
  if (!hostPort && !containerPort) {
    return '-'
  }
  return `${hostPort ? `${hostIp}:${hostPort}` : '-'} -> ${containerPort || '-'}/${protocol}`
}

export function renderProjectPortSummary(record: DockerProject) {
  const ports = projectPortItems(record)
  if (ports.length === 0) {
    return <Text type="secondary">-</Text>
  }
  return (
    <Space orientation="vertical" size={0} className="soha-docker-port-summary">
      {ports.slice(0, 2).map((item, index) => {
        const domainName = stringValue(item.domainName)
        return (
          <span
            key={`${formatProjectPortItem(item)}-${index}`}
            className="soha-docker-port-summary-item"
          >
            <Text code>{formatProjectPortItem(item)}</Text>
            {domainName ? <Text type="secondary">{domainName}</Text> : null}
          </span>
        )
      })}
      {ports.length > 2 ? <MetadataTag label={`+${ports.length - 2}`} /> : null}
    </Space>
  )
}

export function formatAccessURL(record: DockerPortMapping) {
  if (record.accessUrl) return record.accessUrl
  if (record.domainName)
    return `${record.domainScheme || (record.domainTlsEnabled ? 'https' : 'http')}://${record.domainName}`
  return ''
}

export function compactRecord<T extends object>(values: T): T {
  return Object.fromEntries(
    Object.entries(values).filter(
      ([, value]) => value !== undefined && value !== '' && value !== null,
    ),
  ) as T
}

export function bytesFromMiB(value?: number) {
  return value && value > 0 ? Math.round(value * 1024 ** 2) : undefined
}

export function normalizePage<T>(
  data: DockerPage<T> | undefined,
  fallbackPage: number,
  fallbackPageSize: number,
): DockerPage<T> {
  return data ?? { items: [], total: 0, page: fallbackPage, pageSize: fallbackPageSize }
}

export function queryData<T>(data: T | undefined, fallback: T) {
  return data ?? fallback
}

export function pageTablePagination<T>(
  page: DockerPage<T>,
  embedded: boolean,
  setFilters: Dispatch<SetStateAction<DockerFilterState>>,
) {
  if (embedded) return false
  return {
    current: page.page,
    pageSize: page.pageSize,
    total: page.total,
    onPageChange: (pageNumber: number) =>
      setFilters((current) => ({ ...current, page: pageNumber })),
    onPageSizeChange: (pageSize: number) =>
      setFilters((current) => ({ ...current, page: 1, pageSize })),
  }
}

export function refreshDocker(queryClient: QueryClient) {
  return invalidateDockerQueries(queryClient)
}

export function isPendingOperation(status?: string) {
  return ['queued', 'running'].includes(String(status || '').toLowerCase())
}

export function isAbnormalOperation(status?: string) {
  return ['failed', 'callback_timeout', 'timeout', 'error'].includes(
    String(status || '').toLowerCase(),
  )
}

export function badgeStatusForTone(
  tone: OverviewTone,
): 'success' | 'warning' | 'error' | 'default' {
  if (tone === 'success') return 'success'
  if (tone === 'warning') return 'warning'
  if (tone === 'danger') return 'error'
  return 'default'
}

export function operationTone(record: DockerOperation): OverviewTone {
  if (isAbnormalOperation(record.status)) return 'danger'
  if (isPendingOperation(record.status)) return 'warning'
  if (record.status === 'completed') return 'success'
  return 'default'
}

export function operationActionLabel(
  action: string,
  localeCode: Parameters<typeof formatStatusLabel>[1] = 'zh_CN',
) {
  if (localeCode !== 'zh_CN') {
    return (
      (
        {
          deploy: 'Deploy',
          redeploy: 'Redeploy',
          start: 'Start',
          stop: 'Stop',
          restart: 'Restart',
          down: 'Down',
          pull: 'Pull',
          build: 'Build',
          destroy: 'Destroy',
        } as Record<string, string>
      )[action] ?? action
    )
  }
  return (
    (
      {
        deploy: '部署',
        redeploy: '销毁重建',
        start: '启动',
        stop: '停止',
        restart: '重启',
        down: 'Down',
        pull: 'Pull',
        build: 'Build',
        destroy: '销毁',
      } as Record<string, string>
    )[action] ?? action
  )
}

export function useDockerPermissions() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const { moduleEnabled: dockerModuleEnabled } = useWorkbenchModuleEnabled('docker')
  const snapshot = permissionSnapshotQuery.data?.data
  const hasDockerPermission = (key: string) => dockerModuleEnabled && hasPermission(snapshot, key)
  return {
    dockerModuleEnabled,
    canViewHosts: hasDockerPermission('docker.hosts.view'),
    canCreateHosts: hasDockerPermission('docker.hosts.create'),
    canUpdateHosts: hasDockerPermission('docker.hosts.update'),
    canDeleteHosts: hasDockerPermission('docker.hosts.delete'),
    canViewProjects: hasDockerPermission('docker.projects.view'),
    canCreateProjects: hasDockerPermission('docker.projects.create'),
    canUpdateProjects: hasDockerPermission('docker.projects.update'),
    canDeleteProjects: hasDockerPermission('docker.projects.delete'),
    canDeployProjects: hasDockerPermission('docker.projects.deploy'),
    canViewServices: hasDockerPermission('docker.services.view'),
    canReadServiceFiles: hasDockerPermission('docker.services.files'),
    canStartServices: hasDockerPermission('docker.services.start'),
    canStopServices: hasDockerPermission('docker.services.stop'),
    canRestartServices: hasDockerPermission('docker.services.restart'),
    canViewServiceLogs: hasDockerPermission('docker.services.logs'),
    canAccessServiceTerminal: hasDockerPermission('docker.services.terminal'),
    canViewPorts: hasDockerPermission('docker.ports.view'),
    canCreatePorts: hasDockerPermission('docker.ports.create'),
    canUpdatePorts: hasDockerPermission('docker.ports.update'),
    canDeletePorts: hasDockerPermission('docker.ports.delete'),
    canCreateTemplates: hasDockerPermission('docker.templates.create'),
    canUpdateTemplates: hasDockerPermission('docker.templates.update'),
    canDeleteTemplates: hasDockerPermission('docker.templates.delete'),
    canViewOperations: hasDockerPermission('docker.operations.view'),
    canCancelOperations: hasDockerPermission('docker.operations.cancel'),
    canRetryOperations: hasDockerPermission('docker.operations.retry'),
  }
}

export function DockerTableHeader({
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
  const { localeCode } = useI18n()
  return (
    <ManagementDetailHeader
      title={
        <Space size={8} wrap>
          <span>{title}</span>
          {status ? (
            <Badge status={badgeStatusForTone(tone)} text={formatStatusLabel(status, localeCode)} />
          ) : null}
        </Space>
      }
      meta={meta.length > 0 ? meta.map((item) => <span key={item}>{item}</span>) : undefined}
      actions={actions ? <ManagementTableToolbar>{actions}</ManagementTableToolbar> : undefined}
    />
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

export function DockerAdminTable({
  actions,
  className,
  enableDensity = true,
  onRefresh,
  paginationSummary,
  refreshing,
  scroll,
  shellClassName,
  showColumnSettings = true,
  showRefresh = true,
  title,
  viewportScroll,
  ...tableProps
}: DockerAdminTableProps) {
  const { localeCode } = useI18n()
  const resolvedPaginationSummary =
    paginationSummary ??
    ((total: number, range: [number, number]) =>
      total <= 0
        ? localeText(localeCode, '当前 0 / 0 条', '0 of 0')
        : localeText(
            localeCode,
            `当前 ${range[0]}-${range[1]} / ${total} 条`,
            `${range[0]}-${range[1]} of ${total}`,
          ))
  const [tableSize, setTableSize] = useState<NonNullable<AdminTableProps['tableSize']>>('small')
  const toolbarExtra =
    actions || enableDensity || (showRefresh && onRefresh) ? (
      <ManagementTableToolbar>
        {actions}
        {enableDensity ? (
          <ManagementDensityButton
            aria-label={localeText(localeCode, '切换表格密度', 'Toggle table density')}
            size="small"
            tooltip={tableText(
              localeCode,
              tableSize === 'small' ? '切换为宽松密度' : '切换为紧凑密度',
              tableSize === 'small' ? 'Switch to relaxed density' : 'Switch to compact density',
            )}
            onClick={() => setTableSize((current) => (current === 'small' ? 'middle' : 'small'))}
          />
        ) : null}
        {showRefresh && onRefresh ? (
          <ManagementRefreshButton
            aria-label={localeText(localeCode, '刷新列表', 'Refresh list')}
            loading={refreshing}
            size="small"
            tooltip={localeText(localeCode, '刷新', 'Refresh')}
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
      paginationSummary={resolvedPaginationSummary}
      scroll={scroll}
      shellClassName={classNames(
        'soha-management-table-shell',
        'soha-docker-table-shell',
        shellClassName,
      )}
      tableSize={tableSize}
      title={title}
      toolbarExtra={toolbarExtra}
      viewportScroll={viewportScroll ?? showColumnSettings}
    />
  )
}

function tableText(
  localeCode: Parameters<typeof formatStatusLabel>[1],
  chinese: string,
  english: string,
) {
  return localeText(localeCode, chinese, english)
}

export function MetricCard({
  label,
  value,
  helper,
  tone = 'default',
  onClick,
}: {
  label: string
  value: number | string
  helper?: string
  tone?: OverviewTone
  onClick?: () => void
}) {
  return (
    <Card size="small" variant="outlined" className={`soha-vrt-metric-card is-${tone}`}>
      <button
        type="button"
        className="soha-vrt-metric-card-button"
        onClick={onClick}
        disabled={!onClick}
      >
        <span className="soha-overview-metric-label">{label}</span>
        <span className="soha-vrt-stat-value">{value}</span>
        {helper ? <span className="soha-overview-metric-helper">{helper}</span> : null}
      </button>
    </Card>
  )
}

export function SummaryChips({
  counts,
  compact = false,
}: {
  counts: Array<{ key: string; label: string; value?: number; tone?: OverviewTone }>
  compact?: boolean
}) {
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

export function DrawerFooter({
  form,
  loading,
  onCancel,
  submitLabel = '提交',
}: {
  form: FormInstance
  loading?: boolean
  onCancel: () => void
  submitLabel?: string
}) {
  return (
    <Space>
      <Button type="primary" loading={loading} onClick={() => form.submit()}>
        {submitLabel}
      </Button>
      <Button onClick={onCancel}>取消</Button>
    </Space>
  )
}

export function useDockerOptions({
  includeProjects = true,
  includeServices = true,
}: {
  includeProjects?: boolean
  includeServices?: boolean
} = {}) {
  const { canViewHosts, canViewProjects, canViewServices } = useDockerPermissions()
  const hostsQuery = useQuery(dockerQueries.hostOptions(canViewHosts))
  const projectsQuery = useQuery(dockerQueries.projectOptions(canViewProjects && includeProjects))
  const servicesQuery = useQuery(dockerQueries.serviceOptions(canViewServices && includeServices))
  const hosts = canViewHosts ? normalizePage(hostsQuery.data, 1, 200).items : []
  const projects = canViewProjects ? normalizePage(projectsQuery.data, 1, 200).items : []
  const services = canViewServices ? normalizePage(servicesQuery.data, 1, 300).items : []
  return {
    hosts,
    projects,
    services,
    hostOptions: hosts.map((item) => ({ value: item.id, label: item.name || item.id })),
    projectOptions: projects.map((item) => ({ value: item.id, label: item.name || item.id })),
    serviceOptions: services.map((item) => ({ value: item.id, label: item.name || item.id })),
  }
}
