import { hasAllowedAction } from '@/features/auth'
import type { DockerPayloadMap, DockerQuickCreateHostInput } from '@/features/docker'
import { localeText } from '@/i18n'
import type {
  CreateVirtualMachineInput,
  VirtualMachine,
  VirtualMachineDetail,
  VirtualizationCluster,
  VirtualizationClusterConfig,
  VirtualizationClusterCredential,
  VirtualizationClusterInput,
  VirtualizationFlavor,
  VirtualizationImageInput,
  VirtualizationOperation,
  VirtualizationPage,
  VirtualizationPayloadMap,
  VirtualizationVmProviderParams,
} from './virtualization-types'

export const VIRTUALIZATION_PROVIDER_LABELS: Record<string, string> = {
  kubevirt: 'KubeVirt',
  pve: 'PVE',
}

export function providerLabel(provider?: string) {
  if (!provider) return '-'
  return VIRTUALIZATION_PROVIDER_LABELS[provider] ?? provider
}

export function filterVmCreateFlavors(
  flavors: VirtualizationFlavor[],
  provider?: string,
  connectionId?: string,
) {
  return flavors.filter(
    (flavor) =>
      (!provider || !flavor.provider || flavor.provider === provider) &&
      (!connectionId || !flavor.connectionId || flavor.connectionId === connectionId),
  )
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
export type ProviderFilter = 'all' | string
export type EnabledFilter = 'all' | 'enabled' | 'disabled'

export function isAbnormalOperation(status?: string) {
  return ['failed', 'callback_timeout'].includes(String(status || '').toLowerCase())
}

export function isPendingOperation(status?: string) {
  return ['queued', 'running'].includes(String(status || '').toLowerCase())
}

export function isStaleVirtualMachine(record?: { status?: string }) {
  return String(record?.status || '').toLowerCase() === 'stale'
}

export function virtualMachineDisplayStatus(record?: { status?: string; powerState?: string }) {
  if (!record) return ''
  const status = String(record.status || '').trim()
  if (status.toLowerCase() === 'stale') return status
  return record.powerState || status
}

function configString(value: unknown) {
  return typeof value === 'string' ? value.trim() : ''
}

function configList(value: unknown) {
  return configString(value)
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function configBoolean(value: unknown) {
  return value === true || String(value).toLowerCase() === 'true'
}

function configNumber(value: unknown) {
  const number = Number(value)
  return Number.isFinite(number) && number > 0 ? number : undefined
}

export function virtualMachineRootDiskGiB(record?: Pick<VirtualMachine, 'diskGiB' | 'config'>) {
  const root = configNumber(record?.config?.rootDiskGiB)
  if (root) return root
  if ((configNumber(record?.config?.diskCount) ?? 1) <= 1) return record?.diskGiB
  return undefined
}

export function virtualMachineObservation(record?: VirtualMachine) {
  const config = record?.config
  return {
    hostname: configString(config?.hostname),
    fqdn: configString(config?.fqdn),
    dnsServers: configList(config?.dnsServers),
    searchDomains: configList(config?.searchDomains),
    gateway: configString(config?.gateway),
    cloudInitKnown: config?.cloudInitConfigured !== undefined,
    cloudInitConfigured: configBoolean(config?.cloudInitConfigured),
    cloudInitUser: configString(config?.cloudInitUser),
    cloudInitSource: configString(config?.cloudInitSource),
    cloudInitUserDataConfigured: configBoolean(config?.cloudInitUserDataConfigured),
    sshKeysConfigured: configBoolean(config?.sshKeysConfigured),
    guestAgentKnown: config?.guestAgentEnabled !== undefined,
    guestAgentEnabled: configBoolean(config?.guestAgentEnabled),
    diskCount: configNumber(config?.diskCount),
    networkInterfaceCount: configNumber(config?.networkInterfaceCount),
    networks: configList(config?.networks),
    firmware: configString(config?.firmware),
    machine: configString(config?.machine),
    osType: configString(config?.osType),
    dnsPolicy: configString(config?.dnsPolicy),
  }
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

export function buildOperationFilter(
  records: VirtualizationOperation[],
  preset: OperationFilterPreset,
) {
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

export function selectableOperationIds(
  records: VirtualizationOperation[],
  action: 'cancel' | 'retry',
) {
  return records
    .filter((record) => hasAllowedAction(record.allowedActions, action))
    .map((record) => record.id)
}

export function badgeStatusForTone(
  tone: OverviewTone,
): 'success' | 'warning' | 'error' | 'default' {
  if (tone === 'success') return 'success'
  if (tone === 'warning') return 'warning'
  if (tone === 'danger') return 'error'
  return 'default'
}

export function operationKind(record: VirtualizationOperation) {
  return record.operationType || record.type || record.action || '-'
}

export function operationKindLabel(record: VirtualizationOperation, localeCode: 'zh_CN' | 'en_US') {
  const kind = operationKind(record)
  const labels: Record<string, [string, string]> = {
    asset_sync: ['资产同步', 'Asset sync'],
    connection_test: ['连接检查', 'Connection test'],
    vm_action: ['虚拟机操作', 'VM action'],
    vm_create: ['创建虚拟机', 'Create VM'],
  }
  const label = labels[kind]
  return label ? localeText(localeCode, label[0], label[1]) : kind
}

export function operationTime(record: VirtualizationOperation) {
  return record.startedAt || record.createdAt || record.updatedAt
}

export const VIRTUALIZATION_PROVIDER_OPTIONS: Array<{
  label: string
  value: VirtualizationProvider
}> = [
  { value: 'kubevirt', label: 'KubeVirt' },
  { value: 'pve', label: 'PVE' },
]

export const VIRTUALIZATION_PROVIDER_FILTER_OPTIONS: Array<{
  label: string
  value: ProviderFilter
}> = [{ value: 'all', label: '全部' }, ...VIRTUALIZATION_PROVIDER_OPTIONS]

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

export function normalizePage<T>(
  data: VirtualizationPage<T> | T[] | undefined,
  fallbackPage: number,
  fallbackPageSize: number,
): VirtualizationPage<T> {
  if (Array.isArray(data)) {
    return { items: data, total: data.length, page: fallbackPage, pageSize: fallbackPageSize }
  }
  return data ?? { items: [], total: 0, page: fallbackPage, pageSize: fallbackPageSize }
}

export function compactRecord<T extends VirtualizationPayloadMap>(values: T): T {
  return Object.fromEntries(
    Object.entries(values).filter(
      ([, value]) => value !== undefined && value !== '' && value !== null,
    ),
  ) as T
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
  username?: string
  password?: string
  ticket?: string
  csrfToken?: string
  defaultNode?: string
  defaultStorage?: string
  defaultBridge?: string
  defaultSnippetStorage?: string
  backendUrl?: string
  prometheusUrl?: string
  prometheusBearerToken?: string
  mode?: string
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
  const statuses =
    params
      .get('statuses')
      ?.split(',')
      .map((item) => item.trim())
      .filter(Boolean) || undefined
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

export function nextOperationSearch(
  preset: OperationFilterPreset,
  base: {
    connectionId?: string
    vmId?: string
    taskKind?: string
    search?: string
    statuses?: string[]
  },
) {
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
    requireCapacity: values.requireCapacity,
    name: values.name,
    guestAccess: values.guestAccess,
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
    templateId: values.templateId,
    imageId: values.imageId,
    network: values.network || undefined,
    cloudInit: values.cloudInit || undefined,
    providerParams: values.providerParams,
    startAfterCreate: Boolean(values.startAfterCreate),
    disks: values.disks,
    networks: values.networks,
  }
}

export interface VirtualMachineFormValues extends CreateVirtualMachineInput {
  managedGuestAccess?: boolean
  guestUsername?: string
  guestPublicKeyRefs?: string[]
  provider?: string
  sourceMode?: string
  enableCloudInit?: boolean
  pveStorage?: string
  pveBridge?: string
  pveIso?: string
  pveOsType?: string
  pveCloudInitUser?: string
  pveCloudInitSSHKeys?: string
  pveSnippetStorage?: string
  pveCICustom?: string
  kubevirtStorageClass?: string
  kubevirtDataVolumeName?: string
  kubevirtNetworkType?: string
  kubevirtNetworkAttachmentDefinition?: string
  kubevirtInterfaceModel?: string
  kubevirtInterfaceBinding?: string
  kubevirtInterfaceName?: string
  registerRuntimeHost?: boolean
  runtimeEnvironment?: string
  runtimeOwner?: string
  runtimeTeam?: string
  runtimeAvailablePortStart?: number
  runtimeAvailablePortEnd?: number
  runtimeTTLSeconds?: number
  runtimeControlPlaneBaseURL?: string
  runtimeEndpoint?: string
  runtimeAgentInstallScript?: string
  runtimeSnippetStorage?: string
}

export function buildCreateVmPayload(values: VirtualMachineFormValues): CreateVirtualMachineInput {
  const cloudInitEnabled = values.enableCloudInit === true
  const managedAccess =
    (cloudInitEnabled || values.registerRuntimeHost === true) && values.managedGuestAccess === true
  const providerParams = compactRecord<VirtualizationVmProviderParams>({
    storage: values.pveStorage,
    bridge: values.pveBridge,
    iso: values.pveIso,
    osType: values.pveOsType,
    ciuser: cloudInitEnabled && !managedAccess ? values.pveCloudInitUser : undefined,
    sshkeys: cloudInitEnabled && !managedAccess ? values.pveCloudInitSSHKeys : undefined,
    snippetStorage: cloudInitEnabled ? values.pveSnippetStorage : undefined,
    cicustom: cloudInitEnabled && !managedAccess ? values.pveCICustom : undefined,
    storageClass: values.kubevirtStorageClass,
    dataVolumeName: values.kubevirtDataVolumeName,
    networkType: values.kubevirtNetworkType,
    networkAttachmentDefinition: values.kubevirtNetworkAttachmentDefinition,
    interfaceModel: values.kubevirtInterfaceModel,
    interfaceBinding: values.kubevirtInterfaceBinding,
    interfaceName: values.kubevirtInterfaceName,
  })
  const sourceMode =
    values.sourceMode || (values.provider === 'pve' ? 'template_clone' : 'datasource_clone')
  const vmClone = sourceMode === 'vm_clone'
  return buildVmPayload({
    ...values,
    bootImageId: vmClone ? undefined : values.bootImageId,
    cloudInit: cloudInitEnabled && !managedAccess ? values.cloudInit : undefined,
    guestAccess: managedAccess
      ? { username: values.guestUsername ?? '', publicKeyRefs: values.guestPublicKeyRefs ?? [] }
      : undefined,
    sourceMode,
    sourceId: vmClone ? values.templateId : values.bootImageId,
    templateId: vmClone ? values.templateId : undefined,
    imageId: vmClone ? undefined : values.bootImageId,
    providerParams: Object.keys(providerParams).length ? providerParams : undefined,
  })
}

export function buildRuntimeHostProvisionPayload(
  values: VirtualMachineFormValues,
  vm = buildCreateVmPayload(values),
): DockerQuickCreateHostInput {
  const providerParams: DockerPayloadMap = {
    ...(vm.providerParams as DockerPayloadMap | undefined),
    sourceMode: vm.sourceMode,
    node: vm.node,
    controlPlaneBaseURL: values.runtimeControlPlaneBaseURL,
    runtimeEndpoint: values.runtimeEndpoint,
    agentInstallScript: values.runtimeAgentInstallScript,
    snippetStorage: values.runtimeSnippetStorage,
  }
  Object.keys(providerParams).forEach((key) => {
    if (providerParams[key] === undefined || providerParams[key] === '') delete providerParams[key]
  })
  return {
    name: vm.name,
    requireCapacity: vm.requireCapacity,
    guestAccess: vm.guestAccess,
    environment: values.runtimeEnvironment,
    owner: values.runtimeOwner,
    team: values.runtimeTeam,
    virtualizationConnectionId: vm.connectionId,
    vmTemplateId: vm.templateId,
    flavorId: vm.flavorId,
    imageId: vm.imageId || vm.bootImageId,
    cpuCoreCount: vm.cpu,
    memoryBytes: vm.memoryMiB ? vm.memoryMiB * 1024 ** 2 : undefined,
    diskBytes: vm.diskGiB ? vm.diskGiB * 1024 ** 3 : undefined,
    network: vm.network,
    availablePortStart: values.runtimeAvailablePortStart,
    availablePortEnd: values.runtimeAvailablePortEnd,
    ttlSeconds: values.runtimeTTLSeconds,
    config: { providerParams },
  }
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
    config: values.config,
    description: values.description || undefined,
  }
}

export function buildClusterPayload(
  values: VirtualizationClusterFormValues,
  existing?: VirtualizationCluster | null,
): VirtualizationClusterInput {
  const provider = values.provider ?? 'kubevirt'
  const config: VirtualizationClusterConfig =
    existing?.provider === provider ? { ...existing.config } : {}
  // Preserve provider extensions while allowing the form's own fields to be cleared.
  for (const key of [
    'region',
    'description',
    ...(provider === 'pve'
      ? ['defaultNode', 'defaultStorage', 'defaultBridge', 'defaultSnippetStorage', 'snippetStorage']
      : ['backendUrl', 'prometheusUrl', 'mode']),
  ]) delete config[key]
  const credential: VirtualizationClusterCredential = {}
  if (values.region) config.region = values.region
  if (values.description) config.description = values.description
  if (provider === 'pve') {
    if (values.defaultNode) config.defaultNode = values.defaultNode
    if (values.defaultStorage) config.defaultStorage = values.defaultStorage
    if (values.defaultBridge) config.defaultBridge = values.defaultBridge
    if (values.defaultSnippetStorage) {
      config.defaultSnippetStorage = values.defaultSnippetStorage
      config.snippetStorage = values.defaultSnippetStorage
    }
    if (values.username) credential.username = values.username
    if (values.password) credential.password = values.password
    if (values.tokenID) credential.tokenID = values.tokenID
    if (values.tokenSecret) credential.tokenSecret = values.tokenSecret
    if (values.ticket) credential.ticket = values.ticket
    if (values.csrfToken) credential.csrfToken = values.csrfToken
  } else {
    if (values.backendUrl) config.backendUrl = values.backendUrl
    if (values.prometheusUrl) config.prometheusUrl = values.prometheusUrl
    if (values.mode) config.mode = values.mode
    if (values.prometheusBearerToken)
      credential.prometheusBearerToken = values.prometheusBearerToken
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
    config: existing ? config : Object.keys(config).length ? config : undefined,
    credential: Object.keys(credential).length ? credential : undefined,
  }
}
