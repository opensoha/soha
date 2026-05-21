package virtualization

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestCredentialCryptoRoundTripRawAndBase64Keys(t *testing.T) {
	credential := map[string]any{"token": "super-secret", "user": "root@pam"}

	for _, key := range []string{"raw-key-material", base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))} {
		encrypted, err := EncryptCredentialJSON(key, credential)
		if err != nil {
			t.Fatalf("EncryptCredentialJSON() error = %v", err)
		}
		if strings.Contains(string(encrypted), "super-secret") {
			t.Fatalf("encrypted payload contains plaintext credential")
		}
		decrypted, err := DecryptCredentialJSON(key, encrypted)
		if err != nil {
			t.Fatalf("DecryptCredentialJSON() error = %v", err)
		}
		if decrypted["token"] != credential["token"] || decrypted["user"] != credential["user"] {
			t.Fatalf("decrypted credential = %#v", decrypted)
		}
	}
}

func TestCredentialCryptoRequiresKey(t *testing.T) {
	if _, err := EncryptCredentialJSON("", map[string]any{"token": "secret"}); !errors.Is(err, ErrCredentialKeyRequired) {
		t.Fatalf("EncryptCredentialJSON() error = %v, want ErrCredentialKeyRequired", err)
	}
	if _, err := DecryptCredentialJSON("", []byte("ciphertext")); !errors.Is(err, ErrCredentialKeyRequired) {
		t.Fatalf("DecryptCredentialJSON() error = %v, want ErrCredentialKeyRequired", err)
	}
}
