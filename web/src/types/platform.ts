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
  createdAt?: string
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
  createdAt?: string
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

export interface HelmChartRepository {
  id: string
  name: string
  displayName?: string
  url: string
  indexUrl?: string
  organizationName?: string
  organizationDisplayName?: string
  official?: boolean
  verifiedPublisher?: boolean
}

export interface HelmChartMaintainer {
  name?: string
  email?: string
  url?: string
}

export interface HelmChart {
  packageId?: string
  name: string
  normalizedName?: string
  repositoryName?: string
  repositoryUrl?: string
  repositoryDisplay?: string
  latestVersion?: string
  appVersion?: string
  description?: string
  type?: string
  category?: string
  deprecated?: boolean
  home?: string
  homeUrl?: string
  icon?: string
  logoImageId?: string
  logoImageUrl?: string
  artifactHubUrl?: string
  kubeVersion?: string
  createdAt?: string
  updatedAt?: string
  digest?: string
  urls?: string[]
  sources?: string[]
  keywords?: string[]
  maintainers?: HelmChartMaintainer[]
  versions?: string[]
  versionCount: number
  stars?: number
  official?: boolean
  cncf?: boolean
  signed?: boolean
  hasValuesSchema?: boolean
  verifiedPublisher?: boolean
  securityCritical?: number
  securityHigh?: number
  securityMedium?: number
  securityLow?: number
  securityUnknown?: number
  allowedActions?: string[]
}

export interface HelmChartLink {
  name?: string
  url?: string
}

export interface HelmChartVersion {
  version: string
  appVersion?: string
  createdAt?: string
  prerelease?: boolean
  containsSecurityUpdates?: boolean
}

export interface HelmChartDetail extends HelmChart {
  readme?: string
  contentUrl?: string
  links?: HelmChartLink[]
  availableVersions?: HelmChartVersion[]
}

export interface HelmChartValuesTemplate {
  packageId: string
  name?: string
  version: string
  content: string
}

export interface HelmChartInstallResource {
  apiVersion?: string
  kind: string
  namespace?: string
  name: string
}

export interface HelmChartInstallResult {
  name: string
  namespace: string
  revision?: string
  status?: string
  chart?: string
  chartName?: string
  chartVersion?: string
  appVersion?: string
  description?: string
  notes?: string
  resources?: HelmChartInstallResource[]
}

export interface HelmChartCatalog {
  repository: HelmChartRepository
  source?: string
  query?: string
  limit?: number
  offset?: number
  generatedAt?: string
  refreshedAt: string
  totalCount?: number
  loadedCount?: number
  chartCount: number
  versionCount: number
  charts: HelmChart[]
}
