package networkprotocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const (
	RuntimeSchemaVersion          = "network-runtime/v1alpha1"
	IngestSchemaVersion           = "network-ingest/v1alpha1"
	DefaultRuntimeIntervalSeconds = 60
	MinRuntimeIntervalSeconds     = 30
	MaxRuntimeIntervalSeconds     = 300

	MessageEnrollmentRequest       = "runtime.enroll.request"
	MessageEnrollmentResult        = "runtime.enroll.result"
	MessageConfiguration           = "configuration.desired"
	MessageConfigurationApply      = "configuration.applied"
	MessageLeaseRenewRequest       = "lease.renew.request"
	MessageLeaseRenewResult        = "lease.renew.result"
	MessageLeaseRevoke             = "lease.revoke"
	MessageVPNConnectRequest       = "vpn.connect.request"
	MessageVPNConnectResult        = "vpn.connect.result"
	MessageNASAuthorizationRequest = "nas.authorization.request"
	MessageNASAuthorizationResult  = "nas.authorization.result"
	MessageNASSessionCommand       = "nas.session.command"
	MessageNASSessionCommandResult = "nas.session.command.result"

	NASAuthenticationEAPTLS             = "eap-tls"
	NASAuthenticationPasswordCompatible = "password-compatible"

	EventHeartbeat          = "runtime.heartbeat"
	EventRadiusAccounting   = "radius.accounting"
	EventFlowAggregate      = "network.flow.aggregate"
	EventConnectionSummary  = "network.connection.summary"
	EventProxyFlowAggregate = "proxy.flow.aggregate"
)

const RadiusAccountingSchemaVersion = "network-radius-accounting/v1alpha1"

type RuntimeMessage struct {
	SchemaVersion string          `json:"schemaVersion"`
	MessageID     string          `json:"messageId"`
	MessageType   string          `json:"messageType"`
	ProducerID    string          `json:"producerId"`
	RuntimeID     string          `json:"runtimeId"`
	RuntimeKind   string          `json:"runtimeKind"`
	OccurredAt    time.Time       `json:"occurredAt"`
	ExpiresAt     time.Time       `json:"expiresAt"`
	Payload       json.RawMessage `json:"payload"`
}

type EnrollmentRequest struct {
	EnrollmentID       string   `json:"enrollmentId"`
	ChallengeID        string   `json:"challengeId"`
	DeviceID           string   `json:"deviceId"`
	DevicePublicKey    string   `json:"devicePublicKey"`
	WireGuardPublicKey string   `json:"wireguardPublicKey,omitempty"`
	Platform           string   `json:"platform"`
	ClientVersion      string   `json:"clientVersion"`
	Capabilities       []string `json:"capabilities"`
}

type EnrollmentResult struct {
	Accepted            bool       `json:"accepted"`
	ReasonCode          string     `json:"reasonCode"`
	CredentialReference string     `json:"credentialReference,omitempty"`
	CredentialExpiresAt *time.Time `json:"credentialExpiresAt,omitempty"`
}

type NetworkLease struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"sessionId"`
	SubjectID      string    `json:"subjectId"`
	DeviceID       string    `json:"deviceId"`
	NetworkSpaceID string    `json:"networkSpaceId"`
	CIDRs          []string  `json:"cidrs"`
	PolicyVersion  int       `json:"policyVersion"`
	IssuedAt       time.Time `json:"issuedAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

