package endpointcontrol

import (
	"testing"
	"time"
)

func TestNativeReceiptCannotClaimContradictoryControl(t *testing.T) {
	cases := []Channel{
		{Name: "usb", Scope: "current_usb_volumes", State: "controlled", Targets: 1, Controlled: 0, ReasonCode: "native_scope_only"},
		{Name: "usb", Scope: "current_usb_volumes", State: "controlled", Targets: 0, Controlled: 0, ReasonCode: "native_scope_only"},
		{Name: "clipboard", Scope: "current_usb_volumes", State: "controlled", Targets: 1, Controlled: 1, ReasonCode: "native_scope_only"},
		{Name: "print", Scope: "local_new_print_jobs", State: "off", Targets: 1, Controlled: 1, ReasonCode: "disabled"},
	}
	for _, c := range cases {
		if ValidChannel(c) {
			t.Fatalf("contradictory control accepted: %+v", c)
		}
	}
	if !ValidChannel(Channel{Name: "clipboard", Scope: "enrolled_console_win32", State: "controlled", Targets: 1, Controlled: 1, ReasonCode: "native_scope_only"}) {
		t.Fatal("bounded native control rejected")
	}
}
func TestUnconfiguredIsNotAPublishedRelease(t *testing.T) {
	now := time.Now().UTC()
	d := Desired{SchemaVersion: Version, DeviceID: "d", RuntimeID: "r", CredentialID: "c", Challenge: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", IssuedAt: now, ValidUntil: now.Add(time.Minute)}
	if !ValidDesired(d, now) {
		t.Fatal("unconfigured envelope rejected")
	}
	d.PolicyVersion = 1
	if ValidDesired(d, now) {
		t.Fatal("positive unpublished revision accepted")
	}
	d.Configured = true
	if !ValidDesired(d, now) {
		t.Fatal("explicit published release rejected")
	}
}

func TestMissingBooleanCannotDecodeAsRelease(t *testing.T) {
	var controls Controls
	for _, raw := range []string{`{}`, `{"usb":false,"print":false}`, `{"usb":false,"print":false,"clipboard":null}`} {
		if Decode([]byte(raw), &controls) == nil {
			t.Fatal("incomplete channel decisions accepted", raw)
		}
	}
	var desired Desired
	if Decode([]byte(`{"configured":true,"schemaVersion":"endpoint-controls/v1alpha1","deviceId":"device","runtimeId":"runtime","credentialId":"credential","policyVersion":99,"challenge":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","issuedAt":"2026-10-28T00:00:00Z","validUntil":"2026-10-28T00:01:00Z"}`), &desired) == nil {
		t.Fatal("missing controls became release")
	}
}
