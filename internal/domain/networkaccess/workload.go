package networkaccess

import "time"

// A workload binding is an administrator's ceiling, not an access grant. Each
// connection also needs an explicit service-account policy for every resource.
type WorkloadBinding struct {
	ID               string    `json:"id"`
	ServiceAccountID string    `json:"serviceAccountId"`
	RuntimeID        string    `json:"runtimeId"`
	DeviceID         string    `json:"deviceId"`
	SiteID           string    `json:"siteId"`
	NetworkSpaceID   string    `json:"networkSpaceId"`
	ResourceIDs      []string  `json:"resourceIds"`
	Enabled          bool      `json:"enabled"`
	ExpiresAt        time.Time `json:"expiresAt"`
	Revision         int       `json:"revision"`
}

type ManagementProofInput struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// Payload is signed as the exact base64url string, with no JSON reserialization.
type ManagementProofClaims struct {
	Audience             string    `json:"audience"`
	DeviceID             string    `json:"deviceId"`
	SiteID               string    `json:"siteId"`
	CredentialGeneration int       `json:"credentialGeneration"`
	PublicKeyFingerprint string    `json:"publicKeyFingerprint"`
	Sequence             int64     `json:"sequence"`
	ManagementState      string    `json:"managementState"`
	ObservedAt           time.Time `json:"observedAt"`
	ExpiresAt            time.Time `json:"expiresAt"`
}
