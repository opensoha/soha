package feishu

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

var (
	ErrInvalidEventSignature = errors.New("invalid Feishu event signature")
	ErrInvalidEventToken     = errors.New("invalid Feishu event verification token")
	ErrEventOutsideWindow    = errors.New("Feishu event is outside replay window")
)

type Event struct {
	ID         string
	Type       string
	OccurredAt time.Time
	TenantKey  string
	Object     json.RawMessage
}

type Challenge struct {
	Challenge string
}

// VerifySignature verifies Feishu's SHA-256(timestamp + nonce + encryptKey + body) signature.
func VerifySignature(timestamp, nonce, encryptKey string, body []byte, signature string, now time.Time, replayWindow time.Duration) error {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrInvalidEventSignature
	}
	eventTime := time.Unix(seconds, 0)
	if replayWindow > 0 && absDuration(now.Sub(eventTime)) > replayWindow {
		return ErrEventOutsideWindow
	}
	sum := sha256.Sum256(append([]byte(timestamp+nonce+encryptKey), body...))
	expected := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
		return ErrInvalidEventSignature
	}
	return nil
}

// ParseEvent validates the verification token and returns only the normalized envelope.
// Signature verification must be performed before this function when an encrypt key is configured.
func ParseEvent(body []byte, verificationToken string) (Event, *Challenge, error) {
	var envelope struct {
		Challenge string `json:"challenge"`
		Token     string `json:"token"`
		Header    struct {
			EventID    string `json:"event_id"`
			EventType  string `json:"event_type"`
			CreateTime string `json:"create_time"`
			Token      string `json:"token"`
			TenantKey  string `json:"tenant_key"`
		} `json:"header"`
		Event json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Event{}, nil, fmt.Errorf("decode Feishu event: %w", err)
	}
	token := envelope.Header.Token
	if token == "" {
		token = envelope.Token
	}
	if verificationToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(verificationToken)) != 1 {
		return Event{}, nil, ErrInvalidEventToken
	}
	if envelope.Challenge != "" {
		return Event{}, &Challenge{Challenge: envelope.Challenge}, nil
	}
	if envelope.Header.EventID == "" || envelope.Header.EventType == "" {
		return Event{}, nil, errors.New("Feishu event ID and type are required")
	}
	occurredAt, err := parseEventTime(envelope.Header.CreateTime)
	if err != nil {
		return Event{}, nil, err
	}
	return Event{ID: envelope.Header.EventID, Type: envelope.Header.EventType, OccurredAt: occurredAt, TenantKey: envelope.Header.TenantKey, Object: envelope.Event}, nil, nil
}

func parseEventTime(value string) (time.Time, error) {
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid Feishu event create_time: %w", err)
	}
	if seconds > 1_000_000_000_000 {
		return time.UnixMilli(seconds), nil
	}
	return time.Unix(seconds, 0), nil
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
