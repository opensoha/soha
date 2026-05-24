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
  environment?: string
  owner?: string
  team?: string
  virtualizationConnectionId?: string
  vmTemplateId?: string
  flavorId?: string
  imageId?: string
  cpuCoreCount?: number
  memoryBytes?: number
  diskBytes?: number
  network?: string
  availablePortStart?: number
  availablePortEnd?: number
  ttlSeconds?: number
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

export interface DockerContainerStartInput {
  hostId: string
  name: string
  image: string
  imagePullPolicy?: string
  containerPort: number
  hostIp?: string
  hostPort: number
  protocol?: string
  exposureScope?: string
  domainName?: string
  domainScheme?: string
  domainTlsEnabled?: boolean
  command?: string
  entrypoint?: string
  envContent?: string
  restartPolicy?: string
  network?: string
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
  hostId?: string
  projectId?: string
  serviceId?: string
  environment?: string
  page?: number
  pageSize?: number
}
