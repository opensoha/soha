import type { DockerVolumeDLPDecision, OperationState } from '@opensoha/contracts/gen/ts/sohaapi'

export type DockerPayloadPrimitive = string | number | boolean | null

export type DockerPayloadValue =
  | DockerPayloadPrimitive
  | DockerPayloadMap
  | DockerPayloadValue[]
  | undefined

export interface DockerPayloadMap {
  [key: string]: DockerPayloadValue
}

export type DockerLabels = DockerPayloadMap

export interface DockerHostConfig extends DockerPayloadMap {
  virtualizationTaskId?: string
  virtualizationTaskStatus?: string
  virtualizationProvider?: string
}

export interface DockerQuickCreateHostConfig extends DockerHostConfig {
  providerParams?: DockerPayloadMap
  providerExtra?: DockerPayloadMap
  providerExtraJSON?: DockerPayloadMap
}

export interface DockerProjectConfig extends DockerPayloadMap {
  sourceKind?: string
  serviceName?: string
  image?: string
  architecture?: string
  platform?: string
  ports?: DockerContainerPortInput[]
  volumes?: DockerContainerVolumeInput[]
  environmentVariables?: DockerContainerEnvironmentVariableInput[]
  resources?: DockerContainerResourceInput
  containerPort?: number
  hostIp?: string
  hostPort?: number
  protocol?: string
  exposureScope?: string
  domainName?: string
  domainScheme?: string
  domainTlsEnabled?: boolean
  command?: string
  entrypoint?: string
  imagePullPolicy?: string
  restartPolicy?: string
}

export type DockerServiceConfig = DockerProjectConfig
export type DockerContainerStartConfig = DockerProjectConfig
export type DockerPortMappingConfig = DockerPayloadMap
export type DockerTemplateVariables = DockerPayloadMap
export type DockerOperationPayload = DockerPayloadMap
export type DockerOperationResult = DockerPayloadMap

export interface DockerPage<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

export interface DockerHost {
  id: string
  name: string
  status?: string
  endpoint?: string
  agentId?: string
  agentVersion?: string
  dockerVersion?: string
  composeVersion?: string
  architecture?: string
  environment?: string
  owner?: string
  team?: string
  virtualizationConnectionId?: string
  vmId?: string
  vmName?: string
  ipAddress?: string
  cpuCoreCount?: number
  memoryBytes?: number
  diskBytes?: number
  availablePortStart?: number
  availablePortEnd?: number
  labels?: DockerLabels
  config?: DockerHostConfig
  lastHeartbeatAt?: string
  createdAt?: string
  updatedAt?: string
}

export interface DockerHostInput {
  name: string
  status?: string
  endpoint?: string
  agentId?: string
  dockerVersion?: string
  composeVersion?: string
  architecture?: string
  environment?: string
  owner?: string
  team?: string
  virtualizationConnectionId?: string
  vmId?: string
  vmName?: string
  ipAddress?: string
  cpuCoreCount?: number
  memoryBytes?: number
  diskBytes?: number
  availablePortStart?: number
  availablePortEnd?: number
  labels?: DockerLabels
  config?: DockerHostConfig
}

export interface DockerHostAgentInstallation {
  hostId: string
  operationId: string
  scriptUrl: string
  command: string
  expiresAt: string
}

export interface DockerQuickCreateHostInput {
  requireCapacity?: boolean
  guestAccess?: import('@opensoha/contracts/gen/ts/sohaapi').VirtualMachineGuestAccessInput
  name: string
  architecture?: string
  environment?: string
  owner?: string
  team?: string
  virtualizationConnectionId?: string
  vmTemplateId?: string
  flavorId?: string
  imageId?: string
  cloudInit?: string
  cpuCoreCount?: number
  memoryBytes?: number
  diskBytes?: number
  network?: string
  availablePortStart?: number
  availablePortEnd?: number
  ttlSeconds?: number
  labels?: DockerLabels
  config?: DockerQuickCreateHostConfig
}

export interface DockerProject {
  id: string
  hostId: string
  name: string
  slug?: string
  description?: string
  environment?: string
  owner?: string
  team?: string
  sourceKind?: string
  sourceRef?: string
  composeContent?: string
  envContent?: string
  status?: string
  desiredState?: string
  templateId?: string
  ttlSeconds?: number
  expiresAt?: string
  lastDeployedAt?: string
  labels?: DockerLabels
  config?: DockerProjectConfig
  createdAt?: string
  updatedAt?: string
}

export interface DockerProjectInput {
  hostId: string
  name: string
  slug?: string
  description?: string
  environment?: string
  owner?: string
  team?: string
  sourceKind?: string
  sourceRef?: string
  composeContent?: string
  envContent?: string
  status?: string
  desiredState?: string
  templateId?: string
  ttlSeconds?: number
  labels?: DockerLabels
  config?: DockerProjectConfig
}

export interface DockerProjectRuntimeLogs {
  projectId: string
  serviceName?: string
  tailLines: number
  content: string
  source?: string
}

export interface DockerProjectVolume {
  name?: string
  type?: string
  source?: string
  target: string
  readOnly?: boolean
  subPath?: string
  browseSupported?: boolean
}

