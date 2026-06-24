package secretcrypto

import (
	"errors"
	"testing"
)

func TestEncryptStringProducesEncryptedPayload(t *testing.T) {
	encrypted, err := EncryptString("stable-test-key-32-bytes-or-more", "registry-token")
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}
	if encrypted == "registry-token" || !Encrypted(encrypted) {
		t.Fatalf("expected encrypted payload, got %q", encrypted)
	}
}

func TestEncryptStringRequiresKey(t *testing.T) {
	if _, err := EncryptString("", "registry-token"); !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("EncryptString error = %v, want ErrKeyRequired", err)
	}
}

func TestSecretStorageLabel(t *testing.T) {
	if got := SecretStorageLabel(""); got != SecretStorageNone {
		t.Fatalf("SecretStorageLabel(\"\") = %q, want %q", got, SecretStorageNone)
	}
	if got := SecretStorageLabel("legacy-token"); got != SecretStorageLegacyPlaintext {
		t.Fatalf("SecretStorageLabel(legacy-token) = %q, want %q", got, SecretStorageLegacyPlaintext)
	}
	if got := SecretStorageLabel(Prefix + "payload"); got != SecretStorageEncrypted {
		t.Fatalf("SecretStorageLabel(encrypted) = %q, want %q", got, SecretStorageEncrypted)
	}
}
