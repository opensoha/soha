package sohaapi

import (
	"encoding/json"
	"testing"
)

func TestRADIUSKnownZeroAndExplicitScaleZero(t *testing.T) {
	zero := 0
	for _, input := range []struct {
		value any
		key   string
	}{{NetworkRADIUSRuntimeACK{StateCountKnown: true, ActiveStates: &zero}, "activeStates"}, {NetworkRADIUSMember{StateCountKnown: true, ActiveStates: &zero}, "activeStates"}, {NetworkRADIUSControlInput{Action: "scale", Replicas: &zero}, "replicas"}, {NetworkRADIUSPool{ObservedReplicas: &zero, ReadyReplicas: &zero}, "observedReplicas"}} {
		encoded, err := json.Marshal(input.value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields[input.key]) != "0" {
			t.Fatalf("explicit zero lost: %s", encoded)
		}
	}
	encoded, err := json.Marshal(NetworkRADIUSRuntimeACK{StateCountKnown: false})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(encoded, &fields)
	if _, present := fields["activeStates"]; present {
		t.Fatalf("unknown count serialized as zero: %s", encoded)
	}
}
