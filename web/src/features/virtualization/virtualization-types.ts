export interface VirtualizationConnectionHealth {
  total: number
  healthy: number
  degraded: number
  unavailable: number
}

export interface VirtualizationOverviewConnectionSummary {
  total?: number
  healthy?: number
  degraded?: number
  unavailable?: number
  neverSynced?: number
  credentialMissing?: number
}

export interface VirtualizationOverviewTaskSummary {
  queued?: number
  running?: number
  failed?: number
  timeout?: number
  canceled?: number
  completed?: number
}

export interface VirtualizationOverviewProviderSummary {
  provider: string
  connections?: number
  healthy?: number
  degraded?: number
  unavailable?: number
  vms?: number
  runningVms?: number
}

export interface VirtualizationOverviewAttention {
  riskyConnections?: VirtualizationCluster[]
  failedSyncTasks?: VirtualizationOperation[]
  failedOperations?: VirtualizationOperation[]
}

export interface VirtualizationOverviewStats {
  connections?: VirtualizationConnectionHealth
  vmCount?: number
  runningVmCount?: number
  stoppedVmCount?: number
  imageCount?: number
  flavorCount?: number
  pendingTaskCount?: number
  failedTaskCount?: number
}

export interface VirtualizationOverview {
  stats?: VirtualizationOverviewStats
  recentOperations?: VirtualizationOperation[]
  lastSyncTask?: VirtualizationOperation | null
  connectionSummary?: VirtualizationOverviewConnectionSummary
  taskSummary?: VirtualizationOverviewTaskSummary
  providerSummary?: VirtualizationOverviewProviderSummary[]
  attention?: VirtualizationOverviewAttention
}

export interface VirtualMachine {
  id: string
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
  createdAt?: string
  updatedAt?: string
  allowedActions?: string[]
}

export interface CreateVirtualMachineInput {
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
  diskGiB?: number
  network?: string
  cloudInit?: string
  providerParams?: Record<string, unknown>
  startAfterCreate?: boolean
}

export type VirtualMachinePowerAction = 'start' | 'stop' | 'restart' | 'shutdown' | 'delete'

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
  config?: Record<string, unknown>
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
}

export interface VirtualizationClusterInput {
  name: string
  provider?: string
  endpoint?: string
  kubernetesClusterId?: string
  defaultNamespace?: string
  enabled?: boolean
  verifyTls?: boolean
  credential?: Record<string, unknown>
  config?: Record<string, unknown>
  region?: string
  description?: string
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
  config?: Record<string, unknown>
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
  config?: Record<string, unknown>
}

export interface VirtualizationFlavor {
  id: string
  name: string
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
  payload?: Record<string, unknown>
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
  providerRaw?: Record<string, unknown> | string | null
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
