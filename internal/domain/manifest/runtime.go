package manifest

import "time"

const (
	TaskActionPreflight = "preflight"
	TaskActionApply     = "apply"
	TaskActionObserve   = "observe"
	TaskActionRepair    = "repair"
	TaskActionAdopt     = "adopt"
	TaskActionRollback  = "rollback"
	TaskActionSync      = "sync"

	TaskKindPreflight = "manifest_preflight"
	TaskKindApply     = "manifest_apply"
	TaskKindObserve   = "manifest_observe"
	TaskKindRepair    = "manifest_repair"
	TaskKindAdopt     = "manifest_adopt"
	TaskKindRollback  = "manifest_rollback"
	TaskKindSync      = "manifest_sync"

	TaskProviderDirect = "manifest_direct"
	TaskProviderGit    = "manifest_git"
	TaskProviderAgent  = "ci_agent_runner"
)

type RenderInput struct {
	BindingID string `json:"bindingId"`
	Revision  int    `json:"revision,omitempty"`
}

type PreflightInput struct {
	BindingID      string `json:"bindingId"`
	Revision       int    `json:"revision,omitempty"`
	ForceConflicts bool   `json:"forceConflicts"`
}

type RenderedDocument struct {
	Index         int    `json:"index"`
	Path          string `json:"path"`
	Content       string `json:"content"`
	ContentDigest string `json:"contentDigest"`
	APIVersion    string `json:"apiVersion"`
	Kind          string `json:"kind"`
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
}

type Diagnostic struct {
	Stage         string `json:"stage"`
	Severity      string `json:"severity"`
	Code          string `json:"code"`
	Message       string `json:"message"`
	Path          string `json:"path,omitempty"`
	DocumentIndex int    `json:"documentIndex,omitempty"`
	Field         string `json:"field,omitempty"`
	APIVersion    string `json:"apiVersion,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Namespace     string `json:"namespace,omitempty"`
	Name          string `json:"name,omitempty"`
	FieldManager  string `json:"fieldManager,omitempty"`
}

type RenderResult struct {
	PackageID      string             `json:"packageId"`
	BindingID      string             `json:"bindingId"`
	Revision       int                `json:"revision"`
	Renderer       string             `json:"renderer"`
	RenderedDigest string             `json:"renderedDigest"`
	Documents      []RenderedDocument `json:"documents"`
	Diagnostics    []Diagnostic       `json:"diagnostics"`
}

type PreflightResult struct {
	Ready          bool         `json:"ready"`
	Capability     string       `json:"capability"`
	RenderedDigest string       `json:"renderedDigest"`
	ResourceCount  int          `json:"resourceCount"`
	Diagnostics    []Diagnostic `json:"diagnostics"`
}

type DriftField struct {
	Path          string `json:"path"`
	DesiredValue  any    `json:"desiredValue"`
	ObservedValue any    `json:"observedValue"`
	FieldManager  string `json:"fieldManager,omitempty"`
}

type DriftResource struct {
	APIVersion string       `json:"apiVersion"`
	Kind       string       `json:"kind"`
	Namespace  string       `json:"namespace"`
	Name       string       `json:"name"`
	Fields     []DriftField `json:"fields"`
}

type DriftReport struct {
	Drifted      bool            `json:"drifted"`
	ObservedAt   time.Time       `json:"observedAt"`
	AgeSeconds   int64           `json:"ageSeconds,omitempty"`
	Resources    []DriftResource `json:"resources"`
	EvidenceRefs []string        `json:"evidenceRefs"`
}

type TaskPayload struct {
	Action          string              `json:"action"`
	PackageID       string              `json:"packageId"`
	BindingID       string              `json:"bindingId,omitempty"`
	DeploymentID    string              `json:"deploymentId,omitempty"`
	SourceID        string              `json:"sourceId,omitempty"`
	Generation      int64               `json:"generation"`
	Revision        int                 `json:"revision,omitempty"`
	RenderedDigest  string              `json:"renderedDigest,omitempty"`
	ClusterID       string              `json:"clusterId,omitempty"`
	Namespace       string              `json:"namespace,omitempty"`
	FieldManager    string              `json:"fieldManager,omitempty"`
	ForceConflicts  bool                `json:"forceConflicts"`
	IdempotencyKey  string              `json:"idempotencyKey"`
	Documents       []RenderedDocument  `json:"documents,omitempty"`
	Inventory       []ResourceInventory `json:"inventory,omitempty"`
	RepositoryID    string              `json:"repositoryId,omitempty"`
	RepositoryURL   string              `json:"repositoryUrl,omitempty"`
	Renderer        string              `json:"renderer,omitempty"`
	RefType         string              `json:"refType,omitempty"`
	RefValue        string              `json:"refValue,omitempty"`
	Path            string              `json:"path,omitempty"`
	IncludePatterns []string            `json:"includePatterns,omitempty"`
	ExcludePatterns []string            `json:"excludePatterns,omitempty"`
	RequestedCommit string              `json:"requestedCommit,omitempty"`
	RequestedBy     string              `json:"requestedBy,omitempty"`
}

type TaskResult struct {
	Action          string              `json:"action"`
	DeploymentID    string              `json:"deploymentId,omitempty"`
	Generation      int64               `json:"generation"`
	Stale           bool                `json:"stale"`
	RenderedDigest  string              `json:"renderedDigest,omitempty"`
	Preflight       *PreflightResult    `json:"preflight,omitempty"`
	Diagnostics     []Diagnostic        `json:"diagnostics"`
	Inventory       []ResourceInventory `json:"inventory"`
	Drift           *DriftReport        `json:"drift,omitempty"`
	AdoptedFiles    []File              `json:"adoptedFiles,omitempty"`
	EvidenceRefs    []string            `json:"evidenceRefs,omitempty"`
	ResolvedCommit  string              `json:"resolvedCommit,omitempty"`
	TreeDigest      string              `json:"treeDigest,omitempty"`
	CanonicalDigest string              `json:"canonicalDigest,omitempty"`
	SyncedFiles     []File              `json:"syncedFiles,omitempty"`
}

type OperationRun struct {
	ID              string    `json:"id"`
	PackageID       string    `json:"packageId"`
	BindingID       string    `json:"bindingId,omitempty"`
	DeploymentID    string    `json:"deploymentId,omitempty"`
	Generation      int64     `json:"generation"`
	Action          string    `json:"action"`
	IdempotencyKey  string    `json:"idempotencyKey"`
	ExecutionTaskID string    `json:"executionTaskId"`
	CreatedAt       time.Time `json:"createdAt"`
}
