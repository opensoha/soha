package sohaapi

import (
	"encoding/json"
	"testing"
)

func TestProxyDeploymentReplicaPresence(t *testing.T) {
	zero, two := int64(0), int64(2)
	for _, tc := range []struct {
		name         string
		desired      int64
		observed     *int64
		wantObserved string
	}{
		{"stopped", 0, &zero, "0"},
		{"unobserved", 0, nil, ""},
		{"positive", 2, &two, "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dto := NetworkProxyDeployment{Revision: 1, State: "stopped", Members: []NetworkProxyDeploymentMember{}, DesiredReplicas: tc.desired, ObservedReplicas: tc.observed}
			raw, err := json.Marshal(dto)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			wantDesired, _ := json.Marshal(tc.desired)
			if string(fields["desiredReplicas"]) != string(wantDesired) {
				t.Fatalf("desired count lost: %s", raw)
			}
			observed, present := fields["observedReplicas"]
			if present != (tc.observed != nil) || string(observed) != tc.wantObserved {
				t.Fatalf("observed presence lost: %s", raw)
			}
			var decoded NetworkProxyDeployment
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if (decoded.ObservedReplicas == nil) != (tc.observed == nil) || decoded.ObservedReplicas != nil && *decoded.ObservedReplicas != *tc.observed {
				t.Fatalf("round trip lost observation: %+v", decoded)
			}
		})
	}
}
