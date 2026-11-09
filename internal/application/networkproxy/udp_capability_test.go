package networkproxy

import (
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"testing"
)

func TestGuardedUDPCapabilityRequiresProtectedMihomoTransport(t *testing.T) {
	for _, engine := range []string{domain.EngineMihomo, domain.EngineExternal} {
		if err := validateCapabilities(engine, []string{"protected_destinations_v1", "protection_rules_readback", "guarded_dns_tcp_v1", "guarded_udp_v1"}); err != nil {
			t.Fatalf("guarded %s rejected: %v", engine, err)
		}
	}
	for _, tt := range []struct {
		name, engine string
		capabilities []string
	}{
		{"missing protection", domain.EngineMihomo, []string{"guarded_dns_tcp_v1", "guarded_udp_v1"}},
		{"missing guarded DNS", domain.EngineMihomo, []string{"protected_destinations_v1", "guarded_udp_v1"}},
		{"unsupported sing-box", domain.EngineSingBox, []string{"protected_destinations_v1", "guarded_dns_tcp_v1", "guarded_udp_v1"}},
		{"unsupported v2ray", domain.EngineV2Ray, []string{"protected_destinations_v1", "guarded_dns_tcp_v1", "guarded_udp_v1"}},
		{"duplicate", domain.EngineMihomo, []string{"protected_destinations_v1", "guarded_dns_tcp_v1", "guarded_udp_v1", "guarded_udp_v1"}},
		{"unknown", domain.EngineMihomo, []string{"unguarded_udp"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateCapabilities(tt.engine, tt.capabilities); err == nil {
				t.Fatal("unproven UDP capability accepted")
			}
		})
	}
	if err := validateCapabilities(domain.EngineSingBox, []string{"traffic", "protected_destinations_v1", "guarded_dns_tcp_v1"}); err != nil {
		t.Fatal("legacy TCP capability changed", err)
	}
}
