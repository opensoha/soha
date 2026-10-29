package networkruntime

import (
	"testing"
	"time"

	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func TestNASAuthorizationReplayRequiresCurrentDecision(t *testing.T) {
	now := time.Now().UTC()
	saved := domainnetworkruntime.NASAuthorization{Decision: "allow", AccessProfile: "full", PolicyVersion: 3, ReasonCode: "policy_allowed", CreatedAt: now, ValidUntil: now.Add(time.Minute), RadiusAttributes: &networkprotocol.RadiusAttributes{VLANID: 10, FilterID: "full", SessionTimeoutSeconds: 60}}
	for _, test := range []struct {
		name   string
		change func(*domainnetworkruntime.NASAuthorization)
		want   bool
	}{
		{"same decision", func(value *domainnetworkruntime.NASAuthorization) { value.CreatedAt = now.Add(time.Second) }, true},
		{"expired", func(value *domainnetworkruntime.NASAuthorization) { value.CreatedAt = saved.ValidUntil }, false},
		{"new policy", func(value *domainnetworkruntime.NASAuthorization) { value.PolicyVersion++ }, false},
		{"denied", func(value *domainnetworkruntime.NASAuthorization) { value.Decision = "deny" }, false},
		{"restricted", func(value *domainnetworkruntime.NASAuthorization) { value.AccessProfile = "restricted" }, false},
		{"new reason", func(value *domainnetworkruntime.NASAuthorization) { value.ReasonCode = "device_inactive" }, false},
		{"changed authorization inputs", func(value *domainnetworkruntime.NASAuthorization) { value.InputsHash = "new-inputs" }, false},
		{"changed VLAN", func(value *domainnetworkruntime.NASAuthorization) {
			attributes := *value.RadiusAttributes
			attributes.VLANID = 20
			value.RadiusAttributes = &attributes
		}, false},
		{"missing attributes", func(value *domainnetworkruntime.NASAuthorization) { value.RadiusAttributes = nil }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := saved
			test.change(&current)
			if got := nasAuthorizationReplayMatches(saved, current); got != test.want {
				t.Fatalf("replay matches = %v, want %v", got, test.want)
			}
		})
	}
}
