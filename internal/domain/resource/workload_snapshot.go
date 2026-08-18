package resource

import sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"

type (
	WorkloadSnapshot              = sohaapi.KubernetesWorkloadSnapshot
	WorkloadSnapshotContainer     = sohaapi.KubernetesWorkloadSnapshotContainer
	WorkloadSnapshotInheritance   = sohaapi.KubernetesWorkloadSnapshotInheritance
	WorkloadSnapshotRequest       = sohaapi.KubernetesWorkloadSnapshotRequest
	WorkloadSnapshotRestartPolicy = sohaapi.KubernetesWorkloadSnapshotRestartPolicy
	WorkloadSnapshotSourceKind    = sohaapi.KubernetesWorkloadSnapshotSourceKind
	WorkloadSnapshotTargetKind    = sohaapi.KubernetesWorkloadSnapshotTargetKind
)

const (
	WorkloadSnapshotSourceDeployment        = sohaapi.KubernetesWorkloadSnapshotSourceKindDeployment
	WorkloadSnapshotSourceStatefulSet       = sohaapi.KubernetesWorkloadSnapshotSourceKindStatefulSet
	WorkloadSnapshotSourceDaemonSet         = sohaapi.KubernetesWorkloadSnapshotSourceKindDaemonSet
	WorkloadSnapshotTargetJob               = sohaapi.KubernetesWorkloadSnapshotTargetKindJob
	WorkloadSnapshotTargetCronJob           = sohaapi.KubernetesWorkloadSnapshotTargetKindCronJob
	WorkloadSnapshotTargetWorkloadCronJob   = sohaapi.KubernetesWorkloadSnapshotTargetKindWorkloadCronJob
	WorkloadSnapshotRestartNever            = sohaapi.KubernetesWorkloadSnapshotRestartPolicyNever
	WorkloadSnapshotRestartOnFailure        = sohaapi.KubernetesWorkloadSnapshotRestartPolicyOnFailure
	WorkloadSnapshotInheritEnvironment      = sohaapi.KubernetesWorkloadSnapshotInheritanceEnvironment
	WorkloadSnapshotInheritStorage          = sohaapi.KubernetesWorkloadSnapshotInheritanceStorage
	WorkloadSnapshotInheritResources        = sohaapi.KubernetesWorkloadSnapshotInheritanceResources
	WorkloadSnapshotInheritSecurityContext  = sohaapi.KubernetesWorkloadSnapshotInheritanceSecurityContext
	WorkloadSnapshotInheritScheduling       = sohaapi.KubernetesWorkloadSnapshotInheritanceScheduling
	WorkloadSnapshotInheritInitContainers   = sohaapi.KubernetesWorkloadSnapshotInheritanceInitContainers
	WorkloadSnapshotInheritTemplateMetadata = sohaapi.KubernetesWorkloadSnapshotInheritanceTemplateMetadata
	WorkloadSnapshotInheritServiceRuntime   = sohaapi.KubernetesWorkloadSnapshotInheritanceServiceRuntime
)
