export type VirtualizationPayloadPrimitive = string | number | boolean | null

export type VirtualizationPayloadValue =
  | VirtualizationPayloadPrimitive
  | VirtualizationPayloadMap
  | VirtualizationPayloadValue[]
  | undefined

export interface VirtualizationPayloadMap {
  [key: string]: VirtualizationPayloadValue
}

export interface VirtualizationVmProviderParams extends VirtualizationPayloadMap {
  storage?: string
  bridge?: string
  iso?: string
  ciuser?: string
  sshkeys?: string
  snippetStorage?: string
  cicustom?: string
  storageClass?: string
  dataVolumeName?: string
  networkType?: string
  networkAttachmentDefinition?: string
  interfaceModel?: string
  interfaceBinding?: string
  interfaceName?: string
}

export interface VirtualizationClusterConfig extends VirtualizationPayloadMap {
  region?: string
  description?: string
  defaultNode?: string
  defaultStorage?: string
  defaultBridge?: string
  defaultSnippetStorage?: string
  snippetStorage?: string
  backendUrl?: string
  prometheusUrl?: string
  prometheusBearerTokenConfigured?: boolean
  credentialSecretRef?: string
  mode?: string
}

export interface VirtualizationClusterCredential extends VirtualizationPayloadMap {
  username?: string
  password?: string
  tokenID?: string
  tokenSecret?: string
  ticket?: string
  csrfToken?: string
  prometheusBearerToken?: string
}

export interface VirtualizationImageConfig extends VirtualizationPayloadMap {
  description?: string
  sourceKind?: string
  sourceRef?: string
  namespace?: string
  storageClass?: string
  url?: string
}

export type VirtualizationOperationPayload = VirtualizationPayloadMap
export type VirtualizationProviderRaw = VirtualizationPayloadMap | string | null

export interface VirtualMachineConfig extends VirtualizationPayloadMap {
  hostname?: string
  fqdn?: string
  dnsServers?: string
  searchDomains?: string
  gateway?: string
  cloudInitConfigured?: boolean | string
  cloudInitUser?: string
  cloudInitSource?: string
  cloudInitUserDataConfigured?: boolean | string
  sshKeysConfigured?: boolean | string
  guestAgentEnabled?: boolean | string
  diskCount?: number | string
  networkInterfaceCount?: number | string
  networks?: string
  firmware?: string
  machine?: string
  osType?: string
  dnsPolicy?: string
}

export interface VirtualMachine {
  id: string
  externalId?: string
  name: string
  provider?: string
  connectionId?: string
  connectionName?: string
  namespace?: string
  node?: string
  status?: string
  powerState?: string
  flavorId?: string
  flavorName?: string
  sourceMode?: string
  sourceRef?: string
  cpu?: number
  memoryMiB?: number
  diskGiB?: number
  bootImageId?: string
  bootImageName?: string
  ipAddresses?: string[]
  network?: string
  config?: VirtualMachineConfig
  createdAt?: string
  updatedAt?: string
  allowedActions?: string[]
  capabilities?: string[]
}

export interface VirtualMachineDiskChange {
  id?: string
  name?: string
  storage?: string
  bus?: string
  sizeGiB: number
  add?: boolean
}
export interface VirtualMachineNetworkChange {
  id?: string
  name?: string
  network: string
  model?: string
  binding?: string
  add?: boolean
  remove?: boolean
}
export interface VirtualMachineDevice {
  id: string
  kind: 'disk' | 'network'
  name?: string
  sizeGiB?: number
  storage?: string
  network?: string
  model?: string
}

export interface CreateVirtualMachineInput {
  requireCapacity?: boolean
  guestAccess?: import('@opensoha/contracts/gen/ts/sohaapi').VirtualMachineGuestAccessInput
  name: string
  connectionId: string
  flavorId?: string
  namespace?: string
  node?: string
  cpu?: number
  memoryMiB?: number
  bootImageId?: string
  imageId?: string
  sourceMode?: string
  sourceId?: string
  templateId?: string
  diskGiB?: number
  network?: string
  cloudInit?: string
  providerParams?: VirtualizationVmProviderParams
  startAfterCreate?: boolean
  disks?: VirtualMachineDiskChange[]
  networks?: VirtualMachineNetworkChange[]
}

export type VirtualMachinePowerAction = 'start' | 'stop' | 'restart' | 'shutdown' | 'delete'
export interface VirtualMachineResizeInput {
  cpu?: number
  memoryMiB?: number
  diskGiB?: number
  disks?: VirtualMachineDiskChange[]
  networks?: VirtualMachineNetworkChange[]
}

export interface VirtualizationCluster {
  id: string
  name: string
  provider?: string
  endpoint?: string
  kubernetesClusterId?: string
  defaultNamespace?: string
  enabled?: boolean
  verifyTls?: boolean
  credentialConfigured?: boolean
  config?: VirtualizationClusterConfig
  status?: string
  health?: string
  version?: string
  region?: string
  description?: string
  riskLevel?: 'normal' | 'attention' | 'warning' | 'critical'
  riskReasons?: string[]
  lastSyncedAt?: string
  createdAt?: string
  updatedAt?: string
  allowedActions?: string[]
  capabilities?: string[]
}

