export interface RouteMeta {
  id: string
  path: string
  title: string
  description: string
  icon: string
  group: string
  requiresAuth: boolean
  tabbar: boolean
  navVisible: boolean
  parentId?: string
  redirectTo?: string
  menuId?: string
  permissionKey?: string
  permissionStrategy?: 'self' | 'any-child'
}

export interface SidebarNavItem {
  route: RouteMeta
  children?: RouteMeta[]
}

export interface User {
  userId: string
  userName: string
  email: string
  roles: string[]
  teams: string[]
  projects: string[]
  tags: string[] | null
}

export interface AuthTokens {
  accessToken: string
  refreshToken: string
}

export interface ApiResponse<T = unknown> {
  data: T
  message?: string
}

export interface ApiItemsResponse<T = unknown> {
  items: T[]
}

export interface AuthResult {
  user: User
  tokens: {
    accessToken: string
    refreshToken: string
    tokenType: string
    expiresIn: number
    expiresAt: string
  }
}

export interface VisibleMenu {
  id: string
  parentId?: string
  path: string
}

export interface PermissionSnapshot {
  permissionKeys: string[]
  visibleMenuIds: string[]
  visibleMenus: VisibleMenu[]
}

export interface BrandingSettings {
  appTitle: string
  sidebarTitle: string
  loginLogoUrl: string
  expandedLogoUrl: string
  collapsedLogoUrl: string
  faviconUrl: string
}

export interface PaginatedResponse<T = unknown> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

export interface ClusterHealth {
  status: string
  message?: string
  lastChecked?: string
}

export interface Cluster {
  id: string
  name: string
  region: string
  environment: string
  labels: Record<string, string>
  connectionMode: string
  version: string
  capabilities?: string[]
  health: ClusterHealth
}

export interface ClusterDiagnostics {
  transport: string
  syncStrategy: string
  cacheStatus: string
  cacheReady: boolean
  lastChecked?: string
  connectionState: string
  message?: string
}

export interface ClusterConnectionDetail {
  mode: string
  credentialType: string
  sourceType: string
  sourceRef?: string
  context?: string
  endpoint?: string
  hasInlineKubeconfig: boolean
  hasToken: boolean
  usesInformerCache: boolean
}

export interface ClusterMonitoringDetail {
  prometheus: {
    baseUrl?: string
    clusterLabel?: string
    grafanaBaseUrl?: string
    hasBearerToken: boolean
  }
}

export interface ClusterDetail {
  summary: Cluster
  diagnostics: ClusterDiagnostics
  connection: ClusterConnectionDetail
  monitoring: ClusterMonitoringDetail
}

export interface ResourceQuantity {
  cpu?: string
  memory?: string
  ephemeralStorage?: string
  pods?: string
}

export interface ResourcePercentage {
  cpu?: number
  memory?: number
  ephemeralStorage?: number
  pods?: number
}

export interface NodeResourceSummary {
  capacity?: ResourceQuantity
  allocatable?: ResourceQuantity
  requests?: ResourceQuantity
  limits?: ResourceQuantity
  usage?: ResourceQuantity
  requestPercentages?: ResourcePercentage
  limitPercentages?: ResourcePercentage
  usagePercentages?: ResourcePercentage
}

export interface Node {
  name: string
  status: string
  roles: string[]
  version?: string
  internalIp?: string
  podCount: number
  ageSeconds: number
  resources?: NodeResourceSummary
  allowedActions?: string[]
}

export interface NodeTaint {
  key: string
  value?: string
  effect: string
}

export interface NodePod {
  name: string
  namespace: string
  phase: string
  podIp?: string
  readyContainers: string
  restarts: number
  cpu?: string
  memory?: string
  labels?: Record<string, string>
  requests?: ResourceQuantity
  limits?: ResourceQuantity
  ageSeconds: number
}

export interface NodeDetail extends Node {
  labels?: Record<string, string>
  annotations?: Record<string, string>
  taints?: NodeTaint[]
  conditions?: WorkloadCondition[]
  metricsConfigured?: boolean
  metricsMessage?: string
  pods?: NodePod[]
}

export interface Namespace {
  name: string
  status: string
  labels: Record<string, string>
  annotations?: Record<string, string>
}

