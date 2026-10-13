package copilot

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

// PersonalGuestRecord grants authenticated liveness only, never environment readiness.
type PersonalGuestRecord struct {
	Binding                guest.Binding
	CertificateFingerprint string
	AccessVersion          int64
	EnrollmentID           string
	ChallengeHash          string
	EnrollmentIssuedAt     time.Time
	EnrollmentExpiresAt    time.Time
	Status                 string
	LastSequence           int64
	LastHeartbeatAt        *time.Time
}

func (r PersonalGuestRecord) Validate() error {
	if err := r.Binding.Validate(); err != nil {
		return err
	}
	id, err := uuid.Parse(r.EnrollmentID)
	if err != nil || id == uuid.Nil || id.String() != r.EnrollmentID || r.AccessVersion < 1 || r.LastSequence < 0 || r.LastSequence > guest.MaxCounter {
		return fmt.Errorf("invalid personal guest registration identity")
	}
	for _, fingerprint := range []string{r.CertificateFingerprint, r.ChallengeHash} {
		digest, err := hex.DecodeString(strings.TrimPrefix(fingerprint, "sha256:"))
		if err != nil || len(digest) != 32 || len(fingerprint) != 71 || strings.ToLower(fingerprint) != fingerprint {
			return fmt.Errorf("invalid personal guest registration fingerprint")
		}
	}
	if err := r.validateEnrollmentLifetime(); err != nil {
		return err
	}
	if r.Status != "pending" && r.Status != "registered" && r.Status != "revoked" {
		return fmt.Errorf("invalid personal guest registration status")
	}
	return nil
}

func (r PersonalGuestRecord) validateEnrollmentLifetime() error {
	lifetime := r.EnrollmentExpiresAt.Sub(r.EnrollmentIssuedAt)
	if lifetime < guest.EnrollmentMinTTL || lifetime > guest.EnrollmentMaxTTL || r.Status == "pending" && (r.EnrollmentIssuedAt.Before(r.Binding.IssuedAt.Truncate(time.Microsecond)) || r.EnrollmentExpiresAt.After(r.Binding.ExpiresAt)) {
		return fmt.Errorf("invalid personal guest enrollment lifetime")
	}
	return nil
}
