package delivery

import (
	"encoding/json"
	"testing"
)

func TestPublicExecutionTaskRetainsCallbackPresenceWithoutCapability(t *testing.T) {
	for _, token := range []string{"", "synthetic-callback-capability"} {
		original := ExecutionTask{ID: "task", CallbackToken: token}
		public := PublicExecutionTask(original)
		expected := ""
		if token != "" {
			expected = "[redacted]"
		}
		if public.CallbackToken != expected || original.CallbackToken != token || public.SecretLease != nil || public.Payload == nil {
			t.Fatal("public projection changed callback presence, leaked authority, or mutated the source")
		}
		raw, err := json.Marshal(public)
		var fields map[string]json.RawMessage
		if err != nil || json.Unmarshal(raw, &fields) != nil || fields["callbackToken"] == nil || string(fields["payload"]) != "{}" {
			t.Fatal("public projection lost required wire fields")
		}
	}
}
