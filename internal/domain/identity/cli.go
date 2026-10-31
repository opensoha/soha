package identity

import "time"

// CLIGrant contains hashes and encrypted short-lived receipts only. Raw device
// credentials, the verification handle, user code and PKCE verifier are never persisted.
type CLIGrant struct {
	ID                    string
	DeviceHash            string
	HandleHash            string
	CodeHash              string
	CodeKeyID             string
	CodeChallenge         string
	SourceHash            string
	ClientVersion         string
	DeviceName            string
	RequestedPermissions  []string
	GrantedPermissions    []string
	State                 string
	Attempts              int
	PollInterval          int
	NextPollAt            time.Time
	ExpiresAt             time.Time
	ConfirmationHash      string
	ConfirmationUserID    string
	ConfirmationSessionID string
	ConfirmationExpiresAt time.Time
	UserID                string
	SessionID             string
	ClaimRequestHash      string
	Receipt               string
	ReceiptExpiresAt      time.Time
}

type CLIRefreshCredential struct {
	Generation       int
	ReplacementHash  string
	IssuedAt         time.Time
	ConsumedAt       *time.Time
	Hash             string
	SessionID        string
	RequestHash      string
	Receipt          string
	ReceiptExpiresAt time.Time
	ExpiresAt        time.Time
}

type CLIAuthOptions struct {
	Enabled                  bool     `json:"enabled"`
	SupportsGovernedAI       bool     `json:"supportsGovernedAI"`
	ProtocolVersion          int      `json:"protocolVersion"`
	SupportedMethods         []string `json:"supportedMethods"`
	VerificationOrigin       string   `json:"verificationOrigin,omitempty"`
	DeviceCodeExpiresIn      int      `json:"deviceCodeExpiresIn"`
	Interval                 int      `json:"interval"`
	SessionIdleExpiresIn     int      `json:"sessionIdleExpiresIn"`
	SessionAbsoluteExpiresIn int      `json:"sessionAbsoluteExpiresIn"`
}

type CLIDeviceCreate struct {
	ClientID            string   `json:"clientId"`
	ClientVersion       string   `json:"clientVersion,omitempty"`
	DeviceName          string   `json:"deviceName,omitempty"`
	CodeChallenge       string   `json:"codeChallenge"`
	CodeChallengeMethod string   `json:"codeChallengeMethod"`
	PermissionKeys      []string `json:"permissionKeys,omitempty"`
}

type CLIDeviceAuthorization struct {
	GrantID         string    `json:"grantId"`
	DeviceCode      string    `json:"deviceCode"`
	UserCode        string    `json:"userCode"`
	VerificationURI string    `json:"verificationUri"`
	ExpiresIn       int       `json:"expiresIn"`
	ExpiresAt       time.Time `json:"expiresAt"`
	Interval        int       `json:"interval"`
}

type CLIDeviceInspect struct {
	VerificationHandle string `json:"verificationHandle"`
	UserCode           string `json:"userCode"`
}

type CLIDeviceDecision struct {
	VerificationHandle string `json:"verificationHandle"`
	UserCode           string `json:"userCode"`
	ConfirmationID     string `json:"confirmationId"`
	Decision           string `json:"decision"`
}

type CLIDeviceConfirmation struct {
	GrantID                  string    `json:"grantId"`
	ConfirmationID           string    `json:"confirmationId"`
	DeviceName               string    `json:"deviceName"`
	ClientVersion            string    `json:"clientVersion"`
	User                     Principal `json:"user"`
	PermissionKeys           []string  `json:"permissionKeys"`
	ExpiresAt                time.Time `json:"expiresAt"`
	ConfirmationExpiresAt    time.Time `json:"confirmationExpiresAt"`
	SessionAbsoluteExpiresIn int       `json:"sessionAbsoluteExpiresIn"`
}

type CLIDeviceStatus struct {
	GrantID   string    `json:"grantId"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expiresAt"`
	SessionID string    `json:"sessionId,omitempty"`
}

type CLIDeviceTokenRequest struct {
	DeviceCode     string `json:"deviceCode"`
	CodeVerifier   string `json:"codeVerifier"`
	ClaimRequestID string `json:"claimRequestId"`
}

type CLIDeviceCancelRequest struct {
	DeviceCode   string `json:"deviceCode"`
	CodeVerifier string `json:"codeVerifier"`
}

type CLIRefreshRequest struct {
	RefreshToken     string `json:"refreshToken"`
	RefreshRequestID string `json:"refreshRequestId"`
}

type CLIAuthContext struct {
	GovernedAI               *GovernedAIBinding `json:"governedAI,omitempty"`
	CredentialKind           string             `json:"credentialKind"`
	SubjectType              string             `json:"subjectType"`
	SubjectID                string             `json:"subjectId"`
	DisplayName              string             `json:"displayName"`
	TokenID                  string             `json:"tokenId,omitempty"`
	SessionID                string             `json:"sessionId,omitempty"`
	PermissionKeys           []string           `json:"permissionKeys"`
	Scopes                   []string           `json:"scopes"`
	ExpiresAt                *time.Time         `json:"expiresAt,omitempty"`
	RefreshExpiresAt         *time.Time         `json:"refreshExpiresAt,omitempty"`
	SessionAbsoluteExpiresAt *time.Time         `json:"sessionAbsoluteExpiresAt,omitempty"`
}

type CLISession struct {
	ID                string    `json:"id"`
	DeviceName        string    `json:"deviceName"`
	ClientVersion     string    `json:"clientVersion"`
	CreatedAt         time.Time `json:"createdAt"`
	LastSeenAt        time.Time `json:"lastSeenAt"`
	ExpiresAt         time.Time `json:"expiresAt"`
	AbsoluteExpiresAt time.Time `json:"absoluteExpiresAt"`
	Status            string    `json:"status"`
	Current           bool      `json:"current"`
}

type CLISessionPage struct {
	Items      []CLISession `json:"items"`
	NextCursor string       `json:"nextCursor,omitempty"`
}
