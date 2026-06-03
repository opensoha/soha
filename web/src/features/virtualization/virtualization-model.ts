import { hasAllowedAction } from '@/features/auth/permission-snapshot'
import type {
  CreateVirtualMachineInput,
  VirtualMachineDetail,
  VirtualizationCluster,
  VirtualizationClusterInput,
  VirtualizationImageInput,
  VirtualizationOperation,
  VirtualizationPage,
} from './virtualization-types'

export const STATUS_COLORS: Record<string, string> = {
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

export const VIRTUALIZATION_PROVIDER_LABELS: Record<string, string> = {
  kubevirt: 'KubeVirt',
  pve: 'PVE',
}

export function providerLabel(provider?: string) {
  if (!provider) return '-'
  return VIRTUALIZATION_PROVIDER_LABELS[provider] ?? provider
}

export const OPERATION_FILTER_PRESETS = [
  { key: 'all', label: '全部任务' },
  { key: 'pending', label: '待处理' },
  { key: 'abnormal', label: '失败/超时' },
  { key: 'asset_sync', label: '同步任务' },
  { key: 'vm', label: 'VM 任务' },
] as const

export type OperationFilterPreset = (typeof OPERATION_FILTER_PRESETS)[number]['key']
export type OverviewTone = 'default' | 'success' | 'warning' | 'danger'
export type VirtualizationProvider = 'kubevirt' | 'pve'
export type ProviderFilter = 'all' | VirtualizationProvider
export type EnabledFilter = 'all' | 'enabled' | 'disabled'

export function isAbnormalOperation(status?: string) {
  return ['failed', 'callback_timeout'].includes(String(status || '').toLowerCase())
}

export function isPendingOperation(status?: string) {
  return ['queued', 'running'].includes(String(status || '').toLowerCase())
}

export function isSyncOperation(record: VirtualizationOperation) {
  return operationKind(record) === 'asset_sync'
}

export function isVMOperation(record: VirtualizationOperation) {
  return ['vm_create', 'vm_action'].includes(operationKind(record))
}

export function formatOperationDuration(record: VirtualizationOperation) {
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

export function buildOperationFilter(records: VirtualizationOperation[], preset: OperationFilterPreset) {
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

export function riskReasons(record: VirtualizationCluster) {
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

export function clusterRiskScore(record: VirtualizationCluster) {
  const health = String(record.health || record.status || '').toLowerCase()
  if (health === 'unavailable') return 0
  if (health === 'degraded') return 1
  if (record.enabled !== false && record.credentialConfigured === false) return 2
  if (!record.lastSyncedAt) return 3
  return 4
}

export function latestNonEmptyOperationMessage(record: VirtualizationOperation) {
  return record.message || '-'
}

export function bulkActionSummary(label: string, items: string[]) {
  if (items.length === 0) {
    return `${label} 0 个对象`
  }
  return `${label} ${items.length} 个对象：${items.slice(0, 3).join('、')}${items.length > 3 ? ' 等' : ''}`
}

export function selectableOperationIds(records: VirtualizationOperation[], action: 'cancel' | 'retry') {
  return records.filter((record) => hasAllowedAction(record.allowedActions, action)).map((record) => record.id)
}

export function badgeStatusForTone(tone: OverviewTone): 'success' | 'warning' | 'error' | 'default' {
  if (tone === 'success') return 'success'
  if (tone === 'warning') return 'warning'
  if (tone === 'danger') return 'error'
  return 'default'
}

export function operationKind(record: VirtualizationOperation) {
  return record.operationType || record.type || record.action || '-'
}

export function operationTime(record: VirtualizationOperation) {
  return record.startedAt || record.createdAt || record.updatedAt
}

export const VIRTUALIZATION_PROVIDER_OPTIONS: Array<{ label: string; value: VirtualizationProvider }> = [
  { value: 'kubevirt', label: 'KubeVirt' },
  { value: 'pve', label: 'PVE' },
]

export const VIRTUALIZATION_PROVIDER_FILTER_OPTIONS: Array<{ label: string; value: ProviderFilter }> = [
  { value: 'all', label: '全部' },
  ...VIRTUALIZATION_PROVIDER_OPTIONS,
]

export const ENABLED_FILTER_OPTIONS: Array<{ label: string; value: EnabledFilter }> = [
  { value: 'all', label: '全部' },
  { value: 'enabled', label: '仅启用' },
  { value: 'disabled', label: '仅禁用' },
]

export function classNames(...items: Array<string | false | null | undefined>) {
  return items.filter(Boolean).join(' ')
}

export function virtualizationPageSummary(total: number, range: [number, number]) {
  return total > 0 ? `当前 ${range[0]}-${range[1]} / ${total} 条` : '当前 0 / 0 条'
}

export function localTableSummary(filteredCount: number, totalCount: number) {
  return `当前 ${filteredCount} / ${totalCount} 条`
}

export function normalizePage<T>(data: VirtualizationPage<T> | T[] | undefined, fallbackPage: number, fallbackPageSize: number): VirtualizationPage<T> {
  if (Array.isArray(data)) {
    return { items: data, total: data.length, page: fallbackPage, pageSize: fallbackPageSize }
  }
  return data ?? { items: [], total: 0, page: fallbackPage, pageSize: fallbackPageSize }
}

export function compactRecord(values: Record<string, unknown>) {
  return Object.fromEntries(
    Object.entries(values).filter(([, value]) => value !== undefined && value !== '' && value !== null),
  )
}

export function stringifyRaw(value: VirtualMachineDetail['providerRaw'] | undefined) {
  if (!value) return ''
  if (typeof value === 'string') return value
  return JSON.stringify(value, null, 2)
}

export interface VirtualizationClusterFormValues {
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
  defaultBridge?: string
  backendUrl?: string
  prometheusUrl?: string
  prometheusBearerToken?: string
}

export function operationPresetFromSearch(search: string): OperationFilterPreset {
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

export function operationParamsFromSearch(search: string) {
  const params = new URLSearchParams(search)
  const abnormal = params.get('abnormal') === 'true'
  const pending = params.get('pending') === 'true'
  const taskKind = params.get('taskKind') || params.get('assetType') || undefined
  const connectionId = params.get('connectionId') || undefined
  const vmId = params.get('vmId') || undefined
  const searchText = params.get('search') || undefined
  const statuses = params.get('statuses')?.split(',').map((item) => item.trim()).filter(Boolean) || undefined
  return {
    preset: operationPresetFromSearch(search),
    query: {
      abnormal,
      pending,
      taskKind,
      assetType: taskKind === 'asset_sync' ? taskKind : undefined,
      connectionId,
      vmId,
      search: searchText,
      statuses,
    },
  }
}

export function nextOperationSearch(preset: OperationFilterPreset, base: { connectionId?: string; vmId?: string; taskKind?: string; search?: string; statuses?: string[] }) {
  const params = new URLSearchParams()
  if (base.connectionId) params.set('connectionId', base.connectionId)
  if (base.vmId) params.set('vmId', base.vmId)
  if (base.search) params.set('search', base.search)
  if (base.statuses?.length) params.set('statuses', base.statuses.join(','))
  switch (preset) {
    case 'pending':
      params.set('pending', 'true')
      break
    case 'abnormal':
      params.set('abnormal', 'true')
      break
    case 'asset_sync':
      params.set('taskKind', 'asset_sync')
      break
    case 'vm':
      params.set('taskKind', 'vm_action')
      break
    default:
      if (base.taskKind) params.set('taskKind', base.taskKind)
      break
  }
  const query = params.toString()
  return query ? `?${query}` : ''
}

export function buildVmPayload(values: CreateVirtualMachineInput): CreateVirtualMachineInput {
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
    sourceMode: values.sourceMode,
    sourceId: values.sourceId,
    imageId: values.imageId,
    network: values.network || undefined,
    cloudInit: values.cloudInit || undefined,
    providerParams: values.providerParams,
    startAfterCreate: Boolean(values.startAfterCreate),
  }
}

export interface VirtualMachineFormValues extends CreateVirtualMachineInput {
  provider?: string
  sourceMode?: string
  pveStorage?: string
  pveBridge?: string
  pveIso?: string
  pveCloudInitUser?: string
  pveCloudInitSSHKeys?: string
  kubevirtStorageClass?: string
  kubevirtDataVolumeName?: string
}

export function buildCreateVmPayload(values: VirtualMachineFormValues): CreateVirtualMachineInput {
  const providerParams = compactRecord({
    storage: values.pveStorage,
    bridge: values.pveBridge,
    iso: values.pveIso,
    ciuser: values.pveCloudInitUser,
    sshkeys: values.pveCloudInitSSHKeys,
    storageClass: values.kubevirtStorageClass,
    dataVolumeName: values.kubevirtDataVolumeName,
  })
  const sourceMode = values.sourceMode || (values.provider === 'pve' ? 'template_clone' : 'datasource_clone')
  return buildVmPayload({
    ...values,
    sourceMode,
    sourceId: values.bootImageId,
    imageId: values.bootImageId,
    providerParams: Object.keys(providerParams).length ? providerParams : undefined,
  })
}

export function buildImagePayload(values: VirtualizationImageInput): VirtualizationImageInput {
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

export function buildClusterPayload(values: VirtualizationClusterFormValues): VirtualizationClusterInput {
  const provider = values.provider ?? 'kubevirt'
  const config: Record<string, unknown> = {}
  const credential: Record<string, unknown> = {}
  if (values.region) config.region = values.region
  if (values.description) config.description = values.description
  if (provider === 'pve') {
    if (values.defaultNode) config.defaultNode = values.defaultNode
    if (values.defaultStorage) config.defaultStorage = values.defaultStorage
    if (values.defaultBridge) config.defaultBridge = values.defaultBridge
    if (values.tokenID) credential.tokenID = values.tokenID
    if (values.tokenSecret) credential.tokenSecret = values.tokenSecret
    if (values.ticket) credential.ticket = values.ticket
    if (values.csrfToken) credential.csrfToken = values.csrfToken
  } else {
    if (values.backendUrl) config.backendUrl = values.backendUrl
    if (values.prometheusUrl) config.prometheusUrl = values.prometheusUrl
    if (values.prometheusBearerToken) config.prometheusBearerToken = values.prometheusBearerToken
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