export interface DockerProjectVolumeFileEntry {
  name: string
  path: string
  kind: 'directory' | 'file' | 'symlink' | string
  sizeBytes?: number
  modifiedAt?: string
}

export interface DockerProjectVolumeFileList {
  projectId: string
  serviceName?: string
  target: string
  path: string
  items: DockerProjectVolumeFileEntry[]
}

export interface DockerProjectVolumeFileContent {
  dlp?: DockerVolumeDLPDecision
  projectId: string
  serviceName?: string
  target: string
  path: string
  content: string
  sizeBytes?: number
  truncated?: boolean
}

export interface DockerService {
  id: string
  projectId: string
  hostId: string
  name: string
  image?: string
  status?: string
  containerId?: string
  restartCount?: number
  cpuPercent?: number
  memoryBytes?: number
  networkRxBytes?: number
  networkTxBytes?: number
  config?: DockerServiceConfig
  lastSeenAt?: string
  createdAt?: string
  updatedAt?: string
}

export interface DockerContainerPortInput extends DockerPayloadMap {
  name?: string
  hostIp?: string
  hostPort: number
  containerPort: number
  protocol?: string
  exposureScope?: string
  domainName?: string
  domainScheme?: string
  domainTlsEnabled?: boolean
}

export interface DockerContainerVolumeInput extends DockerPayloadMap {
  name?: string
  type?: string
  source: string
  target: string
  readOnly?: boolean
  subPath?: string
}

export interface DockerContainerEnvironmentVariableInput extends DockerPayloadMap {
  name: string
  value?: string
}

export interface DockerContainerResourceInput extends DockerPayloadMap {
  cpus?: number
  memoryBytes?: number
  memoryReservationBytes?: number
}

export interface DockerGitBuildInput {
  repositoryUrl: string
  ref?: string
  dockerfilePath?: string
  contextDir?: string
  pull?: boolean
  noCache?: boolean
}

export interface DockerContainerStartInput {
  hostId: string
  name: string
  image: string
  sourceKind?: 'image' | 'git_dockerfile'
  gitBuild?: DockerGitBuildInput
  architecture?: string
  imagePullPolicy?: string
  containerPort?: number
  hostIp?: string
  hostPort?: number
  protocol?: string
  exposureScope?: string
  domainName?: string
  domainScheme?: string
  domainTlsEnabled?: boolean
  command?: string
  entrypoint?: string
  envContent?: string
  environmentVariables?: DockerContainerEnvironmentVariableInput[]
  restartPolicy?: string
  network?: string
  ports?: DockerContainerPortInput[]
  volumes?: DockerContainerVolumeInput[]
  resources?: DockerContainerResourceInput
  environment?: string
  owner?: string
  team?: string
  ttlSeconds?: number
  labels?: DockerLabels
  config?: DockerContainerStartConfig
}

export interface DockerPortMapping {
  id: string
  hostId: string
  projectId?: string
  serviceId?: string
  name: string
  hostIp?: string
  hostPort: number
  containerPort: number
  protocol?: string
  exposureScope?: string
  status?: string
  domainName?: string
  domainScheme?: string
  domainTlsEnabled?: boolean
  accessUrl?: string
  owner?: string
  expiresAt?: string
  config?: DockerPortMappingConfig
  createdAt?: string
  updatedAt?: string
}

export interface DockerPortMappingInput {
  hostId: string
  projectId?: string
  serviceId?: string
  name: string
  hostIp?: string
  hostPort: number
  containerPort: number
  protocol?: string
  exposureScope?: string
  status?: string
  domainName?: string
  domainScheme?: string
  domainTlsEnabled?: boolean
  accessUrl?: string
  owner?: string
  expiresAt?: string
  config?: DockerPortMappingConfig
}

export interface DockerTemplate {
  id: string
  name: string
  description?: string
  templateKind?: string
  composeContent?: string
  envContent?: string
  variables?: DockerTemplateVariables
  enabled?: boolean
  createdAt?: string
  updatedAt?: string
}

export interface DockerTemplateInput {
  name: string
  description?: string
  templateKind?: string
  composeContent?: string
  envContent?: string
  variables?: DockerTemplateVariables
  enabled?: boolean
}

export interface DockerOperation {
  id: string
  operationState?: Pick<OperationState, 'cancelable' | 'retryable'>
  hostId?: string
  projectId?: string
  serviceId?: string
  operationKind?: string
  status?: string
  requestedBy?: string
  claimedByWorkerId?: string
  attemptCount?: number
  maxRetries?: number
  timeoutSeconds?: number
  payload?: DockerOperationPayload
  result?: DockerOperationResult
  startedAt?: string
  lastHeartbeatAt?: string
  finishedAt?: string
  createdAt?: string
  updatedAt?: string
}

export interface DockerOperationLog {
  id: string
  operationId: string
  logLevel?: string
  message: string
  payload?: DockerOperationPayload
  createdAt?: string
}

export interface DockerListParams {
  search?: string
  status?: string
  architecture?: string
  sourceKind?: string
  hostId?: string
  projectId?: string
  serviceId?: string
  environment?: string
  page?: number
  pageSize?: number
}
