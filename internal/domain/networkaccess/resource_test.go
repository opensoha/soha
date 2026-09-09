package networkaccess

import "testing"

func TestWireGuardResourceTargetFailsClosedOutsideL3Scope(t *testing.T) {
	space := Space{ID: "space-1", CIDRs: []string{"10.20.0.0/16"}}
	tests := []struct {
		name       string
		resource   Resource
		wantTarget string
		wantOK     bool
	}{
		{name: "IPv4", resource: Resource{SpaceID: space.ID, Kind: "ip", Target: "10.20.8.10", Protocol: "tcp", Ports: []int{443}}, wantTarget: "10.20.8.10/32", wantOK: true},
		{name: "CIDR", resource: Resource{SpaceID: space.ID, Kind: "cidr", Target: "10.20.16.0/24", Protocol: "any"}, wantTarget: "10.20.16.0/24", wantOK: true},
		{name: "outside space", resource: Resource{SpaceID: space.ID, Kind: "ip", Target: "10.30.0.1", Protocol: "tcp", Ports: []int{443}}},
		{name: "FQDN", resource: Resource{SpaceID: space.ID, Kind: "fqdn", Target: "db.internal", Protocol: "tcp", Ports: []int{5432}}},
		{name: "ICMP", resource: Resource{SpaceID: space.ID, Kind: "ip", Target: "10.20.8.10", Protocol: "icmp"}},
		{name: "bad TCP ports", resource: Resource{SpaceID: space.ID, Kind: "ip", Target: "10.20.8.10", Protocol: "tcp"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, ok := WireGuardResourceTarget(tt.resource, space)
			if target != tt.wantTarget || ok != tt.wantOK {
				t.Fatalf("WireGuardResourceTarget() = %q, %v; want %q, %v", target, ok, tt.wantTarget, tt.wantOK)
			}
		})
	}
}
