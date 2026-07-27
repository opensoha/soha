package identitymfa

import "time"

const (
	CredentialTypeTOTP     = "totp"
	CredentialTypeWebAuthn = "webauthn"

	ChallengeTypeTOTPEnrollment         = "totp_enrollment"
	ChallengeTypeTOTPStepUp             = "totp_step_up"
	ChallengeTypeRecoveryCode           = "recovery_code"
	ChallengeTypeWebAuthnEnrollment     = "webauthn_enrollment"
	ChallengeTypeWebAuthnAuthentication = "webauthn_authentication"
)

type Credential struct {
	ID               string
	UserID           string
	Type             string
	DisplayName      string
	ExternalID       string
	SecretCiphertext string
	SignCount        uint32
	CreatedAt        time.Time
	LastUsedAt       *time.Time
	RevokedAt        *time.Time
}

type Challenge struct {
	ID               string
	UserID           string
	SessionID        string
	Type             string
	SecretCiphertext string
	Attempts         int
	MaxAttempts      int
	ExpiresAt        time.Time
	ConsumedAt       *time.Time
	CreatedAt        time.Time
}

type RecoveryCode struct {
	ID        string
	UserID    string
	CodeHash  string
	CreatedAt time.Time
	UsedAt    *time.Time
}

type AdminResetCounts struct {
	RevokedCredentials   int
	RevokedRecoveryCodes int
	RevokedChallenges    int
	RevokedSessions      int
}

type WebAuthnUser struct {
	ID                []byte
	Name              string
	DisplayName       string
	Credentials       [][]byte
	CredentialRecords []WebAuthnCredential
}

type WebAuthnRegistration struct {
	Challenge            string
	RPID                 string
	RPName               string
	UserID               string
	UserName             string
	Algorithms           []int
	ExcludeCredentialIDs []string
	Timeout              time.Duration
	Session              []byte
}

type WebAuthnResponse struct {
	CredentialID      string
	ClientDataJSON    string
	AttestationObject string
	AuthenticatorData string
	Signature         string
	UserHandle        string
}

type WebAuthnCredential struct {
	ExternalID string
	Data       []byte
	SignCount  uint32
}

type WebAuthnAuthentication struct {
	Challenge          string
	RPID               string
	AllowCredentialIDs []string
	UserVerification   string
	Timeout            time.Duration
	Session            []byte
}
