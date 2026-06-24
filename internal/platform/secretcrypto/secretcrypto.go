package secretcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const Prefix = "soha:v1:"

type SecretStorage string

const (
	SecretStorageNone            SecretStorage = "none"
	SecretStorageLegacyPlaintext SecretStorage = "legacy_plaintext"
	SecretStorageEncrypted       SecretStorage = "encrypted"
)

var ErrKeyRequired = errors.New("secret crypto key is required")

func EncryptString(key, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	normalized, err := NormalizeKey(key)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(normalized)
	if err != nil {
		return "", fmt.Errorf("build secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("build secret gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate secret nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, []byte(value), nil)
	payload := append(append([]byte{}, nonce...), sealed...)
	return Prefix + base64.StdEncoding.EncodeToString(payload), nil
}

func Encrypted(value string) bool {
	return SecretStorageLabel(value) == SecretStorageEncrypted
}

func SecretStorageLabel(value string) SecretStorage {
	value = strings.TrimSpace(value)
	if value == "" {
		return SecretStorageNone
	}
	if strings.HasPrefix(value, Prefix) {
		return SecretStorageEncrypted
	}
	return SecretStorageLegacyPlaintext
}

func NormalizeKey(key string) ([]byte, error) {
	if strings.TrimSpace(key) == "" {
		return nil, ErrKeyRequired
	}
	if decoded, err := base64.StdEncoding.DecodeString(key); err == nil && len(decoded) > 0 {
		return normalizeKeyBytes(decoded), nil
	}
	return normalizeKeyBytes([]byte(key)), nil
}

func normalizeKeyBytes(raw []byte) []byte {
	if len(raw) == 32 {
		out := make([]byte, 32)
		copy(out, raw)
		return out
	}
	sum := sha256.Sum256(raw)
	out := make([]byte, 32)
	copy(out, sum[:])
	return out
}
