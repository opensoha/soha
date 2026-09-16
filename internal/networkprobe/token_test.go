package networkprobe

import (
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/platform/keyring"
)

func TestProbeTokenBindsBothRuntimesAndExpires(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	key, err := keyring.NewKey("test", strings.Repeat("a", 64), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSigner(keys)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Sign(Claims{Version: 1, GatewayRuntimeID: "gateway-1", EndpointRuntimeID: "endpoint-1", IntentID: "intent-1", IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, gateway, endpoint, value string
		at                             time.Time
		allowed                        bool
	}{
		{"valid", "gateway-1", "endpoint-1", token, now, true},
		{"wrong gateway", "gateway-2", "endpoint-1", token, now, false},
		{"wrong endpoint", "gateway-1", "endpoint-2", token, now, false},
		{"expired", "gateway-1", "endpoint-1", token, now.Add(time.Minute), false},
		{"tampered", "gateway-1", "endpoint-1", token + "A", now, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Verify(signer.PublicKey(), tt.value, tt.gateway, tt.endpoint, tt.at)
			if (err == nil) != tt.allowed {
				t.Fatalf("Verify allowed=%v, want=%v", err == nil, tt.allowed)
			}
		})
	}
	if _, err := NewSigner(keyring.Ring{}); err == nil {
		t.Fatal("missing signing key accepted")
	}
}
