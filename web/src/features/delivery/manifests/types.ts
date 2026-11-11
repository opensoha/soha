import type { components } from '@opensoha/contracts/gen/ts/sohaapi'

export type ManifestKustomizeOptions = components['schemas']['ManifestKustomizeOptions']
export type ManifestDeliverySnapshot = components['schemas']['ManifestDeliverySnapshot']

export type ManifestRenderer = 'raw_yaml' | 'kustomize'
export type ManifestStatus = 'draft' | 'published'

export interface ManifestFile {
  path: string
  content: string
}

export interface ManifestBinding {
  templateParameters?: components['schemas']['TemplateParameterValues']
  id?: string
  applicationEnvironmentId: string
  environmentKey: string
  clusterId: string
  namespace: string
  overlay?: Record<string, string>
  kustomize?: ManifestKustomizeOptions
  status?: string
}

export interface ManifestPackage {
  id: string
  name: string
  description?: string
  applicationId: string
  serviceId?: string
  businessLineId?: string
  renderer: ManifestRenderer
  status: ManifestStatus
  currentRevision: number
  files: ManifestFile[]
  bindings: ManifestBinding[]
  createdBy?: string
  updatedBy?: string
  createdAt: string
  updatedAt: string
}

export interface ManifestPackageInput {
  expectedUpdatedAt?: string
  name: string
  description?: string
  applicationId: string
  serviceId?: string
  businessLineId?: string
  renderer: ManifestRenderer
  files: ManifestFile[]
  bindings: ManifestBinding[]
}

export interface ManifestFilter {
  applicationId?: string
  serviceId?: string
  clusterId?: string
  namespace?: string
  search?: string
  page?: number
  pageSize?: number
  limit?: number
}

export interface ManifestPage {
  items: ManifestPackage[]
  total: number
  page: number
  pageSize: number
}

export interface ManifestRevision {
  id: string
  packageId: string
  version: number
  digest: string
  note?: string
  files: ManifestFile[]
  bindings: ManifestBinding[]
  createdBy?: string
  createdAt: string
}

export type ManifestSourceMode = 'soha_managed' | 'git_synced'
export type ManifestSyncPolicy = 'manual' | 'webhook' | 'poll'

export interface ManifestSource {
  id: string
  packageId: string
  mode: ManifestSourceMode
  repositoryId?: string
  refType?: 'branch' | 'tag' | 'commit'
  refValue?: string
  path?: string
  includePatterns?: string[]
  excludePatterns?: string[]
  syncPolicy: ManifestSyncPolicy
  pollIntervalSeconds?: number
  autoPublish: boolean
  autoDeploy: boolean
  lastResolvedCommit?: string
  lastTreeDigest?: string
  lastCanonicalDigest?: string
  lastSuccessfulSyncAt?: string
  lastErrorCode?: string
  lastErrorMessage?: string
  generation: number
  createdAt: string
  updatedAt: string
}

export interface ManifestEnvironmentBinding {
  templateParameters?: components['schemas']['TemplateParameterValues']
  id: string
  packageId: string
  applicationEnvironmentId: string
  environmentKey: string
  clusterId: string
  namespace: string
  overlay: Record<string, string>
  kustomize?: ManifestKustomizeOptions
  rolloutStrategyId?: string
  verificationPolicyId?: string
  driftPolicy: 'report' | 'repair' | 'adopt'
  deletionPolicy: 'orphan' | 'delete_managed'
  enabled: boolean
  version: number
  createdAt: string
  updatedAt: string
}

export interface ManifestEnvironmentBindingUpdateInput {
  templateParameters?: components['schemas']['TemplateParameterValues']
  applicationEnvironmentId: string
  clusterId: string
  namespace: string
  overlay: Record<string, string>
  kustomize?: ManifestKustomizeOptions
  rolloutStrategyId?: string
  verificationPolicyId?: string
  driftPolicy: ManifestEnvironmentBinding['driftPolicy']
  deletionPolicy: ManifestEnvironmentBinding['deletionPolicy']
  enabled: boolean
  expectedVersion: number
}

export interface ManifestRenderedDocument extends ManifestFile {
  index: number
  contentDigest: string
  apiVersion: string
  kind: string
  namespace: string
  name: string
}

export interface ManifestRenderResult {
  packageId: string
  bindingId: string
  revision: number
  renderer: ManifestRenderer
  renderedDigest: string
  inputDigest?: string
  rendererVersion?: string
  documents: ManifestRenderedDocument[]
  diagnostics: ManifestDiagnostic[]
}

export interface ManifestDiagnostic {
  stage: string
  severity: 'info' | 'warning' | 'error'
  code: string
  message: string
  path?: string
}

export interface ManifestExecutionTask {
  id: string
  taskKind: string
  providerKind: string
  status: string
  payload: Record<string, unknown>
  result?: Record<string, unknown>
  createdAt?: string
  updatedAt?: string
}

export interface ManifestSyncRun {
  id: string
  sourceId: string
  packageId: string
  executionTaskId?: string
  sourceGeneration: number
  trigger: 'manual' | 'webhook' | 'poll'
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'ignored'
  requestedCommit?: string
  resolvedCommit?: string
  files: string[]
  revision?: number
  errorMessage?: string
  createdAt: string
  updatedAt: string
}

export interface ManifestDriftReport {
  drifted: boolean
  observedAt: string
  resources: Array<{
    apiVersion: string
    kind: string
    namespace: string
    name: string
    fields: Array<{ path: string; desiredValue: unknown; observedValue: unknown }>
  }>
}

export type ManifestResourceInventory = components['schemas']['ManifestResourceInventory']

export interface ManifestDeployment {
  id: string
  packageId: string
  bindingId: string
  generation: number
  spec: {
    deliverySnapshot?: ManifestDeliverySnapshot
    desiredRevision: number
    desiredDigest: string
    reconcilePolicy: 'manual' | 'continuous'
    driftPolicy: 'report' | 'repair' | 'adopt'
    deletionPolicy: 'orphan' | 'delete_managed'
  }
  status: {
    observedGeneration: number
    appliedRevision?: number
    appliedDigest?: string
    lastKnownGoodRevision?: number
    phase: string
    conditions: Array<{
      type: string
      status: 'true' | 'false' | 'unknown'
      reason?: string
      message?: string
      observedGeneration?: number
      lastTransitionAt?: string
      evidenceRefs?: string[]
    }>
    inventory: ManifestResourceInventory[]
    lastReconciledAt?: string
    lastExecutionTaskId?: string
    drift?: ManifestDriftReport
    lastErrorCode?: string
    lastErrorMessage?: string
  }
  createdAt: string
  updatedAt: string
}

export interface ManifestDeliveryIntent {
  id: string
  packageId: string
  status: 'draft' | 'accepted' | 'rejected'
  files: ManifestFile[]
  provider: string
  model: string
  promptTemplateVersion: string
  evidenceDigest: string
  evidenceRefs: string[]
  proposalDigest: string
  rationale: string
  risk: string
  validation: { ready: boolean; renderedDigest: string; resourceCount: number }
  decisionComment?: string
  createdBy: string
  decidedBy?: string
  createdAt: string
  updatedAt: string
}
