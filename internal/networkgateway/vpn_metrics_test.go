package networkgateway

import (
	"testing"
	"time"

	"github.com/opensoha/soha/internal/networkprotocol"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestVPNMetricsCountersDirectionResetAndPeerScope(t *testing.T) {
	now := time.Now().UTC()
	key := wgtypes.Key{1}
	peer := wgtypes.Peer{PublicKey: key, ReceiveBytes: 100, TransmitBytes: 200}
	client := &TelemetryClient{peerCounters: func(string) ([]wgtypes.Peer, error) {
		return []wgtypes.Peer{peer, {PublicKey: wgtypes.Key{2}, ReceiveBytes: 999999}}, nil
	}}
	status := RuntimeStatus{Status: "healthy", ConfigurationVersion: 1, desired: &networkprotocol.ConfigurationDesired{VPNProbe: &networkprotocol.VPNProbeConfiguration{GatewayID: "gw-1"}, WireGuard: &networkprotocol.WireGuardConfiguration{InterfaceName: "soha0", Peers: []networkprotocol.WireGuardPeer{{PublicKey: key.String(), RuntimeID: "endpoint-1", SessionID: "session-1", VPNProfileID: "profile-1"}, {PublicKey: (wgtypes.Key{2}).String(), RuntimeID: "site-peer"}}}}}
	read := func(at time.Time) []networkprotocol.VPNTunnelStats {
		t.Helper()
		events, err := client.vpnEvents(status, at)
		if err != nil {
			t.Fatal(err)
		}
		var stats []networkprotocol.VPNTunnelStats
		for _, event := range events {
			if event.Type == networkprotocol.EventVPNTunnelStats {
				item, err := networkprotocol.DecodePayload[networkprotocol.VPNTunnelStats](event.Payload)
				if err != nil {
					t.Fatal(err)
				}
				stats = append(stats, item)
			}
		}
		return stats
	}
	if got := read(now); len(got) != 0 {
		t.Fatalf("initial counters treated as deltas: %+v", got)
	}
	peer.ReceiveBytes, peer.TransmitBytes = 160, 320
	got := read(now.Add(time.Minute))
	if len(got) != 1 || got[0].UploadBytes != 60 || got[0].DownloadBytes != 120 || got[0].EndpointRuntimeID != "endpoint-1" {
		t.Fatalf("invalid direction or scope: %+v", got)
	}
	oldEpoch := got[0].Epoch
	peer.ReceiveBytes, peer.TransmitBytes = 1, 2
	if got := read(now.Add(2 * time.Minute)); len(got) != 0 {
		t.Fatalf("reset produced a delta: %+v", got)
	}
	peer.ReceiveBytes, peer.TransmitBytes = 4, 6
	got = read(now.Add(3 * time.Minute))
	if len(got) != 1 || got[0].Epoch == oldEpoch || got[0].UploadBytes != 3 || got[0].DownloadBytes != 4 {
		t.Fatalf("reset epoch invalid: %+v", got)
	}
	status.desired.WireGuard.Peers[0].SessionID = "session-2"
	if got := read(now.Add(4 * time.Minute)); len(got) != 0 {
		t.Fatalf("new session inherited counters: %+v", got)
	}
}
