package networkaccess

import (
	"encoding/json"
	"slices"
	"time"
)

const (
	VPNSelectionAuto    = "auto"
	VPNSelectionManual  = "manual"
	VPNStrategyLatency  = "latency"
	VPNStrategyProvider = "provider"
	VPNStrategyPriority = "priority"
)

type VPNAssignment struct {
	UserIDs   []string `json:"userIds"`
	TeamIDs   []string `json:"teamIds"`
	DeviceIDs []string `json:"deviceIds"`
}

// Matches intersects an optional device restriction with the assigned people.
// A device-only assignment admits its owner, never another user's device.
func (a VPNAssignment) Matches(subject Subject, device Device) bool {
	if subject.UserID == "" || device.OwnerUserID != subject.UserID || len(a.UserIDs)+len(a.TeamIDs)+len(a.DeviceIDs) == 0 {
		return false
	}
	person := len(a.UserIDs)+len(a.TeamIDs) == 0 || slices.Contains(a.UserIDs, subject.UserID)
	for _, team := range subject.Teams {
		person = person || slices.Contains(a.TeamIDs, team)
	}
	return person && (len(a.DeviceIDs) == 0 || slices.Contains(a.DeviceIDs, device.ID))
}

type VPNProfileConfig struct {
	Name                 string        `json:"name"`
	SiteID               string        `json:"siteId"`
	NetworkSpaceID       string        `json:"networkSpaceId"`
	Mode                 string        `json:"mode"`
	ResourceIDs          []string      `json:"resourceIds"`
	GatewayIDs           []string      `json:"gatewayIds"`
	SelectionPolicyID    string        `json:"selectionPolicyId"`
	AllowManualSelection bool          `json:"allowManualSelection"`
	Enabled              bool          `json:"enabled"`
	Assignments          VPNAssignment `json:"assignments"`
}

type VPNProfile struct {
	ID                     string            `json:"id"`
	Revision               int               `json:"revision"`
	PublishedRevision      int               `json:"publishedRevision"`
	Configuration          VPNProfileConfig  `json:"configuration"`
	PublishedConfiguration *VPNProfileConfig `json:"-"`
	CreatedAt              time.Time         `json:"createdAt"`
	UpdatedAt              time.Time         `json:"updatedAt"`
}

type VPNSelectionPolicyConfig struct {
	Name                 string   `json:"name"`
	Strategy             string   `json:"strategy"`
	ProviderOrder        []string `json:"providerOrder"`
	ProviderPreference   string   `json:"providerPreference"`
	MaxLatencyMs         int      `json:"maxLatencyMs"`
	MaxTimeoutPercent    int      `json:"maxTimeoutPercent"`
	MaxSampleAgeSeconds  int      `json:"maxSampleAgeSeconds"`
	MinSamples           int      `json:"minSamples"`
	MissingMeasurements  string   `json:"missingMeasurements"`
	MaxAttempts          int      `json:"maxAttempts"`
	RetryCooldownSeconds int      `json:"retryCooldownSeconds"`
	FailoverOnDisconnect bool     `json:"failoverOnDisconnect"`
	AllowManualFallback  bool     `json:"allowManualFallback"`
}

type VPNSelectionPolicy struct {
	ID                     string                    `json:"id"`
	Revision               int                       `json:"revision"`
	PublishedRevision      int                       `json:"publishedRevision"`
	Configuration          VPNSelectionPolicyConfig  `json:"configuration"`
	PublishedConfiguration *VPNSelectionPolicyConfig `json:"-"`
	CreatedAt              time.Time                 `json:"createdAt"`
	UpdatedAt              time.Time                 `json:"updatedAt"`
}

type VPNRevision[T any] struct {
	Revision      int       `json:"revision"`
	Configuration T         `json:"configuration"`
	CreatedAt     time.Time `json:"createdAt"`
	CreatedBy     string    `json:"createdBy"`
}

type VPNDocumentFilter struct {
	Search  string
	AfterID string
	Limit   int
}

