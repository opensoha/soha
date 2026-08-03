package idempotency

import "testing"

func TestDeriveIsStableAndBindsInput(t *testing.T) {
	id, hash, err := Derive("vm.create", "user-1", "request-123", map[string]any{"name": "demo"})
	if err != nil {
		t.Fatal(err)
	}
	repeatedID, repeatedHash, err := Derive("vm.create", "user-1", "request-123", map[string]any{"name": "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if id != repeatedID || hash != repeatedHash || !Matches(map[string]any{PayloadHashKey: hash}, repeatedHash) {
		t.Fatalf("derivation is not stable: %q/%q and %q/%q", id, hash, repeatedID, repeatedHash)
	}
	_, changedHash, err := Derive("vm.create", "user-1", "request-123", map[string]any{"name": "other"})
	if err != nil {
		t.Fatal(err)
	}
	if hash == changedHash {
		t.Fatal("different input produced the same hash")
	}
}
