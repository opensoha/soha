package keyring

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRingMatchHonorsPreviousExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	active, err := NewKey("active-id", "active-secret", now.Add(-time.Hour), nil)
	if err != nil {
		t.Fatalf("NewKey() active error = %v", err)
	}
	validUntil := now.Add(time.Hour)
	expiredAt := now.Add(-time.Second)
	valid, err := NewKey("valid-id", "valid-previous-secret", now.Add(-2*time.Hour), &validUntil)
	if err != nil {
		t.Fatalf("NewKey() valid previous error = %v", err)
	}
	expired, err := NewKey("expired-id", "expired-previous-secret", now.Add(-3*time.Hour), &expiredAt)
	if err != nil {
		t.Fatalf("NewKey() expired previous error = %v", err)
	}
	ring, err := New(active, []Key{valid, expired})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if !ring.Match("active-secret", now) || !ring.Match("valid-previous-secret", now) {
		t.Fatal("active and unexpired previous keys should match")
	}
	if ring.Match("expired-previous-secret", now) || ring.Match("wrong", now) {
		t.Fatal("expired and unknown keys must not match")
	}
}

func TestKeyAndRingFormattingRedactsSecrets(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	key, err := NewKey("key-id", "do-not-print-this-secret", now, nil)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	ring, err := New(key, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, formatted := range []string{fmt.Sprint(key), fmt.Sprintf("%#v", key), fmt.Sprint(ring), fmt.Sprintf("%#v", ring)} {
		if strings.Contains(formatted, key.Secret()) {
			t.Fatalf("formatted keyring leaked secret: %s", formatted)
		}
	}
}
