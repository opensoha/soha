package secretcrypto

import (
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/platform/keyring"
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

func TestV2EncryptAndDecryptWithKeyring(t *testing.T) {
	previousExpiry := time.Now().UTC().Add(time.Hour)
	previous, err := keyring.NewKey("previous-key", "previous-stable-test-key-32-bytes", time.Now().Add(-time.Hour), &previousExpiry)
	if err != nil {
		t.Fatal(err)
	}
	active, err := keyring.NewKey("active-key", "active-stable-test-key-32-bytes-long", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(active, []keyring.Key{previous})
	if err != nil {
		t.Fatal(err)
	}

	encrypted, err := EncryptStringWithKeyring(keys, "registry-token")
	if err != nil {
		t.Fatalf("EncryptStringWithKeyring() error = %v", err)
	}
	if got := encrypted[:len(PrefixV2+active.ID()+":")]; got != PrefixV2+active.ID()+":" {
		t.Fatalf("encrypted prefix = %q", got)
	}
	keyID, versioned, err := KeyID(encrypted)
	if err != nil || !versioned || keyID != active.ID() {
		t.Fatalf("KeyID() = %q, %v, %v", keyID, versioned, err)
	}
	plaintext, err := DecryptStringWithKeyring(keys, encrypted)
	if err != nil || plaintext != "registry-token" {
		t.Fatalf("DecryptStringWithKeyring() = %q, %v", plaintext, err)
	}
}

func TestDecryptStringWithKeyringReadsV1UsingPreviousKey(t *testing.T) {
	encrypted, err := EncryptString("previous-stable-test-key-32-bytes", "legacy-token")
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().UTC().Add(time.Hour)
	previous, _ := keyring.NewKey("previous-key", "previous-stable-test-key-32-bytes", time.Now().Add(-time.Hour), &expires)
	active, _ := keyring.NewKey("active-key", "active-stable-test-key-32-bytes-long", time.Now(), nil)
	keys, _ := keyring.New(active, []keyring.Key{previous})

	plaintext, err := DecryptStringWithKeyring(keys, encrypted)
	if err != nil || plaintext != "legacy-token" {
		t.Fatalf("DecryptStringWithKeyring() = %q, %v", plaintext, err)
	}
}

func TestDecryptStringWithKeyringRejectsUnknownV2Key(t *testing.T) {
	encrypted, err := EncryptStringV2("other-key", "other-stable-test-key-32-bytes-long", "secret")
	if err != nil {
		t.Fatal(err)
	}
	active, _ := keyring.NewKey("active-key", "active-stable-test-key-32-bytes-long", time.Now(), nil)
	keys, _ := keyring.New(active, nil)
	if _, err := DecryptStringWithKeyring(keys, encrypted); err == nil {
		t.Fatal("DecryptStringWithKeyring() error = nil, want unknown key rejection")
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
