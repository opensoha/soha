package networkruntime

import "time"

// PasswordReceipt contains no password or reusable credential. TokenHash is
// SHA-256 of the random, short-lived opaque proof held by the NAS.
type PasswordReceipt struct {
	TokenHash              string
	RuntimeID              string
	NASID                  string
	SiteID                 string
	StationID              string
	SubjectID              string
	PasswordUpdatedAt      time.Time
	CreatedAt              time.Time
	ExpiresAt              time.Time
	AuthorizationRequestID string
}

type PasswordAttempt struct {
	ID        string
	RuntimeID string
	NASID     string
	Source    string
	SubjectID string
	Now       time.Time
}
