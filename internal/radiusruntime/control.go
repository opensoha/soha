package radiusruntime

import (
	"context"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

// Lease is private supervisor input. The C1 adapter maps generated contracts to
// this value; it is not a second public wire DTO.
type Lease struct {
	Configuration         api.NetworkRADIUSConfiguration
	Control               Control
	ConfigurationSHA256   string
	CertificateValidUntil time.Time
	HardUntil             time.Time
	AcceptNew             bool
	NASID                 string
	RuntimeProof          string
	RunProcess            bool
}

type ControlSource interface {
	Fetch(context.Context, string) (Lease, error)
}

type Observation struct {
	Epoch               string
	PID                 int
	StartToken          string
	ConfigurationSHA256 string
	Version             string
	ObservedNASUse      bool
	HooksReady          bool
	ProcessStopped      bool
	State               Snapshot
	Reason              string
}

type Observer interface {
	Record(context.Context, Observation) error
}
