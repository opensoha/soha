package proxypublication

import (
	"fmt"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"time"
)

const ConverterVersion = "proxy-ir/v1"
const MaxNodes = 128
const MaxSourceBytes = 1 << 20

// ConversionError deliberately excludes parser input and vendor error strings.
type ConversionError struct {
	Code     string `json:"code"`
	Position string `json:"position"`
}

func (e *ConversionError) Error() string { return fmt.Sprintf("%s at %s", e.Code, e.Position) }
func Invalid(code, position string) error {
	switch code {
	case "node_limit", "source_limit", "rule_limit":
		code = "quota_exceeded"
	case "invalid_target", "rule_target_scope":
		code = "dangling_reference"
	case "unsupported_transport", "invalid_transport":
		code = "incompatible_transport"
	case "unsupported_protocol":
	case "unsupported_tls", "invalid_tls", "direct_protection_unproven":
		code = "unsafe_field"
	case "source_denied":
		code = "protected_destination"
	case "invalid_or_unknown_field", "unsupported_field", "unsupported_format", "unsupported_cipher", "unsupported_group", "unsupported_matcher", "unsupported_action", "unsupported_encryption", "unsupported_vmess", "unsupported_user", "unsupported_servers", "unsupported_delivery_mode":
		code = "unsupported_field"
	default:
		code = "invalid_source"
	}
	return &ConversionError{Code: code, Position: position}
}

// IR is private server material. It must never be used as an employee response.
type IR struct {
	Nodes  []api.NetworkProxyNode
	Groups []api.NetworkProxyGroup
	Rules  []api.NetworkProxyRule
}
type SourceRecord struct {
	InstanceID         string
	Public             api.NetworkProxySource
	EncryptedInput     string
	EncryptedIR        string
	NodeMaterial       map[string]string
	Deleted            bool
	CompatibilityFloor int64
}
type Credential struct {
	RenewalKey                                                                                           string
	ID, UserID, DeviceID, ProfileID, RuntimeID, BundleID, InstanceID, EndpointCredentialID, ProcessEpoch string
	NodeIDs                                                                                              []string
	Epoch, Revision                                                                                      int64
	EncryptedSecret                                                                                      string
	SecretHash                                                                                           string
	ExpiresAt                                                                                            time.Time
}

// EndpointContext contains only the result of current enrolled-device authorization.
type EndpointContext struct {
	ConfigurationValidUntil                                                      time.Time
	UserID, DeviceID, RuntimeID, CredentialID, ProfileID, BundleID, DeliveryMode string
	OrgRevision, SourceRevision                                                  int64
	ExpiresAt                                                                    time.Time
}

type PoolMember struct {
	RuntimeID        string
	ExpectedRevision int64
	Running          bool
	// Serving is current observed eligibility, independent of desired replica count.
	Serving bool
	// ServingUntil is an optional trusted drain bound, never a caller supplied TTL.
	ServingUntil *time.Time
}
type ApprovedPool struct {
	MasterID      string
	OwnerEpoch    int64
	MemberSetHash string
	Members       []PoolMember
	Endpoints     []api.NetworkProxyEmployeeEndpoint
}
type BundleRecord struct {
	Version                                                           int64
	InstanceID, RequestID, IntentHash, EncryptedIR, ActivationRequest string
	Public                                                            api.NetworkProxyBundle
	Pool                                                              ApprovedPool
	Scope                                                             api.NetworkProxyScope
	Groups                                                            []api.NetworkProxyGroup
	Rules                                                             []api.NetworkProxyRule
	SourceNodeIDs                                                     []string
	Nodes                                                             []string
	Outbounds                                                         map[string]string
	Readbacks                                                         map[string]api.NetworkProxyBundleReadback
}

func (r BundleRecord) Publication(until time.Time) api.NetworkProxyServerPublication {
	out := api.NetworkProxyServerPublication{BundleID: r.Public.BundleID, Revision: r.Public.Revision, ConverterVersion: r.Public.ConverterVersion, CredentialEpoch: r.Public.CredentialEpoch, DeliveryMode: api.NetworkProxyServerPublicationDeliveryMode(r.Public.DeliveryMode), MemberSetHash: r.Pool.MemberSetHash, OrgRevision: r.Public.OrgRevision, ServerHash: r.Public.ServerHash, SourceRevision: r.Public.SourceRevision, ValidUntil: until, Groups: r.Groups, Rules: r.Rules, Nodes: []api.NetworkProxyServerPublicationNode{}}
	for _, node := range r.Nodes {
		out.Nodes = append(out.Nodes, api.NetworkProxyServerPublicationNode{NodeID: node, OutboundID: r.Outbounds[node]})
	}
	return out
}
