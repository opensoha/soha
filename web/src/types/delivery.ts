import type { Ingress, Pod, Service, WorkloadCondition, WorkloadContainer } from './platform'

export interface WorkflowNodeRun {
  nodeId: string
  name: string
  type: string
  status: string
  summary?: string
  startedAt?: string
  finishedAt?: string
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

export interface BlueprintFileTemplate {
  path: string
  kind: string
  content: string
  required: boolean
  purpose?: string
}

export interface BlueprintApplicationDraft {
  id?: string
  name: string
  key: string
  group: string
  businessLineId?: string
  language: string
  description?: string
  ownerTeam?: string
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
}

export interface BlueprintEnvironmentBindingTemplate {
  environmentId?: string
  environmentKey?: string
  businessLineId?: string
  strategyProfileId?: string
  promotionPolicyId?: string
  approvalPolicyId?: string
  artifactPolicyId?: string
  workflowTemplateId?: string
  buildPolicy?: BuildPolicy
  releasePolicy?: ReleasePolicy
  resourceSelector?: {
    matchLabels?: Record<string, string>
  }
  targets?: ReleaseTarget[]
}

export interface DeliveryBlueprint {
  id: string
  key: string
  name: string
  description?: string
  applicationDraft: BlueprintApplicationDraft
  buildSources?: BuildSource[]
  environmentBindings?: BlueprintEnvironmentBindingTemplate[]
  files?: BlueprintFileTemplate[]
  executionHints?: Record<string, unknown>
  postCreateActions?: string[]
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export interface RenderedDeliverySpec {
  applicationDraft: BlueprintApplicationDraft
  buildSources?: BuildSource[]
  environmentBindings?: BlueprintEnvironmentBindingTemplate[]
  files?: BlueprintFileTemplate[]
  executionHints?: Record<string, unknown>
  postCreateActions?: string[]
}

export interface BlueprintBootstrapResult {
  application: DeliveryApplication
  environmentBindings?: ApplicationEnvironment[]
  spec: RenderedDeliverySpec
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

export type ApplicationServiceKind = 'kubernetes_workload' | 'helm_release' | 'external_service' | 'job'

export interface ApplicationServiceContainer {
  id: string
  serviceId?: string
  name: string
  imageRepository?: string
  defaultTagTemplate?: string
  dockerfilePath?: string
  buildContextDir?: string
  runtimePorts?: number[]
  envSchema?: Record<string, unknown>
  resourceProfile?: Record<string, unknown>
  healthCheck?: Record<string, unknown>
  metadata?: Record<string, unknown>
  createdAt?: string
  updatedAt?: string
}

export interface ApplicationServiceComponent {
  id: string
  applicationId: string
  key: string
  name: string
  description?: string
  serviceKind: ApplicationServiceKind
  ownerTeam?: string
  repositoryProvider?: string
  repositoryProjectId?: string
  repositoryPath?: string
  defaultBranch?: string
  buildSourceId?: string
  enabled: boolean
  metadata?: Record<string, unknown>
  containers?: ApplicationServiceContainer[]
  createdAt: string
  updatedAt: string
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
  applicationGroup?: string
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
  workflowTemplate?: WorkflowTemplate
  targetCount: number
  targets?: ReleaseTarget[]
  buildSourceId?: string
  buildSource?: BuildSource
  buildPolicy?: BuildPolicy
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

export type ApplicationDeliveryActionKind = 'build' | 'deploy' | 'build_deploy' | 'workflow' | 'verify'

export interface ApplicationDeliveryActionRequest {
  action: ApplicationDeliveryActionKind
  applicationEnvironmentId: string
  targetId?: string
  buildSourceId?: string
  refType?: string
  refName?: string
  imageTag?: string
  releaseName?: string
  containerName?: string
  variables?: Record<string, unknown>
  buildArgs?: Record<string, unknown>
}

export interface ApplicationDeliveryActionResponse {
  action: ApplicationDeliveryActionKind
  applicationId: string
  applicationEnvironmentId: string
  target?: ReleaseTarget
  build?: BuildRecord
  workflow?: WorkflowRun
  release?: ReleaseRecord
  relatedIds?: {
    releaseBundleId?: string
    executionTaskId?: string
  }
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
  buildPolicy?: BuildPolicy
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
