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

func TestDecryptStringReturnsPlaintext(t *testing.T) {
	encrypted, err := EncryptString("stable-test-key-32-bytes-or-more", "registry-token")
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}
	plaintext, err := DecryptString("stable-test-key-32-bytes-or-more", encrypted)
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if plaintext != "registry-token" {
		t.Fatalf("DecryptString() = %q, want registry-token", plaintext)
	}
}

func TestDecryptStringKeepsLegacyPlaintext(t *testing.T) {
	plaintext, err := DecryptString("stable-test-key-32-bytes-or-more", "legacy-token")
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if plaintext != "legacy-token" {
		t.Fatalf("DecryptString() = %q, want legacy-token", plaintext)
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
