package sohaapi

import (
	"encoding/json"
	"testing"
)

func TestMemoryPolicyOptionalConsent(t *testing.T) {
	disabled := false
	enabled := true
	cases := []struct {
		name    string
		value   any
		present bool
		want    bool
	}{
		{"input inherit", AIMemoryPolicyInput{}, false, false},
		{"input disable", AIMemoryPolicyInput{ReadEnabled: &disabled, WriteEnabled: &disabled}, true, false},
		{"input enable", AIMemoryPolicyInput{ReadEnabled: &enabled, WriteEnabled: &enabled}, true, true},
		{"response inherit", AIMemoryPolicy{}, false, false},
		{"response disable", AIMemoryPolicy{ReadEnabled: &disabled, WriteEnabled: &disabled}, true, false},
		{"response enable", AIMemoryPolicy{ReadEnabled: &enabled, WriteEnabled: &enabled}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(body, &fields); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"readEnabled", "writeEnabled"} {
				field, present := fields[key]
				if present != tc.present {
					t.Fatalf("%s presence = %v, want %v: %s", key, present, tc.present, body)
				}
				if !present {
					continue
				}
				var value bool
				if err := json.Unmarshal(field, &value); err != nil {
					t.Fatal(err)
				}
				if value != tc.want {
					t.Fatalf("%s = %v, want %v", key, value, tc.want)
				}
			}
		})
	}
	var decoded AIMemoryPolicyInput
	if err := json.Unmarshal([]byte(`{"readEnabled":false,"writeEnabled":false}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ReadEnabled == nil || *decoded.ReadEnabled || decoded.WriteEnabled == nil || *decoded.WriteEnabled {
		t.Fatal("explicit false must survive decoding as a present value")
	}
}
