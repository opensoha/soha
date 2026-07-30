package manifest

import "time"

const (
	SourceModeSohaManaged = "soha_managed"
	SourceModeGitSynced   = "git_synced"

	SourceRefBranch = "branch"
	SourceRefTag    = "tag"
	SourceRefCommit = "commit"

	SyncPolicyManual  = "manual"
	SyncPolicyWebhook = "webhook"
	SyncPolicyPoll    = "poll"

	DriftPolicyReport = "report"
	DriftPolicyRepair = "repair"
	DriftPolicyAdopt  = "adopt"

	DeletionPolicyOrphan        = "orphan"
	DeletionPolicyDeleteManaged = "delete_managed"

	ReconcilePolicyManual     = "manual"
	ReconcilePolicyContinuous = "continuous"

	DeploymentPhasePending         = "pending"
	DeploymentPhaseWaitingApproval = "waiting_approval"
	DeploymentPhaseReconciling     = "reconciling"
	DeploymentPhaseConverged       = "converged"
	DeploymentPhaseDrifted         = "drifted"
	DeploymentPhaseDegraded        = "degraded"
	DeploymentPhaseDeleting        = "deleting"
)

type Source struct {
	ID                   string     `json:"id"`
	PackageID            string     `json:"packageId"`
	Mode                 string     `json:"mode"`
	RepositoryID         string     `json:"repositoryId,omitempty"`
	RefType              string     `json:"refType,omitempty"`
	RefValue             string     `json:"refValue,omitempty"`
	Path                 string     `json:"path,omitempty"`
	IncludePatterns      []string   `json:"includePatterns,omitempty"`
	ExcludePatterns      []string   `json:"excludePatterns,omitempty"`
	SyncPolicy           string     `json:"syncPolicy"`
	PollIntervalSeconds  int        `json:"pollIntervalSeconds,omitempty"`
	AutoPublish          bool       `json:"autoPublish"`
	AutoDeploy           bool       `json:"autoDeploy"`
	LastResolvedCommit   string     `json:"lastResolvedCommit,omitempty"`
	LastTreeDigest       string     `json:"lastTreeDigest,omitempty"`
	LastCanonicalDigest  string     `json:"lastCanonicalDigest,omitempty"`
	LastSuccessfulSyncAt *time.Time `json:"lastSuccessfulSyncAt,omitempty"`
	LastErrorCode        string     `json:"lastErrorCode,omitempty"`
	LastErrorMessage     string     `json:"lastErrorMessage,omitempty"`
	Generation           int64      `json:"generation"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type SourceInput struct {
	Mode                string   `json:"mode"`
	RepositoryID        string   `json:"repositoryId,omitempty"`
	RefType             string   `json:"refType,omitempty"`
	RefValue            string   `json:"refValue,omitempty"`
	Path                string   `json:"path,omitempty"`
	IncludePatterns     []string `json:"includePatterns,omitempty"`
	ExcludePatterns     []string `json:"excludePatterns,omitempty"`
	SyncPolicy          string   `json:"syncPolicy"`
	PollIntervalSeconds int      `json:"pollIntervalSeconds,omitempty"`
	AutoPublish         bool     `json:"autoPublish"`
	AutoDeploy          bool     `json:"autoDeploy"`
	ExpectedGeneration  int64    `json:"expectedGeneration"`
}

type EnvironmentBinding struct {
	ID                       string            `json:"id"`
	PackageID                string            `json:"packageId"`
	ApplicationEnvironmentID string            `json:"applicationEnvironmentId"`
	EnvironmentKey           string            `json:"environmentKey"`
	ClusterID                string            `json:"clusterId"`
	Namespace                string            `json:"namespace"`
	Overlay                  map[string]string `json:"overlay"`
	RolloutStrategyID        string            `json:"rolloutStrategyId,omitempty"`
	VerificationPolicyID     string            `json:"verificationPolicyId,omitempty"`
	DriftPolicy              string            `json:"driftPolicy"`
	DeletionPolicy           string            `json:"deletionPolicy"`
	Enabled                  bool              `json:"enabled"`
	Version                  int64             `json:"version"`
	CreatedAt                time.Time         `json:"createdAt"`
	UpdatedAt                time.Time         `json:"updatedAt"`
}

type BindingInput struct {
	ApplicationEnvironmentID string            `json:"applicationEnvironmentId"`
	ClusterID                string            `json:"clusterId"`
	Namespace                string            `json:"namespace"`
	Overlay                  map[string]string `json:"overlay,omitempty"`
	RolloutStrategyID        string            `json:"rolloutStrategyId,omitempty"`
	VerificationPolicyID     string            `json:"verificationPolicyId,omitempty"`
	DriftPolicy              string            `json:"driftPolicy"`
	DeletionPolicy           string            `json:"deletionPolicy"`
	Enabled                  bool              `json:"enabled"`
}

type BindingUpdateInput struct {
	BindingInput
	ExpectedVersion int64 `json:"expectedVersion"`
}

type Condition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason"`
	Message            string    `json:"message"`
	ObservedGeneration int64     `json:"observedGeneration"`
	LastTransitionAt   time.Time `json:"lastTransitionAt"`
	EvidenceRefs       []string  `json:"evidenceRefs"`
}

