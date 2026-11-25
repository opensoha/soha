package assistantmesh

import "time"

const ManagedOSVersion = "desktop.os-version.v1"

const (
	DelegationAwaitingApproval = "awaiting_owner_approval"
	DelegationApproved         = "approved"
	DelegationExecuting        = "executing"
	DelegationPreviewed        = "awaiting_confirmation"
	DelegationCompleted        = "completed"
	DelegationDeclined         = "declined"
	DelegationInvalidated      = "invalidated"
	DelegationExpired          = "expired"
	DelegationFailed           = "failed"
)

type Delegation struct {
	ID         string     `json:"id"`
	CallerID   string     `json:"callerId"`
	OwnerID    string     `json:"ownerId"`
	Capability string     `json:"capability"`
	Field      string     `json:"field"`
	InputHash  string     `json:"inputHash"`
	Status     string     `json:"status"`
	Result     string     `json:"result,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	ClosedAt   *time.Time `json:"closedAt,omitempty"`
}

type DelegationRecord struct {
	Delegation
	RequestID       string    `json:"requestId"`
	DeviceID        string    `json:"deviceId,omitempty"`
	Mode            string    `json:"mode,omitempty"`
	TicketHash      string    `json:"ticketHash,omitempty"`
	TicketExpiresAt time.Time `json:"ticketExpiresAt,omitempty"`
	Fence           int       `json:"fence"`
	ResultHash      string    `json:"resultHash,omitempty"`
}

type ManagedDevice struct {
	OwnerID   string
	DeviceID  string
	SessionID string
	ProofHash string
}
