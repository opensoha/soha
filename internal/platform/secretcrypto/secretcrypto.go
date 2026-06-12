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

func DecryptString(key, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !Encrypted(value) {
		return value, nil
	}
	normalized, err := NormalizeKey(key)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, Prefix))
	if err != nil {
		return "", fmt.Errorf("decode encrypted secret: %w", err)
	}
	block, err := aes.NewCipher(normalized)
	if err != nil {
		return "", fmt.Errorf("build secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("build secret gcm: %w", err)
	}
	if len(raw) <= gcm.NonceSize() {
		return "", errors.New("encrypted secret payload is too short")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("decrypt secret")
	}
	return string(plain), nil
}

func Encrypted(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), Prefix)
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
