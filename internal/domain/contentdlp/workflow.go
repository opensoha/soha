package contentdlp

import "time"

type AuthorizationInput struct {
	SessionID string `json:"-"`
	Action    string `json:"action"`
}

// TransferAccess carries credentials from the authenticated transport only.
type TransferAccess struct {
	Token     string `json:"-"`
	SessionID string `json:"-"`
}
type Authorization struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type AppealInput struct {
	ReasonCode string `json:"reasonCode"`
}
type ReviewInput struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	Resolution       string `json:"resolution"`
	ReasonCode       string `json:"reasonCode"`
}
type Appeal struct {
	EventID      string    `json:"eventId"`
	ProjectID    string    `json:"projectId"`
	ActorID      string    `json:"actorId"`
	ReasonCode   string    `json:"reasonCode"`
	Status       string    `json:"status"`
	Revision     int64     `json:"revision"`
	ReviewerID   string    `json:"reviewerId,omitempty"`
	ReviewReason string    `json:"reviewReason,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
type NetworkTarget struct {
	Kind            string `json:"kind"`
	RuntimeID       string `json:"runtimeId,omitempty"`
	SessionID       string `json:"sessionId,omitempty"`
	CommandID       string `json:"commandId,omitempty"`
	BaseVersion     int64  `json:"baseVersion"`
	ObservedVersion int64  `json:"observedVersion"`
	Status          string `json:"status"`
}
type NetworkHold struct {
	ID            string          `json:"id"`
	SourceEventID string          `json:"sourceEventId"`
	ProjectID     string          `json:"projectId"`
	SubjectID     string          `json:"subjectId"`
	Active        bool            `json:"active"`
	Revision      int64           `json:"revision"`
	CreatedAt     time.Time       `json:"createdAt"`
	ReleasedAt    *time.Time      `json:"releasedAt,omitempty"`
	Targets       []NetworkTarget `json:"targets"`
}
type Case struct {
	Event   Event        `json:"event"`
	Appeal  *Appeal      `json:"appeal,omitempty"`
	Linkage *NetworkHold `json:"linkage,omitempty"`
}

// Binding is private durable verifier material. Neither the key nor tag is
// returned by case/event APIs; no file content is persisted.
type Binding struct {
	Key              []byte
	Tag              []byte
	ExpiresAt        time.Time
	ProjectUpdatedAt time.Time
}
type EventRecord struct {
	Event   Event
	Binding Binding
	Appeal  *Appeal
}
type Proof struct {
	TokenHash string
	Kind      string
	Record    EventRecord
}

// NetworkCause contains only the verified subject restriction fact, not a Docker identity.
type NetworkCause struct {
	ID        string
	ActorID   string
	CreatedAt time.Time
}
