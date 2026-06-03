export interface DockerPage<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

export interface DockerOverview {
  stats?: {
    hostCount?: number
    onlineHostCount?: number
    projectCount?: number
    runningProjectCount?: number
    serviceCount?: number
    runningServiceCount?: number
    portMappingCount?: number
    pendingTaskCount?: number
    failedTaskCount?: number
  }
  hostSummary?: Record<string, number>
  projectSummary?: Record<string, number>
  serviceSummary?: Record<string, number>
  portSummary?: Record<string, number>
  recentOperations?: DockerOperation[]
  expiringProjects?: DockerProject[]
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
  labels?: Record<string, unknown>
  config?: Record<string, unknown>
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
  labels?: Record<string, unknown>
  config?: Record<string, unknown>
}

export interface DockerQuickCreateHostInput {
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
  config?: Record<string, unknown>
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
  labels?: Record<string, unknown>
  config?: Record<string, unknown>
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
  config?: Record<string, unknown>
  lastSeenAt?: string
  createdAt?: string
  updatedAt?: string
}

export interface DockerContainerPortInput {
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

export interface DockerContainerVolumeInput {
  name?: string
  type?: string
  source: string
  target: string
  readOnly?: boolean
  subPath?: string
}

export interface DockerContainerEnvironmentVariableInput {
  name: string
  value?: string
}

export interface DockerContainerResourceInput {
  cpus?: number
  memoryBytes?: number
  memoryReservationBytes?: number
}

export interface DockerContainerStartInput {
  hostId: string
  name: string
  image: string
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
  labels?: Record<string, unknown>
  config?: Record<string, unknown>
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
  config?: Record<string, unknown>
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
  config?: Record<string, unknown>
}

export interface DockerTemplate {
  id: string
  name: string
  description?: string
  templateKind?: string
  composeContent?: string
  envContent?: string
  variables?: Record<string, unknown>
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
  variables?: Record<string, unknown>
  enabled?: boolean
}

export interface DockerOperation {
  id: string
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
  payload?: Record<string, unknown>
  result?: Record<string, unknown>
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
  payload?: Record<string, unknown>
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
