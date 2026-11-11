package sohaapi

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestManagedVPNModelsKeepStructuredBoundaries(t *testing.T) {
	profile := NetworkVPNProfileConfig{Name: "Office", GatewayIDs: []string{"gateway-a"}}
	policy := NetworkVPNSelectionPolicyConfig{Name: "Latency", MinSamples: 3}
	if profile.Name == "" || policy.MinSamples != 3 {
		t.Fatal("structured VPN models were lost during generation")
	}
	var input NetworkVPNConnectionIntentInput
	decoder := json.NewDecoder(bytes.NewBufferString(`{"deviceId":"device-1","profileId":"profile-1","selection":"auto","mode":"external_vpn"}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err == nil {
		t.Fatal("managed connection DTO accepted a caller access-mode override")
	}
}