export interface ResourceYAMLView {
  kind: string
  name: string
  namespace?: string
  content: string
}

export interface PodLogs {
  podName: string
  namespace: string
  container?: string
  content: string
  contentBytes: number
  maxBytes?: number
  tailLines?: number
  previous?: boolean
  truncated: boolean
}

export interface WorkloadCondition {
  type: string
  status: string
  reason?: string
  message?: string
  lastTransitionTime?: string
}

export interface WorkloadContainer {
  name: string
  image: string
  ready: boolean
  restartCount: number
  state?: string
  lastState?: string
}

export interface MetricPoint {
  timestamp: string
  value: number
}

export interface MetricSeries {
  key: string
  label: string
  unit: string
  latest: number
  points?: MetricPoint[]
}

export interface MetricsSnapshot {
  configured: boolean
  source: string
  generatedAt: string
  rangeMinutes: number
  stepSeconds: number
  message?: string
  grafanaBaseUrl?: string
  series?: MetricSeries[]
}

export interface PodMetrics {
  podName: string
  namespace: string
  configured: boolean
  source: string
  generatedAt: string
  rangeMinutes: number
  stepSeconds: number
  message?: string
  grafanaBaseUrl?: string
  series?: MetricSeries[]
}

export interface ResourceMetrics {
  resourceKind: string
  resourceName: string
  namespace?: string
  configured: boolean
  source: string
  generatedAt: string
  rangeMinutes: number
  stepSeconds: number
  message?: string
  grafanaBaseUrl?: string
  series?: MetricSeries[]
}

export interface PodExecResult {
  podName: string
  namespace: string
  container?: string
  command: string
  stdout: string
  stderr: string
  stdoutBytes: number
  stderrBytes: number
  maxBytes?: number
  stdoutTruncated?: boolean
  stderrTruncated?: boolean
  success: boolean
  exitMessage?: string
  executedAt: string
}

export interface PodDetail {
  name: string
  namespace: string
  phase: string
  podIp?: string
  hostIp?: string
  nodeName?: string
  serviceAccountName?: string
  qosClass?: string
  startTime?: string
  requests?: ResourceQuantity
  limits?: ResourceQuantity
  labels?: Record<string, string>
  annotations?: Record<string, string>
  containers?: WorkloadContainer[]
  conditions?: WorkloadCondition[]
  allowedActions?: string[]
}

export interface RolloutHistory {
  name: string
  namespace: string
  revision: string
  images?: string[]
  replicas: number
  readyReplicas: number
  createdAt?: string
}

export interface DeploymentRolloutStatus {
  name: string
  namespace: string
  revision: string
  status: string
  message: string
  desiredReplicas: number
  updatedReplicas: number
  readyReplicas: number
  availableReplicas: number
  observedGeneration: number
  conditions?: WorkloadCondition[]
}

export interface WorkflowNodeRun {
  nodeId: string
  name: string
  type: string
  status: string
  summary?: string
  startedAt?: string
  finishedAt?: string
}

export interface BusinessLine {
  id: string
  key: string
  name: string
  description?: string
  owners?: string[]
  sortOrder: number
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface DeliveryEnvironment {
  id: string
  key: string
  name: string
  tier?: string
  stageLevel: number
  sortOrder: number
  isProduction: boolean
  requiresApproval: boolean
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface ReleaseTarget {
  id: string
  clusterId: string
  namespace: string
  workloadKind: string
  workloadName: string
  containerName?: string
  enabled: boolean
}

export interface ApplicationEnvironment {
  id: string
  applicationId: string
  businessLineId?: string
  environmentId: string
  environmentKey?: string
  workflowTemplateId?: string
  workflowTemplate?: WorkflowTemplate
  buildPolicy?: Record<string, unknown>
  releasePolicy?: Record<string, unknown>
  targets?: ReleaseTarget[]
  createdAt: string
  updatedAt: string
}

export interface WorkflowTemplate {
  id: string
  key: string
  name: string
  description?: string
  category?: string
  definition?: Record<string, unknown>
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface ScopeGrant {
  id: string
  subjectType: string
  subjectId: string
  businessLineId: string
  environmentIds: string[]
  applicationIds: string[]
  role: string
  effect: string
  enabled: boolean
  createdAt: string
  updatedAt: string
}
