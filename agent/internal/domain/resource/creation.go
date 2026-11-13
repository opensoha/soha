package resource

import sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"

type (
	KubernetesResourceAgentCreateDocument  = sohaapi.KubernetesResourceAgentCreateDocument
	KubernetesResourceAgentCreateRequest   = sohaapi.KubernetesResourceAgentCreateRequest
	KubernetesResourceAgentCreateResult    = sohaapi.KubernetesResourceAgentCreateResult
	KubernetesResourceAgentPreflightItem   = sohaapi.KubernetesResourceAgentPreflightItem
	KubernetesResourceAgentPreflightResult = sohaapi.KubernetesResourceAgentPreflightResult

	KubernetesResourceCreateBatchStatus  = sohaapi.KubernetesResourceCreateBatchStatus
	KubernetesResourceCreateError        = sohaapi.KubernetesResourceCreateError
	KubernetesResourceCreateErrorCode    = sohaapi.KubernetesResourceCreateErrorCode
	KubernetesResourceCreateResultItem   = sohaapi.KubernetesResourceCreateResultItem
	KubernetesResourceCreateResultStatus = sohaapi.KubernetesResourceCreateResultStatus
	KubernetesResourceDocument           = sohaapi.KubernetesResourceDocument
	KubernetesResourceDryRunDecision     = sohaapi.KubernetesResourceDryRunDecision
	KubernetesResourceDryRunStatus       = sohaapi.KubernetesResourceDryRunStatus
	KubernetesResourceRef                = sohaapi.KubernetesResourceRef
	KubernetesResourceScopeMode          = sohaapi.KubernetesResourceScopeMode
	KubernetesResourceWarning            = sohaapi.KubernetesResourceWarning
)

const (
	KubernetesResourceCreateBatchStatusFailed    = sohaapi.KubernetesResourceCreateBatchStatusFailed
	KubernetesResourceCreateBatchStatusPartial   = sohaapi.KubernetesResourceCreateBatchStatusPartial
	KubernetesResourceCreateBatchStatusSucceeded = sohaapi.KubernetesResourceCreateBatchStatusSucceeded

	KubernetesResourceCreateResultStatusFailed     = sohaapi.KubernetesResourceCreateResultStatusFailed
	KubernetesResourceCreateResultStatusNotStarted = sohaapi.KubernetesResourceCreateResultStatusNotStarted
	KubernetesResourceCreateResultStatusSucceeded  = sohaapi.KubernetesResourceCreateResultStatusSucceeded

	KubernetesResourceDryRunStatusFailed  = sohaapi.KubernetesResourceDryRunStatusFailed
	KubernetesResourceDryRunStatusPassed  = sohaapi.KubernetesResourceDryRunStatusPassed
	KubernetesResourceDryRunStatusSkipped = sohaapi.KubernetesResourceDryRunStatusSkipped

	KubernetesResourceScopeModeCluster   = sohaapi.KubernetesResourceScopeModeCluster
	KubernetesResourceScopeModeNamespace = sohaapi.KubernetesResourceScopeModeNamespace

	KubernetesResourceCreateErrorCodeClusterScopedNamespaceIgnored = sohaapi.KubernetesResourceCreateErrorCodeClusterScopedNamespaceIgnored
	KubernetesResourceCreateErrorCodeMultiDocumentNotAllowed       = sohaapi.KubernetesResourceCreateErrorCodeMultiDocumentNotAllowed
	KubernetesResourceCreateErrorCodeNamespaceMismatch             = sohaapi.KubernetesResourceCreateErrorCodeNamespaceMismatch
	KubernetesResourceCreateErrorCodeNamespaceRequired             = sohaapi.KubernetesResourceCreateErrorCodeNamespaceRequired
	KubernetesResourceCreateErrorCodeResourceCapabilityUnsupported = sohaapi.KubernetesResourceCreateErrorCodeResourceCapabilityUnsupported
	KubernetesResourceCreateErrorCodeResourceCreateDenied          = sohaapi.KubernetesResourceCreateErrorCodeResourceCreateDenied
	KubernetesResourceCreateErrorCodeResourceDryRunFailed          = sohaapi.KubernetesResourceCreateErrorCodeResourceDryRunFailed
	KubernetesResourceCreateErrorCodeResourceKindMismatch          = sohaapi.KubernetesResourceCreateErrorCodeResourceKindMismatch
)
