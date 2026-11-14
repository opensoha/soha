import type {
  KubernetesResourceAuthorizationDecision,
  KubernetesResourceCapability,
  KubernetesResourceCreateError,
  KubernetesResourceCreateRequest,
  KubernetesResourceCreateResult,
  KubernetesResourceCreateResultItem,
  KubernetesResourceCreateResultStatus,
  KubernetesResourceCreateScopeDecision,
  KubernetesResourceCreateScopeDecisionRequest,
  KubernetesResourceCreateSource,
  KubernetesResourceDocument,
  KubernetesResourcePreflight,
  KubernetesResourcePreflightItem,
  KubernetesResourceRef,
  KubernetesResourceScope,
  KubernetesResourceScopeMode,
  KubernetesResourceWarning,
  KubernetesWorkloadSnapshot,
  KubernetesWorkloadSnapshotRequest,
  KubernetesWorkloadSnapshotSourceKind,
  KubernetesWorkloadSnapshotTargetKind,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type ResourceAuthorizationDecision = KubernetesResourceAuthorizationDecision
export type ResourceCapability = KubernetesResourceCapability
export type ResourceCreateDocument = KubernetesResourceDocument
export type ResourceCreateError = KubernetesResourceCreateError
export type ResourceCreateRequest = KubernetesResourceCreateRequest
export type ResourceCreateResult = KubernetesResourceCreateResult
export type ResourceCreateResultItem = KubernetesResourceCreateResultItem
export type ResourceCreateResultStatus = KubernetesResourceCreateResultStatus
export type ResourceCreateScopeDecision = KubernetesResourceCreateScopeDecision
export type ResourceCreateScopeDecisionRequest = KubernetesResourceCreateScopeDecisionRequest
export type ResourceCreateSource = KubernetesResourceCreateSource
export type ResourceCreateWarning = KubernetesResourceWarning
export type ResourcePreflight = KubernetesResourcePreflight
export type ResourcePreflightItem = KubernetesResourcePreflightItem
export type ResourceRef = KubernetesResourceRef
export type ResourceScope = KubernetesResourceScope
export type ResourceScopeMode = KubernetesResourceScopeMode
export type WorkloadSnapshot = KubernetesWorkloadSnapshot
export type WorkloadSnapshotInheritance =
  | 'environment'
  | 'storage'
  | 'resources'
  | 'securityContext'
  | 'scheduling'
  | 'initContainers'
  | 'templateMetadata'
  | 'serviceRuntime'
export type WorkloadSnapshotRequest = Omit<
  KubernetesWorkloadSnapshotRequest,
  'inherit' | 'targetKind'
> & {
  inherit?: WorkloadSnapshotInheritance[]
  targetKind: WorkloadSnapshotTargetKind
}
export type WorkloadSnapshotSourceKind = KubernetesWorkloadSnapshotSourceKind
export type WorkloadSnapshotTargetKind = KubernetesWorkloadSnapshotTargetKind | 'WorkloadCronJob'

export interface ResourceCreateContext {
  readonly clusterId: string
  readonly defaultNamespace?: string
  readonly resourceGroup?: string
  readonly expectedApiVersion?: string
  readonly expectedKind?: string
  readonly scopeMode?: ResourceScopeMode
  readonly source: ResourceCreateSource
}
