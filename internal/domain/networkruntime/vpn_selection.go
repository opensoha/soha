package networkruntime

import domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"

type VPNGatewayCandidate struct {
	Gateway         domainnetworkaccess.Gateway
	Credential      Credential
	Reachable       bool
	ExecutionReady  bool
	ActiveSessions  int
	OverlayCapacity int
	ReasonCode      string
}

type ManagedVPNConnection struct {
	Intent      domainnetworkaccess.VPNIntent
	RequestID   string
	RequestHash string
	Profile     domainnetworkaccess.VPNProfileConfig
	Policy      domainnetworkaccess.VPNSelectionPolicyConfig
	Decision    domainnetworkaccess.VPNDecision
}
