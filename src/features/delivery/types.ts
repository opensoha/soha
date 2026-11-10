import type {
  ApplicationEnvironment,
  ApplicationRuntimeDetail,
  ApplicationServiceComponent,
  ApplicationWorkloadRuntimeDetail,
  BuildRecord,
  BuildTemplate,
  DeliveryApplication,
  DeliveryRepository,
  DeliveryApplicationEnvironmentDetail,
  DeliveryApplicationDetail,
  DeliveryBlueprint,
  DeliveryPlan,
  DeliveryPlanConfirmResult,
  DeliveryPlanRequest,
  DeliveryTargetCandidate,
  ExecutionArtifact,
  ExecutionTask,
  ReleaseBoardEntry,
  ReleaseBundle,
  ReleaseRecord,
  RenderedDeliverySpec,
  RuntimeObjectDetail,
  TemplateUsageSummary,
  WorkflowRun,
  WorkflowTemplate,
  GitProject,
  GitReference,
  GitCommit,
} from './domain-types'
import type { Cluster, DeploymentDetail, Pod, ResourceMetrics } from '@/types/platform'
import type { GatewayManifest, GatewayTool } from '@/features/copilot'
import type {
  RegistryConnection,
  RegistryConnectionInput,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type * from './domain-types'

export type DeliveryRecordInput = Record<string, unknown>

export interface DeliveryUpdateInput<TPayload> {
  id: string
  payload: TPayload
}

export interface ApplicationServiceUpdateInput {
  applicationId: string
  payload: DeliveryRecordInput
  serviceId: string
}

export interface ApplicationServiceCreateInput {
  applicationId: string
  payload: DeliveryRecordInput
}

export interface ApplicationServiceDeleteInput {
  applicationId: string
  serviceId: string
}

export interface ApplicationWorkflowSaveInput {
  applicationId: string
  id: string
  payload: ApplicationWorkflowInput
}

export interface ApplicationWorkflowInput {
  expectedRevision?: number
  name: string
  description?: string
  definition: object
  enabled: boolean
}

export interface BuildTemplateInput {
  migrationPolicy?: import('@opensoha/contracts/gen/ts/sohaapi').TemplateMigrationPolicy
  copiedFrom?: { id: string; revision: number; version?: number }
  expectedRevision?: number
  publish?: boolean
  key?: string
  name?: string
  description?: string
  builderKind?: string
  dockerfileTemplate?: string
  buildCommands: string[]
  variableSchema: Record<string, unknown>
  defaultVariables: Record<string, unknown>
  enabled?: boolean
}

export interface WorkflowDecisionInput {
  comment: string
  id: string
}

export type RegistryRecord = RegistryConnection
export type RegistryInput = RegistryConnectionInput

export interface DeliveryListParams {
  buildSourceId?: string
  applicationEnvironmentId?: string
  applicationId?: string
  limit?: number
}

export interface RepositoryListParams {
  applicationId?: string
  search?: string
  limit?: number
}

export interface GitReferenceParams {
  projectId: string
  search?: string
  limit?: number
}

export interface GitCommitParams extends GitReferenceParams {
  page?: number
}

export interface DeliveryTargetCandidateParams {
  workloadKind?: import('./domain-types').KubernetesImportKind
  crdName?: string
  clusterId: string
  namespace: string
  search?: string
  limit?: number
}

export interface DeliveryExecutionLog {
  id: string
  logLevel: string
  message: string
  createdAt: string
}

export interface ExecutionCallbackInput {
  callbackToken?: string
  status: string
  payload?: Record<string, unknown>
}

export interface ExecutionTaskActionInput {
  id: string
  reason: string
}

export type DeliveryRuntimeKind =
  | 'build'
  | 'workflow'
  | 'release'
  | 'release_bundle'
  | 'execution_task'

export type DeliveryRuntimeRecord =
  | BuildRecord
  | WorkflowRun
  | ReleaseRecord
  | ReleaseBundle
  | ExecutionTask

export type DeliveryRuntimeDetail = RuntimeObjectDetail<DeliveryRuntimeRecord>
export type RuntimeKind = DeliveryRuntimeKind
export type RuntimeRecord = DeliveryRuntimeRecord

export interface DeliveryWorkloadRef {
  applicationEnvironmentId: string
  applicationId: string
  workloadName: string
}

export interface DeliveryWorkloadMetricsRef {
  clusterId: string
  namespace: string
  workloadName: string
  rangeMinutes?: number
}

export interface DeliveryDeploymentRef {
  clusterId: string
  namespace: string
  workloadName: string
}

export interface DeliveryDeploymentRollbackInput extends DeliveryDeploymentRef {
  revision: string
}

export type DeliveryWorkloadRestartInput = DeliveryDeploymentRef

export interface RolloutHistoryRecord {
  name: string
  namespace: string
  revision: string
  images?: string[]
  replicas: number
  readyReplicas: number
  createdAt?: string
}

export type DeliveryGatewayTool = GatewayTool
export type DeliveryGatewayManifest = GatewayManifest

export interface DeliveryGatewayReadinessParams {
  skillId: string
  source?: string
}

export type DeliveryApplicationList = DeliveryApplication[]
export type DeliveryEnvironmentList = ApplicationEnvironment[]
export type DeliveryBuildTemplateList = BuildTemplate[]
export type DeliveryWorkflowTemplateList = WorkflowTemplate[]
export type DeliveryBlueprintList = DeliveryBlueprint[]
export type DeliveryWorkflowList = WorkflowRun[]
export type DeliveryReleaseList = ReleaseRecord[]
export type DeliveryReleaseBoard = ReleaseBoardEntry[]
export type DeliveryReleaseBundleList = ReleaseBundle[]
export type DeliveryExecutionTaskList = ExecutionTask[]
export type DeliveryClusterList = Cluster[]

export type {
  ApplicationEnvironment,
  ApplicationRuntimeDetail,
  ApplicationServiceComponent,
  ApplicationWorkloadRuntimeDetail,
  BuildRecord,
  BuildTemplate,
  Cluster,
  DeliveryApplication,
  DeliveryRepository,
  DeliveryApplicationEnvironmentDetail,
  DeliveryApplicationDetail,
  DeliveryBlueprint,
  DeploymentDetail,
  DeliveryPlan,
  DeliveryPlanConfirmResult,
  DeliveryPlanRequest,
  DeliveryTargetCandidate,
  ExecutionArtifact,
  ExecutionTask,
  Pod,
  ReleaseBoardEntry,
  ReleaseBundle,
  ReleaseRecord,
  RenderedDeliverySpec,
  ResourceMetrics,
  TemplateUsageSummary,
  WorkflowRun,
  WorkflowTemplate,
  GitProject,
  GitReference,
  GitCommit,
}

export type {
  ServiceDeploymentTemplate,
  ServiceDeploymentTemplateInput,
  ServiceDeploymentTemplateBinding,
  DeploymentTemplatePreview,
  DeploymentTemplatePreviewInput,
  TemplateParameterSchema,
  TemplateParameterValues,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type {
  DeliveryBatch,
  DeliveryBatchInput,
  DeliveryBatchActionInput,
  DeliveryTargetInput,
  DeliveryTargetSnapshot,
  DeliveryWorkflow,
  DeliveryWorkflowInput,
  DeliveryWorkflowDefinition,
  DeliveryBatchTemplateDefinition,
} from '@opensoha/contracts/gen/ts/sohaapi'

export interface DeliveryBatchListParams extends DeliveryListParams {
  workflowId?: string
  serviceId?: string
}