export interface VirtualizationDeleteDependencySample {
  id: string
  kind?: string
  name?: string
  externalId?: string
  status?: string
  nodeName?: string
  taskKind?: string
  vmId?: string
}

export interface VirtualizationConnectionDeleteDependencies {
  connection?: VirtualizationCluster
  vmCount?: number
  imageCount?: number
  flavorCount?: number
  taskCount?: number
  pendingTaskCount?: number
  dockerHostCount?: number
  vmSamples?: VirtualizationDeleteDependencySample[]
  imageSamples?: VirtualizationDeleteDependencySample[]
  flavorSamples?: VirtualizationDeleteDependencySample[]
  taskSamples?: VirtualizationDeleteDependencySample[]
  forceRequired?: boolean
  blocking?: boolean
  blockingReasons?: string[]
}

export interface VirtualizationClusterInput {
  name: string
  provider?: string
  endpoint?: string
  kubernetesClusterId?: string
  defaultNamespace?: string
  enabled?: boolean
  verifyTls?: boolean
  credential?: VirtualizationClusterCredential
  config?: VirtualizationClusterConfig
  region?: string
  description?: string
}

export interface VirtualizationImageConfig extends VirtualizationPayloadMap {
  sourceKind?: string
  assetKind?: string
  sourceRef?: string
  namespace?: string
  node?: string
  storage?: string
  storageClass?: string
  content?: string
  contentType?: string
  iface?: string
  network?: string
  bridge?: string | boolean
  active?: string | boolean
  supportsISO?: string | boolean
  supportsImages?: string | boolean
  supportsSnippets?: string | boolean
  supportsBackup?: string | boolean
  supportsRootdir?: string | boolean
}

export interface VirtualizationImage {
  id: string
  name: string
  provider?: string
  connectionId?: string
  connectionName?: string
  namespace?: string
  source?: string
  sourceKind?: string
  sourceRef?: string
  assetKind?: string
  ready?: boolean
  node?: string
  storage?: string
  storageClass?: string
  osType?: string
  status?: string
  sizeGiB?: number
  description?: string
  config?: VirtualizationImageConfig
  createdAt?: string
  updatedAt?: string
  allowedActions?: string[]
}

export interface VirtualizationImageInput {
  name: string
  provider?: string
  connectionId?: string
  namespace?: string
  source?: string
  sourceKind?: string
  sourceRef?: string
  osType?: string
  sizeGiB?: number
  description?: string
  config?: VirtualizationImageConfig
}

export interface VirtualizationFlavor {
  id: string
  name: string
  provider?: string
  connectionId?: string
  description?: string
  cpu: number
  memoryMiB: number
  diskGiB: number
  enabled?: boolean
  createdAt?: string
  updatedAt?: string
  allowedActions?: string[]
}

export interface VirtualizationFlavorInput {
  name: string
  description?: string
  cpu: number
  memoryMiB: number
  diskGiB: number
  enabled?: boolean
}

export interface VirtualizationOperation {
  payload?: VirtualizationOperationPayload
  result?: VirtualizationOperationPayload
  id: string
  type?: string
  operationType?: string
  action?: string
  assetType?: string
  targetType?: string
  targetName?: string
  status?: string
  message?: string
  connectionId?: string
  connectionName?: string
  vmId?: string
  actor?: string
  lastHeartbeatAt?: string
  startedAt?: string
  completedAt?: string
  createdAt?: string
  updatedAt?: string
  logs?: string[]
  logText?: string
  allowedActions?: string[]
}

export interface VirtualizationOperationLog {
  id: string
  taskId: string
  logLevel?: string
  message: string
  payload?: VirtualizationOperationPayload
  createdAt?: string
}

export interface VirtualizationListParams {
  search?: string
  page?: number
  pageSize?: number
  provider?: string
  connectionId?: string
  status?: string
}

export type VirtualizationImageCategory = 'catalog' | 'storage'

export interface VirtualizationImageListParams extends VirtualizationListParams {
  category?: VirtualizationImageCategory
}

export interface VirtualizationOperationListParams {
  assetType?: string
  taskKind?: string
  abnormal?: boolean
  pending?: boolean
  statuses?: string[]
  connectionId?: string
  vmId?: string
  search?: string
}

export interface VirtualizationPage<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

export interface VirtualMachineDetail {
  vm: VirtualMachine
  connection?: VirtualizationCluster
  image?: VirtualizationImage
  providerRaw?: VirtualizationProviderRaw
  operations?: VirtualizationOperation[]
  logs?: VirtualizationOperationLog[]
}

export interface MetricPoint {
  timestamp: number
  value: number
}

export interface MetricSeries {
  key: string
  label: string
  unit: string
  points: MetricPoint[]
}

export interface VirtualizationVMMetrics {
  series: MetricSeries[]
  message?: string
  ready?: boolean
  source?: string
}

export interface VirtualizationConsoleURL {
  type: string
  url: string
  backendUrl?: string
  token?: string
  message?: string
  ready?: boolean
  provider?: string
  proxyMode?: string
}
