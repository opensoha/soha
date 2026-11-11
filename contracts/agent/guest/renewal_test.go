package guest

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRenewalRejectsDuplicateAndUnknownAuthority(t *testing.T) {
	for _, raw := range []string{`{"schemaVersion":"opensoha.dev/personal-guest-renewal/v1","requestId":"11111111-1111-4111-8111-111111111111","bindingVersion":1,"bindingVersion":2,"csrPEM":"public"}`, `{"schemaVersion":"opensoha.dev/personal-guest-renewal/v1","requestId":"11111111-1111-4111-8111-111111111111","bindingVersion":1,"csrPEM":"public","ownerId":"other"}`} {
		var request Renewal
		if json.Unmarshal([]byte(raw), &request) == nil {
			t.Fatal("untrusted renewal fields accepted")
		}
	}
}

func TestSameWorkspaceIgnoresOnlyRegistrationAndLeaseFields(t *testing.T) {
	binding := guestFixture[Binding](t, "binding")
	renewed := binding
	renewed.ID = "another-registration"
	renewed.Version++
	renewed.IssuedAt = renewed.IssuedAt.Add(time.Hour)
	renewed.ExpiresAt = renewed.ExpiresAt.Add(time.Hour)
	beforeBinding, beforeRenewed := binding, renewed
	if !binding.SameWorkspace(renewed) || !renewed.SameWorkspace(binding) {
		t.Fatal("registration or lease changes changed workspace identity")
	}
	if binding != beforeBinding || renewed != beforeRenewed {
		t.Fatal("workspace comparison mutated a binding")
	}
	for name, mutate := range map[string]func(*Binding){
		"schema":            func(b *Binding) { b.SchemaVersion += "-other" },
		"assistant":         func(b *Binding) { b.AssistantID += "-other" },
		"owner":             func(b *Binding) { b.OwnerID += "-other" },
		"provider":          func(b *Binding) { b.Provider += "-other" },
		"compute":           func(b *Binding) { b.ComputeRef += "-other" },
		"generation":        func(b *Binding) { b.ComputeGeneration++ },
		"provider identity": func(b *Binding) { b.ProviderIdentityHash += "-other" },
		"data":              func(b *Binding) { b.DataRef += "-other" },
		"disk serial":       func(b *Binding) { b.DataDiskSerial += "-other" },
		"disk size":         func(b *Binding) { b.DataDiskBytes++ },
		"policy":            func(b *Binding) { b.PolicyRevision += "-other" },
	} {
		t.Run(name, func(t *testing.T) {
			other := renewed
			mutate(&other)
			if binding.SameWorkspace(other) || other.SameWorkspace(binding) {
				t.Fatal("different workspace identity accepted")
			}
		})
	}
}
