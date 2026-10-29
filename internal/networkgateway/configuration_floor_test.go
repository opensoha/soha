package networkgateway

import (
	"testing"
	"time"

	"github.com/opensoha/soha/internal/networkprotocol"
)

func TestGatewayFloorSeparatesIntervalsFromEnforcement(t *testing.T) {
	desired := networkprotocol.ConfigurationDesired{ConfigurationVersion: 2, PolicyVersion: 3, AccessProfile: "full", ValidUntil: time.Now().Add(time.Minute)}
	runtime := &Runtime{}
	if err := runtime.rememberConfiguration(desired); err != nil {
		t.Fatal(err)
	}
	desired.RuntimeIntervals = &networkprotocol.RuntimeIntervals{HeartbeatIntervalSeconds: 60, ConfigurationPollIntervalSeconds: 90}
	if err := runtime.rememberConfiguration(desired); err != nil {
		t.Fatal("operational intervals invalidated an unchanged enforcement document", err)
	}
	for _, name := range []string{"profile", "expiry", "policy"} {
		t.Run(name, func(t *testing.T) {
			changed := desired
			switch name {
			case "profile":
				changed.AccessProfile = "restricted"
			case "expiry":
				changed.ValidUntil = changed.ValidUntil.Add(time.Minute)
			case "policy":
				changed.PolicyVersion++
			}
			if err := runtime.rememberConfiguration(changed); err == nil {
				t.Fatal("same-version enforcement change accepted")
			}
			if runtime.floor.ConfigurationVersion != 2 || runtime.floor.PolicyVersion != 3 {
				t.Fatal("rejected content advanced the floor")
			}
		})
	}
}