type ResourceLease struct {
	ID            string    `json:"id"`
	SessionID     string    `json:"sessionId"`
	SubjectID     string    `json:"subjectId"`
	DeviceID      string    `json:"deviceId"`
	ResourceIDs   []string  `json:"resourceIds"`
	PolicyVersion int       `json:"policyVersion"`
	IssuedAt      time.Time `json:"issuedAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type ConfigurationDesired struct {
	ConfigurationVersion int                     `json:"configurationVersion"`
	PolicyVersion        int                     `json:"policyVersion"`
	ValidUntil           time.Time               `json:"validUntil"`
	AccessProfile        string                  `json:"accessProfile"`
	ProtectedResourceIDs []string                `json:"protectedResourceIds"`
	NetworkLeases        []NetworkLease          `json:"networkLeases"`
	ResourceLeases       []ResourceLease         `json:"resourceLeases"`
	RuntimeIntervals     *RuntimeIntervals       `json:"runtimeIntervals,omitempty"`
	WireGuard            *WireGuardConfiguration `json:"wireguard,omitempty"`
	Mihomo               *MihomoConfiguration    `json:"mihomo,omitempty"`
	VPNProbe             *VPNProbeConfiguration  `json:"vpnProbe,omitempty"`
}

type RuntimeIntervals struct {
	HeartbeatIntervalSeconds         int `json:"heartbeatIntervalSeconds"`
	ConfigurationPollIntervalSeconds int `json:"configurationPollIntervalSeconds"`
}

type MihomoConfiguration struct {
	Mode            string   `json:"mode"`
	SourceType      string   `json:"sourceType,omitempty"`
	ProfileID       string   `json:"profileId"`
	ProfileRevision int      `json:"profileRevision"`
	MixedPort       int      `json:"mixedPort"`
	ControllerPort  int      `json:"controllerPort"`
	DNSMode         string   `json:"dnsMode"`
	FakeIPRange     string   `json:"fakeIpRange,omitempty"`
	SelectorGroup   string   `json:"selectorGroup"`
	SelectedProxy   string   `json:"selectedProxy,omitempty"`
	BypassCIDRs     []string `json:"bypassCidrs"`
	BypassHosts     []string `json:"bypassHosts"`
	FailClosed      bool     `json:"failClosed"`
}

type MihomoManualNode struct {
	Protocol string `json:"protocol"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type MihomoSource struct {
	ProfileID       string            `json:"profileId"`
	ProfileRevision int               `json:"profileRevision"`
	SourceType      string            `json:"sourceType"`
	SubscriptionURL string            `json:"subscriptionUrl,omitempty"`
	ManualNode      *MihomoManualNode `json:"manualNode,omitempty"`
}

type MihomoSubscription struct {
	ProfileID       string `json:"profileId"`
	ProfileRevision int    `json:"profileRevision"`
	SubscriptionURL string `json:"subscriptionUrl"`
}

type WireGuardPeer struct {
	SessionID                  string   `json:"sessionId,omitempty"`
	VPNProfileID               string   `json:"vpnProfileId,omitempty"`
	RuntimeID                  string   `json:"runtimeId"`
	DeviceID                   string   `json:"deviceId,omitempty"`
	PublicKey                  string   `json:"publicKey"`
	EndpointHost               string   `json:"endpointHost,omitempty"`
	EndpointPort               int      `json:"endpointPort,omitempty"`
	AllowedIPs                 []string `json:"allowedIPs"`
	PersistentKeepaliveSeconds int      `json:"persistentKeepaliveSeconds"`
}

type WireGuardFirewallRule struct {
	ID                string     `json:"id"`
	Effect            string     `json:"effect"`
	LeaseID           string     `json:"leaseId,omitempty"`
	SiteLinkRuntimeID string     `json:"siteLinkRuntimeId,omitempty"`
	Direction         string     `json:"direction,omitempty"`
	SourceCIDR        string     `json:"sourceCidr"`
	DestinationCIDR   string     `json:"destinationCidr"`
	Protocol          string     `json:"protocol"`
	Ports             []int      `json:"ports,omitempty"`
	ExpiresAt         *time.Time `json:"expiresAt,omitempty"`
}

type WireGuardConfiguration struct {
	Role            string                  `json:"role"`
	InterfaceName   string                  `json:"interfaceName"`
	PublicKey       string                  `json:"publicKey"`
	Addresses       []string                `json:"addresses"`
	ListenPort      int                     `json:"listenPort,omitempty"`
	MTU             int                     `json:"mtu"`
	RoutingMode     string                  `json:"routingMode"`
	FirewallDefault string                  `json:"firewallDefault"`
	Peers           []WireGuardPeer         `json:"peers"`
	Routes          []string                `json:"routes"`
	DNSServers      []string                `json:"dnsServers,omitempty"`
	FirewallRules   []WireGuardFirewallRule `json:"firewallRules"`
}

type ConfigurationApplied struct {
	ConfigurationVersion int    `json:"configurationVersion"`
	PolicyVersion        int    `json:"policyVersion"`
	Status               string `json:"status"`
	ReadbackHash         string `json:"readbackHash"`
	ReasonCode           string `json:"reasonCode,omitempty"`
}

type LeaseRenewRequest struct {
	SessionID                    string   `json:"sessionId"`
	LeaseIDs                     []string `json:"leaseIds"`
	ObservedConfigurationVersion int      `json:"observedConfigurationVersion"`
	PostureVersion               *int     `json:"postureVersion,omitempty"`
}

type LeaseRenewResult struct {
	PolicyVersion   int             `json:"policyVersion"`
	ValidUntil      time.Time       `json:"validUntil"`
	NetworkLeases   []NetworkLease  `json:"networkLeases"`
	ResourceLeases  []ResourceLease `json:"resourceLeases"`
	RevokedLeaseIDs []string        `json:"revokedLeaseIds"`
}

type LeaseRevoke struct {
	SessionID   string    `json:"sessionId"`
	LeaseIDs    []string  `json:"leaseIds"`
	ReasonCode  string    `json:"reasonCode"`
	EffectiveAt time.Time `json:"effectiveAt"`
}

type VPNConnectRequest struct {
	RequestID        string   `json:"requestId"`
	SiteID           string   `json:"siteId"`
	NetworkSpaceID   string   `json:"networkSpaceId"`
	GatewayID        string   `json:"gatewayId,omitempty"`
	Mode             string   `json:"mode"`
	ResourceIDs      []string `json:"resourceIds,omitempty"`
	AccessGrantID    string   `json:"accessGrantId,omitempty"`
	AccessGrantToken string   `json:"accessGrantToken,omitempty"`
}

type VPNConnectResult struct {
	RequestID            string          `json:"requestId"`
	Decision             string          `json:"decision"`
	ReasonCode           string          `json:"reasonCode"`
	SessionID            string          `json:"sessionId,omitempty"`
	GatewayID            string          `json:"gatewayId,omitempty"`
	ConfigurationVersion int             `json:"configurationVersion,omitempty"`
	PolicyVersion        int             `json:"policyVersion"`
	ValidUntil           time.Time       `json:"validUntil"`
	NetworkLeases        []NetworkLease  `json:"networkLeases"`
	ResourceLeases       []ResourceLease `json:"resourceLeases"`
}

type RadiusAttributes struct {
	VLANID                int    `json:"vlanId,omitempty"`
	FilterID              string `json:"filterId,omitempty"`
	SessionTimeoutSeconds int    `json:"sessionTimeoutSeconds"`
}

type NASAuthorizationRequest struct {
	RequestID                       string `json:"requestId"`
	NASID                           string `json:"nasId"`
	SubjectID                       string `json:"subjectId"`
	DeviceID                        string `json:"deviceId,omitempty"`
	StationID                       string `json:"stationId,omitempty"`
	AuthenticationMethod            string `json:"authenticationMethod"`
	ClientCertificateSerial         string `json:"clientCertificateSerial,omitempty"`
	ClientCertificateAuthorityKeyID string `json:"clientCertificateAuthorityKeyId,omitempty"`
}

type NASAuthorizationResult struct {
	RequestID        string            `json:"requestId"`
	SessionID        string            `json:"sessionId"`
	Decision         string            `json:"decision"`
	AccessProfile    string            `json:"accessProfile"`
	PolicyVersion    int               `json:"policyVersion"`
	ValidUntil       time.Time         `json:"validUntil"`
	ReasonCode       string            `json:"reasonCode"`
	RadiusAttributes *RadiusAttributes `json:"radiusAttributes,omitempty"`
}

type NASSessionCommand struct {
	CommandID           string            `json:"commandId"`
	SessionID           string            `json:"sessionId"`
	NASID               string            `json:"nasId"`
	SubjectID           string            `json:"subjectId"`
	DeviceID            string            `json:"deviceId"`
	Action              string            `json:"action"`
	TargetAccessProfile string            `json:"targetAccessProfile"`
	PolicyVersion       int               `json:"policyVersion"`
	EffectiveAt         time.Time         `json:"effectiveAt"`
	ReasonCode          string            `json:"reasonCode"`
	RadiusAttributes    *RadiusAttributes `json:"radiusAttributes,omitempty"`
}

type NASSessionCommandResult struct {
	CommandID   string    `json:"commandId"`
	SessionID   string    `json:"sessionId"`
	Status      string    `json:"status"`
	ReasonCode  string    `json:"reasonCode"`
	CompletedAt time.Time `json:"completedAt"`
}

type IngestBatch struct {
	SchemaVersion string        `json:"schemaVersion"`
	BatchID       string        `json:"batchId"`
	ProducerID    string        `json:"producerId"`
	ProducerKind  string        `json:"producerKind"`
	SentAt        time.Time     `json:"sentAt"`
	Events        []IngestEvent `json:"events"`
}

type IngestEvent struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Sequence   int64           `json:"sequence"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}

type RadiusAccounting struct {
	StatusType          string `json:"statusType"`
	AccountingSessionID string `json:"accountingSessionId"`
	SessionID           string `json:"sessionId,omitempty"`
	NASID               string `json:"nasId"`
	NASPortID           string `json:"nasPortId,omitempty"`
	UserID              string `json:"userId,omitempty"`
	DeviceID            string `json:"deviceId,omitempty"`
	SessionTimeSeconds  int64  `json:"sessionTimeSeconds"`
	InputOctets         int64  `json:"inputOctets,omitempty"`
	OutputOctets        int64  `json:"outputOctets,omitempty"`
	InputPackets        int64  `json:"inputPackets,omitempty"`
	OutputPackets       int64  `json:"outputPackets,omitempty"`
	TerminationCause    string `json:"terminationCause,omitempty"`
}

type RadiusAccountingRequest struct {
	SchemaVersion  string `json:"schemaVersion"`
	EventTimestamp int64  `json:"eventTimestamp"`
	RadiusAccounting
}

type FlowAggregate struct {
	WindowStartedAt   time.Time `json:"windowStartedAt"`
	WindowEndedAt     time.Time `json:"windowEndedAt"`
	Connections       int64     `json:"connections"`
	DeniedConnections int64     `json:"deniedConnections"`
}

type ProxyFlowAggregate struct {
	WindowStartedAt   time.Time `json:"windowStartedAt"`
	WindowEndedAt     time.Time `json:"windowEndedAt"`
	Engine            string    `json:"engine"`
	ProfileID         string    `json:"profileId"`
	ProfileRevision   int       `json:"profileRevision"`
	Mode              string    `json:"mode"`
	SelectedProxy     string    `json:"selectedProxy"`
	UploadBytes       int64     `json:"uploadBytes"`
	DownloadBytes     int64     `json:"downloadBytes"`
	ActiveConnections int64     `json:"activeConnections"`
}

func DecodeRuntimeMessage(raw []byte) (RuntimeMessage, error) {
	var message RuntimeMessage
	err := json.Unmarshal(raw, &message)
	return message, err
}

func DecodeIngestBatch(raw []byte) (IngestBatch, error) {
	var batch IngestBatch
	err := json.Unmarshal(raw, &batch)
	return batch, err
}

func DecodePayload[T any](raw json.RawMessage) (T, error) {
	var payload T
	err := json.Unmarshal(raw, &payload)
	return payload, err
}

func ValidateRuntimeMessageWindow(message RuntimeMessage, now time.Time, maxClockSkew time.Duration) error {
	if message.OccurredAt.After(now.Add(maxClockSkew)) || message.OccurredAt.Before(now.Add(-maxClockSkew)) {
		return fmt.Errorf("runtime message occurredAt is outside the accepted clock window")
	}
	if !message.ExpiresAt.After(message.OccurredAt) || !message.ExpiresAt.After(now) {
		return fmt.Errorf("runtime message has expired or an invalid validity window")
	}
	return nil
}

func ValidateIngestBatch(batch IngestBatch, now time.Time, maxClockSkew, retention time.Duration, maxEvents int) error {
	if len(batch.Events) == 0 || len(batch.Events) > maxEvents {
		return fmt.Errorf("ingest batch event count must be between 1 and %d", maxEvents)
	}
	if batch.SentAt.After(now.Add(maxClockSkew)) || batch.SentAt.Before(now.Add(-maxClockSkew)) {
		return fmt.Errorf("ingest batch sentAt is outside the accepted clock window")
	}
	seen := make(map[string]struct{}, len(batch.Events))
	for index, event := range batch.Events {
		if _, exists := seen[event.ID]; exists {
			return fmt.Errorf("ingest event %q is duplicate", event.ID)
		}
		seen[event.ID] = struct{}{}
		if err := validateIngestEvent(batch.ProducerKind, event, index, now, maxClockSkew, retention); err != nil {
			return err
		}
	}
	return nil
}

func validateIngestEvent(producerKind string, event IngestEvent, index int, now time.Time, maxClockSkew, retention time.Duration) error {
	if event.OccurredAt.After(now.Add(maxClockSkew)) {
		return fmt.Errorf("ingest event %q occurred in the future", event.ID)
	}
	if event.OccurredAt.Before(now.Add(-retention)) {
		return fmt.Errorf("ingest event %q is older than retention", event.ID)
	}
	if !eventAllowed(producerKind, event.Type) {
		return fmt.Errorf("ingest event %q type %q is not allowed for producer kind %q", event.ID, event.Type, producerKind)
	}
	switch event.Type {
	case EventFlowAggregate:
		return validateFlowAggregate(event, index)
	case EventProxyFlowAggregate:
		return validateProxyFlowAggregate(event, index)
	case EventVPNProbeBatch:
		return validateVPNProbeBatch(event)
	case EventVPNTunnelStats:
		return validateVPNTunnelStats(event)
	case EventVPNGatewayHealth:
		return validateVPNGatewayHealth(event)
	default:
		return nil
	}
}

func validateFlowAggregate(event IngestEvent, index int) error {
	var payload FlowAggregate
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode flow event %d: %w", index, err)
	}
	if payload.WindowEndedAt.Before(payload.WindowStartedAt) {
		return fmt.Errorf("flow event %q has an invalid window", event.ID)
	}
	if payload.DeniedConnections > payload.Connections {
		return fmt.Errorf("flow event %q denied connections exceed total connections", event.ID)
	}
	return nil
}

func validateProxyFlowAggregate(event IngestEvent, index int) error {
	var payload ProxyFlowAggregate
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode proxy flow event %d: %w", index, err)
	}
	if payload.WindowEndedAt.Before(payload.WindowStartedAt) || payload.WindowEndedAt.After(event.OccurredAt) || payload.Engine != "mihomo" || payload.ProfileRevision < 1 {
		return fmt.Errorf("proxy flow event %q has invalid aggregate semantics", event.ID)
	}
	if payload.UploadBytes < 0 || payload.DownloadBytes < 0 || payload.ActiveConnections < 0 {
		return fmt.Errorf("proxy flow event %q has invalid aggregate semantics", event.ID)
	}
	return nil
}

func EventHash(raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", fmt.Errorf("event contains trailing JSON")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return fmt.Sprintf("sha256:%x", digest), nil
}

func eventAllowed(producerKind, eventType string) bool {
	switch producerKind {
	case "endpoint":
		return eventType == EventHeartbeat || eventType == EventConnectionSummary || eventType == EventProxyFlowAggregate || eventType == EventVPNProbeBatch
	case "gateway":
		return eventType == EventHeartbeat || eventType == EventFlowAggregate || eventType == EventConnectionSummary || eventType == EventVPNTunnelStats || eventType == EventVPNGatewayHealth
	case "freeradius":
		return eventType == EventRadiusAccounting
	case "network-control":
		return eventType == EventHeartbeat
	default:
		return false
	}
}
