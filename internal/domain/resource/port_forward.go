package resource

import "time"

// PortForwardRecord is the persistence-neutral state of a port-forward
// session. Repository implementations may store it without leaking their
// concrete record types into the application layer.
type PortForwardRecord struct {
	SessionID      string
	ClusterID      string
	Namespace      string
	TargetKind     string
	TargetName     string
	LocalPort      int
	RemotePort     int
	Status         string
	ConnectionMode string
	LastError      string
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
