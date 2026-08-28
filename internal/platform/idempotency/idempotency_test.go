package idempotency

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestReceiptRoundTripAndConflict(t *testing.T) {
	payload := map[string]any{}
	id, hash, found, err := LookupReceipt(payload, "compute.cancel/task-1", "user-1", "request-key", map[string]any{"reason": "maintenance"})
	if err != nil || found {
		t.Fatalf("initial lookup found=%v err=%v", found, err)
	}
	RecordReceipt(payload, id, hash)

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var restored map[string]any
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	_, _, found, err = LookupReceipt(restored, "compute.cancel/task-1", "user-1", "request-key", map[string]any{"reason": "maintenance"})
	if err != nil || !found {
		t.Fatalf("replay found=%v err=%v", found, err)
	}
	_, _, _, err = LookupReceipt(restored, "compute.cancel/task-1", "user-1", "request-key", map[string]any{"reason": "different"})
	if !errors.Is(err, ErrInputMismatch) {
		t.Fatalf("conflict error = %v", err)
	}
}
