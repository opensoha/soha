package copilot

import (
	"errors"
	"time"

	contractenv "github.com/opensoha/soha-contracts/agent/environment"
)

// Local dispatch checks alone may return this marker. A network failure or an
// Agent error cannot prove that the personal-data operation was not dispatched.
var ErrPersonalEnvironmentNotDispatched = errors.New("personal environment request was not dispatched")

// PersonalEnvironment exposes the latest receipt, without node credentials or
// Docker handles. DataInitialized records a past receipt, not current readiness.
type PersonalEnvironment struct {
	AssistantID       string             `json:"assistantId"`
	TemplateID        string             `json:"templateId,omitempty"`
	Backend           string             `json:"backend,omitempty"`
	Provider          string             `json:"provider,omitempty"`
	PolicyRevision    string             `json:"policyRevision,omitempty"`
	ComputeRef        string             `json:"computeRef,omitempty"`
	ComputeGeneration int64              `json:"computeGeneration,omitempty"`
	DataRef           string             `json:"dataRef,omitempty"`
	State             string             `json:"state"`
	Version           int64              `json:"version"`
	DataInitialized   bool               `json:"dataInitialized"`
	Quota             *contractenv.Quota `json:"quota,omitempty"`
	Usage             *contractenv.Usage `json:"usage,omitempty"`
	ObservedAt        *time.Time         `json:"observedAt,omitempty"`
	ExpiresAt         *time.Time         `json:"expiresAt,omitempty"`
	UpdatedAt         *time.Time         `json:"updatedAt,omitempty"`
}

// File content is a bounded current observation, never an execution receipt or
// a published artifact. Gateway output policy still applies before model use.
type PersonalEnvironmentFile struct {
	AssistantID        string    `json:"assistantId"`
	EnvironmentVersion int64     `json:"environmentVersion"`
	Path               string    `json:"path"`
	Content            string    `json:"content"`
	ObservedAt         time.Time `json:"observedAt"`
}

// Binding identity and quota are immutable. A pending operation is committed
// before provider dispatch and completed only by its matching reservation.
type PersonalEnvironmentRecord struct {
	AssistantID           string
	CreatedBy             string
	TemplateID            string
	Backend               string
	Provider              string
	PolicyRevision        string
	ComputeRef            string
	ComputeGeneration     int64
	DataRef               string
	VMIdentity            *PersonalVMIdentity
	AgentClusterID        string
	LeaseSeconds          int
	Lease                 contractenv.Lease
	State                 string
	Version               int64
	DataInitialized       bool
	DataCreationAttempted bool
	Usage                 *contractenv.Usage
	ObservedAt            *time.Time
	UpdatedAt             time.Time
	OperationID           string
	OperationUntil        *time.Time
	ActiveTaskID          string
}

type PersonalEnvironmentPolicy struct {
	TemplateID     string
	Name           string
	Backend        string
	Provider       string
	PolicyRevision string
	AgentClusterID string
	LeaseSeconds   int
	Mode           string
	Quota          contractenv.Quota
	VM             *PersonalVMPolicy `json:"vm,omitempty"`
}

// Template choices disclose only user-relevant configuration, never node scope.
type PersonalEnvironmentTemplate struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Backend        string            `json:"backend,omitempty"`
	Provider       string            `json:"provider,omitempty"`
	PolicyRevision string            `json:"policyRevision,omitempty"`
	Mode           string            `json:"mode"`
	LeaseSeconds   int               `json:"leaseSeconds"`
	Quota          contractenv.Quota `json:"quota"`
}
