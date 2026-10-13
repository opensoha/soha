package copilot

import (
	"errors"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"time"
)

var ErrPersonalGuestRenewalExpired = errors.New("personal guest pending renewal expired")

type PersonalGuestRenewal struct {
	Result         guest.RenewalResult
	OldFingerprint string
	NewFingerprint string
	CSRHash        string
	CommitBefore   time.Time
	Committed      bool
}