type VPNCandidate struct {
	GatewayID      string     `json:"gatewayId"`
	Name           string     `json:"name"`
	Region         string     `json:"region"`
	ProviderCode   string     `json:"providerCode"`
	ProviderName   string     `json:"providerName"`
	Available      bool       `json:"available"`
	ReasonCode     string     `json:"reasonCode"`
	Priority       int        `json:"priority"`
	LatencyMs      *float64   `json:"latencyMs,omitempty"`
	TimeoutPercent *float64   `json:"timeoutPercent,omitempty"`
	MeasuredAt     *time.Time `json:"measuredAt,omitempty"`
	SampleCount    int        `json:"-"`
}

type VPNConnectionOption struct {
	ProfileID               string         `json:"profileId"`
	ProfileRevision         int            `json:"profileRevision"`
	Name                    string         `json:"name"`
	SiteID                  string         `json:"siteId"`
	NetworkSpaceID          string         `json:"networkSpaceId"`
	Mode                    string         `json:"mode"`
	AllowManualSelection    bool           `json:"allowManualSelection"`
	SelectionPolicyID       string         `json:"selectionPolicyId"`
	SelectionPolicyRevision int            `json:"selectionPolicyRevision"`
	SelectionStrategy       string         `json:"selectionStrategy"`
	Candidates              []VPNCandidate `json:"candidates"`
	Available               bool           `json:"available"`
	ReasonCode              string         `json:"reasonCode"`
}

type VPNIntentInput struct {
	DeviceID  string `json:"deviceId"`
	ProfileID string `json:"profileId"`
	Selection string `json:"selection"`
	GatewayID string `json:"gatewayId,omitempty"`
}

type VPNIntentSecret struct {
	IntentID  string    `json:"intentId"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type VPNIntent struct {
	ID                      string
	RuntimeID               string
	CredentialID            string
	SubjectID               string
	DeviceID                string
	AuthSessionID           string
	ProfileID               string
	ProfileRevision         int
	SelectionPolicyID       string
	SelectionPolicyRevision int
	Selection               string
	RequestedGatewayID      string
	TokenHash               string
	Status                  string
	ExpiresAt               time.Time
	CreatedAt               time.Time
	ConsumedAt              *time.Time
	SessionID               string
	RequestID               string
	RequestHash             string
	ManagedResult           json.RawMessage
}

type VPNCandidateDecision struct {
	GatewayID      string     `json:"gatewayId"`
	Name           string     `json:"name"`
	ProviderCode   string     `json:"providerCode"`
	Rank           int        `json:"rank"`
	Eligible       bool       `json:"eligible"`
	ReasonCode     string     `json:"reasonCode"`
	LatencyMs      *float64   `json:"latencyMs,omitempty"`
	TimeoutPercent *float64   `json:"timeoutPercent,omitempty"`
	MeasuredAt     *time.Time `json:"measuredAt,omitempty"`
}

type VPNDecision struct {
	SiteID                  string                 `json:"-"`
	NetworkSpaceID          string                 `json:"-"`
	ID                      string                 `json:"id"`
	ProfileID               string                 `json:"profileId"`
	ProfileRevision         int                    `json:"profileRevision"`
	SelectionPolicyID       string                 `json:"selectionPolicyId"`
	SelectionPolicyRevision int                    `json:"selectionPolicyRevision"`
	SubjectID               string                 `json:"subjectId"`
	DeviceID                string                 `json:"deviceId"`
	Selection               string                 `json:"selection"`
	RequestedGatewayID      string                 `json:"requestedGatewayId,omitempty"`
	EffectiveGatewayID      string                 `json:"effectiveGatewayId,omitempty"`
	Strategy                string                 `json:"strategy"`
	ReasonCode              string                 `json:"reasonCode"`
	State                   string                 `json:"state"`
	SessionID               string                 `json:"sessionId,omitempty"`
	CreatedAt               time.Time              `json:"createdAt"`
	UpdatedAt               time.Time              `json:"updatedAt"`
	Candidates              []VPNCandidateDecision `json:"candidates"`
}
