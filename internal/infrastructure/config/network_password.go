package config

import (
	"fmt"
	"net/netip"
)

type RadiusPasswordVerifierConfig struct {
	Enabled          bool     `mapstructure:"enabled"`
	AllowedPeerCIDRs []string `mapstructure:"allowed_peer_cidrs"`
}

func (c RadiusPasswordVerifierConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if len(c.AllowedPeerCIDRs) == 0 || len(c.AllowedPeerCIDRs) > 32 {
		return fmt.Errorf("network_control.password_verifier requires 1-32 direct peer CIDRs")
	}
	for _, value := range c.AllowedPeerCIDRs {
		if _, err := netip.ParsePrefix(value); err != nil {
			return fmt.Errorf("network_control.password_verifier contains an invalid direct peer CIDR")
		}
	}
	return nil
}
