package networkgateway

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/networkprotocol"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type vpnCounterSample struct {
	epoch                 string
	at                    time.Time
	received, transmitted int64
}

// SetVPNPeerCounters is called before the heartbeat loop starts.
func (c *TelemetryClient) SetVPNPeerCounters(read func(string) ([]wgtypes.Peer, error)) {
	c.peerCounters = read
}

func (c *TelemetryClient) vpnEvents(status RuntimeStatus, now time.Time) ([]networkprotocol.IngestEvent, error) {
	desired := status.desired
	if c.peerCounters == nil || desired == nil || desired.WireGuard == nil || desired.VPNProbe == nil {
		c.previous = nil
		return nil, nil
	}
	peers, err := c.peerCounters(desired.WireGuard.InterfaceName)
	if err != nil {
		c.previous = nil
		return nil, err
	}
	actual := make(map[string]wgtypes.Peer, len(peers))
	for _, peer := range peers {
		actual[peer.PublicKey.String()] = peer
	}
	next := make(map[string]vpnCounterSample)
	events := []networkprotocol.IngestEvent{}
	for _, binding := range desired.WireGuard.Peers {
		if binding.SessionID == "" || binding.VPNProfileID == "" {
			continue
		} // Site-to-site peers have no endpoint session binding.
		peer, ok := actual[binding.PublicKey]
		if !ok || peer.ReceiveBytes < 0 || peer.TransmitBytes < 0 {
			continue
		}
		key := binding.SessionID + "|" + binding.PublicKey + "|" + strconv.Itoa(status.ConfigurationVersion)
		previous, exists := c.previous[key]
		current := vpnCounterSample{epoch: previous.epoch, at: now, received: peer.ReceiveBytes, transmitted: peer.TransmitBytes}
		validWindow := validVPNCounterWindow(exists, now, previous, peer)
		if !validWindow {
			current.epoch = uuid.NewString()
			next[key] = current
			continue
		}
		next[key] = current
		stats := networkprotocol.VPNTunnelStats{SessionID: binding.SessionID, ProfileID: binding.VPNProfileID, GatewayID: desired.VPNProbe.GatewayID, EndpointRuntimeID: binding.RuntimeID,
			Epoch: current.epoch, WindowStartedAt: previous.at, WindowEndedAt: now, UploadBytes: peer.ReceiveBytes - previous.received, DownloadBytes: peer.TransmitBytes - previous.transmitted}
		if !peer.LastHandshakeTime.IsZero() && !peer.LastHandshakeTime.After(now) {
			at := peer.LastHandshakeTime.UTC()
			stats.LastHandshakeAt = &at
		}
		event, err := c.vpnEvent(networkprotocol.EventVPNTunnelStats, stats, now)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	c.previous = next
	health, err := c.vpnEvent(networkprotocol.EventVPNGatewayHealth, networkprotocol.VPNGatewayHealth{GatewayID: desired.VPNProbe.GatewayID, Ready: status.Status == "healthy", ActiveEndpointPeers: len(next), ConfigurationVersion: status.ConfigurationVersion, ObservedAt: now}, now)
	if err != nil {
		return nil, err
	}
	return append(events, health), nil
}

func (c *TelemetryClient) vpnEvent(kind string, payload any, now time.Time) (networkprotocol.IngestEvent, error) {
	raw, err := json.Marshal(payload)
	return networkprotocol.IngestEvent{ID: uuid.NewString(), Type: kind, Sequence: c.sequence.Add(1), OccurredAt: now, Payload: raw}, err
}

func validVPNCounterWindow(exists bool, now time.Time, previous vpnCounterSample, peer wgtypes.Peer) bool {
	return exists && now.After(previous.at) && now.Sub(previous.at) <= 10*time.Minute && peer.ReceiveBytes >= previous.received && peer.TransmitBytes >= previous.transmitted
}
