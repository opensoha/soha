package resource

// ResourceCreateSource identifies the namespace and document rules used by a
// creation request.
type ResourceCreateSource string

const (
	ResourceCreateSourceList   ResourceCreateSource = "list"
	ResourceCreateSourceGlobal ResourceCreateSource = "global_yaml"
	ResourceCreateSourceForm   ResourceCreateSource = "form"
)

const (
	ResourceCreateMaxBodyBytes     = 1 << 20
	ResourceCreateMaxDocumentBytes = 512 << 10
	ResourceCreateMaxDocuments     = 20
)

type ResourceCreateRequest struct {
	Source             ResourceCreateSource `json:"source"`
	DefaultNamespace   string               `json:"defaultNamespace,omitempty"`
	ResourceGroup      string               `json:"resourceGroup,omitempty"`
	ExpectedAPIVersion string               `json:"expectedApiVersion,omitempty"`
	ExpectedKind       string               `json:"expectedKind,omitempty"`
	Content            string               `json:"content"`
	RequestID          string               `json:"requestId,omitempty"`
}

type ResourceCreateScopeDecisionRequest struct {
	Namespace     string
	ResourceGroup string
	APIVersion    string
	Kind          string
}

type ResourceCreateCapability struct {
	Key    string
	Status string
	Mode   string
	Reason string
}

type ResourceCreateScopeDecision struct {
	Allowed        bool
	Reason         string
	AllowedActions []string
	ClusterIDs     []string
	Namespaces     []string
	ResourceGroups []string
	ResourceKinds  []string
	Capability     ResourceCreateCapability
}

type ResourceCreateRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	Namespaced bool   `json:"namespaced"`
}

type ResourceCreateWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ResourceCreateCheck struct {
	Allowed        bool     `json:"allowed"`
	Reason         string   `json:"reason,omitempty"`
	AllowedActions []string `json:"allowedActions,omitempty"`
	ClusterIDs     []string `json:"clusterIds,omitempty"`
	Namespaces     []string `json:"namespaces,omitempty"`
	ResourceGroups []string `json:"resourceGroups,omitempty"`
	ResourceKinds  []string `json:"resourceKinds,omitempty"`
}

type ResourceDryRunCheck struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message,omitempty"`
}

type ResourceCreateDocument struct {
	Index             int                      `json:"index"`
	Resource          ResourceCreateRef        `json:"resource"`
	Warnings          []ResourceCreateWarning  `json:"warnings,omitempty"`
	Authorization     ResourceCreateCheck      `json:"authorization"`
	Capability        ResourceCreateCapability `json:"capability"`
	DryRun            ResourceDryRunCheck      `json:"dryRun"`
	Status            string                   `json:"status"`
	ErrorCode         string                   `json:"errorCode,omitempty"`
	Error             string                   `json:"error,omitempty"`
	OriginalNamespace string                   `json:"-"`
	DocumentHash      string                   `json:"-"`
}

type ResourceCreatePreflight struct {
	Ready       bool                     `json:"ready"`
	ContentHash string                   `json:"contentHash"`
	Documents   []ResourceCreateDocument `json:"documents"`
}

type ResourceCreateExecutionDocument struct {
	Index        int               `json:"index"`
	Resource     ResourceCreateRef `json:"resource"`
	Status       string            `json:"status"`
	ErrorCode    string            `json:"errorCode,omitempty"`
	Error        string            `json:"error,omitempty"`
	DocumentHash string            `json:"-"`
}

type ResourceCreateExecution struct {
	OperationID string                            `json:"operationId"`
	ContentHash string                            `json:"contentHash"`
	Status      string                            `json:"status"`
	Documents   []ResourceCreateExecutionDocument `json:"documents"`
}

// ResolvedCreateManifest is an internal provider-neutral representation. The
// object is used only for policy checks and must never be persisted in logs.
type ResolvedCreateManifest struct {
	Index       int
	Ref         ResourceCreateRef
	Group       string
	Version     string
	Resource    string
	Content     string
	Object      map[string]any
	ContentHash string
}
