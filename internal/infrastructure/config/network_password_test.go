package config

import "testing"

func TestNetworkPasswordRequiresExplicitSource(t *testing.T) {
	for _, config := range []RadiusPasswordVerifierConfig{{}, {Enabled: true}, {Enabled: true, AllowedPeerCIDRs: []string{"not-a-cidr"}}, {Enabled: true, AllowedPeerCIDRs: []string{"192.0.2.0/24"}}} {
		err := config.Validate()
		valid := !config.Enabled || len(config.AllowedPeerCIDRs) > 0 && config.AllowedPeerCIDRs[0] == "192.0.2.0/24"
		if (err == nil) != valid {
			t.Fatalf("config=%+v error=%v", config, err)
		}
	}
}
