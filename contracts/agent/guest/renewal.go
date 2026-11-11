package guest

import (
	"errors"
	"time"
)

const RenewalVersion = "opensoha.dev/personal-guest-renewal/v1"

// Renewal is authenticated with the current certificate. Core derives every
// identity field; requestId makes a lost issuance response recoverable.
type Renewal struct {
	SchemaVersion  string `json:"schemaVersion"`
	RequestID      string `json:"requestId"`
	BindingVersion int64  `json:"bindingVersion"`
	CSRPEM         string `json:"csrPEM"`
}

type RenewalResult struct {
	RequestID      string    `json:"requestId"`
	Binding        Binding   `json:"binding"`
	CertificatePEM string    `json:"certificatePEM"`
	ClientCAPEM    string    `json:"clientCAPEM"`
	CommitBefore   time.Time `json:"commitBefore"`
}

func (r Renewal) Validate() error {
	if r.SchemaVersion != RenewalVersion || !uuidPattern.MatchString(r.RequestID) || r.BindingVersion < 1 || r.BindingVersion >= MaxCounter || len(r.CSRPEM) == 0 || len(r.CSRPEM) > 2048 {
		return errors.New("invalid personal guest renewal")
	}
	return nil
}

func (r *Renewal) UnmarshalJSON(data []byte) error {
	type wire Renewal
	var value wire
	if err := decodeStrict(data, &value, "schemaVersion", "requestId", "bindingVersion", "csrPEM"); err != nil {
		return err
	}
	decoded := Renewal(value)
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// SameWorkspace excludes only registration and lease fields. It is not an
// authorization check and must never authorize a disk format by itself.
func (b Binding) SameWorkspace(other Binding) bool {
	b.ID, other.ID = "", ""
	b.Version, other.Version = 0, 0
	b.IssuedAt = other.IssuedAt
	b.ExpiresAt = other.ExpiresAt
	return b == other
}
