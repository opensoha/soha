export type WorkspaceType = 'application' | 'resource' | 'system'
export type BusinessWorkspaceType = Exclude<WorkspaceType, 'system'>

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
  scopeMode?: 'hidden' | 'passive' | 'cluster' | 'namespace'
  workspace?: WorkspaceType
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
  labelZh?: string
  labelEn?: string
  iconKey?: string
  section?: string
  sortOrder?: number
  enabled?: boolean
}

export interface RuntimeMenuNode {
  id: string
  parentId?: string
  path: string
  labelZh: string
  labelEn: string
  iconKey: string
  section: string
  sortOrder: number
  enabled: boolean
  workspace?: WorkspaceType
  route?: RouteMeta
  children?: RuntimeMenuNode[]
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

export interface ServiceAccountDetail {
  name: string
  namespace: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  secrets?: string[]
  imagePullSecrets?: string[]
  automountServiceAccountToken: boolean
  createdAt?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface RoleDetail {
  name: string
  namespace: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  rules: number
  ruleSummaries?: string[]
  createdAt?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface RoleBindingDetail {
  name: string
  namespace: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  roleRef: string
  subjects?: string[]
  createdAt?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface ClusterRoleDetail {
  name: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  rules: number
  aggregationRules: number
  ruleSummaries?: string[]
  createdAt?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface ClusterRoleBindingDetail {
  name: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  roleRef: string
  subjects?: string[]
  createdAt?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface PersistentVolumeClaim {
  name: string
  namespace: string
  status: string
  volumeName?: string
  storageClass?: string
  accessModes?: string[]
  requested?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface PersistentVolumeClaimDetail {
  name: string
  namespace: string
  status: string
  volumeName?: string
  storageClass?: string
  accessModes?: string[]
  requested?: string
  volumeMode?: string
  capacity?: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  createdAt?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface PersistentVolume {
  name: string
  status: string
  storageClass?: string
  claimRef?: string
  accessModes?: string[]
  capacity?: string
  reclaimPolicy?: string
  volumeMode?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface PersistentVolumeDetail {
  name: string
  status: string
  storageClass?: string
  claimRef?: string
  accessModes?: string[]
  capacity?: string
  reclaimPolicy?: string
  volumeMode?: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  createdAt?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface StorageClass {
  name: string
  provisioner: string
  reclaimPolicy?: string
  volumeBindingMode?: string
  allowVolumeExpansion: boolean
  parameters?: Record<string, string>
  ageSeconds: number
  allowedActions?: string[]
}

export interface StorageClassDetail {
  name: string
  provisioner: string
  reclaimPolicy?: string
  volumeBindingMode?: string
  allowVolumeExpansion: boolean
  parameters?: Record<string, string>
  labels?: Record<string, string>
  annotations?: Record<string, string>
  createdAt?: string
  ageSeconds: number
  allowedActions?: string[]
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
  containerId?: string
  startedAt?: string
  reason?: string
  message?: string
}

export interface PodVolumeMount {
  name: string
  mountPath: string
  subPath?: string
  readOnly: boolean
  volumeType?: string
  sourceName?: string
  description?: string
}

export interface PodVolume {
  name: string
  type: string
  sourceName?: string
  readOnly: boolean
  details?: string[]
  volumeMounts?: PodVolumeMount[]
  referencedConfigMaps?: string[]
}

export interface PodRelatedResource {
  kind: string
  name: string
  namespace?: string
  relations?: string[]
  details?: string[]
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
  volumes?: PodVolume[]
  relatedResources?: PodRelatedResource[]
  allowedActions?: string[]
}

export interface Pod {
  name: string
  namespace: string
  phase: string
  nodeName?: string
  podIp?: string
  createdAt?: string
  cpu?: string
  memory?: string
  requests?: ResourceQuantity
  limits?: ResourceQuantity
  labels?: Record<string, string>
  persistentVolumeClaims?: string[]
  readyContainers: string
  restarts: number
  ageSeconds: number
  allowedActions?: string[]
}

export interface Service {
  name: string
  namespace: string
  type: string
  clusterIp?: string
  ports?: string[]
  selector?: Record<string, string>
  ageSeconds: number
  allowedActions?: string[]
}

export interface Ingress {
  name: string
  namespace: string
  className?: string
  hosts?: string[]
  address?: string
  backendServices?: string[]
  ageSeconds: number
  allowedActions?: string[]
}

export interface DeploymentDetail {
  name: string
  namespace: string
  desiredReplicas: number
  readyReplicas: number
  updatedReplicas: number
  availableReplicas: number
  observedGeneration: number
  strategy: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  selector?: Record<string, string>
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

export interface HelmRelease {
  name: string
  namespace: string
  revision?: string
  status?: string
  chart?: string
  appVersion?: string
  storageDriver?: string
  ageSeconds: number
  allowedActions?: string[]
}

export interface HelmReleaseDetail {
  name: string
  namespace: string
  revision?: string
  status?: string
  chart?: string
  chartName?: string
  chartVersion?: string
  appVersion?: string
  storageDriver?: string
  description?: string
  createdAt?: string
  updatedAt?: string
  firstDeployedAt?: string
  lastDeployedAt?: string
  notes?: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  ageSeconds: number
  allowedActions?: string[]
  valuesEditable: boolean
  valuesDiffEnabled: boolean
}

export interface HelmReleaseHistory {
  name: string
  namespace: string
  revision: string
  status?: string
  chart?: string
  chartVersion?: string
  appVersion?: string
  description?: string
  updatedAt?: string
  createdAt?: string
  manifestDigest?: string
  valuesDigest?: string
  allowedActions?: string[]
}

export interface HelmValues {
  name: string
  namespace: string
  revision?: string
  content: string
  original?: string
  editable: boolean
  diffEnabled: boolean
  allowedActions?: string[]
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
  targetKind?: string
  executorKind?: string
  groupKey?: string
  waveKey?: string
  regionKey?: string
  configRef?: string
  workloadKind: string
  workloadName: string
  containerName?: string
  metadata?: Record<string, unknown>
  enabled: boolean
}

export interface BuildSource {
  id: string
  name: string
  type: 'repo_dockerfile' | 'platform_build_template' | 'external_pipeline'
  enabled: boolean
  isDefault: boolean
  buildImage?: string
  defaultTag?: string
  config?: Record<string, unknown>
}

export interface BuildPolicy {
  sourceId?: string
  refType?: string
  refValue?: string
  imageTagMode?: string
  imageTagTemplate?: string
  variables?: Record<string, unknown>
  buildArgs?: Record<string, unknown>
}

export interface ReleasePolicy {
  actionKind?: 'deploy' | 'release'
  requiresApproval?: boolean
  approverRoles?: string[]
  autoRollback?: boolean
  rolloutTimeoutSeconds?: number
  verificationMode?: 'none' | 'workflow'
}

export interface ApplicationEnvironment {
  id: string
  applicationId: string
  businessLineId?: string
  environmentId: string
  environmentKey?: string
  strategyProfileId?: string
  promotionPolicyId?: string
  approvalPolicyId?: string
  artifactPolicyId?: string
  workflowTemplateId?: string
  workflowTemplate?: WorkflowTemplate
  buildPolicy?: BuildPolicy
  releasePolicy?: ReleasePolicy
  resourceSelector?: {
    matchLabels?: Record<string, string>
  }
  targets?: ReleaseTarget[]
  createdAt: string
  updatedAt: string
}

export interface BuildTemplate {
  id: string
  key: string
  name: string
  description?: string
  builderKind?: string
  dockerfileTemplate?: string
  buildCommands?: string[]
  variableSchema?: Record<string, unknown>
  defaultVariables?: Record<string, unknown>
  enabled: boolean
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

export interface DeliveryApplication {
  id: string
  name: string
  key: string
  group: string
  businessLineId?: string
  language: string
  repositoryProvider?: string
  repositoryProjectId?: string
  repositoryPath?: string
  defaultBranch?: string
  defaultTag?: string
  buildImage?: string
  buildContextDir?: string
  dockerfilePath?: string
  enabled: boolean
  metadata?: Record<string, unknown>
  buildSources?: BuildSource[]
  environmentCount?: number
  createdAt: string
  updatedAt: string
}

export interface BuildRecord {
  id: string
  applicationId: string
  sourceSystem: string
  status: string
  metadata?: Record<string, unknown>
  startedAt?: string
  finishedAt?: string
  createdAt: string
}

export interface ReleaseRecord {
  id: string
  applicationId: string
  clusterId: string
  namespace: string
  deploymentName: string
  status: string
  metadata?: Record<string, unknown>
  deployedAt?: string
  createdAt: string
}

export interface WorkflowRun {
  id: string
  applicationId: string
  workflowName: string
  clusterId?: string
  namespace?: string
  deploymentName?: string
  status: string
  steps: Array<{ name: string; status: string; summary?: string }>
  nodeRuns?: WorkflowNodeRun[]
  metadata?: Record<string, unknown>
  createdAt: string
  updatedAt: string
}

export interface ReleaseBundle {
  id: string
  applicationId: string
  applicationEnvironmentId?: string
  version: string
  sourceType: string
  status: string
  artifactRef?: string
  artifactDigest?: string
  metadata?: Record<string, unknown>
  createdAt: string
  updatedAt: string
}

export interface ExecutionArtifact {
  kind: string
  name?: string
  ref?: string
  digest?: string
  path?: string
  status?: string
  sizeBytes?: number
  metadata?: Record<string, unknown>
  modifiedAt?: string
}

export interface ExecutionTask {
  id: string
  releaseBundleId?: string
  applicationId: string
  applicationEnvironmentId?: string
  taskKind: string
  providerKind: string
  targetKind: string
  status: string
  queueKey?: string
  lockKey?: string
  maxRetries: number
  attemptCount: number
  timeoutSeconds: number
  callbackToken?: string
  payload?: Record<string, unknown>
  result?: Record<string, unknown>
  artifacts?: ExecutionArtifact[]
  startedAt?: string
  lastHeartbeatAt?: string
  finishedAt?: string
  createdAt: string
  updatedAt: string
}

export interface ApprovalPolicy {
  id: string
  key: string
  name: string
  description?: string
  mode?: string
  requiredApprovals: number
  slaMinutes: number
  approverRoles?: string[]
  changeWindow?: Record<string, unknown>
  enabled: boolean
  metadata?: Record<string, unknown>
  createdAt: string
  updatedAt: string
}

export interface DeliveryApplicationBindingSummary {
  applicationEnvironmentId: string
  environmentId: string
  environmentName?: string
  environmentKey?: string
  actionKind?: string
  requiresApproval: boolean
  workflowTemplateId?: string
  workflowTemplateName?: string
  targetCount: number
  targets?: ReleaseTarget[]
  buildSourceId?: string
  buildSource?: BuildSource
  latestBundle?: ReleaseBundle
  latestExecutionTask?: ExecutionTask
  latestBuild?: BuildRecord
  latestWorkflow?: WorkflowRun
  latestRelease?: ReleaseRecord
}

export interface DeliveryApplicationDetail {
  application: DeliveryApplication
  bindings?: DeliveryApplicationBindingSummary[]
  latestBundle?: ReleaseBundle
  latestExecutionTask?: ExecutionTask
  latestBuild?: BuildRecord
  latestWorkflow?: WorkflowRun
  latestRelease?: ReleaseRecord
}

export interface ApplicationRuntimeWorkload {
  applicationEnvironmentId: string
  clusterId: string
  namespace: string
  workloadKind: string
  workloadName: string
  labels?: Record<string, string>
  selector?: Record<string, string>
  desiredReplicas: number
  readyReplicas: number
  updatedReplicas: number
  availableReplicas: number
  buildSource?: BuildSource
  latestBundle?: ReleaseBundle
  latestExecutionTask?: ExecutionTask
  latestBuild?: BuildRecord
  latestWorkflow?: WorkflowRun
  latestRelease?: ReleaseRecord
}

export interface ApplicationRuntimeEnvironment {
  applicationEnvironmentId: string
  environmentId: string
  environmentName?: string
  environmentKey?: string
  actionKind?: string
  requiresApproval: boolean
  resourceSelector?: {
    matchLabels?: Record<string, string>
  }
  targets?: ReleaseTarget[]
  workloads?: ApplicationRuntimeWorkload[]
}

export interface ApplicationRuntimeDetail {
  application: DeliveryApplication
  environments?: ApplicationRuntimeEnvironment[]
}

export interface ApplicationWorkloadRuntimeDetail {
  application: DeliveryApplication
  binding: ApplicationEnvironment
  environment?: DeliveryEnvironment
  workload: ApplicationRuntimeWorkload
  deployment: {
    name: string
    namespace: string
    desiredReplicas: number
    readyReplicas: number
    updatedReplicas: number
    availableReplicas: number
    observedGeneration: number
    strategy: string
    labels?: Record<string, string>
    annotations?: Record<string, string>
    selector?: Record<string, string>
    containers?: WorkloadContainer[]
    conditions?: WorkloadCondition[]
    allowedActions?: string[]
  }
  pods?: Pod[]
  services?: Service[]
  ingresses?: Ingress[]
}

export interface DeliveryApplicationEnvironmentDetail {
  binding: ApplicationEnvironment
  application: DeliveryApplication
  environment?: DeliveryEnvironment
  actionKind?: string
  requiresApproval: boolean
  buildSource?: BuildSource
  latestBundle?: ReleaseBundle
  latestExecutionTask?: ExecutionTask
  latestBuild?: BuildRecord
  latestWorkflow?: WorkflowRun
  latestRelease?: ReleaseRecord
}

export interface ReleaseBoardEntry {
  applicationEnvironmentId: string
  applicationId: string
  applicationName: string
  businessLineId?: string
  environmentId: string
  environmentName?: string
  environmentKey?: string
  actionKind?: string
  requiresApproval: boolean
  workflowTemplateId?: string
  workflowTemplateName?: string
  buildSourceId?: string
  buildSource?: BuildSource
  latestBundle?: ReleaseBundle
  latestExecutionTask?: ExecutionTask
  targets?: ReleaseTarget[]
  latestBuild?: BuildRecord
  latestWorkflow?: WorkflowRun
  latestRelease?: ReleaseRecord
}

export interface DeliveryTargetCandidate {
  clusterId: string
  namespace: string
  workloadKind: string
  workloadName: string
  containers?: string[]
  labels?: Record<string, string>
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
