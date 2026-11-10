package delivery

import "strings"

// Wire values are specified by Contracts' claimExecutionTask operation. They
// use the existing v0.1.26 task and HTTP surface, without new SDK symbols.
const (
	ManifestAdmissionProviderPrefix = "manifest_delivery_v1."
	ManifestAdmissionCapability     = "manifest.delivery-admission.v1"
	ManifestAdmissionHeader         = "X-Soha-Manifest-Admission-Version"
)

func RequiresManifestAdmission(task ExecutionTask) bool {
	return strings.HasPrefix(task.ProviderKind, ManifestAdmissionProviderPrefix)
}

func ManifestBaseProvider(provider string) string {
	return strings.TrimPrefix(provider, ManifestAdmissionProviderPrefix)
}
