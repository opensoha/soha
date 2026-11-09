package config

import (
	"encoding/base64"
	"testing"
)

func TestOutpostTrustedKeys(t *testing.T) {
	first := base64.StdEncoding.EncodeToString(make([]byte, 32))
	second := base64.StdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	for _, test := range []struct {
		name   string
		config OutpostConfig
		want   int
	}{
		{"legacy", OutpostConfig{TrustKeyID: "old", TrustPublicKey: first}, 1},
		{"overlap", OutpostConfig{TrustKeyID: "old", TrustPublicKey: first, TrustPublicKeys: map[string]string{"new": second}}, 2},
		{"map only", OutpostConfig{TrustPublicKeys: map[string]string{"new": second}}, 1},
		{"same duplicate", OutpostConfig{TrustKeyID: "old", TrustPublicKey: first, TrustPublicKeys: map[string]string{"old": first}}, 1},
		{"conflicting duplicate", OutpostConfig{TrustKeyID: "old", TrustPublicKey: first, TrustPublicKeys: map[string]string{"old": second}}, 0},
		{"empty", OutpostConfig{}, 0},
		{"missing legacy ID", OutpostConfig{TrustPublicKey: first}, 0},
		{"malformed", OutpostConfig{TrustPublicKeys: map[string]string{"new": "bad"}}, 0},
		{"empty ID", OutpostConfig{TrustPublicKeys: map[string]string{"": first}}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			keys, err := test.config.TrustedKeys()
			if (err != nil) != (test.want == 0) || len(keys) != test.want {
				t.Fatalf("keys=%d, error=%v, want %d", len(keys), err, test.want)
			}
		})
	}
}
