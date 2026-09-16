package networkprotocol

import (
	"fmt"
	"math"
	"time"
)

const (
	CapabilityManagedVPN            = "managed-vpn-v1"
	CapabilityVPNProbe              = "vpn-probe-v1"
	CapabilityVPNMetrics            = "vpn-metrics-v1"
	MessageVPNManagedPrepareRequest = "vpn.managed.prepare.request"
	MessageVPNManagedPrepareResult  = "vpn.managed.prepare.result"
	MessageVPNManagedConnectRequest = "vpn.managed.connect.request"
	MessageVPNManagedConnectResult  = "vpn.managed.connect.result"
	EventVPNProbeBatch              = "vpn.probe.batch"
	EventVPNTunnelStats             = "vpn.tunnel.stats"
	EventVPNGatewayHealth           = "vpn.gateway.health"
)

type VPNManagedConnectRequest struct {
	RequestID    string `json:"requestId"`
	IntentID     string `json:"intentId"`
	IntentToken  string `json:"intentToken"`
	ProbeBatchID string `json:"probeBatchId,omitempty"`
}

type VPNProbeDescriptor struct {
	GatewayID    string     `json:"gatewayId"`
	RuntimeID    string     `json:"runtimeId"`
	Name         string     `json:"name"`
	ProviderCode string     `json:"providerCode"`
	URL          string     `json:"url,omitempty"`
	Token        string     `json:"token,omitempty"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
}

type VPNManagedPrepareResult struct {
	RequestID               string               `json:"requestId"`
	IntentID                string               `json:"intentId"`
	ProfileID               string               `json:"profileId"`
	ProfileRevision         int                  `json:"profileRevision"`
	SelectionPolicyRevision int                  `json:"selectionPolicyRevision"`
	ExpiresAt               time.Time            `json:"expiresAt"`
	SamplesPerGateway       int                  `json:"samplesPerGateway"`
	TimeoutMillis           int                  `json:"timeoutMillis"`
	MaxConcurrency          int                  `json:"maxConcurrency"`
	Descriptors             []VPNProbeDescriptor `json:"descriptors"`
}

type VPNManagedConnectResult struct {
	VPNConnectResult
	ProfileID               string   `json:"profileId"`
	ProfileRevision         int      `json:"profileRevision"`
	SelectionPolicyRevision int      `json:"selectionPolicyRevision"`
	Selection               string   `json:"selection"`
	SelectionReason         string   `json:"selectionReason"`
	SiteID                  string   `json:"siteId"`
	NetworkSpaceID          string   `json:"networkSpaceId"`
	Mode                    string   `json:"mode"`
	ResourceIDs             []string `json:"resourceIds"`
	DecisionID              string   `json:"decisionId"`
	FailoverOnDisconnect    bool     `json:"failoverOnDisconnect"`
	RetryCooldownSeconds    int      `json:"retryCooldownSeconds"`
	MaxAttempts             int      `json:"maxAttempts"`
}

type VPNProbeConfiguration struct {
	GatewayID       string `json:"gatewayId"`
	VerificationKey string `json:"verificationKey"`
}

type VPNProbeResult struct {
	GatewayID    string    `json:"gatewayId"`
	SentCount    int       `json:"sentCount"`
	RTTSamplesMs []float64 `json:"rttSamplesMs"`
}

type VPNProbeBatch struct {
	IntentID                string           `json:"intentId"`
	ProfileID               string           `json:"profileId"`
	ProfileRevision         int              `json:"profileRevision"`
	SelectionPolicyRevision int              `json:"selectionPolicyRevision"`
	BatchID                 string           `json:"batchId"`
	NetworkEpoch            string           `json:"networkEpoch"`
	WindowStartedAt         time.Time        `json:"windowStartedAt"`
	WindowEndedAt           time.Time        `json:"windowEndedAt"`
	Results                 []VPNProbeResult `json:"results"`
}

type VPNTunnelStats struct {
	SessionID         string     `json:"sessionId"`
	ProfileID         string     `json:"profileId"`
	GatewayID         string     `json:"gatewayId"`
	EndpointRuntimeID string     `json:"endpointRuntimeId"`
	Epoch             string     `json:"epoch"`
	WindowStartedAt   time.Time  `json:"windowStartedAt"`
	WindowEndedAt     time.Time  `json:"windowEndedAt"`
	UploadBytes       int64      `json:"uploadBytes"`
	DownloadBytes     int64      `json:"downloadBytes"`
	LastHandshakeAt   *time.Time `json:"lastHandshakeAt,omitempty"`
}

type VPNGatewayHealth struct {
	GatewayID            string    `json:"gatewayId"`
	Ready                bool      `json:"ready"`
	ActiveEndpointPeers  int       `json:"activeEndpointPeers"`
	ConfigurationVersion int       `json:"configurationVersion"`
	ObservedAt           time.Time `json:"observedAt"`
}

func validateVPNProbeBatch(event IngestEvent) error {
	batch, err := DecodePayload[VPNProbeBatch](event.Payload)
	if err != nil {
		return err
	}
	return ValidateVPNProbeBatch(batch, event.OccurredAt)
}

// ValidateVPNProbeBatch also protects bounded query responses, which do not carry an event envelope.
func ValidateVPNProbeBatch(batch VPNProbeBatch, observedAt time.Time) error {
	if batch.IntentID == "" || batch.ProfileID == "" || batch.BatchID == "" || batch.NetworkEpoch == "" || batch.ProfileRevision < 1 || batch.SelectionPolicyRevision < 1 || len(batch.Results) > 32 {
		return fmt.Errorf("VPN probe binding is invalid")
	}
	if !batch.WindowEndedAt.After(batch.WindowStartedAt) || batch.WindowEndedAt.After(observedAt) || batch.WindowEndedAt.Sub(batch.WindowStartedAt) > time.Minute {
		return fmt.Errorf("VPN probe window is invalid")
	}
	return validateVPNProbeResults(batch.Results)
}

func validateVPNProbeResults(results []VPNProbeResult) error {
	seen := make(map[string]bool, len(results))
	for _, sample := range results {
		if sample.GatewayID == "" || seen[sample.GatewayID] || sample.SentCount < 1 || sample.SentCount > 10 || len(sample.RTTSamplesMs) > sample.SentCount {
			return fmt.Errorf("VPN probe sample count is invalid")
		}
		seen[sample.GatewayID] = true
		for _, ms := range sample.RTTSamplesMs {
			if math.IsNaN(ms) || math.IsInf(ms, 0) || ms < 0 || ms > 10000 {
				return fmt.Errorf("VPN probe RTT is invalid")
			}
		}
	}
	return nil
}

func validateVPNTunnelStats(event IngestEvent) error {
	stats, err := DecodePayload[VPNTunnelStats](event.Payload)
	if err != nil {
		return err
	}
	if !stats.WindowEndedAt.After(stats.WindowStartedAt) || stats.WindowEndedAt.After(event.OccurredAt) || stats.WindowEndedAt.Sub(stats.WindowStartedAt) > 10*time.Minute || stats.UploadBytes < 0 || stats.DownloadBytes < 0 {
		return fmt.Errorf("VPN statistics window or counters are invalid")
	}
	if stats.LastHandshakeAt != nil && stats.LastHandshakeAt.After(event.OccurredAt) {
		return fmt.Errorf("VPN last handshake is in the future")
	}
	return nil
}

func validateVPNGatewayHealth(event IngestEvent) error {
	health, err := DecodePayload[VPNGatewayHealth](event.Payload)
	if err != nil {
		return err
	}
	if health.ObservedAt.IsZero() || health.ObservedAt.After(event.OccurredAt) || event.OccurredAt.Sub(health.ObservedAt) > time.Minute {
		return fmt.Errorf("VPN gateway health observation is invalid")
	}
	return nil
}
