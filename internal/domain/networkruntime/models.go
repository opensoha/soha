package networkruntime

import (
	"encoding/json"
	"time"

	"github.com/opensoha/soha/internal/networkprotocol"
)

const (
	EnrollmentPending  = "pending"
	EnrollmentConsumed = "consumed"
	EnrollmentRevoked  = "revoked"

	CredentialActive  = "active"
	CredentialRevoked = "revoked"
	CredentialExpired = "expired"

	AccessGrantIssued   = "issued"
	AccessGrantConsumed = "consumed"
	AccessGrantRevoked  = "revoked"
	AccessGrantExpired  = "expired"
)

type EnrollmentChallenge struct {
	ID            string     `json:"id"`
	ChallengeID   string     `json:"challengeId"`
	RuntimeID     string     `json:"runtimeId"`
	RuntimeKind   string     `json:"runtimeKind"`
	DeviceID      string     `json:"deviceId"`
	SubjectID     string     `json:"subjectId"`
	Status        string     `json:"status"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	CreatedBy     string     `json:"createdBy"`
	CreatedAt     time.Time  `json:"createdAt"`
	ConsumedAt    *time.Time `json:"consumedAt,omitempty"`
	RevokedAt     *time.Time `json:"revokedAt,omitempty"`
	ChallengeHash string     `json:"-"`
}

type EnrollmentChallengeInput struct {
	RuntimeID   string
	RuntimeKind string
	DeviceID    string
	SubjectID   string
	TTL         time.Duration
}

type EnrollmentSecret struct {
	EnrollmentChallenge
	Token string `json:"token"`
}

type AccessGrant struct {
	ID               string     `json:"id"`
	SubjectID        string     `json:"subjectId"`
	AuthSessionID    string     `json:"-"`
	DeviceID         string     `json:"deviceId"`
	SiteID           string     `json:"siteId"`
	NetworkSpaceID   string     `json:"networkSpaceId"`
	Mode             string     `json:"mode"`
	ResourceIDs      []string   `json:"resourceIds"`
	PolicyVersion    int        `json:"policyVersion"`
	Status           string     `json:"status"`
	TokenHash        string     `json:"-"`
	SessionID        string     `json:"sessionId,omitempty"`
	ResourceLeaseIDs []string   `json:"resourceLeaseIds,omitempty"`
	ReasonCode       string     `json:"reasonCode,omitempty"`
	ExpiresAt        time.Time  `json:"expiresAt"`
	ConsumedAt       *time.Time `json:"consumedAt,omitempty"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
	CreatedBy        string     `json:"createdBy"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"-"`
}

type AccessGrantFilter struct {
	SubjectID string
	DeviceID  string
	Status    string
	Limit     int
}

type AccessGrantSecret struct {
	Grant AccessGrant `json:"grant"`
	Token string      `json:"token"`
}

type Credential struct {
	ID                        string
	EnrollmentID              string
	RuntimeID                 string
	RuntimeKind               string
	DeviceID                  string
	SubjectID                 string
	CertificateFingerprint    string
	PublicKeyFingerprint      string
	WireGuardPublicKey        string
	CertificateSerial         string
	CertificateAuthorityKeyID string
	Generation                int
	Capabilities              []string
	Status                    string
	NotBefore                 time.Time
	ExpiresAt                 time.Time
	RevokedAt                 *time.Time
	LastControlSeenAt         *time.Time
	CreatedAt                 time.Time
}

type EnrollmentConsumption struct {
	EnrollmentID              string
	ChallengeID               string
	TokenHash                 string
	RuntimeID                 string
	RuntimeKind               string
	DeviceID                  string
	CertificateFingerprint    string
	PublicKeyFingerprint      string
	WireGuardPublicKey        string
	CertificateSerial         string
	CertificateAuthorityKeyID string
	Capabilities              []string
	NotBefore                 time.Time
	ExpiresAt                 time.Time
	ConsumedAt                time.Time
}

type PolicySnapshot struct {
	PolicyVersion        int
	ContentHash          string
	Policies             json.RawMessage
	ProtectedResourceIDs []string
	PublishedAt          time.Time
}

type Configuration struct {
	RuntimeID            string
	ConfigurationVersion int
	PolicyVersion        int
	Desired              networkprotocol.ConfigurationDesired
	DesiredHash          string
	ValidUntil           time.Time
	ApplyStatus          string
	ReadbackHash         string
	ReasonCode           string
	AppliedAt            *time.Time
	CreatedAt            time.Time
}

type Session struct {
	ID                   string
	RuntimeID            string
	SubjectID            string
	DeviceID             string
	SiteID               string
	Mode                 string
	AccessProfile        string
	Status               string
	PolicyVersion        int
	ConfigurationVersion int
	PostureVersion       int
	ValidUntil           time.Time
	LastControlSeenAt    *time.Time
	RevokedAt            *time.Time
	RevokeReason         string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type NASAuthorization struct {
	RequestID            string
	RequestHash          string
	RuntimeID            string
	NASID                string
	SubjectID            string
	DeviceID             string
	AuthenticationMethod string
	SessionID            string
	Decision             string
	AccessProfile        string
	PolicyVersion        int
	ReasonCode           string
	RadiusAttributes     *networkprotocol.RadiusAttributes
	ValidUntil           time.Time
	CreatedAt            time.Time
}

type VPNConnection struct {
	RequestID            string
	RequestHash          string
	RuntimeID            string
	CredentialID         string
	SubjectID            string
	DeviceID             string
	SiteID               string
	NetworkSpaceID       string
	ResourceIDs          []string
	AccessGrantID        string
	AccessGrantTokenHash string
	Mode                 string
	Decision             string
	AccessProfile        string
	PolicyVersion        int
	PostureVersion       int
	ReasonCode           string
	SessionID            string
	GatewayID            string
	GatewayRuntimeID     string
	GatewayCredentialID  string
	EndpointPublicKey    string
	ValidUntil           time.Time
	CreatedAt            time.Time
}
