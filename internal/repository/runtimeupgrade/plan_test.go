package runtimeupgrade

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPlanStableAndLegacyDrain(t *testing.T) {
	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	rows := []row{{"session", "s", json.RawMessage(`{"id":"s","metadata":{"agentProviderId":"internal","scope":{"cluster":"keep"}}}`)}, {"run", "r", json.RawMessage(`{"provider_kind":"native","status":"waiting_approval"}`)}}
	p := build(rows, now)
	q := build([]row{rows[1], rows[0]}, now)
	if p.Digest != q.Digest {
		t.Fatal("unstable digest")
	}
	if checkDrain(p, map[string]bool{}) == nil {
		t.Fatal("unaccounted run accepted")
	}
	if err := checkDrain(p, map[string]bool{"r": true}); err != nil {
		t.Fatal(err)
	}
	rows[1].Data = json.RawMessage(`{"provider_kind":"native","status":"running","native_lease_until":"2026-11-08T01:00:00Z"}`)
	if checkDrain(build(rows, now), map[string]bool{"r": true}) == nil {
		t.Fatal("live lease accepted")
	}
	rows[1].Data = json.RawMessage(`{"provider_kind":"native","status":"completed"}`)
	if err := checkDrain(build(rows, now), nil); err != nil {
		t.Fatal(err)
	}
}
func TestMemoryNeverInventsMissingProvenance(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	expiry := now.Add(time.Hour).Format(time.RFC3339Nano)
	v := object([]byte(`{"id":"m","status":"active","owner_type":"user","owner_id":"u","scope_hash":"scope","expires_at":"` + expiry + `","_ownerExists":true,"payload":{"id":"m","status":"active","ownerType":"user","ownerId":"u","scopeHash":"scope","expiresAt":"` + expiry + `"}}`))
	action, reason := memoryAction(v, nil, now)
	if action != "quarantine" || reason != "invalid_scope_hash" {
		t.Fatalf("%s %s", action, reason)
	}
	v["status"] = "deleted"
	if a, _ := memoryAction(v, nil, now); a != "disabled" {
		t.Fatal(a)
	}
}
func TestProviderInventoryDiscriminatesAIClient(t *testing.T) {
	rows := []row{{"ai_client", "codex", json.RawMessage(`{"kind":"codex"}`)}, {"run", "external", json.RawMessage(`{"provider_kind":"cli","provider_id":"custom","status":"running"}`)}, {"provider_catalog", "current", json.RawMessage(`{"catalog":{"providers":[{"id":"legacy-cli","kind":"cli","runtime":{"command":"python3","args":["deploy/codex-runner/adapter.py"]}}]}}`)}, {"run", "legacy", json.RawMessage(`{"provider_kind":"cli","provider_id":"legacy-cli","status":"unknown"}`)}}
	p := build(rows, time.Now())
	for _, i := range p.Items {
		if i.ID == "external" && i.Action != "preserve" {
			t.Fatal(i)
		}
		if i.ID == "legacy" && i.Action != "drain" {
			t.Fatal(i)
		}
		if i.Entity == "ai_client" && i.Action != "preserve" {
			t.Fatal(i)
		}
	}
}
func TestStableOutboxIdentityAndBoundedQuarantine(t *testing.T) {
	a, d := stableJob("memory", 1)
	b, e := stableJob("memory", 1)
	c, _ := stableJob("memory", 2)
	if a != b || d != e || a == c || !strings.HasSuffix(d, "-r1") {
		t.Fatal("unstable identity")
	}
	if _, err := boundedIDs([]string{"a", "a"}); err == nil {
		t.Fatal("duplicate accepted")
	}
}
