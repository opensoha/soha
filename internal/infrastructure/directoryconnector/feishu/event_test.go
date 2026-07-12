package feishu_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/infrastructure/directoryconnector/feishu"
)

func TestVerifySignature(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	timestamp, nonce, key := "1700000000", "nonce", "encrypt-key"
	body := []byte(`{"event":"value"}`)
	sum := sha256.Sum256(append([]byte(timestamp+nonce+key), body...))
	signature := hex.EncodeToString(sum[:])

	if err := feishu.VerifySignature(timestamp, nonce, key, body, signature, now, 5*time.Minute); err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}
	if err := feishu.VerifySignature(timestamp, nonce, key, body, "bad", now, 5*time.Minute); !errors.Is(err, feishu.ErrInvalidEventSignature) {
		t.Fatalf("bad signature error = %v", err)
	}
	if err := feishu.VerifySignature(timestamp, nonce, key, body, signature, now.Add(6*time.Minute), 5*time.Minute); !errors.Is(err, feishu.ErrEventOutsideWindow) {
		t.Fatalf("old event error = %v", err)
	}
}

func TestParseEvent(t *testing.T) {
	t.Parallel()
	body := []byte(`{"schema":"2.0","header":{"event_id":"event-1","event_type":"contact.department.updated_v3","create_time":"1700000000001","token":"verify-token","tenant_key":"tenant-1"},"event":{"object":{"department_id":"od-1"}}}`)
	event, challenge, err := feishu.ParseEvent(body, "verify-token")
	if err != nil {
		t.Fatalf("ParseEvent() error = %v", err)
	}
	if challenge != nil || event.ID != "event-1" || event.Type != "contact.department.updated_v3" || event.OccurredAt.UnixMilli() != 1_700_000_000_001 {
		t.Fatalf("event = %#v, challenge = %#v", event, challenge)
	}
	if _, _, err := feishu.ParseEvent(body, "wrong"); !errors.Is(err, feishu.ErrInvalidEventToken) {
		t.Fatalf("wrong token error = %v", err)
	}
}

func TestParseEvent_Challenge(t *testing.T) {
	t.Parallel()
	_, challenge, err := feishu.ParseEvent([]byte(`{"challenge":"challenge-value","token":"verify-token","type":"url_verification"}`), "verify-token")
	if err != nil {
		t.Fatalf("ParseEvent() error = %v", err)
	}
	if challenge == nil || challenge.Challenge != "challenge-value" {
		t.Fatalf("challenge = %#v", challenge)
	}
}