type ResourceInventory struct {
	DeploymentID         string    `json:"deploymentId"`
	Generation           int64     `json:"generation"`
	APIVersion           string    `json:"apiVersion"`
	Kind                 string    `json:"kind"`
	Namespace            string    `json:"namespace"`
	Name                 string    `json:"name"`
	UID                  string    `json:"uid,omitempty"`
	ResourceVersion      string    `json:"resourceVersion,omitempty"`
	DesiredObjectDigest  string    `json:"desiredObjectDigest"`
	ObservedObjectDigest string    `json:"observedObjectDigest"`
	Health               string    `json:"health"`
	LastObservedAt       time.Time `json:"lastObservedAt"`
}

type DeploymentSpec struct {
	DesiredRevision int    `json:"desiredRevision"`
	DesiredDigest   string `json:"desiredDigest"`
	ReconcilePolicy string `json:"reconcilePolicy"`
	DriftPolicy     string `json:"driftPolicy"`
	DeletionPolicy  string `json:"deletionPolicy"`
}

type DeploymentStatus struct {
	ObservedGeneration    int64               `json:"observedGeneration"`
	AppliedRevision       int                 `json:"appliedRevision,omitempty"`
	AppliedDigest         string              `json:"appliedDigest,omitempty"`
	LastKnownGoodRevision int                 `json:"lastKnownGoodRevision,omitempty"`
	Phase                 string              `json:"phase"`
	Conditions            []Condition         `json:"conditions"`
	Inventory             []ResourceInventory `json:"inventory"`
	LastReconciledAt      *time.Time          `json:"lastReconciledAt,omitempty"`
	LastExecutionTaskID   string              `json:"lastExecutionTaskId,omitempty"`
	Drift                 *DriftReport        `json:"drift,omitempty"`
	LastErrorCode         string              `json:"lastErrorCode,omitempty"`
	LastErrorMessage      string              `json:"lastErrorMessage,omitempty"`
}

type DesiredRevisionInput struct {
	DesiredRevision    int    `json:"desiredRevision"`
	ReconcilePolicy    string `json:"reconcilePolicy"`
	ExpectedGeneration int64  `json:"expectedGeneration"`
	Reason             string `json:"reason,omitempty"`
}

type ActionInput struct {
	ExpectedGeneration int64  `json:"expectedGeneration"`
	Reason             string `json:"reason,omitempty"`
	ForceConflicts     bool   `json:"forceConflicts"`
}

type RollbackInput struct {
	ExpectedGeneration int64  `json:"expectedGeneration"`
	TargetRevision     int    `json:"targetRevision,omitempty"`
	UseLastKnownGood   bool   `json:"useLastKnownGood"`
	Reason             string `json:"reason,omitempty"`
}

type Deployment struct {
	ID         string           `json:"id"`
	PackageID  string           `json:"packageId"`
	BindingID  string           `json:"bindingId"`
	Generation int64            `json:"generation"`
	Spec       DeploymentSpec   `json:"spec"`
	Status     DeploymentStatus `json:"status"`
	CreatedAt  time.Time        `json:"createdAt"`
	UpdatedAt  time.Time        `json:"updatedAt"`
}

type DeploymentFilter struct {
	PackageID                string
	ApplicationID            string
	ApplicationIDs           []string
	ApplicationEnvironmentID string
	ClusterID                string
	Namespace                string
	SourceMode               string
	Phase                    string
	Page                     int
	PageSize                 int
}

type DeploymentPage struct {
	Items    []Deployment `json:"items"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
}
