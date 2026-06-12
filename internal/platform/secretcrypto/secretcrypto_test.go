package secretcrypto

import (
	"errors"
	"testing"
)

func TestEncryptDecryptString(t *testing.T) {
	encrypted, err := EncryptString("stable-test-key-32-bytes-or-more", "registry-token")
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}
	if encrypted == "registry-token" || !Encrypted(encrypted) {
		t.Fatalf("expected encrypted payload, got %q", encrypted)
	}
	decrypted, err := DecryptString("stable-test-key-32-bytes-or-more", encrypted)
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if decrypted != "registry-token" {
		t.Fatalf("decrypted = %q, want registry-token", decrypted)
	}
}

func TestDecryptStringReturnsPlaintextForLegacyValues(t *testing.T) {
	value, err := DecryptString("", "legacy-token")
	if err != nil {
		t.Fatalf("DecryptString returned error for legacy value: %v", err)
	}
	if value != "legacy-token" {
		t.Fatalf("value = %q, want legacy-token", value)
	}
}

func TestEncryptStringRequiresKey(t *testing.T) {
	if _, err := EncryptString("", "registry-token"); !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("EncryptString error = %v, want ErrKeyRequired", err)
	}
}
